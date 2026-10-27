// Посещаемость: события в GA4 с сервера — что уходит, что нет.

package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// collector — поддельный Measurement Protocol: копит тела запросов.
type collector struct {
	srv *httptest.Server
	mu  sync.Mutex
	got []map[string]any
	qs  []string
}

func newCollector(t *testing.T) *collector {
	t.Helper()
	c := &collector{}
	c.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var m map[string]any
		if err := json.Unmarshal(body, &m); err != nil {
			t.Errorf("bad json: %v", err)
		}
		c.mu.Lock()
		c.got = append(c.got, m)
		c.qs = append(c.qs, r.URL.RawQuery)
		c.mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(c.srv.Close)
	return c
}

// wait — ждёт n событий (очередь отправляется в фоне).
func (c *collector) wait(t *testing.T, n int) []map[string]any {
	t.Helper()
	for deadline := time.Now().Add(3 * time.Second); time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
		c.mu.Lock()
		if len(c.got) >= n {
			defer c.mu.Unlock()
			return c.got
		}
		c.mu.Unlock()
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	t.Fatalf("got %d events, want %d", len(c.got), n)
	return nil
}

func analyticsApp(t *testing.T) (*App, *collector) {
	t.Helper()
	c := newCollector(t)
	a := testApp(t, true)
	a.Analytics = NewAnalytics("G-TEST", "secret", c.srv.URL, "https://gate4.ai")
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go a.Analytics.Run(ctx)
	return a, c
}

func event(m map[string]any) (string, map[string]any) {
	ev := m["events"].([]any)[0].(map[string]any)
	return ev["name"].(string), ev["params"].(map[string]any)
}

func TestAnalyticsPageView(t *testing.T) {
	a, c := analyticsApp(t)
	h := a.Handler()
	req := httptest.NewRequest("GET", "/ru/connectors?q=x", nil)
	req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64) Firefox/130.0")
	req.Header.Set("X-Forwarded-For", "203.0.113.7, 10.0.0.1")
	req.Header.Set("Accept-Language", "ru-RU,ru;q=0.9")
	req.Header.Set("Referer", "https://news.ycombinator.com/")
	h.ServeHTTP(httptest.NewRecorder(), req)

	got := c.wait(t, 1)
	m := got[0]
	if !strings.Contains(c.qs[0], "measurement_id=G-TEST") || !strings.Contains(c.qs[0], "api_secret=secret") {
		t.Errorf("query %q", c.qs[0])
	}
	name, p := event(m)
	if name != "page_view" || p["page_location"] != "https://gate4.ai/ru/connectors?q=x" || p["page_referrer"] != "https://news.ycombinator.com/" {
		t.Errorf("event %s %v", name, p)
	}
	if p["session_id"] == "" || p["engagement_time_msec"] == nil {
		t.Errorf("session params missing: %v", p)
	}
	if m["ip_override"] != "203.0.113.7" || m["user_agent"] != req.UserAgent() {
		t.Errorf("ip/ua: %v %v", m["ip_override"], m["user_agent"])
	}
	if m["device"].(map[string]any)["language"] != "ru-RU" {
		t.Errorf("device %v", m["device"])
	}
	cid, _ := m["client_id"].(string)
	if n := strings.Split(cid, "."); len(n) != 2 || n[0] == "" || n[1] == "" {
		t.Errorf("client_id %q", cid)
	}

	// Тот же посетитель — тот же client_id; другой IP — другой.
	h.ServeHTTP(httptest.NewRecorder(), req)
	req2 := req.Clone(context.Background())
	req2.Header.Set("X-Forwarded-For", "203.0.113.8")
	h.ServeHTTP(httptest.NewRecorder(), req2)
	got = c.wait(t, 3)
	if got[1]["client_id"] != cid || got[2]["client_id"] == cid {
		t.Errorf("client ids: %v %v %v", cid, got[1]["client_id"], got[2]["client_id"])
	}
	// Внутренний реферер не передаётся.
	req3 := req.Clone(context.Background())
	req3.Header.Set("Referer", "https://gate4.ai/")
	h.ServeHTTP(httptest.NewRecorder(), req3)
	if _, p := event(c.wait(t, 4)[3]); p["page_referrer"] != nil {
		t.Errorf("internal referrer sent: %v", p)
	}
}

func TestAnalyticsInstallScripts(t *testing.T) {
	a, c := analyticsApp(t)
	h := a.Handler()
	for _, p := range []string{"/install.sh", "/install.ps1", "/install.md"} {
		req := httptest.NewRequest("GET", p, nil)
		req.Header.Set("User-Agent", "curl/8.5.0")
		h.ServeHTTP(httptest.NewRecorder(), req)
	}
	req := httptest.NewRequest("GET", "/install", nil)
	req.Header.Set("User-Agent", "curl/8.5.0")
	req.Header.Set("Accept", "text/plain")
	h.ServeHTTP(httptest.NewRecorder(), req)
	got := c.wait(t, 4)
	want := map[string]bool{"install.sh": true, "install.ps1": true, "install.md": true, "install": true}
	for _, m := range got {
		name, p := event(m)
		if name != "install_script" || !want[p["script"].(string)] {
			t.Errorf("event %s %v", name, p)
		}
		delete(want, p["script"].(string))
	}
	if len(want) != 0 {
		t.Errorf("missing %v", want)
	}
}

func TestAnalyticsSkips(t *testing.T) {
	a, c := analyticsApp(t)
	h := a.Handler()
	do := func(path string, hdr ...string) {
		req := httptest.NewRequest("GET", path, nil)
		req.Header.Set("User-Agent", "Mozilla/5.0 Firefox/130.0")
		for i := 0; i+1 < len(hdr); i += 2 {
			req.Header.Set(hdr[i], hdr[i+1])
		}
		h.ServeHTTP(httptest.NewRecorder(), req)
	}
	do("/", "DNT", "1")
	do("/", "Sec-GPC", "1")
	do("/", "User-Agent", "Mozilla/5.0 (compatible; Googlebot/2.1)")
	do("/", "User-Agent", "")
	do("/no-such-page")              // 404
	do("/static/site.css")           // не страница
	do("/healthz")                   // не страница
	do("/connectors.json")           // не страница
	do("/ru")                        // редирект
	do("/", "Accept-Language", "ru") // редирект на /ru/
	do("/install")                   // единственный просмотр
	got := c.wait(t, 1)
	time.Sleep(50 * time.Millisecond)
	c.mu.Lock()
	n := len(c.got)
	c.mu.Unlock()
	if n != 1 {
		t.Fatalf("%d events, want 1: %v", n, c.got)
	}
	if _, p := event(got[0]); p["page_location"] != "https://gate4.ai/install" {
		t.Errorf("%v", p)
	}
}

func TestAnalyticsOff(t *testing.T) {
	if NewAnalytics("", "secret", "", "") != nil || NewAnalytics("G-1", "", "", "") != nil {
		t.Fatal("analytics must be off without id and secret")
	}
	a := testApp(t, true)
	if a.Analytics != nil {
		t.Fatal("test app must have no analytics")
	}
	var nilA *Analytics
	nilA.Track(httptest.NewRequest("GET", "/", nil), "page_view", nil) // не паникует
	r := get(a, "/")
	if strings.Contains(r.body, "googletagmanager") || strings.Contains(r.body, "gtag") {
		t.Error("page must not contain analytics scripts")
	}
	if !strings.Contains(r.h.Get("Content-Security-Policy"), "script-src 'self'") {
		t.Error("csp changed")
	}
}

func TestAnalyticsQueueFull(t *testing.T) {
	// Без Run очередь не разбирается: лишние события теряются, запросы не ждут.
	a := NewAnalytics("G-TEST", "secret", "http://127.0.0.1:9", "https://gate4.ai")
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("User-Agent", "Mozilla/5.0")
	done := make(chan struct{})
	go func() {
		for i := 0; i < 1000; i++ {
			a.Track(req, "page_view", nil)
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Track blocks when the queue is full")
	}
}

func TestAnalyticsClientIDIsStable(t *testing.T) {
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("User-Agent", "Mozilla/5.0 Firefox/130.0")
	req.Header.Set("X-Forwarded-For", "203.0.113.7")
	now := time.Now()
	a := NewAnalytics("G-TEST", "secret", "", "https://gate4.ai")
	b := NewAnalytics("G-TEST", "secret", "", "https://gate4.ai") // другой процесс, тот же секрет
	id := a.payload(req, now, "page_view", nil)["client_id"]
	if b.payload(req, now.Add(72*time.Hour), "page_view", nil)["client_id"] != id {
		t.Error("client_id must not depend on the process or the day")
	}
	if NewAnalytics("G-TEST", "other", "", "").payload(req, now, "page_view", nil)["client_id"] == id {
		t.Error("client_id must depend on the secret")
	}
}

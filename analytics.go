// Посещаемость: сайт сам считает просмотры и шлёт их в Google Analytics 4
// по Measurement Protocol — с сервера, без скриптов и cookies в браузере.
//
// Что уходит в GA: путь страницы, реферер, IP и User-Agent посетителя
// (как при обычном gtag.js — для страны и типа устройства), язык из
// Accept-Language. Идентификатор посетителя — хэш IP и User-Agent с солью
// из секрета GA: постоянный, пока у посетителя тот же адрес и браузер;
// ничего не хранится ни у нас, ни в браузере.
// Заголовки DNT: 1 и Sec-GPC: 1 отключают учёт. Поисковые боты не считаются.

package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const DefaultAnalyticsEndpoint = "https://www.google-analytics.com/mp/collect"

// Analytics — отправка событий в GA4. Nil или пустой MeasurementID — выключено.
type Analytics struct {
	MeasurementID string // G-XXXXXXXXXX (поток данных)
	APISecret     string // секрет Measurement Protocol потока
	Endpoint      string // "" — DefaultAnalyticsEndpoint
	Site          string // https://gate4.ai — для page_location

	client http.Client
	queue  chan []byte
}

// NewAnalytics — nil, если идентификатор или секрет не заданы.
func NewAnalytics(id, secret, endpoint, site string) *Analytics {
	if id == "" || secret == "" {
		return nil
	}
	if endpoint == "" {
		endpoint = DefaultAnalyticsEndpoint
	}
	a := &Analytics{MeasurementID: id, APISecret: secret, Endpoint: endpoint, Site: strings.TrimSuffix(site, "/"),
		client: http.Client{Timeout: 10 * time.Second}, queue: make(chan []byte, 256)}
	return a
}

// Run — отправка из очереди по одному, пока ctx жив.
func (a *Analytics) Run(ctx context.Context) {
	if a == nil {
		return
	}
	for {
		select {
		case <-ctx.Done():
			return
		case body := <-a.queue:
			a.send(ctx, body)
		}
	}
}

func (a *Analytics) send(ctx context.Context, body []byte) {
	u := a.Endpoint + "?" + url.Values{"measurement_id": {a.MeasurementID}, "api_secret": {a.APISecret}}.Encode()
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, bytes.NewReader(body))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := a.client.Do(req)
	if err != nil {
		slog.Warn("analytics", "err", err)
		return
	}
	resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		slog.Warn("analytics", "status", resp.StatusCode)
	}
}

// Middleware — после ответа: HTML 200 → page_view; install.sh/.ps1/.md → install_script.
func (a *Analytics) Middleware(next http.Handler) http.Handler {
	if a == nil {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(sw, r)
		if sw.status != http.StatusOK || r.Method != http.MethodGet {
			return
		}
		ctype := sw.Header().Get("Content-Type")
		switch {
		case strings.HasPrefix(ctype, "text/html"):
			a.Track(r, "page_view", nil)
		case strings.HasPrefix(r.URL.Path, "/install.") || r.URL.Path == "/install" && wantsText(r):
			a.Track(r, "install_script", map[string]any{"script": strings.TrimPrefix(r.URL.Path, "/")})
		}
	})
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

// Track — поставить событие в очередь (если посетитель не отказался и не бот).
func (a *Analytics) Track(r *http.Request, event string, params map[string]any) {
	if a == nil || r.Header.Get("DNT") == "1" || r.Header.Get("Sec-GPC") == "1" || isBot(r.UserAgent()) {
		return
	}
	body, _ := json.Marshal(a.payload(r, time.Now(), event, params))
	select {
	case a.queue <- body:
	default: // очередь полна — событие теряется, сайт не ждёт
	}
}

func (a *Analytics) payload(r *http.Request, now time.Time, event string, params map[string]any) map[string]any {
	ip := clientIP(r)
	ua := r.UserAgent()
	p := map[string]any{
		"page_location":        a.Site + r.URL.RequestURI(),
		"session_id":           strings.ReplaceAll(a.visitor(ip, ua, now, 30*time.Minute), ".", ""),
		"engagement_time_msec": 100,
	}
	if ref := r.Referer(); ref != "" && !strings.HasPrefix(ref, a.Site+"/") && ref != a.Site {
		p["page_referrer"] = ref
	}
	for k, v := range params {
		p[k] = v
	}
	body := map[string]any{
		"client_id":  a.visitor(ip, ua, now, 0),
		"user_agent": ua,
		"events":     []map[string]any{{"name": event, "params": p}},
	}
	if ip != "" {
		body["ip_override"] = ip
	}
	if lang := acceptLanguage(r.Header.Get("Accept-Language")); lang != "" {
		body["device"] = map[string]any{"language": lang}
	}
	return body
}

// visitor — «число.число» из хэша (секрет GA как соль, IP, User-Agent и окно
// времени): client_id без окна — постоянный, session_id — окно в 30 минут.
func (a *Analytics) visitor(ip, ua string, now time.Time, window time.Duration) string {
	h := sha256.New()
	h.Write([]byte(a.APISecret + "\x00" + ip + "\x00" + ua + "\x00"))
	if window > 0 {
		_ = binary.Write(h, binary.BigEndian, now.Unix()/int64(window/time.Second))
	}
	sum := h.Sum(nil)
	return strconv.FormatUint(uint64(binary.BigEndian.Uint32(sum[:4])), 10) + "." +
		strconv.FormatUint(uint64(binary.BigEndian.Uint32(sum[4:8])), 10)
}

// clientIP — из X-Forwarded-For (Caddy перед сайтом), иначе адрес соединения.
func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		first, _, _ := strings.Cut(xff, ",")
		if ip := net.ParseIP(strings.TrimSpace(first)); ip != nil {
			return ip.String()
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return ""
	}
	if ip := net.ParseIP(host); ip != nil && !ip.IsLoopback() {
		return ip.String()
	}
	return ""
}

// acceptLanguage — первый язык из Accept-Language (ru, en-US).
func acceptLanguage(v string) string {
	first, _, _ := strings.Cut(v, ",")
	first, _, _ = strings.Cut(first, ";")
	first = strings.TrimSpace(first)
	if first == "" || first == "*" || len(first) > 10 {
		return ""
	}
	return first
}

var botMarks = []string{"bot", "crawl", "spider", "slurp", "fetch", "preview", "headless", "lighthouse", "monitor", "scan"}

func isBot(ua string) bool {
	if ua == "" {
		return true
	}
	ua = strings.ToLower(ua)
	for _, m := range botMarks {
		if strings.Contains(ua, m) {
			return true
		}
	}
	return false
}

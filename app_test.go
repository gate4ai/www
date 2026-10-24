// Маршруты сайта: ответы, языки, ОС, каталог, webhook, обновление источников.

package main

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/gate4ai/gate4ai/sdk-go/registry"
)

// testdata — индекс-образец, подписанный на ходу ключом теста: (индекс, подпись, открытый ключ).
// Ключ registry у тестов нет, поэтому образец не подписывается заранее.
func testdata(t *testing.T) ([]byte, string, string) {
	t.Helper()
	raw, err := os.ReadFile("testdata/index.json")
	if err != nil {
		t.Fatal(err)
	}
	pub, priv, err := registry.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	sig, err := registry.SignIndex(raw, priv)
	if err != nil {
		t.Fatal(err)
	}
	return raw, sig, pub
}

func testCatalog(t *testing.T) *Catalog {
	t.Helper()
	raw, sig, key := testdata(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, ".sig") {
			_, _ = io.WriteString(w, sig)
			return
		}
		_, _ = w.Write(raw)
	}))
	defer srv.Close()
	c, err := NewSources(srv.URL+"/index.json", []string{key}, DefaultReleasesRepo).FetchCatalog(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func testApp(t *testing.T, withCatalog bool) *App {
	t.Helper()
	a := &App{
		Sources:       NewSources("http://127.0.0.1:9/index.json", []string{DefaultRegistryKey}, DefaultReleasesRepo),
		WebhookSecret: "s3cret",
	}
	a.Sources.GitHubAPI = "http://127.0.0.1:9" // без сети: сверка /latest не удаётся, значение остаётся
	if withCatalog {
		a.catalog.Store(testCatalog(t))
	}
	tag := "v0.1.1"
	a.latest.Store(&tag)
	return a
}

type resp struct {
	status int
	h      http.Header
	body   string
}

func do(a *App, method, uri string, headers ...string) resp {
	req := httptest.NewRequest(method, uri, nil)
	for i := 0; i+1 < len(headers); i += 2 {
		if strings.EqualFold(headers[i], "host") {
			req.Host = headers[i+1]
			continue
		}
		req.Header.Set(headers[i], headers[i+1])
	}
	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, req)
	return resp{w.Code, w.Header(), w.Body.String()}
}

func get(a *App, uri string, headers ...string) resp { return do(a, http.MethodGet, uri, headers...) }

func TestHomePages(t *testing.T) {
	a := testApp(t, true)
	r := get(a, "/")
	if r.status != http.StatusOK {
		t.Fatal(r.status)
	}
	if !strings.Contains(r.h.Get("Content-Security-Policy"), "script-src 'self'") ||
		r.h.Get("X-Content-Type-Options") != "nosniff" || r.h.Get("Referrer-Policy") != "no-referrer" {
		t.Fatalf("security headers: %v", r.h)
	}
	if r.h.Get("Content-Type") != "text/html; charset=utf-8" {
		t.Fatal(r.h.Get("Content-Type"))
	}
	for _, want := range []string{"only</em> what you allow", "curl -fsSL https://gate4.ai/install.sh | sh",
		"/connectors/pagereader", "v0.1.1", `<html lang="en">`, `href="/ru/"`, "Browser · Files · connectors to sites", "browser.act", "Add to allowed sites",
		`domain: "*.bank.example", access: <span class="c-allow">read</span>`, "access:", "folders:"} {
		if !strings.Contains(r.body, want) {
			t.Errorf("home: no %q", want)
		}
	}
	if strings.Count(r.body, `class="card"`) != 3 {
		t.Error("three connector cards on the landing")
	}
	r = get(a, "/ru/")
	if r.status != http.StatusOK || !strings.Contains(r.body, `lang="ru"`) || !strings.Contains(r.body, "только</em> то, что вы разрешили") ||
		!strings.Contains(r.body, `href="/?lang=en"`) {
		t.Fatalf("ru home: %d", r.status)
	}
	if r := get(a, "/ru"); r.status != http.StatusPermanentRedirect || r.h.Get("Location") != "/ru/" {
		t.Fatalf("/ru: %d %v", r.status, r.h)
	}
	// Без каталога главная открывается без карточек.
	if r := get(testApp(t, false), "/"); r.status != http.StatusOK || strings.Contains(r.body, `id="connectors"`) {
		t.Fatal("home without catalog")
	}
}

func TestPageBudget(t *testing.T) {
	a := testApp(t, true)
	assets := len(get(a, "/static/site.css").body) + len(get(a, "/static/site.js").body)
	for _, p := range []string{"/?lang=en", "/ru/", "/install", "/ru/install", "/connectors", "/connectors/pagereader", "/ru/connectors/utilities"} {
		r := get(a, p)
		if r.status != http.StatusOK {
			t.Fatalf("%s: %d", p, r.status)
		}
		if total := len(r.body) + assets; total >= 100_000 {
			t.Errorf("%s: %d bytes with CSS and JS, budget is 100 KB", p, total)
		}
		// Без сторонних скриптов, шрифтов и стилей.
		if strings.Contains(r.body, `src="http`) || strings.Contains(r.body, `<link rel="stylesheet" href="http`) {
			t.Errorf("%s: third-party resource", p)
		}
	}
}

func TestLanguageByAcceptLanguage(t *testing.T) {
	a := testApp(t, true)
	r := get(a, "/", "Accept-Language", "ru-RU,ru;q=0.9,en;q=0.5")
	if r.status != http.StatusTemporaryRedirect || r.h.Get("Location") != "/ru/" {
		t.Fatalf("%d %v", r.status, r.h)
	}
	if r.h.Get("Vary") != "Accept-Language, User-Agent" {
		t.Fatal(r.h.Get("Vary"))
	}
	// Переключатель языка на английский не перенаправляет обратно.
	if r := get(a, "/?lang=en", "Accept-Language", "ru"); r.status != http.StatusOK {
		t.Fatal(r.status)
	}
	if r := get(a, "/", "Accept-Language", "en-US,en;q=0.9,ru;q=0.8"); r.status != http.StatusOK {
		t.Fatal(r.status)
	}
}

func windowsPanel(t *testing.T, body string) string {
	t.Helper()
	i := strings.Index(body, `data-os-panel="windows"`)
	if i < 0 {
		t.Fatal("no windows panel")
	}
	return body[i : i+40]
}

func TestInstallCommandByOS(t *testing.T) {
	a := testApp(t, true)
	win := []string{"User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64)"}
	r := get(a, "/install", win...)
	if strings.Contains(windowsPanel(t, r.body), "hidden") {
		t.Error("windows command hidden for a Windows visitor")
	}
	if r.h.Get("Vary") != "User-Agent" || !strings.Contains(r.body, "releases/tag/v0.1.1") {
		t.Error("vary or latest release")
	}
	r = get(a, "/install?os=linux", win...)
	if !strings.Contains(windowsPanel(t, r.body), "hidden") {
		t.Error("query overrides user agent")
	}
	r = get(a, "/ru/install", "User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 14_5)")
	if !strings.Contains(windowsPanel(t, r.body), "hidden") || !strings.Contains(r.body, "Установка gate4.ai") {
		t.Error("ru install for macOS")
	}
}

// Следующий шаг после установки — gate4ai ui (мастер), параметры — под «Для администраторов».
func TestInstallPageNextStep(t *testing.T) {
	a := testApp(t, true)
	for path, wants := range map[string][]string{
		"/install":    {"Next: run gate4ai ui", "setup wizard opens in your browser", "For administrators"},
		"/ru/install": {"Дальше: запустите gate4ai ui", "мастер настройки", "Для администраторов"},
	} {
		r := get(a, path)
		wants = append(wants, `id="next"`, `data-copy="gate4ai ui"`, "#readme", "GATE4AI_NO_START=1", "GATE4AI_NO_OPEN=1", "GATE4AI_NO_TRAY=1")
		for _, want := range wants {
			if !strings.Contains(r.body, want) {
				t.Errorf("%s: no %q", path, want)
			}
		}
		d := strings.Index(r.body, `<details class="admin"`)
		if d < 0 || strings.Index(r.body, "GATE4AI_VERSION") < d {
			t.Errorf("%s: installer options inside the collapsed block", path)
		}
		if strings.Index(r.body, `id="next"`) > d {
			t.Errorf("%s: the next step comes before the options", path)
		}
	}
}

// Лендинг: финальная подсказка — мастер, а не README; карточки агентов ведут к подключению.
func TestLandingNextStep(t *testing.T) {
	a := testApp(t, true)
	for path, want := range map[string]string{"/?lang=en": "the setup wizard opens", "/ru/": "откроется мастер настройки"} {
		r := get(a, path)
		cta := r.body[strings.Index(r.body, `class="cta"`):]
		cta = cta[:strings.Index(cta, "</section>")]
		if !strings.Contains(cta, want) || !strings.Contains(cta, "<code>gate4ai ui</code>") || strings.Contains(cta, "README") {
			t.Errorf("%s: final hint", path)
		}
		if strings.Count(r.body, `<a class="agent" href="`) != 4 || !strings.Contains(r.body, `/install#next"`) {
			t.Errorf("%s: agent cards are links", path)
		}
	}
	if !strings.Contains(get(a, "/ru/").body, `<a class="agent" href="/ru/install#next">`) {
		t.Error("ru agent cards lead to the ru install page")
	}
}

func TestScriptsAndLatest(t *testing.T) {
	a := testApp(t, true)
	r := get(a, "/install.sh")
	if r.status != http.StatusOK || r.h.Get("Content-Type") != "text/plain; charset=utf-8" ||
		!strings.Contains(r.h.Get("Cache-Control"), "max-age=300") {
		t.Fatalf("install.sh: %d %v", r.status, r.h)
	}
	if !strings.HasSuffix(strings.TrimRight(r.body, "\n"), `main "$@"`) {
		t.Error("install.sh: the last line calls main")
	}
	// Автозапуск по умолчанию, отказ — GATE4AI_NO_START; мастер в браузере — gate4ai ui, отказ — GATE4AI_NO_OPEN.
	for _, want := range []string{"GATE4AI_NO_START", "GATE4AI_NO_OPEN", "service install", `"$gate" ui`,
		"WAYLAND_DISPLAY", "${CI:-}", "Next: gate4ai ui   # opens the setup wizard in your browser", "daemon restart",
		// Браузер агентов: проверка, установка с согласия (ответ — из терминала), отказ от браузера.
		"GATE4AI_INSTALL_DEPS", "GATE4AI_NO_BROWSER", `browser setup </dev/tty`, "browser setup --dry-run"} {
		if !strings.Contains(r.body, want) {
			t.Errorf("install.sh: no %q", want)
		}
	}
	if strings.Contains(r.body, "Next steps:") || strings.Contains(r.body, "GATE4AI_START") {
		t.Error("install.sh: one next step, no GATE4AI_START")
	}
	r = get(a, "/install.ps1")
	if r.status != http.StatusOK || !strings.HasSuffix(strings.TrimRight(r.body, "\r\n"), "Install-Gate4ai") {
		t.Error("install.ps1")
	}
	for _, want := range []string{"GATE4AI_NO_START", "GATE4AI_NO_OPEN", "service", "& $exe ui",
		"$env:CI", "Next: gate4ai ui   # opens the setup wizard in your browser", "GATE4AI_INSTALL_DEPS", "GATE4AI_NO_BROWSER", "'browser', 'setup'"} {
		if !strings.Contains(r.body, want) {
			t.Errorf("install.ps1: no %q", want)
		}
	}
	if strings.Contains(r.body, "Next steps:") {
		t.Error("install.ps1: one next step")
	}
	r = get(a, "/latest")
	if r.status != http.StatusOK || r.body != "v0.1.1\n" || r.h.Get("Content-Type") != "text/plain; charset=utf-8" {
		t.Fatalf("latest: %d %q", r.status, r.body)
	}
	a.latest.Store(nil)
	if r := get(a, "/latest"); r.status != http.StatusServiceUnavailable {
		t.Fatal(r.status)
	}
}

func TestCatalogPages(t *testing.T) {
	a := testApp(t, true)
	r := get(a, "/connectors")
	if r.status != http.StatusOK {
		t.Fatal(r.status)
	}
	for _, name := range []string{"pagereader", "receipts", "utilities"} {
		if !strings.Contains(r.body, "/connectors/"+name+`"`) {
			t.Error(name)
		}
	}
	if !strings.Contains(r.body, "3 connectors from the signed registry") {
		t.Error("count")
	}
	// Браузер и Файлы встроены: на странице каталога без фильтра, не карточками и не ссылками.
	if !strings.Contains(r.body, "Built into gate4ai") || !strings.Contains(r.body, `<span class="card-name">Browser</span>`) ||
		!strings.Contains(r.body, `<span class="card-name">Files</span>`) || strings.Contains(r.body, `/connectors/browser"`) {
		t.Error("built-in Browser and Files")
	}
	if strings.Count(r.body, `class="card"`) != 3 {
		t.Error("built-ins are not cards")
	}
	r = get(a, "/connectors?q=receipt")
	if !strings.Contains(r.body, `/connectors/receipts"`) || strings.Contains(r.body, `/connectors/pagereader"`) || strings.Contains(r.body, "Built into gate4ai") {
		t.Error("search")
	}
	if !strings.Contains(r.body, `href="/connectors?trust=verified&amp;q=receipt"`) {
		t.Error("filters keep the query")
	}
	r = get(a, "/connectors?trust=community")
	if strings.Contains(r.body, `/connectors/receipts"`) || !strings.Contains(r.body, "Nothing found") {
		t.Error("trust filter")
	}
	if r := get(a, "/connectors?trust=bogus"); !strings.Contains(r.body, `/connectors/receipts"`) {
		t.Error("unknown trust level is ignored")
	}

	r = get(a, "/ru/connectors/receipts")
	if r.status != http.StatusOK {
		t.Fatal(r.status)
	}
	for _, want := range []string{"gate4ai connectors install receipts",
		"Ходит на сайты только через ваш Браузер и только на lk.example.org (read) и *.pay.example (read)",
		"Работает с файлами только через ваши Файлы и только в folder (append)",
		"site lk.example.org: read", "<title>receipts: Коннекторы</title>", "Проверен",
		`<h1 class="h1">receipts</h1>`, "Downloads utility receipts",
		`href="http://127.0.0.1:7431/connectors?install=receipts"`, "Открыть в gate4.ai", "если gate4.ai запущен на этом компьютере",
		`<details class="tech">`, "Технически"} {
		if !strings.Contains(r.body, want) {
			t.Errorf("connector page: no %q", want)
		}
	}
	if strings.Contains(r.body, `<p class="connector-id mono">`) {
		t.Error("id equals the title: shown once")
	}
	// Каждая возможность — один раз человеческим языком; исходные строки — только под «Технически».
	if strings.Count(r.body, "Ходит на сайты") != 1 || strings.Count(r.body, "Работает с файлами") != 1 {
		t.Error("capabilities are grouped")
	}
	if i := strings.Index(r.body, `<details class="tech">`); i < 0 || strings.Index(r.body, "site lk.example.org: read") < i {
		t.Error("raw capabilities only inside the Technical block")
	}
	if !strings.Contains(r.body, `<span class="mono">v3</span>`) || strings.Index(r.body, "v3</span>") > strings.Index(r.body, "v2</span>") {
		t.Error("versions, newest first")
	}

	if r := get(a, "/connectors/nope"); r.status != http.StatusNotFound {
		t.Fatal(r.status)
	}
	raw, _, _ := testdata(t)
	r = get(a, "/connectors.json")
	if r.status != http.StatusOK || r.body != string(raw) || r.h.Get("Content-Type") != "application/json" {
		t.Fatal("connectors.json")
	}

	empty := testApp(t, false)
	if r := get(empty, "/connectors"); r.status != http.StatusOK || !strings.Contains(r.body, "signed registry") {
		t.Fatal("catalog not loaded")
	}
	if r := get(empty, "/connectors.json"); r.status != http.StatusServiceUnavailable {
		t.Fatal(r.status)
	}
	if r := get(empty, "/connectors/receipts"); r.status != http.StatusNotFound {
		t.Fatal(r.status)
	}
}

func TestRevokedVersion(t *testing.T) {
	a := testApp(t, true)
	c := a.Catalog()
	c.Index.Revoked = append(c.Index.Revoked, revocation("receipts", "3", "leaks data"))
	r := get(a, "/connectors/receipts")
	if !strings.Contains(r.body, `class="is-revoked"`) || !strings.Contains(r.body, "leaks data") {
		t.Error("revoked version with its reason")
	}
	i := strings.Index(r.body, "v2</span>")
	if i < 0 || !strings.Contains(r.body[i:i+80], "pill-allow") {
		t.Error("v2 is the latest when v3 is revoked")
	}
}

// Коннектор в монорепозитории (path): «Исходник» ведёт в его каталог.
func TestConnectorRepoPath(t *testing.T) {
	a := testApp(t, true)
	if r := get(a, "/connectors/pagereader"); !strings.Contains(r.body, `href="https://github.com/gate4ai/gate4ai/tree/main/e2e/testdata/pagereader"`) {
		t.Error("source link to the connector folder")
	}
}

func TestHTMLIsEscaped(t *testing.T) {
	r := get(testApp(t, true), "/connectors?q=%3Cscript%3Ealert(1)%3C/script%3E")
	if strings.Contains(r.body, "<script>alert(1)") {
		t.Fatal("not escaped")
	}
}

func TestMiscRoutes(t *testing.T) {
	a := testApp(t, true)
	if r := get(a, "/healthz"); r.status != http.StatusOK || r.body != "ok" {
		t.Fatal(r)
	}
	if r := get(a, "/static/site.css"); r.status != http.StatusOK || r.h.Get("Content-Type") != "text/css; charset=utf-8" ||
		!strings.Contains(r.body, "@media print") {
		t.Fatal("site.css: status and print styles without the reveal animation", r.status)
	}
	if r := get(a, "/static/site.js"); r.status != http.StatusOK || r.h.Get("Content-Type") != "text/javascript; charset=utf-8" {
		t.Fatal(r.status)
	}
	if r := get(a, "/favicon.svg"); r.status != http.StatusOK || r.h.Get("Content-Type") != "image/svg+xml" {
		t.Fatal(r.status)
	}
	if r := get(a, "/robots.txt"); !strings.Contains(r.body, "Sitemap: https://gate4.ai/sitemap.xml") {
		t.Fatal(r.body)
	}
	r := get(a, "/sitemap.xml")
	if r.status != http.StatusOK || !strings.Contains(r.body, "https://gate4.ai/ru/connectors/receipts") || !strings.Contains(r.body, "<loc>https://gate4.ai/ru/</loc>") {
		t.Fatal(r.body)
	}
	r = get(a, "/ru/nothing")
	if r.status != http.StatusNotFound || !strings.Contains(r.body, "Страница не найдена") {
		t.Fatal(r.status)
	}
	if r := get(a, "/nothing"); r.status != http.StatusNotFound || !strings.Contains(r.body, "Page not found") {
		t.Fatal(r.status)
	}
	if r := do(a, http.MethodHead, "/healthz"); r.status != http.StatusOK {
		t.Fatal("HEAD", r.status)
	}
}

func TestWebhook(t *testing.T) {
	a := testApp(t, true)
	if r := do(a, http.MethodPost, "/hooks/registry"); r.status != http.StatusUnauthorized {
		t.Fatal(r.status)
	}
	if r := do(a, http.MethodPost, "/hooks/registry", "Authorization", "Bearer wrong"); r.status != http.StatusUnauthorized {
		t.Fatal(r.status)
	}
	// Верный секрет, индекс недоступен (порт 9): ошибка шлюза, прежний каталог остаётся.
	before := a.Catalog()
	if r := do(a, http.MethodPost, "/hooks/registry", "Authorization", "Bearer s3cret"); r.status != http.StatusBadGateway {
		t.Fatal(r.status)
	}
	if a.Catalog() != before {
		t.Fatal("the last verified catalog is replaced")
	}
	a.WebhookSecret = ""
	if r := do(a, http.MethodPost, "/hooks/registry", "Authorization", "Bearer "); r.status != http.StatusNotFound {
		t.Fatal("webhook without a secret", r.status)
	}
}

// Webhook и таймер: верный индекс заменяет каталог, подменённый — нет.
func TestRefreshKeepsLastVerified(t *testing.T) {
	raw, sig, key := testdata(t)
	serve := raw
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/index.json.sig":
			_, _ = io.WriteString(w, sig)
		case "/index.json":
			_, _ = w.Write(serve)
		case "/repos/gate4ai/gate4ai/releases/latest":
			_, _ = io.WriteString(w, `{"tag_name":"v0.7.0"}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	a := testApp(t, false)
	a.Sources.RegistryURL = srv.URL + "/index.json"
	a.Sources.RegistryKeys = []string{key}
	a.Sources.GitHubAPI = srv.URL
	if _, err := registry.ParsePublicKey(DefaultRegistryKey); err != nil {
		t.Fatal(err)
	}
	if r := do(a, http.MethodPost, "/hooks/registry", "Authorization", "Bearer s3cret"); r.status != http.StatusOK || r.body != "refreshed\n" {
		t.Fatal(r.status, r.body)
	}
	good := a.Catalog()
	if good == nil || len(good.Index.Connectors) != 3 {
		t.Fatal("catalog not loaded")
	}
	serve = []byte(strings.Replace(string(raw), "E", "X", 1))
	if err := a.RefreshCatalog(context.Background()); err == nil || a.Catalog() != good {
		t.Fatalf("tampered index accepted: %v", err)
	}
	if err := a.RefreshLatest(context.Background()); err != nil || a.Latest() != "v0.7.0" {
		t.Fatal(err, a.Latest())
	}
	a.Sources.ReleasesRepo = "gate4ai/none"
	if err := a.RefreshLatest(context.Background()); err == nil || a.Latest() != "v0.7.0" {
		t.Fatal("failed refresh replaced /latest")
	}
}

// GET /latest сверяется с GitHub не чаще latestFresh, условным запросом (ETag).
func TestLatestOnRequest(t *testing.T) {
	tag, hits, conditional := "v0.7.0", 0, 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		etag := `"` + tag + `"`
		if r.Header.Get("If-None-Match") == etag {
			conditional++
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("ETag", etag)
		_, _ = io.WriteString(w, `{"tag_name":"`+tag+`"}`)
	}))
	defer srv.Close()
	a := testApp(t, false)
	a.Sources.GitHubAPI = srv.URL
	if r := get(a, "/latest"); r.body != "v0.7.0\n" || hits != 1 {
		t.Fatal(r.body, hits)
	}
	tag = "v0.7.1"
	if r := get(a, "/latest"); r.body != "v0.7.0\n" || hits != 1 {
		t.Fatal("checked GitHub again within latestFresh:", r.body, hits)
	}
	a.latestAt = a.latestAt.Add(-latestFresh)
	if r := get(a, "/latest"); r.body != "v0.7.1\n" || hits != 2 {
		t.Fatal("new release not seen after latestFresh:", r.body, hits)
	}
	a.latestAt = a.latestAt.Add(-latestFresh)
	if r := get(a, "/latest"); r.body != "v0.7.1\n" || hits != 3 || conditional != 1 {
		t.Fatal("same release: 304 keeps the tag", r.body, hits, conditional)
	}
}

func TestCanonicalHostRedirect(t *testing.T) {
	a := testApp(t, true)
	a.CanonicalHost = "gate4.ai"
	r := get(a, "/connectors?q=x", "Host", "www.gate4.ai")
	if r.status != http.StatusPermanentRedirect || r.h.Get("Location") != "https://gate4.ai/connectors?q=x" {
		t.Fatalf("%d %v", r.status, r.h)
	}
	if r.h.Get("Content-Security-Policy") == "" {
		t.Error("security headers on the redirect")
	}
	if r := get(a, "/connectors", "Host", "gate4.ai"); r.status != http.StatusOK {
		t.Fatal(r.status)
	}
	if r := get(a, "/connectors", "Host", "gate4.ai:443"); r.status != http.StatusOK {
		t.Fatal("port", r.status)
	}
	if r := get(a, "/healthz", "Host", "127.0.0.1:8080"); r.status != http.StatusOK {
		t.Fatal("healthz on any host", r.status)
	}
}

// Маршруты сайта, заголовки безопасности, редирект на канонический хост.

package main

import (
	"bytes"
	"context"
	"crypto/subtle"
	"embed"
	"fmt"
	"html/template"
	"log/slog"
	"net/http"
	"slices"
	"sort"
	"strings"
	"sync/atomic"
	"time"

	"github.com/gate4ai/sdk-go/registry"
)

var (
	//go:embed install/install.sh
	installSh string
	//go:embed install/install.ps1
	installPs1 string
	//go:embed static/site.css
	siteCSS string
	//go:embed static/site.js
	siteJS string
	//go:embed static/favicon.svg
	favicon string
	//go:embed templates
	templatesFS embed.FS
)

const GitHub = "https://github.com/gate4ai/gate4ai"

const csp = "default-src 'none'; script-src 'self'; style-src 'self'; img-src 'self' data:; " +
	"connect-src 'self'; form-action 'self'; base-uri 'none'; frame-ancestors 'none'"

// App — состояние сайта: каталог и последняя версия ядра — только в памяти.
type App struct {
	Sources       *Sources
	WebhookSecret string // "" — POST /hooks/registry выключен
	CanonicalHost string // "" — без редиректа

	catalog atomic.Pointer[Catalog]
	latest  atomic.Pointer[string]
}

func (a *App) Catalog() *Catalog { return a.catalog.Load() }

func (a *App) Latest() string {
	if t := a.latest.Load(); t != nil {
		return *t
	}
	return ""
}

// Шаблоны: base.html с блоком content, общие cmd и card и страница.
var pages = map[string]*template.Template{}

func init() {
	funcs := template.FuncMap{
		"withPage": func(p Page, c Card) cardView { return cardView{Page: p, C: c} },
	}
	for _, name := range []string{"index_en", "index_ru", "install", "connectors", "connector", "not_found"} {
		pages[name] = template.Must(template.New("base.html").Funcs(funcs).ParseFS(templatesFS,
			"templates/base.html", "templates/cmd.html", "templates/card.html", "templates/"+name+".html"))
	}
}

type cardView struct {
	Page Page
	C    Card
}

// Page — общие данные страницы: язык, тексты, путь для переключателя языка.
type Page struct {
	Lang        Lang
	T           *Tr
	P           string
	Path        string // путь этой же страницы без языкового префикса (/connectors)
	Title       string
	Description string
	GitHub      string
}

func newPage(lang Lang, path, title, description string) Page {
	return Page{Lang: lang, T: lang.Tr(), P: lang.Prefix(), Path: path, Title: title, Description: description, GitHub: GitHub}
}

// OtherLangHref — ссылка на эту же страницу на другом языке.
func (p Page) OtherLangHref() string {
	switch {
	case p.Lang == Ru && p.Path == "/":
		return "/?lang=en"
	case p.Lang == Ru:
		return p.Path
	case p.Path == "/":
		return "/ru/"
	}
	return "/ru" + p.Path
}

func (p Page) OtherLangCode() string {
	if p.Lang == Ru {
		return "en"
	}
	return "ru"
}

// Handler — все маршруты с заголовками безопасности и каноническим хостом.
func (a *App) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", a.homeEn)
	mux.HandleFunc("GET /ru", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/ru/", http.StatusPermanentRedirect)
	})
	mux.HandleFunc("GET /ru/{$}", a.homeRu)
	mux.HandleFunc("GET /install", a.install(En))
	mux.HandleFunc("GET /ru/install", a.install(Ru))
	mux.HandleFunc("GET /connectors", a.catalogPage(En))
	mux.HandleFunc("GET /ru/connectors", a.catalogPage(Ru))
	mux.HandleFunc("GET /connectors/{name}", a.connector(En))
	mux.HandleFunc("GET /ru/connectors/{name}", a.connector(Ru))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		lang := En
		if strings.HasPrefix(r.URL.Path, "/ru/") {
			lang = Ru
		}
		notFound(w, lang)
	})

	mux.HandleFunc("GET /install.sh", script(installSh))
	mux.HandleFunc("GET /install.ps1", script(installPs1))
	mux.HandleFunc("GET /latest", a.latestTag)
	mux.HandleFunc("GET /connectors.json", a.catalogJSON)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { text(w, http.StatusOK, "ok") })
	mux.HandleFunc("POST /hooks/registry", a.hookRegistry)
	mux.HandleFunc("GET /static/site.css", asset(siteCSS, "text/css; charset=utf-8"))
	mux.HandleFunc("GET /static/site.js", asset(siteJS, "text/javascript; charset=utf-8"))
	mux.HandleFunc("GET /favicon.svg", asset(favicon, "image/svg+xml"))
	mux.HandleFunc("GET /robots.txt", func(w http.ResponseWriter, r *http.Request) {
		text(w, http.StatusOK, "User-agent: *\nAllow: /\nSitemap: https://gate4.ai/sitemap.xml\n")
	})
	mux.HandleFunc("GET /sitemap.xml", a.sitemap)
	return securityHeaders(a.canonical(mux))
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Security-Policy", csp)
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "no-referrer")
		next.ServeHTTP(w, r)
	})
}

// canonical — www.gate4.ai и другие имена → 308 на канонический хост, если он задан.
func (a *App) canonical(next http.Handler) http.Handler {
	if a.CanonicalHost == "" {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host, _, _ := strings.Cut(r.Host, ":")
		if host != "" && host != a.CanonicalHost && r.URL.Path != "/healthz" {
			w.Header().Set("Location", "https://"+a.CanonicalHost+r.URL.RequestURI())
			w.WriteHeader(http.StatusPermanentRedirect)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func text(w http.ResponseWriter, status int, body string) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(body))
}

func script(body string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "public, max-age=300")
		text(w, http.StatusOK, body)
	}
}

func asset(body, ctype string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", ctype)
		w.Header().Set("Cache-Control", "public, max-age=3600")
		_, _ = w.Write([]byte(body))
	}
}

func render(w http.ResponseWriter, status int, name string, data any) {
	var buf bytes.Buffer
	if err := pages[name].Execute(&buf, data); err != nil {
		slog.Error("template", "name", name, "err", err)
		http.Error(w, "", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write(buf.Bytes())
}

// visitorOS — ?os= или по User-Agent.
func visitorOS(r *http.Request) OS {
	if o, ok := osFromQuery(r.URL.Query().Get("os")); ok {
		return o
	}
	return osFromUserAgent(r.UserAgent())
}

type homeView struct {
	Page   Page
	OS     OS
	Cards  []Card
	Latest string
}

func (a *App) homeCards(lang Lang) []Card {
	c := a.Catalog()
	if c == nil {
		return nil
	}
	cards := c.Cards("", "", lang)
	return cards[:min(3, len(cards))]
}

func (a *App) homeEn(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Vary", "Accept-Language, User-Agent")
	q := r.URL.Query()
	if !q.Has("lang") && !q.Has("os") && prefersRu(r.Header.Get("Accept-Language")) {
		http.Redirect(w, r, "/ru/", http.StatusTemporaryRedirect)
		return
	}
	render(w, http.StatusOK, "index_en", homeView{
		Page: newPage(En, "/", "gate4.ai: AI agents get only what you allow",
			"A guard for your computer. Claude, ChatGPT, Claude Code, OpenClaw and Hermes reach your email, files and browser only under your rules: allow, deny or ask you first."),
		OS: visitorOS(r), Cards: a.homeCards(En), Latest: a.Latest(),
	})
}

func (a *App) homeRu(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Vary", "User-Agent")
	render(w, http.StatusOK, "index_ru", homeView{
		Page: newPage(Ru, "/", "gate4.ai: AI-агенты получают только то, что вы разрешили",
			"Страж для вашего компьютера. Claude, ChatGPT, Claude Code, OpenClaw и Hermes получают доступ к почте, файлам и браузеру только по вашим правилам: разрешить, запретить или спросить вас."),
		OS: visitorOS(r), Cards: a.homeCards(Ru), Latest: a.Latest(),
	})
}

type installView struct {
	Page   Page
	OS     OS
	Latest string
}

func (a *App) install(lang Lang) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		t := lang.Tr()
		w.Header().Set("Vary", "User-Agent")
		render(w, http.StatusOK, "install", installView{
			Page: newPage(lang, "/install", t.InstallTitle, t.InstallLead), OS: visitorOS(r), Latest: a.Latest(),
		})
	}
}

type catalogView struct {
	Page   Page
	Q      string
	Trust  string
	Levels []string
	Loaded bool
	Total  int
	Cards  []Card
}

func (a *App) catalogPage(lang Lang) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		t := lang.Tr()
		v := catalogView{Page: newPage(lang, "/connectors", t.CatalogTitle, t.CatalogLead),
			Q: r.URL.Query().Get("q"), Levels: registry.TrustLevels}
		if trust := r.URL.Query().Get("trust"); slices.Contains(registry.TrustLevels, trust) {
			v.Trust = trust
		}
		if c := a.Catalog(); c != nil {
			v.Loaded, v.Total, v.Cards = true, len(c.Index.Connectors), c.Cards(v.Q, v.Trust, lang)
		}
		render(w, http.StatusOK, "connectors", v)
	}
}

// VersionRow — версия на странице коннектора.
type VersionRow struct {
	Version string
	Revoked bool
	Reason  string
	Latest  bool
}

type connectorView struct {
	Page     Page
	Card     Card
	License  string
	Repo     string
	Versions []VersionRow
}

func (a *App) connector(lang Lang) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		name := r.PathValue("name")
		c := a.Catalog()
		if c == nil {
			notFound(w, lang)
			return
		}
		e, ok := c.Index.Connectors[name]
		if !ok {
			notFound(w, lang)
			return
		}
		card := c.Card(name, lang)
		v := connectorView{Card: card, License: e.License, Repo: e.Repo,
			Page: newPage(lang, "/connectors/"+name, name+": "+lang.Tr().CatalogTitle, card.Description)}
		for i := len(e.Versions) - 1; i >= 0; i-- {
			ver := e.Versions[i].Version
			revoked, reason := c.Index.IsRevoked(name, ver)
			v.Versions = append(v.Versions, VersionRow{Version: ver, Revoked: revoked, Reason: reason,
				Latest: card.Version == ver})
		}
		render(w, http.StatusOK, "connector", v)
	}
}

type notFoundView struct{ Page Page }

func notFound(w http.ResponseWriter, lang Lang) {
	t := lang.Tr()
	render(w, http.StatusNotFound, "not_found", notFoundView{Page: newPage(lang, "/", t.NotFoundTitle, t.NotFoundText)})
}

func (a *App) latestTag(w http.ResponseWriter, r *http.Request) {
	tag := a.Latest()
	if tag == "" {
		text(w, http.StatusServiceUnavailable, "latest version is not known yet\n")
		return
	}
	w.Header().Set("Cache-Control", "public, max-age=300")
	text(w, http.StatusOK, tag+"\n")
}

func (a *App) catalogJSON(w http.ResponseWriter, r *http.Request) {
	c := a.Catalog()
	if c == nil {
		text(w, http.StatusServiceUnavailable, "catalog is not loaded yet\n")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "public, max-age=300")
	_, _ = w.Write(c.Raw)
}

// hookRegistry — webhook из CI registry: перечитать индекс сейчас.
func (a *App) hookRegistry(w http.ResponseWriter, r *http.Request) {
	if a.WebhookSecret == "" {
		http.NotFound(w, r)
		return
	}
	given, _ := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if subtle.ConstantTimeCompare([]byte(given), []byte(a.WebhookSecret)) != 1 {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 25*time.Second)
	defer cancel()
	if err := a.RefreshCatalog(ctx); err != nil {
		text(w, http.StatusBadGateway, err.Error()+"\n")
		return
	}
	text(w, http.StatusOK, "refreshed\n")
}

func (a *App) sitemap(w http.ResponseWriter, r *http.Request) {
	paths := []string{"/", "/install", "/connectors"}
	if c := a.Catalog(); c != nil {
		var names []string
		for n := range c.Index.Connectors {
			names = append(names, n)
		}
		sort.Strings(names)
		for _, n := range names {
			paths = append(paths, "/connectors/"+n)
		}
	}
	var b strings.Builder
	b.WriteString("<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n<urlset xmlns=\"http://www.sitemaps.org/schemas/sitemap/0.9\">\n")
	for _, p := range paths {
		ru := "/ru" + p
		if p == "/" {
			ru = "/ru/"
		}
		for _, loc := range []string{p, ru} {
			fmt.Fprintf(&b, "  <url><loc>https://gate4.ai%s</loc></url>\n", loc)
		}
	}
	b.WriteString("</urlset>\n")
	w.Header().Set("Content-Type", "application/xml")
	_, _ = w.Write([]byte(b.String()))
}

// Внешние данные: подписанный индекс registry и последняя версия ядра.
// Хранятся только в памяти и обновляются по таймеру, по webhook (индекс) и по
// запросу /latest (версия); при ошибке остаётся последнее проверенное.

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/gate4ai/sdk-go/registry"
)

const (
	DefaultRegistry     = "https://raw.githubusercontent.com/gate4ai/registry/main/index.json"
	DefaultRegistryKey  = "ed25519:yDcbQuNCGqSqHSt08HdvW5/fNyWRW8FIXtKiL91hqtw="
	DefaultReleasesRepo = "gate4ai/gate4ai"

	catalogEvery = 5 * time.Minute
	latestEvery  = 10 * time.Minute
	// latestFresh — GET /latest сверяет версию с GitHub, если последняя сверка старше:
	// новый релиз виден через минуту, а запросов к API не больше 60 в час (лимит без токена).
	latestFresh = time.Minute
	maxBody     = 8 << 20
)

// Sources — откуда сайт берёт индекс registry и последнюю версию ядра.
type Sources struct {
	RegistryURL  string
	RegistryKeys []string
	ReleasesRepo string
	GitHubAPI    string // https://api.github.com; в тестах — фейковый сервер
	HTTP         *http.Client
}

func NewSources(registryURL string, keys []string, releasesRepo string) *Sources {
	return &Sources{RegistryURL: registryURL, RegistryKeys: keys, ReleasesRepo: releasesRepo,
		GitHubAPI: "https://api.github.com", HTTP: &http.Client{Timeout: 20 * time.Second}}
}

func (s *Sources) get(ctx context.Context, url string) ([]byte, error) {
	b, _, err := s.getETag(ctx, url, "")
	return b, err
}

// getETag — GET с If-None-Match: при 304 тело nil и ошибки нет; вторым — ETag ответа.
func (s *Sources) getETag(ctx context.Context, url, etag string) ([]byte, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, "", fmt.Errorf("%s: %w", url, err)
	}
	req.Header.Set("User-Agent", "gate4ai-www/"+version)
	if etag != "" {
		req.Header.Set("If-None-Match", etag)
	}
	res, err := s.HTTP.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer res.Body.Close()
	if etag != "" && res.StatusCode == http.StatusNotModified {
		return nil, etag, nil
	}
	if res.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("%s: HTTP %s", url, res.Status)
	}
	b, err := io.ReadAll(io.LimitReader(res.Body, maxBody))
	if err != nil {
		return nil, "", fmt.Errorf("%s: %w", url, err)
	}
	return b, res.Header.Get("ETag"), nil
}

// Catalog — проверенный индекс вместе с исходными байтами (для /connectors.json).
type Catalog struct {
	Index *registry.Index
	Raw   []byte
}

// FetchCatalog скачивает индекс с подписью и проверяет их правилами sdk-go/registry.
func (s *Sources) FetchCatalog(ctx context.Context) (*Catalog, error) {
	raw, err := s.get(ctx, s.RegistryURL)
	if err != nil {
		return nil, err
	}
	sig, err := s.get(ctx, s.RegistryURL+".sig")
	if err != nil {
		return nil, err
	}
	ix, err := registry.ParseIndex(raw, string(sig), s.RegistryKeys)
	if err != nil {
		return nil, err
	}
	return &Catalog{Index: ix, Raw: raw}, nil
}

// FetchLatest — тег последнего релиза ядра из GitHub API. С etag прошлого ответа
// запрос условный: если релиз тот же (304, лимит API не тратится), тег "".
func (s *Sources) FetchLatest(ctx context.Context, etag string) (tag, newETag string, err error) {
	body, newETag, err := s.getETag(ctx, s.GitHubAPI+"/repos/"+s.ReleasesRepo+"/releases/latest", etag)
	if err != nil || body == nil {
		return "", newETag, err
	}
	var v struct {
		TagName string `json:"tag_name"`
	}
	if err := json.Unmarshal(body, &v); err != nil {
		return "", "", err
	}
	if !validTag(v.TagName) {
		return "", "", fmt.Errorf("unexpected tag %q", v.TagName)
	}
	return v.TagName, newETag, nil
}

// validTag — v1.2.3 (с необязательным суффиксом -rc.1): тег идёт в установщики как есть.
func validTag(tag string) bool {
	if len(tag) > 40 || len(tag) < 2 || tag[0] != 'v' || tag[1] < '0' || tag[1] > '9' {
		return false
	}
	return strings.Trim(tag[1:], "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789.-") == ""
}

// RefreshCatalog заменяет каталог новым проверенным; при ошибке прежний остаётся.
func (a *App) RefreshCatalog(ctx context.Context) error {
	c, err := a.Sources.FetchCatalog(ctx)
	if err != nil {
		return err
	}
	a.catalog.Store(c)
	return nil
}

// RefreshLatest обновляет /latest; при ошибке прежнее значение остаётся.
func (a *App) RefreshLatest(ctx context.Context) error { return a.refreshLatest(ctx, 0) }

// refreshLatest — то же, если последняя сверка старше maxAge. Сверки идут по одной:
// одновременные запросы /latest ждут первую и не повторяют её.
func (a *App) refreshLatest(ctx context.Context, maxAge time.Duration) error {
	a.latestMu.Lock()
	defer a.latestMu.Unlock()
	if maxAge > 0 && time.Since(a.latestAt) < maxAge {
		return nil
	}
	a.latestAt = time.Now() // и при ошибке: недоступный GitHub не опрашивается на каждый запрос
	tag, etag, err := a.Sources.FetchLatest(ctx, a.latestETag)
	if err != nil {
		return err
	}
	a.latestETag = etag
	if tag != "" {
		a.latest.Store(&tag)
	}
	return nil
}

// LatestFresh — последняя версия, сверенная с GitHub не раньше latestFresh назад.
func (a *App) LatestFresh(ctx context.Context) string {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := a.refreshLatest(ctx, latestFresh); err != nil {
		slog.Warn("latest release", "err", err)
	}
	return a.Latest()
}

// Refresh — фоновое обновление до отмены ctx: индекс — раз в 5 минут, версия — раз в 10.
func (a *App) Refresh(ctx context.Context) {
	go every(ctx, catalogEvery, func() {
		if err := a.RefreshCatalog(ctx); err != nil {
			slog.Warn("registry", "err", err)
		}
	})
	every(ctx, latestEvery, func() {
		if err := a.RefreshLatest(ctx); err != nil {
			slog.Warn("latest release", "err", err)
		}
	})
}

func every(ctx context.Context, d time.Duration, f func()) {
	t := time.NewTicker(d)
	defer t.Stop()
	for {
		f()
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

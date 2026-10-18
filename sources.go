// Внешние данные: подписанный индекс registry и последняя версия ядра.
// Хранятся только в памяти и обновляются по таймеру и по webhook; при ошибке
// остаётся последнее проверенное.

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
	maxBody      = 8 << 20
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
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", url, err)
	}
	req.Header.Set("User-Agent", "gate4ai-www/"+version)
	res, err := s.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s: HTTP %s", url, res.Status)
	}
	b, err := io.ReadAll(io.LimitReader(res.Body, maxBody))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", url, err)
	}
	return b, nil
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

// FetchLatest — тег последнего релиза ядра из GitHub API.
func (s *Sources) FetchLatest(ctx context.Context) (string, error) {
	body, err := s.get(ctx, s.GitHubAPI+"/repos/"+s.ReleasesRepo+"/releases/latest")
	if err != nil {
		return "", err
	}
	var v struct {
		TagName string `json:"tag_name"`
	}
	if err := json.Unmarshal(body, &v); err != nil {
		return "", err
	}
	if !validTag(v.TagName) {
		return "", fmt.Errorf("unexpected tag %q", v.TagName)
	}
	return v.TagName, nil
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
func (a *App) RefreshLatest(ctx context.Context) error {
	tag, err := a.Sources.FetchLatest(ctx)
	if err != nil {
		return err
	}
	a.latest.Store(&tag)
	return nil
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

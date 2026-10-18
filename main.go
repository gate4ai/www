// gate4.ai — сайт: лендинг, установщики, каталог коннекторов.
//
// Один бинарник: HTML, CSS, JS и скрипты установки встроены. Слушает только
// HTTP (по умолчанию 127.0.0.1:8080); TLS завершает Caddy за relay.
// Конфигурация — переменные окружения (см. README.md).
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"
)

// version — версия сборки (-ldflags "-X main.version=…").
var version = "dev"

func env(name string) string { return strings.TrimSpace(os.Getenv(name)) }

func envOr(name, def string) string {
	if v := env(name); v != "" {
		return v
	}
	return def
}

func list(v string) []string {
	var out []string
	for _, s := range strings.Split(v, ",") {
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, s)
		}
	}
	return out
}

func main() {
	if len(os.Args) > 1 && os.Args[1] == "--version" {
		fmt.Println("gate4ai-www", version)
		return
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, nil)))

	app := &App{
		Sources:       NewSources(envOr("GATE4AI_WWW_REGISTRY_URL", DefaultRegistry), list(envOr("GATE4AI_WWW_REGISTRY_KEYS", DefaultRegistryKey)), envOr("GATE4AI_WWW_RELEASES_REPO", DefaultReleasesRepo)),
		WebhookSecret: env("GATE4AI_WWW_WEBHOOK_SECRET"),
		CanonicalHost: env("GATE4AI_WWW_CANONICAL_HOST"),
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go app.Refresh(ctx)

	addr := envOr("GATE4AI_WWW_LISTEN", "127.0.0.1:8080")
	srv := &http.Server{Addr: addr, Handler: app.Handler(), ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout: 30 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 2 * time.Minute}
	go func() {
		<-ctx.Done()
		sctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = srv.Shutdown(sctx)
	}()
	slog.Info("listening on http://" + addr)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		slog.Error("http server", "err", err)
		os.Exit(1)
	}
}

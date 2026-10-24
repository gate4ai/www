# gate4.ai — сайт

Сайт https://gate4.ai: продающий лендинг (en, ru), установщики `install.sh` / `install.ps1`, каталог коннекторов из подписанного [registry](https://github.com/gate4ai/registry). Требования — [REQUIREMENTS.md](REQUIREMENTS.md).

Один бинарник на Go (стандартная библиотека, шаблоны `html/template`): HTML, CSS, JS и скрипты установки встроены в него через `embed`. Формат индекса registry и проверка его подписи — пакет [`sdk-go/registry`](https://github.com/gate4ai/gate4ai/tree/main/sdk-go/registry) модуля `github.com/gate4ai/gate4ai` (обновляется на каждый релиз gate4ai: `dev/scripts/deploy.sh gate4ai-bump`), те же правила, что у `gate4ai registry check`. Страницы рендерятся на сервере и работают без JavaScript; JS (2 КБ) — только «скопировать», вкладки ОС и фильтр каталога. Без трекеров, cookies, сторонних скриптов и шрифтов.

Сайт слушает только HTTP (`127.0.0.1:8080`); TLS и сертификаты — у Caddy за relay (см. «Деплой»).

## Разработка

```sh
go run .                                   # http://127.0.0.1:8080
gofmt -l . && go vet ./... && go test ./...
```

| Путь | Что |
|---|---|
| `main.go` | конфигурация из окружения, HTTP-сервер |
| `app.go` | маршруты, заголовки безопасности, редирект на канонический хост, шаблоны |
| `catalog.go` | карточки каталога; названия и описания официальных коннекторов на обоих языках (таблица `official`); возможности человеческим языком, сгруппированные (каждая — один раз) |
| `sources.go` | индекс registry (5 мин и webhook) и `/latest` (10 мин и по запросу — не чаще раза в минуту, условным запросом с ETag) из GitHub — только в памяти; при ошибке остаётся последнее проверенное |
| `i18n.go` | языки, тексты общих страниц, ОС по User-Agent |
| `templates/` | `index_en.html` и `index_ru.html` — тексты лендинга; остальные страницы общие |
| `static/site.css` | одна тема на токенах (`:root`), тёмная и светлая по `prefers-color-scheme` |
| `install/` | `install.sh` (POSIX sh) и `install.ps1` |
| `deploy/` | `compose.yaml` (служба на сервере) и `deploy.sh` (выкладка из CI) |

Маршруты: `/`, `/ru/…`, `/install`, `/install.sh`, `/install.ps1`, `/connectors`, `/connectors/<имя>`, `/connectors.json`, `/latest`, `/healthz`, `POST /hooks/registry`, `/sitemap.xml`, `/robots.txt`.

## Конфигурация (переменные окружения)

| Переменная | По умолчанию | Смысл |
|---|---|---|
| `GATE4AI_WWW_LISTEN` | `127.0.0.1:8080` | адрес HTTP |
| `GATE4AI_WWW_CANONICAL_HOST` | — | остальные имена получают 308 на этот хост (кроме `/healthz`) |
| `GATE4AI_WWW_WEBHOOK_SECRET` | — | включает `POST /hooks/registry` с `Authorization: Bearer <секрет>` |
| `GATE4AI_WWW_REGISTRY_URL`, `GATE4AI_WWW_REGISTRY_KEYS` | registry gate4ai и его ключ | источник каталога |
| `GATE4AI_WWW_RELEASES_REPO` | `gate4ai/gate4ai` | откуда брать `/latest` |

Каталога состояния нет: индекс и версия живут в памяти и после перезапуска загружаются заново (до загрузки `/connectors` пишет, что каталог загружается, `/latest` отвечает 503).

Параметры установщиков для пользователя (описаны в заголовках скриптов и на `/install` под «Для администраторов»): `GATE4AI_VERSION`, `GATE4AI_INSTALL_DIR`, `GATE4AI_NO_TRAY`, `GATE4AI_NO_START` (не включать автозапуск — по умолчанию установщик вызывает `gate4ai service install`), `GATE4AI_NO_OPEN` (не открывать мастер `gate4ai ui` в браузере после установки — по умолчанию открывается в графической сессии и вне CI), `GATE4AI_INSTALL_DEPS` (поставить Chrome, а на Linux без графики — Xvfb и шрифты без вопроса; по умолчанию `gate4ai browser setup` спрашивает в терминале), `GATE4AI_NO_BROWSER` (без браузера агентов). Для тестов установщики понимают `GATE4AI_SITE` (откуда брать `/latest`) и `GATE4AI_DOWNLOAD_BASE` (зеркало релизов).

## CI и деплой

- `ci.yml`: `gofmt`, `go vet`, `go test`, сборка, бюджет страницы < 100 КБ, `shellcheck`, `PSScriptAnalyzer`; на `main` — образ `ghcr.io/gate4ai/www:<sha>` и деплой.
- `install.yml` (при изменениях в `install/`): настоящая установка последнего релиза на Linux amd64, macOS arm64, Windows (PowerShell 5.1 и 7), откат на GitHub API, повторный запуск, отказ ставить подменённый архив.

Деплой — на сервер relay (`root@136.148.221.24`), Compose-проект `gate4ai-www` в `/opt/gate4ai-www` с сетью хоста, как стек наблюдаемости ([relay/docs/observability.md](https://github.com/gate4ai/gate4ai/blob/main/relay/docs/observability.md#сайт-gate4ai)). CI копирует [deploy/compose.yaml](deploy/compose.yaml), входит в ghcr.io токеном задачи и запускает на сервере [deploy/deploy.sh](deploy/deploy.sh) `up <sha>`: сначала `docker compose pull` нового образа (сбой — `.env` не меняется), затем тег пишется в `.env` (`WWW_TAG`), прежний — в `.env.prev`, и `docker compose up -d`. Если `http://127.0.0.1:8080/healthz` на сервере или затем `https://gate4.ai/healthz` не отвечает, возвращается прежний тег (`deploy.sh rollback`). Секрет webhook — в `/opt/gate4ai-www/www.env` (`GATE4AI_WWW_WEBHOOK_SECRET=…`), CI его не трогает.

Снаружи: relay пробрасывает `gate4.ai` и `www.gate4.ai` по SNI без расшифровки на Caddy (`--route gate4.ai=127.0.0.1:8444 --route www.gate4.ai=127.0.0.1:8444`), Caddy получает сертификаты Let's Encrypt (TLS-ALPN-01), сжимает ответы и проксирует на `127.0.0.1:8080`; `www.gate4.ai` сайт перенаправляет на `gate4.ai`.

Нужно в настройках репозитория: переменная `DEPLOY_HOST`, секреты `DEPLOY_SSH_KEY` (приватный ключ, его публичная часть — в `/root/.ssh/authorized_keys` сервера) и `DEPLOY_KNOWN_HOSTS` (`ssh-keyscan <host>`). Без `DEPLOY_HOST` деплой пропускается.

Локально так же, как на сервере: `docker build -t ghcr.io/gate4ai/www:dev . && WWW_TAG=dev docker compose -f deploy/compose.yaml up -d`.

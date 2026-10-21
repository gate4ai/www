// Языки, тексты общих страниц и определение ОС посетителя.

package main

import (
	"strconv"
	"strings"

	"github.com/gate4ai/gate4ai/sdk-go/registry"
)

// Lang — язык страницы.
type Lang string

const (
	En Lang = "en"
	Ru Lang = "ru"
)

// Prefix — префикс путей: "" для английского, "/ru" для русского.
func (l Lang) Prefix() string {
	if l == Ru {
		return "/ru"
	}
	return ""
}

func (l Lang) Code() string { return string(l) }

func (l Lang) Tr() *Tr {
	if l == Ru {
		return &RuTr
	}
	return &EnTr
}

// prefersRu — предпочитает ли посетитель русский: первый язык из
// Accept-Language с наибольшим весом.
func prefersRu(acceptLanguage string) bool {
	best, bestQ := "", -1.0
	for _, part := range strings.Split(acceptLanguage, ",") {
		params := strings.Split(strings.TrimSpace(part), ";")
		tag := strings.ToLower(strings.TrimSpace(params[0]))
		if tag == "" {
			continue
		}
		q := 1.0
		for _, p := range params[1:] {
			if v, ok := strings.CutPrefix(strings.TrimSpace(p), "q="); ok {
				if f, err := strconv.ParseFloat(v, 32); err == nil {
					q = f
					break
				}
			}
		}
		if q > bestQ {
			best, bestQ = tag, q
		}
	}
	return best == "ru" || strings.HasPrefix(best, "ru-")
}

// OS — для какой ОС показывать команду установки.
type OS int

const (
	Unix OS = iota
	Windows
)

func osFromUserAgent(ua string) OS {
	if strings.Contains(ua, "Windows") {
		return Windows
	}
	return Unix
}

func osFromQuery(v string) (OS, bool) {
	switch v {
	case "windows":
		return Windows, true
	case "macos", "linux", "unix":
		return Unix, true
	}
	return Unix, false
}

func (o OS) IsWindows() bool { return o == Windows }

// Tr — тексты общих частей сайта. Тексты главной — в шаблонах index_<язык>.html.
type Tr struct {
	NavHow               string
	NavAgents            string
	NavConnectors        string
	NavDocs              string
	NavInstall           string
	OtherLangName        string
	Skip                 string
	Copy                 string
	Copied               string
	OsUnix               string
	OsWindows            string
	FooterTagline        string
	FooterProduct        string
	FooterDocs           string
	FooterSpec           string
	FooterSecurity       string
	FooterPrivacy        string
	InstallTitle         string
	InstallLead          string
	InstallWhat          string
	InstallWhatItems     []string
	InstallVars          string
	InstallManual        string
	InstallManualText    string
	InstallSource        string
	InstallNext          string
	InstallNextText      string
	InstallLatest        string
	CatalogTitle         string
	CatalogLead          string
	CatalogSearch        string
	CatalogSearchButton  string
	CatalogAll           string
	CatalogEmpty         string
	CatalogUnavailable   string
	CatalogCount         string
	CatalogJSON          string
	TrustVerified        string
	TrustPublisher       string
	TrustCommunity       string
	TrustVerifiedText    string
	TrustPublisherText   string
	TrustCommunityText   string
	ConnectorPublisher   string
	ConnectorTrust       string
	ConnectorLicense     string
	ConnectorSource      string
	ConnectorCaps        string
	ConnectorCapsLead    string
	ConnectorInstall     string
	ConnectorInstallLead string
	ConnectorNative      string
	ConnectorVersions    string
	ConnectorRevoked     string
	ConnectorLatest      string
	ConnectorBack        string
	NotFoundTitle        string
	NotFoundText         string
	NotFoundHome         string
}

func (t *Tr) TrustName(level string) string {
	switch level {
	case registry.TrustVerified:
		return t.TrustVerified
	case registry.TrustPublisher:
		return t.TrustPublisher
	}
	return t.TrustCommunity
}

func (t *Tr) TrustText(level string) string {
	switch level {
	case registry.TrustVerified:
		return t.TrustVerifiedText
	case registry.TrustPublisher:
		return t.TrustPublisherText
	}
	return t.TrustCommunityText
}

var EnTr = Tr{
	NavHow:         "How it works",
	NavAgents:      "Agents",
	NavConnectors:  "Connectors",
	NavDocs:        "Docs",
	NavInstall:     "Install",
	OtherLangName:  "Русский",
	Skip:           "Skip to content",
	Copy:           "Copy",
	Copied:         "Copied",
	OsUnix:         "macOS / Linux",
	OsWindows:      "Windows",
	FooterTagline:  "A guard for your computer. AI agents get only what you allow.",
	FooterProduct:  "Product",
	FooterDocs:     "Documentation",
	FooterSpec:     "Specification",
	FooterSecurity: "Security",
	FooterPrivacy:  "No trackers, no cookies, no third-party scripts on this site.",
	InstallTitle:   "Install gate4.ai",
	InstallLead:    "One command. No admin rights, no account, nothing leaves your computer.",
	InstallWhat:    "What the installer does",
	InstallWhatItems: []string{
		"Detects your OS and CPU and downloads the latest release from GitHub.",
		"Checks sha256 checksums; with cosign installed, also checks the release signature. A tampered archive is not installed.",
		"Puts gate4ai and the gate4ai-tray icon app into your user folder; no sudo.",
		"Running it again updates gate4ai. Your rules, secrets and journal stay untouched.",
	},
	InstallVars:          "Options",
	InstallManual:        "Manual install",
	InstallManualText:    "Download an archive for your platform from GitHub Releases. Checksums are signed with cosign.",
	InstallSource:        "Build from source (Go 1.27+):",
	InstallNext:          "Next: first run",
	InstallNextText:      "Start the daemon, install connectors and connect your agent. The README walks you through it in five steps.",
	InstallLatest:        "Latest release",
	CatalogTitle:         "Connectors",
	CatalogLead:          "Email, files, browser: each connector declares exactly what it may touch. You see it before you install, and gate4ai enforces it.",
	CatalogSearch:        "Search connectors",
	CatalogSearchButton:  "Search",
	CatalogAll:           "All",
	CatalogEmpty:         "Nothing found. Try another word or clear the filter.",
	CatalogUnavailable:   "The catalog is loading from the signed registry. Refresh in a minute.",
	CatalogCount:         "connectors from the signed registry",
	CatalogJSON:          "Index as JSON",
	TrustVerified:        "Verified",
	TrustPublisher:       "Publisher",
	TrustCommunity:       "Community",
	TrustVerifiedText:    "Code and capabilities reviewed by gate4.ai.",
	TrustPublisherText:   "Published by a verified publisher.",
	TrustCommunityText:   "From the community: gate4ai warns you on install and allows only read actions.",
	ConnectorPublisher:   "Publisher",
	ConnectorTrust:       "Trust",
	ConnectorLicense:     "License",
	ConnectorSource:      "Source code",
	ConnectorCaps:        "What it can touch",
	ConnectorCapsLead:    "Everything else is blocked by gate4ai. Your rules can narrow this further, never widen it.",
	ConnectorInstall:     "Install",
	ConnectorInstallLead: "gate4ai shows these capabilities and asks for your consent before installing.",
	ConnectorNative:      "A native package: a separate program without a sandbox, with your user's rights. The store accepts native packages only from gate4.ai.",
	ConnectorVersions:    "Versions",
	ConnectorRevoked:     "Revoked",
	ConnectorLatest:      "latest",
	ConnectorBack:        "All connectors",
	NotFoundTitle:        "Page not found",
	NotFoundText:         "This page does not exist. The guard is still on duty.",
	NotFoundHome:         "Go to the home page",
}

var RuTr = Tr{
	NavHow:         "Как работает",
	NavAgents:      "Агенты",
	NavConnectors:  "Коннекторы",
	NavDocs:        "Документация",
	NavInstall:     "Установить",
	OtherLangName:  "English",
	Skip:           "К содержанию",
	Copy:           "Копировать",
	Copied:         "Скопировано",
	OsUnix:         "macOS / Linux",
	OsWindows:      "Windows",
	FooterTagline:  "Страж вашего компьютера. AI-агенты получают только то, что вы разрешили.",
	FooterProduct:  "Продукт",
	FooterDocs:     "Документация",
	FooterSpec:     "Спецификация",
	FooterSecurity: "Безопасность",
	FooterPrivacy:  "На сайте нет трекеров, cookies и сторонних скриптов.",
	InstallTitle:   "Установка gate4.ai",
	InstallLead:    "Одна команда. Без прав администратора и без регистрации, ничего не уходит с вашего компьютера.",
	InstallWhat:    "Что делает установщик",
	InstallWhatItems: []string{
		"Определяет ОС и процессор и скачивает последний релиз с GitHub.",
		"Сверяет контрольные суммы sha256, а если установлен cosign, проверяет и подпись релиза. Подменённый архив не ставится.",
		"Кладёт gate4ai и значок в трее gate4ai-tray в ваш пользовательский каталог, без sudo.",
		"Повторный запуск обновляет gate4ai. Правила, секреты и журнал не трогает.",
	},
	InstallVars:          "Параметры",
	InstallManual:        "Установка вручную",
	InstallManualText:    "Скачайте архив для своей платформы с GitHub Releases. Контрольные суммы подписаны cosign.",
	InstallSource:        "Из исходников (Go 1.27+):",
	InstallNext:          "Дальше: первый запуск",
	InstallNextText:      "Запустите daemon, поставьте коннекторы и подключите агента. README проводит через это за пять шагов.",
	InstallLatest:        "Последний релиз",
	CatalogTitle:         "Коннекторы",
	CatalogLead:          "Почта, файлы, браузер: каждый коннектор заранее объявляет, к чему он может прикоснуться. Вы видите это до установки, а gate4ai следит, чтобы он не вышел за эти рамки.",
	CatalogSearch:        "Поиск коннекторов",
	CatalogSearchButton:  "Найти",
	CatalogAll:           "Все",
	CatalogEmpty:         "Ничего не нашлось. Попробуйте другое слово или сбросьте фильтр.",
	CatalogUnavailable:   "Каталог загружается из подписанного registry. Обновите страницу через минуту.",
	CatalogCount:         "коннекторов из подписанного registry",
	CatalogJSON:          "Индекс в JSON",
	TrustVerified:        "Проверен",
	TrustPublisher:       "Издатель",
	TrustCommunity:       "Сообщество",
	TrustVerifiedText:    "Код и возможности проверены gate4.ai.",
	TrustPublisherText:   "Опубликован проверенным издателем.",
	TrustCommunityText:   "От сообщества: gate4ai предупреждает при установке и разрешает только чтение.",
	ConnectorPublisher:   "Издатель",
	ConnectorTrust:       "Доверие",
	ConnectorLicense:     "Лицензия",
	ConnectorSource:      "Исходный код",
	ConnectorCaps:        "К чему имеет доступ",
	ConnectorCapsLead:    "Всё остальное gate4ai блокирует. Ваши правила могут сузить этот список, но не расширить.",
	ConnectorInstall:     "Установка",
	ConnectorInstallLead: "Перед установкой gate4ai покажет эти возможности и спросит согласие.",
	ConnectorNative:      "Нативный пакет: отдельная программа без песочницы, с правами вашего пользователя. Магазин принимает нативные пакеты только от gate4.ai.",
	ConnectorVersions:    "Версии",
	ConnectorRevoked:     "Отозвана",
	ConnectorLatest:      "последняя",
	ConnectorBack:        "Все коннекторы",
	NotFoundTitle:        "Страница не найдена",
	NotFoundText:         "Такой страницы нет. Страж на посту.",
	NotFoundHome:         "На главную",
}

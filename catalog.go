// Каталог коннекторов: карточки из проверенного индекса registry и
// возможности человеческим языком. Формат индекса и подпись — sdk-go/registry.

package main

import (
	"slices"
	"sort"
	"strings"

	"github.com/gate4ai/gate4ai/sdk-go/registry"
)

// Card — коннектор в каталоге.
type Card struct {
	Name         string
	Title        string // человеческое название: из таблицы official, иначе Name
	Description  string // на языке страницы, если есть перевод; иначе из registry
	Publisher    string
	Trust        string
	Version      string       // последняя неотозванная; "" — нет
	Summary      []string     // возможности человеческим языком, сгруппированные: каждая — один раз
	Capabilities []Capability // все строки capabilities последней версии
	Native       bool         // нативный пакет (runtime: native): запись ссылается на список платформ
}

// officialPublisher — издатель официальных коннекторов gate4.ai.
const officialPublisher = "gate4ai"

// officialText — название и описание официального коннектора на одном языке.
type officialText struct{ Title, Description string }

// official — названия и описания официальных коннекторов на обоих языках:
// в registry у них английское описание и технический id вместо названия.
// Английское описание — запасное: на английской странице показывается описание из registry.
var official = map[string]map[Lang]officialText{
	"browser": {
		En: {"Browser", "Opens and reads only the sites you allow, in a separate Chrome profile"},
		Ru: {"Браузер", "Открывает и читает только разрешённые вами сайты, в отдельном профиле Chrome"},
	},
	"email": {
		En: {"Email", "Reads, searches and sends email over IMAP/SMTP"},
		Ru: {"Почта", "Читает, ищет и отправляет письма по IMAP/SMTP"},
	},
	"files": {
		En: {"Files", "Files in the folders you allow"},
		Ru: {"Файлы", "Файлы в разрешённых вами папках"},
	},
}

// titleAndDescription — название и описание коннектора на языке lang.
func titleAndDescription(name string, e registry.Entry, lang Lang) (string, string) {
	title, desc := name, e.Description
	if e.Publisher != officialPublisher {
		return title, desc
	}
	tr, ok := official[name]
	if !ok {
		return title, desc
	}
	t := tr[lang]
	if t.Title != "" {
		title = t.Title
	}
	if lang != En || desc == "" {
		if t.Description != "" {
			desc = t.Description
		}
	}
	return title, desc
}

// Capability — возможность коннектора: человеческим языком и исходная строка.
type Capability struct {
	Human string
	Raw   string
}

// trust — уровень доверия издателя коннектора (неизвестный — community).
func (c *Catalog) trust(name string) string {
	if t := c.Index.Trust(name); t != "" {
		return t
	}
	return registry.TrustCommunity
}

// Cards — карточки каталога: verified выше, затем по имени; фильтр по тексту и доверию.
func (c *Catalog) Cards(q, trust string, lang Lang) []Card {
	q = strings.ToLower(strings.TrimSpace(q))
	var out []Card
	for name, e := range c.Index.Connectors {
		if q != "" {
			title, desc := titleAndDescription(name, e, lang)
			hay := strings.ToLower(name + "\n" + e.Description + "\n" + title + "\n" + desc)
			if !strings.Contains(hay, q) {
				continue
			}
		}
		if trust != "" && c.trust(name) != trust {
			continue
		}
		out = append(out, c.Card(name, lang))
	}
	sort.Slice(out, func(i, j int) bool {
		ti, tj := slices.Index(registry.TrustLevels, out[i].Trust), slices.Index(registry.TrustLevels, out[j].Trust)
		if ti != tj {
			return ti < tj
		}
		return out[i].Name < out[j].Name
	})
	return out
}

// Card — карточка коннектора name.
func (c *Catalog) Card(name string, lang Lang) Card {
	e := c.Index.Connectors[name]
	title, desc := titleAndDescription(name, e, lang)
	card := Card{Name: name, Title: title, Description: desc, Publisher: e.Publisher, Trust: c.trust(name)}
	if rel, ok := c.Index.Latest(name); ok {
		card.Version = rel.Version
		card.Native = rel.Native()
		for _, raw := range rel.Capabilities {
			card.Capabilities = append(card.Capabilities, humanize(raw, lang))
		}
		card.Summary = summarize(rel.Capabilities, lang)
	}
	return card
}

// summarize — возможности человеческим языком, каждая один раз: все адреса
// сети — одной строкой (tcp и tls вместе), все пароли — одной строкой,
// остальные — без повторов. Порядок — по первому появлению.
func summarize(raws []string, lang Lang) []string {
	ru := lang == Ru
	var (
		out                   []string
		seen                  = map[string]bool{}
		addrs, secrets, hosts []string
		allTLS                = true
		netAt, secretAt       = -1, -1
		secretRaw             string
		secretPairs           int
	)
	add := func(list []string, v string) []string {
		if slices.Contains(list, v) {
			return list
		}
		return append(list, v)
	}
	for _, raw := range raws {
		if rest, ok := strings.CutPrefix(raw, "network "); ok {
			tls, addr := false, rest
			if scheme, a, ok := strings.Cut(rest, "://"); ok {
				tls, addr = scheme == "tls", a
			}
			allTLS = allTLS && tls
			addrs = add(addrs, settings(addr))
			if netAt < 0 {
				netAt = len(out)
				out = append(out, "")
			}
			continue
		}
		if rest, ok := strings.CutPrefix(raw, "secret "); ok {
			if secret, host, ok := strings.Cut(rest, " for "); ok {
				secrets, hosts = add(secrets, settings(secret)), add(hosts, settings(host))
				secretRaw = raw
				secretPairs++
				if secretAt < 0 {
					secretAt = len(out)
					out = append(out, "")
				}
				continue
			}
		}
		h := human(raw, ru)
		if !seen[h] {
			seen[h] = true
			out = append(out, h)
		}
	}
	if netAt >= 0 {
		list := joinList(addrs, ru)
		switch {
		case ru && allTLS:
			out[netAt] = "Соединяется по TLS только с " + list + " из ваших настроек"
		case ru:
			out[netAt] = "Соединяется только с " + list + " из ваших настроек"
		case allTLS:
			out[netAt] = "Connects over TLS only to " + list + " from your settings"
		default:
			out[netAt] = "Connects only to " + list + " from your settings"
		}
	}
	if secretAt >= 0 {
		switch {
		case secretPairs == 1:
			out[secretAt] = human(secretRaw, ru)
		case ru:
			out[secretAt] = "Пароли из " + joinList(secrets, ru) + " подставляет gate4ai, и только при входе на " +
				joinList(hosts, ru) + "; коннектор их не видит"
		default:
			out[secretAt] = "gate4ai fills in the passwords from " + joinList(secrets, ru) + ", only when signing in to " +
				joinList(hosts, ru) + "; the connector never sees them"
		}
	}
	return out
}

// joinList — «a, b and c» / «a, b и c».
func joinList(items []string, ru bool) string {
	and := " and "
	if ru {
		and = " и "
	}
	if len(items) <= 1 {
		return strings.Join(items, "")
	}
	return strings.Join(items[:len(items)-1], ", ") + and + items[len(items)-1]
}

// settings — ${setting:imap.host} → imap.host.
func settings(s string) string {
	const open = "${setting:"
	var out strings.Builder
	for {
		i := strings.Index(s, open)
		if i < 0 {
			break
		}
		out.WriteString(s[:i])
		after := s[i+len(open):]
		j := strings.IndexByte(after, '}')
		if j < 0 {
			out.WriteString(s[i:])
			s = ""
			break
		}
		out.WriteString(after[:j])
		s = after[j+1:]
	}
	out.WriteString(s)
	return out.String()
}

// humanize переводит строку capabilities на человеческий язык; незнакомая — как есть.
func humanize(raw string, lang Lang) Capability {
	return Capability{Human: human(raw, lang == Ru), Raw: raw}
}

func human(raw string, ru bool) string {
	if rest, ok := strings.CutPrefix(raw, "network "); ok {
		tls, addr := false, rest
		if scheme, a, ok := strings.Cut(rest, "://"); ok {
			tls, addr = scheme == "tls", a
		}
		addr = settings(addr)
		switch {
		case ru && tls:
			return "Соединяется по TLS только с " + addr + " из ваших настроек"
		case ru:
			return "Соединяется только с " + addr + " из ваших настроек"
		case tls:
			return "Connects over TLS only to " + addr + " from your settings"
		default:
			return "Connects only to " + addr + " from your settings"
		}
	}
	if rest, ok := strings.CutPrefix(raw, "secret "); ok {
		secret, host, ok := strings.Cut(rest, " for ")
		if !ok {
			return raw
		}
		secret, host = settings(secret), settings(host)
		if ru {
			return "Пароль из " + secret + " подставляет gate4ai, и только при входе на " + host + "; коннектор его не видит"
		}
		return "gate4ai fills in the password from " + secret + ", only when signing in to " + host + "; the connector never sees it"
	}
	if rest, ok := strings.CutPrefix(raw, "files "); ok {
		for strings.HasSuffix(rest, "/**") {
			rest = strings.TrimSuffix(rest, "/**")
		}
		path := settings(rest)
		if ru {
			return "Работает с файлами только в папках из настройки " + path
		}
		return "Works with files only inside folders from your " + path + " setting"
	}
	if rest, ok := strings.CutPrefix(raw, "reveal the value of "); ok {
		if rest, ok := strings.CutSuffix(rest, " to the connector"); ok {
			secret := settings(rest)
			if ru {
				return "Получает значение секрета из " + secret + " (только нативный пакет, с вашего согласия)"
			}
			return "Receives the value of the secret from " + secret + " (native packages only, with your consent)"
		}
	}
	if rest, ok := strings.CutPrefix(raw, "action "); ok {
		if action, calls, ok := strings.Cut(rest, " calls "); ok {
			if ru {
				return "Действие " + action + " вызывает " + calls + "; разрешая " + action + ", вы разрешаете и их"
			}
			return "Action " + action + " runs " + calls + "; allowing " + action + " allows them too"
		}
	}
	if rest, ok := strings.CutPrefix(raw, "call "); ok {
		target, actions, _ := strings.Cut(rest, ": ")
		target, _, _ = strings.Cut(target, " (")
		actions, params, _ := strings.Cut(actions, " with ")
		params = settings(params)
		switch {
		case ru && params == "":
			return "Вызывает действия коннектора " + target + ": " + actions
		case ru:
			return "Вызывает действия коннектора " + target + ": " + actions + " — с параметрами " + params
		case params == "":
			return "Calls actions of the " + target + " connector: " + actions
		default:
			return "Calls actions of the " + target + " connector: " + actions + ", with " + params
		}
	}
	if rest, ok := strings.CutPrefix(raw, "browser on "); ok {
		domains := settings(rest)
		if ru {
			return "Открывает в браузере только сайты из настройки " + domains
		}
		return "Opens only sites from your " + domains + " setting in the browser"
	}
	return raw
}

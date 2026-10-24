// Каталог коннекторов: карточки из проверенного индекса registry и
// возможности человеческим языком. Формат индекса и подпись — sdk-go/registry.
//
// Браузер и Файлы в магазине не лежат: они встроены в gate4ai (Builtins), а
// коннекторы магазина — WASM-модули без сети, которые ходят на сайты только
// через Браузер и к файлам только через Файлы, в границах своего манифеста
// (sites, folders) и списков доступа владельца.

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
	Title        string // человеческое название; пока — id из registry
	Description  string // описание из registry
	Publisher    string
	Trust        string
	Version      string       // последняя неотозванная; "" — нет
	Summary      []string     // возможности человеческим языком, сгруппированные: каждая — один раз
	Capabilities []Capability // все строки capabilities последней версии
}

// Builtin — встроенный коннектор gate4ai: в магазине его нет, ставить и удалять нечего.
type Builtin struct{ Name, Title, Description string }

// Builtins — Браузер и Файлы на языке lang: единственные два выхода gate4ai
// наружу; через них же работают коннекторы магазина.
func Builtins(lang Lang) []Builtin {
	if lang == Ru {
		return []Builtin{
			{"browser", "Браузер", "Отдельный профиль Chrome: только сайты из вашего списка, с режимом для каждого — разрешить, читать, спросить, запретить"},
			{"files", "Файлы", "Только папки из вашего списка: чтение, добавление в конец или запись — по режиму папки"},
		}
	}
	return []Builtin{
		{"browser", "Browser", "A dedicated Chrome profile: only the sites on your list, each with its own mode: allow, read, ask or deny"},
		{"files", "Files", "Only the folders on your list: read, append or write, as the folder's mode says"},
	}
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
			hay := strings.ToLower(name + "\n" + e.Description)
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
	card := Card{Name: name, Title: name, Description: e.Description, Publisher: e.Publisher, Trust: c.trust(name)}
	if rel, ok := c.Index.Latest(name); ok {
		card.Version = rel.Version
		for _, raw := range rel.Capabilities {
			card.Capabilities = append(card.Capabilities, humanize(raw, lang))
		}
		card.Summary = summarize(rel.Capabilities, lang)
	}
	return card
}

// summarize — возможности человеческим языком, каждая один раз: все сайты
// манифеста — одной строкой, все папки — одной строкой, остальные — без
// повторов. Порядок — по первому появлению.
func summarize(raws []string, lang Lang) []string {
	ru := lang == Ru
	var (
		out              []string
		seen             = map[string]bool{}
		sites, folders   []string
		siteAt, folderAt = -1, -1
	)
	add := func(list []string, v string) []string {
		if slices.Contains(list, v) {
			return list
		}
		return append(list, v)
	}
	for _, raw := range raws {
		if rest, ok := strings.CutPrefix(raw, "site "); ok {
			if domain, mode, ok := strings.Cut(rest, ": "); ok {
				sites = add(sites, settings(domain)+" ("+mode+")")
				if siteAt < 0 {
					siteAt = len(out)
					out = append(out, "")
				}
				continue
			}
		}
		if rest, ok := strings.CutPrefix(raw, "folder "); ok {
			if path, mode, ok := strings.Cut(rest, ": "); ok {
				folders = add(folders, settings(path)+" ("+mode+")")
				if folderAt < 0 {
					folderAt = len(out)
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
	if siteAt >= 0 {
		out[siteAt] = sitesText(joinList(sites, ru), ru)
	}
	if folderAt >= 0 {
		out[folderAt] = foldersText(joinList(folders, ru), ru)
	}
	return out
}

func sitesText(list string, ru bool) string {
	if ru {
		return "Ходит на сайты только через ваш Браузер и только на " + list + "; сайт должен быть разрешён и в вашем списке"
	}
	return "Reaches sites only through your Browser and only " + list + "; the site must be on your list too"
}

func foldersText(list string, ru bool) string {
	if ru {
		return "Работает с файлами только через ваши Файлы и только в " + list + "; папка должна быть разрешена и в вашем списке"
	}
	return "Works with files only through your Files and only in " + list + "; the folder must be on your list too"
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

// settings — ${setting:domains} → domains.
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
	if rest, ok := strings.CutPrefix(raw, "site "); ok {
		if domain, mode, ok := strings.Cut(rest, ": "); ok {
			return sitesText(settings(domain)+" ("+mode+")", ru)
		}
	}
	if rest, ok := strings.CutPrefix(raw, "folder "); ok {
		if path, mode, ok := strings.Cut(rest, ": "); ok {
			return foldersText(settings(path)+" ("+mode+")", ru)
		}
	}
	if rest, ok := strings.CutPrefix(raw, "signs in on "); ok {
		sites, _, _ := strings.Cut(rest, " with your account")
		sites = settings(sites)
		if ru {
			return "Входит на " + sites + " под вашей учёткой; пароль подставляет gate4ai, коннектор его не видит"
		}
		return "Signs in on " + sites + " with your account; gate4ai fills in the password, the connector never sees it"
	}
	if rest, ok := strings.CutPrefix(raw, "reveal the value of "); ok {
		if rest, ok := strings.CutSuffix(rest, " to the connector"); ok {
			secret := settings(rest)
			if ru {
				return "Получает значение секрета из " + secret + " (с вашего согласия)"
			}
			return "Receives the value of the secret from " + secret + " (with your consent)"
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
	if rest, ok := strings.CutPrefix(raw, "class "); ok {
		if class, actions, ok := strings.Cut(rest, " actions: "); ok {
			if ru {
				return "Действия класса " + class + " (меняют что-то, а не читают): " + actions + " — под вашими правилами и подтверждениями"
			}
			return "Actions of class " + class + " (they change things, not just read): " + actions + "; your rules and approvals apply"
		}
	}
	if rest, ok := strings.CutPrefix(raw, "storage "); ok {
		if ru {
			return "Хранит свои данные на вашем компьютере: до " + rest
		}
		return "Keeps its own data on your computer: up to " + rest
	}
	return raw
}

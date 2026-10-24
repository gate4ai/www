package main

import (
	"strings"
	"testing"

	"github.com/gate4ai/gate4ai/sdk-go/registry"
)

func revocation(name, version, reason string) registry.Revocation {
	return registry.Revocation{Connector: name, Version: version, Reason: reason}
}

func TestHumanizeCapabilities(t *testing.T) {
	cases := []struct {
		raw  string
		lang Lang
		want string
	}{
		{"site ${setting:domains}: allow", En, "Reaches sites only through your Browser and only domains (allow); the site must be on your list too"},
		{"site lk.example.org: read", Ru, "Ходит на сайты только через ваш Браузер и только на lk.example.org (read); сайт должен быть разрешён и в вашем списке"},
		{"folder ${setting:folder}: append", Ru, "Работает с файлами только через ваши Файлы и только в folder (append); папка должна быть разрешена и в вашем списке"},
		{"folder ~/Downloads: read", En, "Works with files only through your Files and only in ~/Downloads (read); the folder must be on your list too"},
		{"signs in on ${setting:login_site} with your account (slot site)", En,
			"Signs in on login_site with your account; gate4ai fills in the password, the connector never sees it"},
		{"action pay_bill calls browser.open, browser.act", Ru,
			"Действие pay_bill вызывает browser.open, browser.act; разрешая pay_bill, вы разрешаете и их"},
		{`call browser (contract 1): open, act with {"domains":["${setting:sites}"]}`, En,
			`Calls actions of the browser connector: open, act, with {"domains":["sites"]}`},
		{"call browser: open", Ru, "Вызывает действия коннектора browser: open"},
		{"class money actions: pay_bill", En, "Actions of class money (they change things, not just read): pay_bill; your rules and approvals apply"},
		{"storage 5 MB", Ru, "Хранит свои данные на вашем компьютере: до 5 MB"},
		{"site broken", En, "site broken"},
		{"${setting:unclosed", En, "${setting:unclosed"},
		{"strange", En, "strange"},
	}
	for _, c := range cases {
		got := humanize(c.raw, c.lang)
		if got.Human != c.want || got.Raw != c.raw {
			t.Errorf("humanize(%q) = %q, want %q", c.raw, got.Human, c.want)
		}
	}
	h := humanize("reveal the value of ${setting:model_key} to the connector", Ru).Human
	if !strings.Contains(h, "model_key") || strings.Contains(h, "натив") {
		t.Error(h)
	}
}

func TestCardsFilterAndRevocations(t *testing.T) {
	c := testCatalog(t)
	if n := len(c.Cards("receipt", "", En)); n != 1 {
		t.Errorf("receipt: %d", n)
	}
	if n := len(c.Cards("", "community", En)); n != 0 {
		t.Errorf("community: %d", n)
	}
	if n := len(c.Cards("", "publisher", En)); n != 1 {
		t.Errorf("publisher: %d", n)
	}
	cards := c.Cards("", "", En)
	if len(cards) != 3 || cards[0].Name != "pagereader" || cards[1].Name != "receipts" || cards[2].Name != "utilities" {
		t.Errorf("order (verified first, then by name): %v", cards)
	}
	c.Index.Revoked = append(c.Index.Revoked, revocation("receipts", "3", "test"))
	if v := c.Card("receipts", En).Version; v != "2" {
		t.Errorf("latest after revocation: %s", v)
	}
	c.Index.Revoked = append(c.Index.Revoked, revocation("receipts", "*", "all"))
	if card := c.Card("receipts", En); card.Version != "" || len(card.Capabilities) != 0 {
		t.Errorf("all revoked: %v", card)
	}
}

// Издатель, которого нет в индексе, — community; карточка — id и описание из registry.
func TestUnknownPublisherAndTitles(t *testing.T) {
	ix := &registry.Index{Version: 1, Connectors: map[string]registry.Entry{
		"hello": {Publisher: "nobody", Description: "Hello", Versions: []registry.Release{{Version: "6", URL: "https://x/hello-6.g4c"}}},
	}}
	c := &Catalog{Index: ix}
	card := c.Card("hello", Ru)
	if card.Trust != registry.TrustCommunity {
		t.Fatal("unknown publisher is community")
	}
	if card.Title != "hello" || card.Description != "Hello" || card.Version != "6" {
		t.Fatalf("card: %+v", card)
	}
}

// Браузер и Файлы встроены: не из магазина, на обоих языках.
func TestBuiltins(t *testing.T) {
	for _, lang := range []Lang{En, Ru} {
		b := Builtins(lang)
		if len(b) != 2 || b[0].Name != "browser" || b[1].Name != "files" || b[0].Title == "" || b[1].Description == "" {
			t.Errorf("%v: %v", lang, b)
		}
	}
	if Builtins(Ru)[0].Title != "Браузер" || Builtins(En)[1].Title != "Files" {
		t.Error("titles")
	}
}

func TestSummarizeCapabilities(t *testing.T) {
	receipts := []string{
		"call browser (contract 1): download, open",
		"site lk.example.org: read", "site *.pay.example: read",
		"folder ${setting:folder}: append",
		"action save calls browser.open, browser.download",
		"storage 5 MB",
	}
	got := summarize(receipts, En)
	want := []string{
		"Calls actions of the browser connector: download, open",
		"Reaches sites only through your Browser and only lk.example.org (read) and *.pay.example (read); the site must be on your list too",
		"Works with files only through your Files and only in folder (append); the folder must be on your list too",
		"Action save runs browser.open, browser.download; allowing save allows them too",
		"Keeps its own data on your computer: up to 5 MB",
	}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("receipts en: %q", got)
	}
	got = summarize(receipts, Ru)
	if len(got) != 5 || !strings.HasPrefix(got[1], "Ходит на сайты только через ваш Браузер и только на lk.example.org (read) и *.pay.example (read)") {
		t.Errorf("receipts ru: %q", got)
	}
	// Повторы схлопываются; порядок — по первому появлению.
	got = summarize([]string{"site a: allow", "storage 1 MB", "site a: allow", "folder ~/x: read", "folder ~/x: read"}, En)
	want = []string{"Reaches sites only through your Browser and only a (allow); the site must be on your list too",
		"Keeps its own data on your computer: up to 1 MB",
		"Works with files only through your Files and only in ~/x (read); the folder must be on your list too"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("mixed: %q", got)
	}
	if got := summarize(nil, En); len(got) != 0 {
		t.Error(got)
	}
}

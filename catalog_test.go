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
		{"network tls://${setting:imap.host}:${setting:imap.port}", En, "Connects over TLS only to imap.host:imap.port from your settings"},
		{"network ${setting:api}", Ru, "Соединяется только с api из ваших настроек"},
		{"files ${setting:roots}/**", Ru, "Работает с файлами только в папках из настройки roots"},
		{"secret ${setting:imap.password} for ${setting:imap.host}", En,
			"gate4ai fills in the password from imap.password, only when signing in to imap.host; the connector never sees it"},
		{"secret broken", En, "secret broken"},
		{"action pay_bill calls browser.open, browser.act", Ru,
			"Действие pay_bill вызывает browser.open, browser.act; разрешая pay_bill, вы разрешаете и их"},
		{`call browser (contract 1): open, act with {"domains":["${setting:sites}"]}`, En,
			`Calls actions of the browser connector: open, act, with {"domains":["sites"]}`},
		{"call browser: open", Ru, "Вызывает действия коннектора browser: open"},
		{"browser on ${setting:sites}", En, "Opens only sites from your sites setting in the browser"},
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
	if !strings.Contains(h, "model_key") || !strings.Contains(h, "нативный") {
		t.Error(h)
	}
}

func TestCardsFilterAndRevocations(t *testing.T) {
	c := testCatalog(t)
	if n := len(c.Cards("mail", "", En)); n != 1 {
		t.Errorf("mail: %d", n)
	}
	if n := len(c.Cards("", "community", En)); n != 0 {
		t.Errorf("community: %d", n)
	}
	cards := c.Cards("", "", En)
	if len(cards) != 3 || cards[0].Name != "browser" || cards[2].Name != "files" {
		t.Errorf("order: %v", cards)
	}
	c.Index.Revoked = append(c.Index.Revoked, revocation("files", "3", "test"))
	if v := c.Card("files", En).Version; v != "2" {
		t.Errorf("latest after revocation: %s", v)
	}
	c.Index.Revoked = append(c.Index.Revoked, revocation("files", "*", "all"))
	if card := c.Card("files", En); card.Version != "" || len(card.Capabilities) != 0 {
		t.Errorf("all revoked: %v", card)
	}
}

func TestNativeCards(t *testing.T) {
	rel := func(url string) registry.Release { return registry.Release{Version: "6", URL: url} }
	ix := &registry.Index{Version: 1, Connectors: map[string]registry.Entry{
		"browser": {Publisher: "gate4ai", Versions: []registry.Release{rel("https://x/browser-6.platforms.json")}},
		"email":   {Publisher: "gate4ai", Versions: []registry.Release{rel("https://x/email-6.g4c")}},
	}}
	c := &Catalog{Index: ix}
	if !c.Card("browser", En).Native || c.Card("email", En).Native {
		t.Fatal("native")
	}
	if c.Card("email", En).Trust != registry.TrustCommunity {
		t.Fatal("unknown publisher is community")
	}
}

package main

import "testing"

func TestAcceptLanguage(t *testing.T) {
	for al, want := range map[string]bool{
		"ru-RU,ru;q=0.9,en;q=0.8": true,
		"en;q=0.5, ru":            true,
		"en-US,en;q=0.9,ru;q=0.8": false,
		"":                        false,
		"rus":                     false,
		"RU":                      true,
		"de;q=0.1,ru;q=bad":       true,
	} {
		if got := prefersRu(al); got != want {
			t.Errorf("prefersRu(%q) = %v", al, got)
		}
	}
}

func TestUserAgent(t *testing.T) {
	win := "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36"
	mac := "Mozilla/5.0 (Macintosh; Intel Mac OS X 14_5) AppleWebKit/605.1.15"
	if osFromUserAgent(win) != Windows || osFromUserAgent(mac) != Unix || osFromUserAgent("curl/8.5.0") != Unix {
		t.Fatal("user agent")
	}
	if o, ok := osFromQuery("windows"); !ok || o != Windows {
		t.Fatal("windows")
	}
	if o, ok := osFromQuery("macos"); !ok || o != Unix {
		t.Fatal("macos")
	}
	if _, ok := osFromQuery("x"); ok {
		t.Fatal("x")
	}
}

func TestValidTag(t *testing.T) {
	for tag, want := range map[string]bool{"v0.1.1": true, "v1.0.0-rc.1": true, "0.1.1": false, "v": false, "v1;rm -rf": false, "vx": false} {
		if validTag(tag) != want {
			t.Errorf("validTag(%q)", tag)
		}
	}
}

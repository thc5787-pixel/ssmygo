package i18n

import "testing"

func TestT(t *testing.T) {
	defer SetLocale("en")
	SetLocale("zh-CN")
	if got := T("Quit"); got != "退出" {
		t.Fatalf("zh Quit = %q", got)
	}
	if got := T("Move Up"); got != "上移" { // "(&U)" mnemonic stripped
		t.Fatalf("zh Move Up = %q", got)
	}
	SetLocale("zh-Hans-CN")
	if T("Group") != "分组" {
		t.Fatal("zh-Hans-CN not mapped to zh-Hans")
	}
	SetLocale("de-DE")
	if got := T("Quit"); got != "Quit" {
		t.Fatalf("fallback = %q", got)
	}
	if got := T("no.such.key"); got != "no.such.key" {
		t.Fatalf("unknown key = %q", got)
	}
}

func TestAllKeysTranslated(t *testing.T) {
	en, zh := table("en"), table("zh-Hans")
	if len(en) < 30 {
		t.Fatalf("only %d en strings", len(en))
	}
	for k := range en {
		if zh[k] == "" {
			t.Errorf("zh-Hans missing %q", k)
		}
	}
	for k, v := range en {
		for _, r := range v + k {
			if r == '&' {
				t.Errorf("mnemonic marker left in %q=%q", k, v)
			}
		}
	}
}

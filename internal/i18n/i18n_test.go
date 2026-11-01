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

// A1: Forward Proxy window strings come from the original WPF resx files.
func TestResxStrings(t *testing.T) {
	defer SetLocale("en")
	SetLocale("zh-CN")
	for key, want := range map[string]string{
		"resx.NoProxy":             "不使用代理",
		"resx.Timeout":             "超时 (秒)",
		"resx.ForwardProxy":        "前置代理",
		"resx.CredentialsOptional": "认证 (可选)",
		"SOCKS5":                   "SOCKS5",
		"Save":                     "保存",
		"ssmygo.trustRun":          "运行",
	} {
		if got := T(key); got != want {
			t.Errorf("zh %s = %q, want %q", key, got, want)
		}
	}
	if T("Forward Proxy...") == "Forward Proxy..." {
		t.Error("Forward Proxy... untranslated in zh")
	}
	SetLocale("en")
	if T("resx.Timeout") != "Timeout (sec)" || T("resx.NoProxy") != "No proxy" {
		t.Errorf("en %q %q", T("resx.Timeout"), T("resx.NoProxy"))
	}
}

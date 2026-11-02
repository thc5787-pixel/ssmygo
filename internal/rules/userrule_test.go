package rules

import (
	"reflect"
	"testing"
)

func TestParseUserRules(t *testing.T) {
	for _, tc := range []struct {
		line            string
		direct, proxied string // "" = none
		skipped         bool
	}{
		{line: "! comment"},
		{line: "[Adblock Plus 2.0]"},
		{line: ""},
		{line: "   "},
		{line: "||example.com^", proxied: "example.com"},
		{line: "||Example.COM", proxied: "example.com"},
		{line: "  ||spaces.com^  ", proxied: "spaces.com"},
		{line: "||slash.com/", proxied: "slash.com"},
		{line: "@@||example.org^", direct: "example.org"},
		{line: "@@||example.org", direct: "example.org"},
		{line: "|http://plain.com/some/path", proxied: "plain.com"},
		{line: "|https://x.com/path", proxied: "x.com"},
		{line: "|https://port.com:8443/", proxied: "port.com"},
		{line: "@@|https://y.com/", direct: "y.com"},
		{line: "@@|http://z.com", direct: "z.com"},
		{line: "bare.example.com", proxied: "bare.example.com"},
		{line: "bare.example.com^", proxied: "bare.example.com"},
		{line: "@@bare.example.net", direct: "bare.example.net"},
		{line: "例子.测试", proxied: "xn--fsqu00a.xn--0zwm56d"},
		{line: "||a.com^$third-party", skipped: true},
		{line: "@@||a.com^$document", skipped: true},
		{line: "/^https?:\\/\\/[^\\/]+example\\.com/", skipped: true},
		{line: "||a.com/path", skipped: true},
		{line: "||*.a.com^", skipped: true},
		{line: "*.a.com", skipped: true},
		{line: "a.com/ads/*", skipped: true},
		{line: "|ftp://a.com/", skipped: true},
		{line: "|https://*.a.com/", skipped: true},
		{line: "keyword", skipped: true},
		{line: "||localhost^", proxied: "localhost"},
		{line: "@@|http://intranet/", direct: "intranet"},
		{line: "||", skipped: true},
		{line: "@@", skipped: true},
		{line: "||bad_host.com^", skipped: true},
		{line: "||-bad.com^", skipped: true},
	} {
		direct, proxied, skipped := ParseUserRules(tc.line)
		want := func(v string) []Domain {
			if v == "" {
				return nil
			}
			return []Domain{{Type: RootDomain, Value: v}}
		}
		if !reflect.DeepEqual(direct, want(tc.direct)) || !reflect.DeepEqual(proxied, want(tc.proxied)) {
			t.Errorf("%q: direct %v proxied %v, want %q %q", tc.line, direct, proxied, tc.direct, tc.proxied)
		}
		if got := len(skipped) == 1; got != tc.skipped || (got && skipped[0] != 1) {
			t.Errorf("%q: skipped %v, want %v", tc.line, skipped, tc.skipped)
		}
	}
}

var bom = string([]byte{0xef, 0xbb, 0xbf})

func TestParseUserRulesFile(t *testing.T) {
	text := bom + "! Put user rules line by line in this file.\r\n" +
		"||google.com^\r\n" +
		"/regex/\r\n" +
		"@@||baidu.com^\r\n" +
		"\r\n" +
		"||a.com^$third-party\r\n" +
		"||google.com^\r\n" + // duplicate: once
		"last.com" // no final newline
	direct, proxied, skipped := ParseUserRules(text)
	if !reflect.DeepEqual(direct, []Domain{{Type: RootDomain, Value: "baidu.com"}}) {
		t.Errorf("direct %v", direct)
	}
	if !reflect.DeepEqual(proxied, []Domain{{Type: RootDomain, Value: "google.com"}, {Type: RootDomain, Value: "last.com"}}) {
		t.Errorf("proxied %v", proxied)
	}
	if !reflect.DeepEqual(skipped, []int{3, 6}) {
		t.Errorf("skipped %v, want [3 6]", skipped)
	}
	// A BOM before a rule on line 1 does not break the rule.
	if _, p, _ := ParseUserRules(bom + "||bom.com^"); !reflect.DeepEqual(p, []Domain{{Type: RootDomain, Value: "bom.com"}}) {
		t.Errorf("BOM line: %v", p)
	}
}

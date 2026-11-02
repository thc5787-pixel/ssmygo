package rules

import (
	"strings"

	"golang.org/x/net/idna"
)

// ParseUserRules parses user-rule.txt, a subset of Adblock Plus filters
// (spec P1-D §3): "||host^", "|http(s)://host/…", a bare "host", each with
// an optional "@@" prefix for direct. Every rule matches the host and its
// subdomains. Comments ("!…", "[…]") and blank lines are ignored; other
// rules (regexes, paths, wildcards, "$" options) are skipped and their
// 1-based line numbers returned. Duplicates are dropped.
func ParseUserRules(text string) (direct, proxied []Domain, skipped []int) {
	text = strings.TrimPrefix(text, string(rune(0xFEFF))) // BOM
	seen := map[[2]string]bool{}
	for i, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line) // also drops "\r"
		if line == "" || line[0] == '!' || line[0] == '[' {
			continue
		}
		isDirect := strings.HasPrefix(line, "@@")
		host, ok := ruleHost(strings.TrimPrefix(line, "@@"))
		if !ok {
			skipped = append(skipped, i+1)
			continue
		}
		kind := "proxied"
		if isDirect {
			kind = "direct"
		}
		if seen[[2]string{kind, host}] {
			continue
		}
		seen[[2]string{kind, host}] = true
		d := Domain{Type: RootDomain, Value: host}
		if isDirect {
			direct = append(direct, d)
		} else {
			proxied = append(proxied, d)
		}
	}
	return direct, proxied, skipped
}

// ruleHost returns the host a supported rule (without "@@") names.
func ruleHost(r string) (string, bool) {
	if strings.Contains(r, "$") {
		return "", false // options change what the rule matches
	}
	switch {
	case strings.HasPrefix(r, "||"):
		r = strings.TrimPrefix(r, "||")
		r = trimOne(r, "^", "/")
	case strings.HasPrefix(r, "|"):
		rest, ok := strings.CutPrefix(r[1:], "http://")
		if !ok {
			if rest, ok = strings.CutPrefix(r[1:], "https://"); !ok {
				return "", false
			}
		}
		// Up to the path: the rest of the URL does not matter here.
		if i := strings.IndexAny(rest, "/^?#"); i >= 0 {
			rest = rest[:i]
		}
		if i := strings.LastIndexByte(rest, ':'); i >= 0 {
			rest = rest[:i] // port
		}
		r = rest
	default:
		r = trimOne(r, "^", "/")
	}
	return validHost(r)
}

// trimOne removes one of the suffixes, if present.
func trimOne(s string, suffixes ...string) string {
	for _, x := range suffixes {
		if t, ok := strings.CutSuffix(s, x); ok {
			return t
		}
	}
	return s
}

// validHost lowercases h (IDNs become punycode, which is what clients send)
// and accepts only names of at least two labels made of letters, digits and
// hyphens: a single word would be an ABP substring rule.
func validHost(h string) (string, bool) {
	if h == "" || !strings.Contains(h, ".") {
		return "", false
	}
	a, err := idna.Lookup.ToASCII(h)
	if err != nil {
		return "", false
	}
	a = strings.ToLower(a)
	for _, label := range strings.Split(a, ".") {
		if label == "" || label[0] == '-' || label[len(label)-1] == '-' {
			return "", false
		}
		for _, c := range []byte(label) {
			if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-') {
				return "", false
			}
		}
	}
	return a, true
}

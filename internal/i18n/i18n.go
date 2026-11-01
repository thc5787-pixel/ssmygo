// Package i18n holds the UI strings, taken from the original client's
// translations, regenerated with gen/ (needs the upstream i18n.csv).
// English is the fallback.
package i18n

import (
	_ "embed"
	"encoding/json"
	"strings"
	"sync"
)


var (
	//go:embed strings_en.json
	enJSON []byte
	//go:embed strings_zh-Hans.json
	zhJSON []byte

	tables = map[string][]byte{"en": enJSON, "zh-Hans": zhJSON}
	parsed sync.Map // locale → map[string]string

	mu     sync.RWMutex
	locale = "en"
)

func table(name string) map[string]string {
	if m, ok := parsed.Load(name); ok {
		return m.(map[string]string)
	}
	m := map[string]string{}
	json.Unmarshal(tables[name], &m)
	parsed.Store(name, m)
	return m
}

// SetLocale picks the table for a BCP 47 tag: any Chinese tag except
// Traditional ones maps to zh-Hans; everything else is English.
func SetLocale(tag string) {
	l := strings.ToLower(tag)
	name := "en"
	if strings.HasPrefix(l, "zh") && !strings.Contains(l, "hant") && !strings.Contains(l, "-tw") && !strings.Contains(l, "-hk") && !strings.Contains(l, "-mo") {
		name = "zh-Hans"
	}
	mu.Lock()
	locale = name
	mu.Unlock()
}

// T returns the string for key in the current locale, falling back to
// English and then to the key itself.
func T(key string) string {
	mu.RLock()
	l := locale
	mu.RUnlock()
	if v := table(l)[key]; v != "" {
		return v
	}
	if v := table("en")[key]; v != "" {
		return v
	}
	return key
}

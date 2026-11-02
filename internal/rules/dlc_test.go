package rules

import (
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/database64128/shadowsocks-go/domainset"
	"github.com/thc5787/ssmygo/assets"
	"google.golang.org/protobuf/encoding/protowire"
)

// fixture encodes a GeoSiteList the way v2fly's dlc.dat does.
func fixture() []byte {
	domain := func(typ int, value string, attrs ...string) []byte {
		var b []byte
		b = protowire.AppendTag(b, 1, protowire.VarintType)
		b = protowire.AppendVarint(b, uint64(typ))
		b = protowire.AppendTag(b, 2, protowire.BytesType)
		b = protowire.AppendString(b, value)
		for _, a := range attrs {
			var ab []byte
			ab = protowire.AppendTag(ab, 1, protowire.BytesType)
			ab = protowire.AppendString(ab, a)
			ab = protowire.AppendTag(ab, 2, protowire.VarintType) // bool_value
			ab = protowire.AppendVarint(ab, 1)
			b = protowire.AppendTag(b, 3, protowire.BytesType)
			b = protowire.AppendBytes(b, ab)
		}
		return b
	}
	site := func(code string, domains ...[]byte) []byte {
		var b []byte
		b = protowire.AppendTag(b, 1, protowire.BytesType)
		b = protowire.AppendString(b, code)
		for _, d := range domains {
			b = protowire.AppendTag(b, 2, protowire.BytesType)
			b = protowire.AppendBytes(b, d)
		}
		return b
	}
	var list []byte
	for _, s := range [][]byte{
		site("CN", domain(2, "baidu.com"), domain(3, "a.qq.com", "cn"), domain(0, "taobao"), domain(1, `^cdn\d+\.example\.cn$`)),
		site("GEOLOCATION-!CN", domain(2, "google.com"), domain(2, "bilibili.com", "cn")),
		site("PRIVATE", domain(3, "localhost")),
	} {
		list = protowire.AppendTag(list, 1, protowire.BytesType)
		list = protowire.AppendBytes(list, s)
	}
	return list
}

func TestParseDLC(t *testing.T) {
	m, err := ParseDLC(fixture())
	if err != nil {
		t.Fatal(err)
	}
	want := []Domain{
		{Type: RootDomain, Value: "baidu.com"},
		{Type: Full, Value: "a.qq.com", Attrs: []string{"cn"}},
		{Type: Plain, Value: "taobao"},
		{Type: Regex, Value: `^cdn\d+\.example\.cn$`},
	}
	if !reflect.DeepEqual(m["cn"], want) {
		t.Fatalf("cn = %+v", m["cn"])
	}
	if _, ok := m["geolocation-!cn"]; !ok {
		t.Fatal("keys not lowercased")
	}
}

func TestParseDLCGarbage(t *testing.T) {
	if _, err := ParseDLC([]byte{0x0a, 0xff, 0xff}); err == nil {
		t.Fatal("garbage parsed")
	}
}

func TestSelectAttr(t *testing.T) {
	m, _ := ParseDLC(fixture())
	got, missing := Select(m, []string{"geolocation-!cn@cn"})
	if len(missing) != 0 {
		t.Fatal(missing)
	}
	if len(got) != 1 || got[0].Value != "bilibili.com" {
		t.Fatalf("got %+v", got)
	}
	// Deferred #8①: an unknown group is reported, the others still apply.
	got, missing = Select(m, []string{"cn", "nope"})
	if len(got) != 4 || strings.Join(missing, ",") != "nope" {
		t.Fatalf("got %d domains, missing %v", len(got), missing)
	}
	all, _ := Select(m, []string{"CN"}) // case-insensitive like v2ray
	if len(all) != 4 {
		t.Fatalf("CN: %d", len(all))
	}
}

func loadSet(t *testing.T, path string) domainset.DomainSet {
	t.Helper()
	ds, err := domainset.Config{Name: "x", Type: "gob", Path: path}.DomainSet()
	if err != nil {
		t.Fatal(err)
	}
	return ds
}

func TestBuildMatches(t *testing.T) {
	dir := t.TempDir()
	r, err := Build(fixture(), []string{"private", "cn"}, []string{"geolocation-!cn"}, "", dir)
	if err != nil {
		t.Fatal(err)
	}
	direct, proxied := loadSet(t, r.Direct), loadSet(t, r.Proxied)
	for d, want := range map[string]bool{
		"www.baidu.com": true, "baidu.com": true, "a.qq.com": true, "b.a.qq.com": false,
		"x.taobao.net": true, "cdn12.example.cn": true, "localhost": true, "google.com": false,
	} {
		if got := direct.Match(d); got != want {
			t.Errorf("direct.Match(%q) = %v", d, got)
		}
	}
	if !proxied.Match("mail.google.com") || proxied.Match("baidu.com") {
		t.Error("proxied set wrong")
	}
}

func TestBuildCache(t *testing.T) {
	dir := t.TempDir()
	r, err := Build(fixture(), []string{"cn"}, []string{"geolocation-!cn"}, "", dir)
	if err != nil {
		t.Fatal(err)
	}
	st1, _ := os.Stat(r.Direct)
	time.Sleep(20 * time.Millisecond)
	r2, _ := Build(fixture(), []string{"cn"}, []string{"geolocation-!cn"}, "", dir)
	st2, _ := os.Stat(r2.Direct)
	if !st1.ModTime().Equal(st2.ModTime()) {
		t.Fatal("cache miss on identical input")
	}
	r3, _ := Build(fixture(), []string{"private"}, []string{"geolocation-!cn"}, "", dir)
	if loadSet(t, r3.Direct).Match("baidu.com") {
		t.Fatal("changed groups not rebuilt")
	}
	if filepath.Dir(r.Direct) != dir {
		t.Fatal("output outside dir")
	}
}

func TestEmbeddedDLCParses(t *testing.T) {
	m, err := ParseDLC(assets.DLC())
	if err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"cn", "private", "geolocation-!cn"} {
		if len(m[k]) == 0 {
			t.Errorf("embedded dlc missing %q", k)
		}
	}
	if _, missing := Select(m, []string{"private", "cn", "geolocation-!cn@cn", "geolocation-!cn"}); len(missing) != 0 {
		t.Fatal(err)
	}
}

// Real data, default groups: every rule must compile (RE2) and a cold build
// must fit the 1 s startup budget.
func TestBuildEmbeddedDefaults(t *testing.T) {
	start := time.Now()
	r, err := Build(assets.DLC(), []string{"private", "cn", "geolocation-!cn@cn"}, []string{"geolocation-!cn"}, "", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	cold := time.Since(start)
	t.Logf("cold build %v", cold)
	if cold > time.Second {
		t.Errorf("cold build %v > 1s", cold)
	}
	if !loadSet(t, r.Direct).Match("www.baidu.com") || !loadSet(t, r.Proxied).Match("www.google.com") {
		t.Error("real data does not route baidu direct / google proxied")
	}
}

// A cold build must not allocate much more than the selected groups need:
// the whole-file parse left ~60 MB resident on first launch (tray target 40 MB).
func TestBuildAllocations(t *testing.T) {
	dlc := assets.DLC()
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	if _, err := Build(dlc, []string{"private", "cn", "geolocation-!cn@cn"}, []string{"geolocation-!cn"}, "", t.TempDir()); err != nil {
		t.Fatal(err)
	}
	runtime.ReadMemStats(&after)
	mb := float64(after.TotalAlloc-before.TotalAlloc) / (1 << 20)
	t.Logf("cold build allocated %.1f MB", mb)
	if mb > 30 { // whole-file parsing was 44 MB; selected groups alone are ~26 MB
		t.Errorf("cold build allocated %.1f MB > 30 MB", mb)
	}
}

// Deferred #8①: one mistyped group must not disable all rules.
func TestBuildSkipsUnknownGroup(t *testing.T) {
	r, err := Build(fixture(), []string{"cn", "nope"}, []string{"geolocation-!cn"}, "", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(r.Missing, ",") != "nope" {
		t.Fatalf("missing %v", r.Missing)
	}
	if !loadSet(t, r.Direct).Match("baidu.com") {
		t.Fatal("known groups not applied")
	}
}

// Deferred #8③: a new converter version must rebuild an otherwise valid cache.
func TestBuildCacheVersion(t *testing.T) {
	dir := t.TempDir()
	r, _ := Build(fixture(), []string{"cn"}, []string{"geolocation-!cn"}, "", dir)
	st1, _ := os.Stat(r.Direct)
	time.Sleep(20 * time.Millisecond)
	old := converterVersion
	converterVersion = old + "-next"
	defer func() { converterVersion = old }()
	r2, _ := Build(fixture(), []string{"cn"}, []string{"geolocation-!cn"}, "", dir)
	st2, _ := os.Stat(r2.Direct)
	if st1.ModTime().Equal(st2.ModTime()) {
		t.Fatal("cache reused across converter versions")
	}
}

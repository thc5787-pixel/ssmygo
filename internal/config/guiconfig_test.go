package config

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestDefaults(t *testing.T) {
	c := Default()
	if c.LocalPort != 1080 || !c.PortableMode || !c.FirstRun || c.GeositePreferDirect {
		t.Fatalf("scalar defaults wrong: %+v", c)
	}
	if !reflect.DeepEqual(c.GeositeDirectGroups, []string{"private", "cn", "geolocation-!cn@cn"}) {
		t.Fatalf("direct groups %v", c.GeositeDirectGroups)
	}
	if !reflect.DeepEqual(c.GeositeProxiedGroups, []string{"geolocation-!cn"}) {
		t.Fatalf("proxied groups %v", c.GeositeProxiedGroups)
	}
	path := filepath.Join(t.TempDir(), "gui-config.json")
	if err := c.Save(path); err != nil {
		t.Fatal(err)
	}
	var m map[string]json.RawMessage
	b, _ := os.ReadFile(path)
	json.Unmarshal(b, &m)
	if string(m["configs"]) != "[]" {
		t.Fatalf(`configs = %s, want []`, m["configs"])
	}
}

func TestRoundTripPreservesUnknown(t *testing.T) {
	src := filepath.Join("testdata", "original-4.4.1.json")
	c, err := Load(src)
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Configs) != 2 || c.Index != 1 || !c.Enabled || c.Configs[1].PluginOpts != "tls;host=example.com" {
		t.Fatalf("loaded wrong: %+v", c)
	}
	dst := filepath.Join(t.TempDir(), "gui-config.json")
	if err := c.Save(dst); err != nil {
		t.Fatal(err)
	}
	want, got := readMap(t, src), readMap(t, dst)
	delete(got, "ssmygoSysproxy")
	if !reflect.DeepEqual(want, got) {
		for k := range want {
			if !reflect.DeepEqual(want[k], got[k]) {
				t.Errorf("key %q: want %v got %v", k, want[k], got[k])
			}
		}
		for k := range got {
			if _, ok := want[k]; !ok {
				t.Errorf("extra key %q", k)
			}
		}
	}
}

func readMap(t *testing.T, path string) map[string]any {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func TestLoadMissingFile(t *testing.T) {
	c, err := Load(filepath.Join(t.TempDir(), "nope.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(c, Default()) {
		t.Fatalf("got %+v", c)
	}
}

func TestLoadCorrupt(t *testing.T) {
	path := filepath.Join(t.TempDir(), "gui-config.json")
	os.WriteFile(path, []byte(`{"configs": [`), 0o644)
	_, err := Load(path)
	if !errors.Is(err, ErrCorrupt) {
		t.Fatalf("err = %v", err)
	}
	bak, err := BackupCorrupt(path, time.Date(2026, 11, 1, 12, 0, 0, 0, time.Local))
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(bak) != "gui-config.json.bad-20261101-120000" {
		t.Fatalf("backup %s", bak)
	}
	if _, err := os.Stat(bak); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("original still present")
	}
}

func TestLoadBOM(t *testing.T) {
	path := filepath.Join(t.TempDir(), "gui-config.json")
	os.WriteFile(path, append([]byte{0xEF, 0xBB, 0xBF}, `{"localPort": 2080}`...), 0o644)
	c, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if c.LocalPort != 2080 {
		t.Fatalf("port %d", c.LocalPort)
	}
}

func TestIndexClamped(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.json")
	os.WriteFile(path, []byte(`{"index":7,"configs":[{"server":"a","server_port":1,"method":"none"},{"server":"b","server_port":2,"method":"none"}]}`), 0o644)
	c, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if c.Index != 0 {
		t.Fatalf("index %d", c.Index)
	}
	path2 := filepath.Join(dir, "b.json")
	os.WriteFile(path2, []byte(`{"index":0,"configs":[]}`), 0o644)
	c2, _ := Load(path2)
	if _, ok := c2.Current(); ok {
		t.Fatal("Current() ok with no servers")
	}
}

func TestUnknownMethodKept(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.json")
	os.WriteFile(path, []byte(`{"configs":[{"server":"a","server_port":1,"method":"2022-blake3-chacha20-poly1305","password":"x"}]}`), 0o644)
	c, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Save(path); err != nil {
		t.Fatal(err)
	}
	c, _ = Load(path)
	if c.Configs[0].Method != "2022-blake3-chacha20-poly1305" {
		t.Fatalf("method %q", c.Configs[0].Method)
	}
	if !hasField(c.Configs[0].Validate(), "method") {
		t.Fatal("unknown method validated")
	}
}

func TestSaveAtomic(t *testing.T) {
	path := filepath.Join(t.TempDir(), "gui-config.json")
	c := Default()
	c.LocalPort = 1234
	if err := c.Save(path); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(path)
	os.Mkdir(path+".tmp", 0o755)
	c.LocalPort = 9999
	if err := c.Save(path); err == nil {
		t.Fatal("Save succeeded with .tmp blocked")
	}
	after, _ := os.ReadFile(path)
	if string(before) != string(after) {
		t.Fatal("file changed after failed save")
	}
}

func TestClone(t *testing.T) {
	c := Default()
	c.Configs = []Server{{Server: "a"}}
	d := c.Clone()
	d.Configs[0].Server = "b"
	d.GeositeDirectGroups[0] = "x"
	if c.Configs[0].Server != "a" || c.GeositeDirectGroups[0] != "private" {
		t.Fatal("Clone shares memory")
	}
}

func TestResolveRoot(t *testing.T) {
	exe, appData := t.TempDir(), t.TempDir()
	// no config anywhere → portable (exe dir)
	if got := ResolveRoot(exe, appData); got != exe {
		t.Fatalf("fresh: %s", got)
	}
	// config in AppData only → AppData\ssmygo
	os.MkdirAll(filepath.Join(appData, "ssmygo"), 0o755)
	os.WriteFile(filepath.Join(appData, "ssmygo", "gui-config.json"), []byte(`{}`), 0o644)
	if got := ResolveRoot(exe, appData); got != filepath.Join(appData, "ssmygo") {
		t.Fatalf("appdata: %s", got)
	}
	// exe-dir config with portableMode true → exe dir
	os.WriteFile(filepath.Join(exe, "gui-config.json"), []byte(`{"portableMode":true}`), 0o644)
	if got := ResolveRoot(exe, appData); got != exe {
		t.Fatalf("portable: %s", got)
	}
	// exe-dir config with portableMode false → AppData
	os.WriteFile(filepath.Join(exe, "gui-config.json"), []byte(`{"portableMode":false}`), 0o644)
	if got := ResolveRoot(exe, appData); got != filepath.Join(appData, "ssmygo") {
		t.Fatalf("non-portable: %s", got)
	}
}

func TestPaths(t *testing.T) {
	p := Paths{Root: `C:\x`}
	if p.Config() != filepath.Join(`C:\x`, "gui-config.json") || p.UserRule() != filepath.Join(`C:\x`, "user-rule.txt") ||
		p.DomainSets() != filepath.Join(`C:\x`, "cache", "domainsets") || p.Logs() != filepath.Join(`C:\x`, "logs") {
		t.Fatal(p)
	}
}

func hasField(errs []FieldError, f string) bool {
	for _, e := range errs {
		if e.Field == f {
			return true
		}
	}
	return false
}

// Deferred #6: a damaged localPort must not become 127.0.0.1:0.
func TestLoadClampsLocalPort(t *testing.T) {
	dir := t.TempDir()
	for _, port := range []string{"0", "-5", "70000"} {
		path := filepath.Join(dir, "p"+port+".json")
		os.WriteFile(path, []byte(`{"localPort":`+port+`}`), 0o644)
		c, err := Load(path)
		if err != nil {
			t.Fatal(err)
		}
		if c.LocalPort != 1080 {
			t.Errorf("localPort %s loaded as %d", port, c.LocalPort)
		}
	}
}

// P1-D: the original's geosite source fields and ssmygo's update time.
func TestGeositeFieldsRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "gui-config.json")
	os.WriteFile(path, []byte(`{"geositeUrl":"https://example.com/dlc.dat","geositeSha256sumUrl":"https://example.com/dlc.dat.sha256sum","ssmygoGeositeUpdatedAt":"2026-10-01T08:00:00Z"}`), 0o644)
	c, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 10, 1, 8, 0, 0, 0, time.UTC)
	if c.GeositeURL != "https://example.com/dlc.dat" || c.GeositeSha256sumURL != "https://example.com/dlc.dat.sha256sum" || !c.GeositeUpdatedAt.Equal(at) {
		t.Fatalf("loaded %q %q %v", c.GeositeURL, c.GeositeSha256sumURL, c.GeositeUpdatedAt)
	}
	if err := c.Save(path); err != nil {
		t.Fatal(err)
	}
	m := readMap(t, path)
	if m["geositeUrl"] != "https://example.com/dlc.dat" || m["geositeSha256sumUrl"] != "https://example.com/dlc.dat.sha256sum" || m["ssmygoGeositeUpdatedAt"] != "2026-10-01T08:00:00Z" {
		t.Fatalf("saved %v %v %v", m["geositeUrl"], m["geositeSha256sumUrl"], m["ssmygoGeositeUpdatedAt"])
	}
	// Never updated: the key is left out; the original's empty strings stay.
	d := Default()
	d.Save(path)
	m = readMap(t, path)
	if _, ok := m["ssmygoGeositeUpdatedAt"]; ok {
		t.Fatal("zero update time written")
	}
	if v, ok := m["geositeUrl"]; !ok || v != "" {
		t.Fatalf("geositeUrl = %v", v)
	}
}

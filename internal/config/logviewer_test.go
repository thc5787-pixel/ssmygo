package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const originalLogViewer = `{"configs":[],"logViewer":{"topMost":true,"wrapText":false,"toolbarShown":true,"Font":"Consolas, 8pt","BackgroundColor":"Black","TextColor":"White","x":1},"showPluginOutput":true}`

func loadString(t *testing.T, s string) (*Config, string) {
	t.Helper()
	p := filepath.Join(t.TempDir(), "gui-config.json")
	if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
		t.Fatal(err)
	}
	c, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	return c, p
}

func logViewerKeys(t *testing.T, path string) map[string]json.RawMessage {
	t.Helper()
	b, _ := os.ReadFile(path)
	var top struct {
		LogViewer map[string]json.RawMessage `json:"logViewer"`
	}
	if err := json.Unmarshal(b, &top); err != nil {
		t.Fatal(err)
	}
	return top.LogViewer
}

// Review Focus 5: a config written by the original client round-trips.
func TestLogViewerRoundTrip(t *testing.T) {
	c, p := loadString(t, originalLogViewer)
	v := c.LogViewer
	if !v.TopMost || v.WrapText || !v.ToolbarShown || v.FontFamily != "Consolas" || v.FontSize != 8 || !c.ShowPluginOutput {
		t.Fatalf("loaded %+v plugin=%v", v, c.ShowPluginOutput)
	}
	if err := c.Save(p); err != nil {
		t.Fatal(err)
	}
	k := logViewerKeys(t, p)
	for name, want := range map[string]string{"Font": `"Consolas, 8pt"`, "BackgroundColor": `"Black"`, "TextColor": `"White"`, "x": `1`} {
		if string(k[name]) != want {
			t.Errorf("%s = %s, want %s", name, k[name], want)
		}
	}
}

func TestLogViewerMissing(t *testing.T) {
	c, _ := loadString(t, `{"configs":[]}`)
	d := DefaultLogViewer()
	if c.LogViewer.FontFamily != d.FontFamily || c.LogViewer.FontSize != 9 || c.ShowPluginOutput {
		t.Fatalf("defaults %+v", c.LogViewer)
	}
}

func TestFontParse(t *testing.T) {
	cases := []struct {
		in     string
		family string
		size   float32
	}{
		{"Consolas, 8pt", "Consolas", 8},
		{"Microsoft YaHei UI, 8.25pt", "Microsoft YaHei UI", 8.25},
		{"garbage", "Consolas", 9},
	}
	for _, c := range cases {
		f, s := parseFont(c.in)
		if f != c.family || s != c.size {
			t.Errorf("parseFont(%q) = %q %v", c.in, f, s)
		}
	}
	// unparsable raw is written back unchanged while the font is untouched
	cfg, p := loadString(t, `{"configs":[],"logViewer":{"Font":"garbage"}}`)
	cfg.Save(p)
	if k := logViewerKeys(t, p); string(k["Font"]) != `"garbage"` {
		t.Fatalf("Font = %s", k["Font"])
	}
}

func TestFontChangeWritesNewString(t *testing.T) {
	c, p := loadString(t, originalLogViewer)
	v := c.LogViewer
	v.FontFamily, v.FontSize, v.Splitter = "Cascadia Mono", 10.5, 0.6
	c.SetLogViewer(v)
	c.Save(p)
	k := logViewerKeys(t, p)
	if string(k["Font"]) != `"Cascadia Mono, 10.5pt"` || string(k["BackgroundColor"]) != `"Black"` || !strings.HasPrefix(string(k["splitter"]), "0.6") {
		t.Fatalf("saved %v", k)
	}
}

func TestShowPluginOutputDefaultFalse(t *testing.T) {
	if Default().ShowPluginOutput {
		t.Fatal("default true")
	}
}

// A logViewer section nobody changed is saved back byte for byte (no new
// keys appear).
func TestLogViewerUnchangedRoundTrip(t *testing.T) {
	const section = `{"topMost":true,"wrapText":false,"toolbarShown":true,"Font":"Consolas, 8pt","BackgroundColor":"Black","TextColor":"White"}`
	c, p := loadString(t, `{"configs":[],"logViewer":`+section+`}`)
	if err := c.Save(p); err != nil {
		t.Fatal(err)
	}
	k := logViewerKeys(t, p)
	if len(k) != 6 {
		t.Fatalf("keys %v", k)
	}
}

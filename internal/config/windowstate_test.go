package config

import (
	"os"
	"strings"
	"testing"
)

func TestWindowStatesRoundTrip(t *testing.T) {
	c, p := loadString(t, `{"configs":[]}`)
	if len(c.WindowStates) != 0 {
		t.Fatalf("default %+v", c.WindowStates)
	}
	c.WindowStates = map[string]WindowState{"logViewer": {X: 10, Y: 20, Width: 800, Height: 600, Maximized: true}}
	if err := c.Save(p); err != nil {
		t.Fatal(err)
	}
	c2, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if got := c2.WindowStates["logViewer"]; got != c.WindowStates["logViewer"] {
		t.Fatalf("loaded %+v", got)
	}
}

// Window size and position moved to ssmygoWindows: the old keys are
// dropped on save instead of lingering as unknown fields.
func TestOldWindowKeysDropped(t *testing.T) {
	c, p := loadString(t, `{"configs":[],"ssmygoConfigWindow":{"width":900,"height":700},
		"logViewer":{"topMost":false,"wrapText":true,"toolbarShown":false,"Font":"Consolas, 8pt","width":800,"height":600,"left":5,"top":6,"maximized":true}}`)
	if err := c.Save(p); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"ssmygoConfigWindow", `"width"`, `"height"`, `"left"`, `"top"`, `"maximized"`} {
		if strings.Contains(string(b), k) {
			t.Errorf("saved file still has %s", k)
		}
	}
	if !c.LogViewer.WrapText {
		t.Fatal("logViewer settings lost")
	}
}

func TestCloneCopiesWindowStates(t *testing.T) {
	c := Default()
	c.WindowStates = map[string]WindowState{"logViewer": {Width: 800}}
	d := c.Clone()
	d.WindowStates["logViewer"] = WindowState{Width: 1}
	if c.WindowStates["logViewer"].Width != 800 {
		t.Fatal("clone shares the window states")
	}
}

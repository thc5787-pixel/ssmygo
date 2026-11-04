package config

import (
	"encoding/json"
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
	var saved struct {
		LogViewer map[string]any `json:"logViewer"`
	}
	if err := json.Unmarshal(b, &saved); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "ssmygoConfigWindow") {
		t.Error("saved file still has ssmygoConfigWindow")
	}
	for _, k := range []string{"width", "height", "left", "top", "maximized"} {
		if _, ok := saved.LogViewer[k]; ok {
			t.Errorf("saved logViewer still has %s", k)
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

// 1.0.0's window places move into WindowStates instead of being lost.
func TestOldWindowKeysMigrated(t *testing.T) {
	c, _ := loadString(t, `{"configs":[],"ssmygoConfigWindow":{"width":900,"height":700,"left":30,"top":40},
		"logViewer":{"topMost":false,"wrapText":false,"toolbarShown":false,"width":800,"height":600,"left":5,"top":6,"maximized":true}}`)
	if got, want := c.WindowStates["editServers"], (WindowState{X: 30, Y: 40, Width: 900, Height: 700}); got != want {
		t.Errorf("editServers %+v, want %+v", got, want)
	}
	if got, want := c.WindowStates["logViewer"], (WindowState{X: 5, Y: 6, Width: 800, Height: 600, Maximized: true}); got != want {
		t.Errorf("logViewer %+v, want %+v", got, want)
	}
}

// What 1.0.0 would not have restored stays out: no position (centred)
// and Windows' minimized parking spot; a size too small was the default.
func TestOldWindowKeysUnusableNotMigrated(t *testing.T) {
	c, _ := loadString(t, `{"configs":[],"ssmygoConfigWindow":{"width":100,"height":100,"left":30,"top":40}}`)
	if got, want := c.WindowStates["editServers"], (WindowState{X: 30, Y: 40, Width: 582, Height: 620}); got != want {
		t.Errorf("small editServers %+v, want %+v", got, want)
	}
	for _, js := range []string{
		`{"configs":[],"ssmygoConfigWindow":{"width":900,"height":700}}`,
		`{"configs":[],"ssmygoConfigWindow":{"width":900,"height":700,"left":-32000,"top":-32000}}`,
	} {
		c, _ := loadString(t, js)
		if s, ok := c.WindowStates["editServers"]; ok {
			t.Errorf("%s: migrated %+v", js, s)
		}
	}
	// The Log Viewer's size had defaults (640×420) when not written.
	c, _ = loadString(t, `{"configs":[],"logViewer":{"left":5,"top":6}}`)
	if got, want := c.WindowStates["logViewer"], (WindowState{X: 5, Y: 6, Width: 640, Height: 420}); got != want {
		t.Errorf("logViewer %+v, want %+v", got, want)
	}
}

// States already saved by MyGo win over leftover old keys.
func TestNewWindowStatesWin(t *testing.T) {
	c, _ := loadString(t, `{"configs":[],"ssmygoConfigWindow":{"width":900,"height":700,"left":30,"top":40},
		"ssmygoWindows":{"editServers":{"x":1,"y":2,"width":600,"height":640}}}`)
	if got := c.WindowStates["editServers"]; got.Width != 600 {
		t.Errorf("editServers %+v", got)
	}
}

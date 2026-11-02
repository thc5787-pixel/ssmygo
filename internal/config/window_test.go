package config

import "testing"

func TestConfigWindowBoundsRoundTrip(t *testing.T) {
	c, p := loadString(t, `{"configs":[]}`)
	if c.ConfigWindow != (WindowBounds{}) {
		t.Fatalf("default %+v", c.ConfigWindow)
	}
	c.ConfigWindow = WindowBounds{Width: 800, Height: 700, Left: 100, Top: 50}
	c.Save(p)
	if k := topKeys(t, p); string(k["ssmygoConfigWindow"]) == "" {
		t.Fatal("not saved")
	}
	c2, _ := Load(p)
	if c2.ConfigWindow != c.ConfigWindow {
		t.Fatalf("loaded %+v", c2.ConfigWindow)
	}
}

// Windows parks minimized windows at -32000: such a position, or a size
// below the minimum, loads as unset (default size, centred).
func TestConfigWindowBoundsSanitized(t *testing.T) {
	c, _ := loadString(t, `{"configs":[],"ssmygoConfigWindow":{"width":160,"height":28,"left":-32000,"top":-32000}}`)
	if b := c.ConfigWindow; b.Width != 0 || b.Height != 0 || b.Left != 0 || b.Top != 0 {
		t.Fatalf("loaded %+v", b)
	}
}

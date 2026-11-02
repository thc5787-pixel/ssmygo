package config

import "testing"

func TestLanguageRoundTrip(t *testing.T) {
	c, p := loadString(t, `{"configs":[]}`)
	if c.Language != "" {
		t.Fatalf("default %q, want \"\" (follow the system)", c.Language)
	}
	c.Language = "zh-Hans"
	c.Save(p)
	c2, _ := Load(p)
	if c2.Language != "zh-Hans" {
		t.Fatalf("loaded %q", c2.Language)
	}
}

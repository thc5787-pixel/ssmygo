package sysproxy

import (
	"strings"
	"testing"
)

func TestLocalSettingsBypass(t *testing.T) {
	s := LocalSettings(1080, "*.corp.com;localhost")
	var b strings.Builder
	b.WriteString("*.corp.com;localhost;<local>;127.*;10.*")
	for i := 16; i <= 31; i++ {
		b.WriteString(";172." + itoa(i) + ".*")
	}
	b.WriteString(";192.168.*")
	if s.Bypass != b.String() {
		t.Fatalf("bypass\n got %s\nwant %s", s.Bypass, b.String())
	}
	if s.Server != "127.0.0.1:1080" || s.Flags != FlagDirect|FlagProxy || s.PacURL != "" {
		t.Fatalf("settings %+v", s)
	}
}

func TestLocalSettingsEmptyUserBypass(t *testing.T) {
	s := LocalSettings(2080, "")
	if !strings.HasPrefix(s.Bypass, "<local>;localhost;127.*;") || strings.Contains(s.Bypass, ";;") {
		t.Fatalf("bypass %q", s.Bypass)
	}
}

func TestPointsTo(t *testing.T) {
	cases := []struct {
		s    Settings
		port int
		want bool
	}{
		{Settings{Flags: FlagDirect | FlagProxy, Server: "127.0.0.1:1080"}, 1080, true},
		{Settings{Flags: FlagDirect | FlagProxy, Server: "localhost:1080"}, 1080, true},
		{Settings{Flags: FlagDirect | FlagProxy, Server: "127.0.0.1:1080"}, 2080, false},
		{Settings{Flags: FlagDirect, Server: "127.0.0.1:1080"}, 1080, false},
		{Settings{Flags: FlagDirect | FlagProxy, Server: "http=127.0.0.1:1080;https=127.0.0.1:1080"}, 1080, true},
		{Settings{Flags: FlagDirect | FlagProxy, Server: "corp-proxy:8080"}, 1080, false},
	}
	for _, c := range cases {
		if got := PointsTo(c.s, c.port); got != c.want {
			t.Errorf("PointsTo(%+v, %d) = %v", c.s, c.port, got)
		}
	}
}

func TestFakeRecordsCalls(t *testing.T) {
	f := &Fake{Cur: Settings{Flags: FlagDirect}}
	var p Proxy = f
	p.Set(LocalSettings(1, ""))
	if got, _ := p.Get(); got.Server != "127.0.0.1:1" || len(f.Calls) != 1 {
		t.Fatalf("fake %+v", f)
	}
}

func itoa(i int) string { return string(rune('0'+i/10)) + string(rune('0'+i%10)) }

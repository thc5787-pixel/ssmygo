package sysproxy

import (
	"strconv"
	"strings"
)

// lanBypass is the original client's bypass list (Util/SystemProxy/Sysproxy.cs).
var lanBypass = func() []string {
	l := []string{"<local>", "localhost", "127.*", "10.*"}
	for i := 16; i <= 31; i++ {
		l = append(l, "172."+strconv.Itoa(i)+".*")
	}
	return append(l, "192.168.*")
}()

// LocalSettings points the system proxy at 127.0.0.1:port, bypassing the
// user's own entries first, then the LAN list, without duplicates.
func LocalSettings(port int, userBypass string) Settings {
	seen := map[string]bool{}
	var out []string
	add := func(e string) {
		if e = strings.TrimSpace(e); e != "" && !seen[e] {
			seen[e] = true
			out = append(out, e)
		}
	}
	for _, e := range strings.Split(userBypass, ";") {
		add(e)
	}
	for _, e := range lanBypass {
		add(e)
	}
	return Settings{
		Flags:  FlagDirect | FlagProxy,
		Server: "127.0.0.1:" + strconv.Itoa(port),
		Bypass: strings.Join(out, ";"),
	}
}

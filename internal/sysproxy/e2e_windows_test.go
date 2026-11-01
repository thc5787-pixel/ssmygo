//go:build windows && sysproxy_e2e

package sysproxy

import "testing"

// Changes the real system proxy and restores it. Run manually:
//
//	go test -tags sysproxy_e2e ./internal/sysproxy/
func TestE2ESetRestore(t *testing.T) {
	p := Windows()
	orig, err := p.Get()
	if err != nil {
		t.Fatal(err)
	}
	defer p.Set(orig)
	want := LocalSettings(1080, orig.Bypass)
	if err := p.Set(want); err != nil {
		t.Fatal(err)
	}
	if got, _ := p.Get(); got != want {
		t.Fatalf("after set: %+v, want %+v", got, want)
	}
	if err := p.Set(orig); err != nil {
		t.Fatal(err)
	}
	if got, _ := p.Get(); got != orig {
		t.Fatalf("after restore: %+v, want %+v", got, orig)
	}
}

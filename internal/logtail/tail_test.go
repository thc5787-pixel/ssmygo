package logtail

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func appendFile(t *testing.T, path, s string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := f.WriteString(s); err != nil {
		t.Fatal(err)
	}
}

func poll(t *testing.T, tl *Tail) ([]string, bool) {
	t.Helper()
	lines, reset, err := tl.Poll()
	if err != nil {
		t.Fatal(err)
	}
	return lines, reset
}

func TestPollAppends(t *testing.T) {
	p := filepath.Join(t.TempDir(), "ssmygo.log")
	appendFile(t, p, "one\ntwo\n")
	tl := Open(p)
	if got, _ := poll(t, tl); strings.Join(got, "|") != "one|two" {
		t.Fatalf("first %q", got)
	}
	appendFile(t, p, "three\r\n")
	if got, reset := poll(t, tl); strings.Join(got, "|") != "three" || reset {
		t.Fatalf("second %q reset=%v", got, reset)
	}
	if got, _ := poll(t, tl); len(got) != 0 {
		t.Fatalf("idle %q", got)
	}
}

func TestPollPartialLine(t *testing.T) {
	p := filepath.Join(t.TempDir(), "ssmygo.log")
	appendFile(t, p, "abc")
	tl := Open(p)
	if got, _ := poll(t, tl); len(got) != 0 {
		t.Fatalf("partial returned %q", got)
	}
	appendFile(t, p, "def\n")
	if got, _ := poll(t, tl); strings.Join(got, "|") != "abcdef" {
		t.Fatalf("got %q", got)
	}
}

func TestFirstPollLast64KB(t *testing.T) {
	p := filepath.Join(t.TempDir(), "ssmygo.log")
	var b strings.Builder
	for i := 0; b.Len() < 200<<10; i++ {
		fmt.Fprintf(&b, "line %06d %s\n", i, strings.Repeat("x", 50))
	}
	appendFile(t, p, b.String())
	got, _ := poll(t, Open(p))
	total := 0
	for _, l := range got {
		total += len(l) + 1
	}
	if total > 64<<10 || !strings.HasPrefix(got[0], "line ") || len(got[0]) != len("line 000000 ")+50 {
		t.Fatalf("total %d first %q", total, got[0])
	}
	if !strings.HasSuffix(b.String(), got[len(got)-1]+"\n") {
		t.Fatal("last line missing")
	}
}

// Review Focus 2: logx rotates by renaming; a Tail must not block that,
// and afterwards it reads the new file from the start.
func TestRotationRename(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "ssmygo.log")
	appendFile(t, p, "old 1\n")
	tl := Open(p)
	poll(t, tl)
	appendFile(t, p, "old 2\n")
	if err := os.Rename(p, filepath.Join(dir, "ssmygo.1.log")); err != nil {
		t.Fatalf("rename blocked: %v", err)
	}
	appendFile(t, p, "new 1\n")
	got, reset := poll(t, tl)
	if !reset || strings.Join(got, "|") != "new 1" {
		t.Fatalf("got %q reset=%v", got, reset)
	}
}

func TestTruncate(t *testing.T) {
	p := filepath.Join(t.TempDir(), "ssmygo.log")
	appendFile(t, p, "aaaaaaaaaa\nbbbbbbbbbb\n")
	tl := Open(p)
	poll(t, tl)
	os.WriteFile(p, []byte("c\n"), 0o644)
	if got, reset := poll(t, tl); !reset || strings.Join(got, "|") != "c" {
		t.Fatalf("got %q reset=%v", got, reset)
	}
}

func TestMissingFileThenAppears(t *testing.T) {
	p := filepath.Join(t.TempDir(), "ssmygo.log")
	tl := Open(p)
	if _, _, err := tl.Poll(); err == nil {
		t.Fatal("missing file: no error")
	}
	appendFile(t, p, "hello\n")
	if got, _ := poll(t, tl); strings.Join(got, "|") != "hello" {
		t.Fatalf("got %q", got)
	}
}

// Review Focus 1: huge and invalid lines are cut, never crash.
func TestCutLine(t *testing.T) {
	if got := CutLine(strings.Repeat("a", 5000)); got != strings.Repeat("a", 4096)+"…" {
		t.Fatalf("len %d", len(got))
	}
	if got := CutLine("\xff\xfe"); got != "��" {
		t.Fatalf("got %q", got)
	}
	// a multi-byte rune at the cut is not split
	s := strings.Repeat("a", 4095) + "中文"
	if got := CutLine(s); got != strings.Repeat("a", 4095)+"…" {
		t.Fatalf("cut split a rune: %q", got[4090:])
	}
	if CutLine("short") != "short" {
		t.Fatal("short line changed")
	}
}

func TestBufferCap(t *testing.T) {
	b := NewBuffer(2000)
	for i := range 2500 {
		b.Append(fmt.Sprint(i))
	}
	got := b.Lines(false)
	if len(got) != 2000 || got[0] != "500" || got[1999] != "2499" {
		t.Fatalf("len %d first %s last %s", len(got), got[0], got[len(got)-1])
	}
	b.Clear()
	if len(b.Lines(false)) != 0 {
		t.Fatal("clear")
	}
}

func TestPluginFilter(t *testing.T) {
	plugin := `time=2026-11-02T10:00:00.000+08:00 level=INFO msg="plugin output" plugin=v2ray-plugin line="started"`
	other := `time=2026-11-02T10:00:01.000+08:00 level=WARN msg="something else"`
	if !IsPluginOutput(plugin) || IsPluginOutput(other) {
		t.Fatal("IsPluginOutput")
	}
	b := NewBuffer(10)
	b.Append(plugin, other)
	if got := b.Lines(true); len(got) != 1 || got[0] != other {
		t.Fatalf("filtered %q", got)
	}
	if got := b.Lines(false); len(got) != 2 {
		t.Fatalf("unfiltered %q", got)
	}
}

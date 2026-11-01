package logx

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRotate(t *testing.T) {
	dir := t.TempDir()
	l, closer, err := New(dir, 200, 2)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 40; i++ { // ~40 lines × ~60 bytes ≫ 3 × 200 bytes
		l.Info("line", "n", i)
	}
	closer.Close()
	for _, f := range []string{"ssmygo.log", "ssmygo.1.log", "ssmygo.2.log"} {
		st, err := os.Stat(filepath.Join(dir, f))
		if err != nil {
			t.Fatalf("%s: %v", f, err)
		}
		if st.Size() > 200 {
			t.Errorf("%s is %d bytes > max", f, st.Size())
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "ssmygo.3.log")); !os.IsNotExist(err) {
		t.Fatal("kept more than 2 rotated files")
	}
	b, _ := os.ReadFile(filepath.Join(dir, "ssmygo.log"))
	if !strings.Contains(string(b), "n=39") {
		t.Fatalf("newest line not in current log: %s", b)
	}
}

func TestCreatesDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "a", "logs")
	_, c, err := New(dir, 1<<20, 2)
	if err != nil {
		t.Fatal(err)
	}
	c.Close()
	if _, err := os.Stat(filepath.Join(dir, "ssmygo.log")); err != nil {
		t.Fatal(err)
	}
}

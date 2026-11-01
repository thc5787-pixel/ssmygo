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

// Deferred #8②: if reopening ssmygo.log after a rotation fails, logging
// must continue (into the rotated file) instead of dying for the session.
func TestRotateReopenFailureKeepsLogging(t *testing.T) {
	dir := t.TempDir()
	l, closer, err := New(dir, 200, 2)
	if err != nil {
		t.Fatal(err)
	}
	defer closer.Close()
	orig := openFile
	defer func() { openFile = orig }()
	openFile = func(name string, flag int, perm os.FileMode) (*os.File, error) {
		if filepath.Base(name) == "ssmygo.log" {
			return nil, os.ErrPermission
		}
		return orig(name, flag, perm)
	}
	for i := 0; i < 20; i++ {
		l.Info("after-failure", "n", i)
	}
	b, _ := os.ReadFile(filepath.Join(dir, "ssmygo.1.log"))
	if !strings.Contains(string(b), "n=19") {
		t.Fatalf("last record lost; ssmygo.1.log:\n%s", b)
	}
}

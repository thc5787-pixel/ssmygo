// Package logx writes slog records to a size-rotated file.
package logx

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
)

// New logs to dir\ssmygo.log. When a write would pass maxBytes the file
// becomes ssmygo.1.log (older ones shift up) and at most keep rotated files
// are kept.
func New(dir string, maxBytes int64, keep int) (*slog.Logger, io.Closer, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, nil, err
	}
	w := &rotator{dir: dir, max: maxBytes, keep: keep}
	if err := w.open(); err != nil {
		return nil, nil, err
	}
	return slog.New(slog.NewTextHandler(w, &slog.HandlerOptions{Level: slog.LevelInfo})), w, nil
}

// openFile is os.OpenFile; tests replace it to simulate failures.
var openFile = os.OpenFile

type rotator struct {
	mu       sync.Mutex
	dir      string
	max      int64
	keep     int
	f        *os.File
	size     int64
	fallback bool // ssmygo.log could not be reopened: append to ssmygo.1.log, stop rotating
}

func (r *rotator) path(i int) string {
	if i == 0 {
		return filepath.Join(r.dir, "ssmygo.log")
	}
	return filepath.Join(r.dir, fmt.Sprintf("ssmygo.%d.log", i))
}

func (r *rotator) open() error { return r.openAt(r.path(0)) }

func (r *rotator) openAt(path string) error {
	f, err := openFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	st, err := f.Stat()
	if err != nil {
		f.Close()
		return err
	}
	r.f, r.size = f, st.Size()
	return nil
}

func (r *rotator) Write(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.fallback && r.size > 0 && r.size+int64(len(p)) > r.max {
		if err := r.rotate(); err != nil {
			return 0, err
		}
	}
	n, err := r.f.Write(p)
	r.size += int64(n)
	return n, err
}

// rotate shifts the files and reopens ssmygo.log. If that fails, logging
// continues in the file just rotated out, without further rotation, rather
// than stopping for the rest of the session.
func (r *rotator) rotate() error {
	r.f.Close()
	os.Remove(r.path(r.keep))
	for i := r.keep - 1; i >= 0; i-- {
		os.Rename(r.path(i), r.path(i+1))
	}
	err := r.open()
	if err == nil {
		return nil
	}
	if ferr := r.openAt(r.path(1)); ferr != nil {
		return err
	}
	r.fallback = true
	return nil
}

func (r *rotator) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.f.Close()
}

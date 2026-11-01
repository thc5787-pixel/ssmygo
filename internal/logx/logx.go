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

type rotator struct {
	mu   sync.Mutex
	dir  string
	max  int64
	keep int
	f    *os.File
	size int64
}

func (r *rotator) path(i int) string {
	if i == 0 {
		return filepath.Join(r.dir, "ssmygo.log")
	}
	return filepath.Join(r.dir, fmt.Sprintf("ssmygo.%d.log", i))
}

func (r *rotator) open() error {
	f, err := os.OpenFile(r.path(0), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
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
	if r.size > 0 && r.size+int64(len(p)) > r.max {
		if err := r.rotate(); err != nil {
			return 0, err
		}
	}
	n, err := r.f.Write(p)
	r.size += int64(n)
	return n, err
}

func (r *rotator) rotate() error {
	r.f.Close()
	os.Remove(r.path(r.keep))
	for i := r.keep - 1; i >= 0; i-- {
		os.Rename(r.path(i), r.path(i+1))
	}
	return r.open()
}

func (r *rotator) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.f.Close()
}

// Package plugin runs SIP003 plugin processes for ssmygo: lookup, start with
// the SS_* environment, readiness probe, restart with backoff, and a Job
// Object so plugins never outlive ssmygo. It does not depend on MyGo.
package plugin

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// ErrNetworkPath: plugins on network shares are refused; even looking at
// one would authenticate to that host.
var ErrNetworkPath = errors.New("plugin: network paths are not allowed")

// NotFoundError lists where a plugin program was looked for.
type NotFoundError struct {
	Name  string
	Tried []string
}

func (e *NotFoundError) Error() string {
	return fmt.Sprintf("plugin %q not found (looked in %s)", e.Name, strings.Join(e.Tried, ", "))
}

// Resolve finds a plugin program (spec A1-D4): an absolute path is used as
// is; a path with a separator is relative to exeDir; a bare name is looked
// up in exeDir, exeDir\plugins, then PATH. ".exe" is added when the name has
// no extension.
func Resolve(name, exeDir string) (string, error) {
	name = strings.TrimSpace(name)
	// Refuse before any file access: UNC and device paths (\\host\…, \\?\UNC\…,
	// \\.\…) would make Windows contact another host (Review #9).
	if strings.HasPrefix(strings.ReplaceAll(name, "/", `\`), `\\`) {
		return "", ErrNetworkPath
	}
	withExe := func(p string) []string {
		if filepath.Ext(p) == "" {
			return []string{p + ".exe", p}
		}
		return []string{p}
	}
	var candidates []string
	switch {
	case filepath.IsAbs(name):
		candidates = withExe(name)
	case strings.ContainsAny(name, `/\`):
		candidates = withExe(filepath.Join(exeDir, name))
	default:
		candidates = append(withExe(filepath.Join(exeDir, name)), withExe(filepath.Join(exeDir, "plugins", name))...)
	}
	tried := []string{}
	for _, c := range candidates {
		tried = append(tried, c)
		if st, err := os.Stat(c); err == nil && !st.IsDir() {
			return filepath.Clean(c), nil
		}
	}
	if !filepath.IsAbs(name) && !strings.ContainsAny(name, `/\`) {
		tried = append(tried, "PATH")
		if p, err := exec.LookPath(name); err == nil {
			if abs, err := filepath.Abs(p); err == nil {
				return abs, nil
			}
		}
	}
	return "", &NotFoundError{Name: name, Tried: tried}
}

// Hash returns the lowercase hex SHA-256 of the file at path.
func Hash(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

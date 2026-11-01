package rules

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"

	"github.com/database64128/shadowsocks-go/domainset"
)

// Result holds the paths of the built gob domain sets and the requested
// groups that dlc does not have (skipped).
type Result struct {
	Direct, Proxied string
	Missing         []string
}

// converterVersion is part of the cache key: bump it when the dlc → domain
// set conversion changes, so existing caches are rebuilt.
var converterVersion = "1"

// Build writes direct.gob and proxied.gob into outDir from the given groups
// of dlc. Files are reused when dlc, the groups and the converter version
// have not changed.
func Build(dlc []byte, direct, proxied []string, outDir string) (Result, error) {
	r := Result{Direct: filepath.Join(outDir, "direct.gob"), Proxied: filepath.Join(outDir, "proxied.gob")}
	stamp := filepath.Join(outDir, "stamp")
	key := cacheKey(dlc, direct, proxied)
	if b, err := os.ReadFile(stamp); err == nil && exists(r.Direct) && exists(r.Proxied) {
		// stamp: key, then the missing groups, one per line
		lines := strings.Split(string(b), "\n")
		if lines[0] == key {
			for _, m := range lines[1:] {
				if m != "" {
					r.Missing = append(r.Missing, m)
				}
			}
			return r, nil
		}
	}
	want := map[string]bool{}
	for _, g := range append(append([]string{}, direct...), proxied...) {
		name, _, _ := strings.Cut(strings.ToLower(g), "@")
		want[name] = true
	}
	list, err := parseDLC(dlc, want)
	if err != nil {
		return Result{}, err
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return Result{}, err
	}
	for _, f := range []struct {
		path   string
		groups []string
	}{{r.Direct, direct}, {r.Proxied, proxied}} {
		ds, missing := Select(list, f.groups)
		r.Missing = append(r.Missing, missing...)
		if err := writeGob(f.path, ds); err != nil {
			return Result{}, err
		}
	}
	stampText := strings.Join(append([]string{key}, r.Missing...), "\n")
	return r, os.WriteFile(stamp, []byte(stampText), 0o644)
}

func writeGob(path string, ds []Domain) error {
	var text strings.Builder
	for _, d := range ds {
		switch d.Type {
		case Plain:
			text.WriteString("keyword:")
		case Regex:
			text.WriteString("regexp:")
		case RootDomain:
			text.WriteString("suffix:")
		case Full:
			text.WriteString("domain:")
		default:
			continue
		}
		text.WriteString(d.Value)
		text.WriteByte('\n')
	}
	b, err := domainset.BuilderFromText(text.String())
	if err != nil {
		return err
	}
	var buf bytes.Buffer
	if err := b.WriteGob(&buf); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, buf.Bytes(), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func cacheKey(dlc []byte, direct, proxied []string) string {
	h := sha256.New()
	h.Write([]byte("v" + converterVersion + "|"))
	h.Write(dlc)
	h.Write([]byte("\x00" + strings.Join(direct, ",") + "\x00" + strings.Join(proxied, ",")))
	return hex.EncodeToString(h.Sum(nil))
}

func exists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

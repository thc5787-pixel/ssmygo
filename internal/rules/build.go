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

// Result holds the paths of the built gob domain sets.
type Result struct{ Direct, Proxied string }

// Build writes direct.gob and proxied.gob into outDir from the given groups
// of dlc. Files are reused when dlc and the groups have not changed.
func Build(dlc []byte, direct, proxied []string, outDir string) (Result, error) {
	r := Result{Direct: filepath.Join(outDir, "direct.gob"), Proxied: filepath.Join(outDir, "proxied.gob")}
	stamp := filepath.Join(outDir, "stamp")
	key := cacheKey(dlc, direct, proxied)
	if b, err := os.ReadFile(stamp); err == nil && string(b) == key && exists(r.Direct) && exists(r.Proxied) {
		return r, nil
	}
	list, err := ParseDLC(dlc)
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
		ds, err := Select(list, f.groups)
		if err != nil {
			return Result{}, err
		}
		if err := writeGob(f.path, ds); err != nil {
			return Result{}, err
		}
	}
	return r, os.WriteFile(stamp, []byte(key), 0o644)
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
	h.Write(dlc)
	h.Write([]byte("\x00" + strings.Join(direct, ",") + "\x00" + strings.Join(proxied, ",")))
	return hex.EncodeToString(h.Sum(nil))
}

func exists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

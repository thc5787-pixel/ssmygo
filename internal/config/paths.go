package config

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
)

// Paths locates ssmygo's files under one root directory.
type Paths struct{ Root string }

func (p Paths) Config() string     { return filepath.Join(p.Root, "gui-config.json") }
func (p Paths) UserRule() string   { return filepath.Join(p.Root, "user-rule.txt") }
func (p Paths) DomainSets() string { return filepath.Join(p.Root, "cache", "domainsets") }
func (p Paths) Logs() string       { return filepath.Join(p.Root, "logs") }

// ResolveRoot picks the data directory: the exe directory when a config
// there is portable (or none exists anywhere), else appData\ssmygo.
func ResolveRoot(exeDir, appData string) string {
	roaming := filepath.Join(appData, "ssmygo")
	if b, err := os.ReadFile(filepath.Join(exeDir, "gui-config.json")); err == nil {
		v := struct {
			PortableMode *bool `json:"portableMode"`
		}{}
		json.Unmarshal(bytes.TrimPrefix(b, []byte{0xEF, 0xBB, 0xBF}), &v)
		if v.PortableMode == nil || *v.PortableMode {
			return exeDir
		}
		return roaming
	}
	if _, err := os.Stat(filepath.Join(roaming, "gui-config.json")); err == nil {
		return roaming
	}
	return exeDir
}

// Package config reads and writes the original shadowsocks-windows
// gui-config.json and parses ss:// links. It does not depend on MyGo.
package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"slices"
	"time"
)

// SysproxyRecord is the user's system proxy before ssmygo changed it.
type SysproxyRecord struct {
	Recorded bool   `json:"recorded"`
	Flags    uint32 `json:"flags"`
	Server   string `json:"server"`
	Bypass   string `json:"bypass"`
	PacURL   string `json:"pacUrl"`
}

// Config is gui-config.json. Fields not listed are preserved on save.
type Config struct {
	Configs              []Server        `json:"configs"`
	Index                int             `json:"index"`
	Enabled              bool            `json:"enabled"`
	Global               bool            `json:"global"`
	LocalPort            int             `json:"localPort"`
	ShareOverLan         bool            `json:"shareOverLan"`
	PortableMode         bool            `json:"portableMode"`
	FirstRun             bool            `json:"firstRun"`
	GeositeDirectGroups  []string        `json:"geositeDirectGroups"`
	GeositeProxiedGroups []string        `json:"geositeProxiedGroups"`
	GeositePreferDirect  bool            `json:"geositePreferDirect"`
	GeositeURL           string          `json:"geositeUrl"`
	GeositeSha256sumURL  string          `json:"geositeSha256sumUrl"`
	GeositeUpdatedAt     time.Time       `json:"ssmygoGeositeUpdatedAt,omitzero"` // last successful geosite update
	Sysproxy             SysproxyRecord  `json:"ssmygoSysproxy"`
	Proxy                ForwardProxy    `json:"proxy"`
	TrustedPlugins       []TrustedPlugin `json:"ssmygoTrustedPlugins,omitempty"`
	LogViewer            LogViewer       `json:"logViewer"`
	ShowPluginOutput     bool            `json:"showPluginOutput"`
	OnlineConfigSource   []string        `json:"onlineConfigSource"`

	extra map[string]json.RawMessage
}

type configFields Config

// ErrCorrupt wraps a gui-config.json that is not valid JSON.
var ErrCorrupt = errors.New("config: corrupt")

// Default returns the original client's defaults (Model/Configuration.cs).
func Default() *Config {
	return &Config{
		Configs:              []Server{},
		LocalPort:            1080,
		PortableMode:         true,
		FirstRun:             true,
		GeositeDirectGroups:  []string{"private", "cn", "geolocation-!cn@cn"},
		GeositeProxiedGroups: []string{"geolocation-!cn"},
		Proxy:                ForwardProxy{ProxyTimeout: DefaultProxyTimeout},
		LogViewer:            DefaultLogViewer(),
		OnlineConfigSource:   []string{},
	}
}

// Load reads path. A missing file yields Default(). Invalid JSON returns an
// error wrapping ErrCorrupt.
func Load(path string) (*Config, error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return Default(), nil
	}
	if err != nil {
		return nil, err
	}
	b = bytes.TrimPrefix(b, []byte{0xEF, 0xBB, 0xBF})
	f := configFields(*Default())
	if err := json.Unmarshal(b, &f); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrCorrupt, err)
	}
	extra, err := unknownFields(b, &f)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrCorrupt, err)
	}
	c := Config(f)
	c.extra = extra
	if c.Configs == nil {
		c.Configs = []Server{}
	}
	if c.Index < 0 || c.Index >= len(c.Configs) {
		c.Index = 0
	}
	if c.LocalPort < 1 || c.LocalPort > 65535 {
		c.LocalPort = Default().LocalPort // never point the system proxy at 127.0.0.1:0
	}
	return &c, nil
}

// BackupCorrupt renames path to path+".bad-<yyyymmdd-hhmmss>".
func BackupCorrupt(path string, now time.Time) (string, error) {
	bak := path + ".bad-" + now.Format("20060102-150405")
	return bak, os.Rename(path, bak)
}

// Save writes c atomically: path+".tmp", fsync, rename.
func (c *Config) Save(path string) error {
	f := configFields(*c)
	if f.Configs == nil {
		f.Configs = []Server{}
	}
	if f.OnlineConfigSource == nil {
		f.OnlineConfigSource = []string{} // the original expects a list
	}
	b, err := marshalWithExtra(f, c.extra)
	if err != nil {
		return err
	}
	var out bytes.Buffer
	if err := json.Indent(&out, b, "", "  "); err != nil {
		return err
	}
	tmp := path + ".tmp"
	fh, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	if _, err := fh.Write(out.Bytes()); err != nil {
		fh.Close()
		os.Remove(tmp)
		return err
	}
	if err := fh.Sync(); err != nil {
		fh.Close()
		os.Remove(tmp)
		return err
	}
	if err := fh.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, path)
}

// Clone returns a deep copy.
func (c *Config) Clone() *Config {
	d := *c
	d.Configs = make([]Server, len(c.Configs))
	for i, s := range c.Configs {
		s.extra = maps.Clone(s.extra)
		d.Configs[i] = s
	}
	d.GeositeDirectGroups = slices.Clone(c.GeositeDirectGroups)
	d.GeositeProxiedGroups = slices.Clone(c.GeositeProxiedGroups)
	d.TrustedPlugins = slices.Clone(c.TrustedPlugins)
	d.Proxy.extra = maps.Clone(c.Proxy.extra)
	d.extra = maps.Clone(c.extra)
	return &d
}

// Current returns the selected server; false when there is none.
func (c *Config) Current() (Server, bool) {
	if c.Index < 0 || c.Index >= len(c.Configs) {
		return Server{}, false
	}
	return c.Configs[c.Index], true
}

// unknownFields returns the top-level keys of raw that known does not
// produce when marshalled.
func unknownFields(raw []byte, known any) (map[string]json.RawMessage, error) {
	var all map[string]json.RawMessage
	if err := json.Unmarshal(raw, &all); err != nil {
		return nil, err
	}
	kb, err := json.Marshal(known)
	if err != nil {
		return nil, err
	}
	var k map[string]json.RawMessage
	if err := json.Unmarshal(kb, &k); err != nil {
		return nil, err
	}
	for name := range k {
		delete(all, name)
	}
	if len(all) == 0 {
		return nil, nil
	}
	return all, nil
}

// marshalWithExtra marshals known and adds the preserved unknown keys.
func marshalWithExtra(known any, extra map[string]json.RawMessage) ([]byte, error) {
	kb, err := json.Marshal(known)
	if err != nil || len(extra) == 0 {
		return kb, err
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(kb, &m); err != nil {
		return nil, err
	}
	for k, v := range extra {
		if _, ok := m[k]; !ok {
			m[k] = v
		}
	}
	return json.Marshal(m)
}

package config

import "encoding/json"

// StrategyID is gui-config.json's "strategy": the original's strategy IDs.
// The empty ID (saved as null, as the original does) means no strategy.
type StrategyID string

// The original's two strategies (Controller/Strategy).
const (
	StrategyBalancing StrategyID = "com.shadowsocks.strategy.balancing"
	StrategyHA        StrategyID = "com.shadowsocks.strategy.ha"
)

// Known reports whether id is one of the strategies ssmygo runs; other IDs
// of the original count as none.
func (id StrategyID) Known() bool { return id == StrategyBalancing || id == StrategyHA }

func (id StrategyID) MarshalJSON() ([]byte, error) {
	if id == "" {
		return []byte("null"), nil
	}
	return json.Marshal(string(id))
}

// HotkeyConfig is gui-config.json's "hotkey" section (original
// Model/HotKeyConfig.cs). Values are WPF strings such as "Ctrl+Shift+Q".
// Unknown keys are kept.
type HotkeyConfig struct {
	SwitchSystemProxy     string `json:"SwitchSystemProxy"`
	SwitchSystemProxyMode string `json:"SwitchSystemProxyMode"`
	SwitchAllowLan        string `json:"SwitchAllowLan"`
	ShowLogs              string `json:"ShowLogs"`
	ServerMoveUp          string `json:"ServerMoveUp"`
	ServerMoveDown        string `json:"ServerMoveDown"`
	RegHotkeysAtStartup   bool   `json:"RegHotkeysAtStartup"`

	extra map[string]json.RawMessage
}

type hotkeyFields HotkeyConfig

func (h *HotkeyConfig) UnmarshalJSON(b []byte) error {
	var f hotkeyFields
	if err := json.Unmarshal(b, &f); err != nil {
		return err
	}
	extra, err := unknownFields(b, &f)
	if err != nil {
		return err
	}
	*h = HotkeyConfig(f)
	h.extra = extra
	return nil
}

func (h HotkeyConfig) MarshalJSON() ([]byte, error) {
	return marshalWithExtra(hotkeyFields(h), h.extra)
}

// SetHotkey replaces the hotkey settings, keeping unknown keys of the
// original "hotkey" object.
func (c *Config) SetHotkey(h HotkeyConfig) {
	h.extra = c.Hotkey.extra
	c.Hotkey = h
}

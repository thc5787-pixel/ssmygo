package config

import (
	"encoding/json"
	"log/slog"
	"path/filepath"
	"strings"
)

// Forward proxy types and limits (original Model/ForwardProxyConfig.cs).
const (
	ProxySOCKS5         = 0
	ProxyHTTP           = 1
	DefaultProxyTimeout = 3
	MaxProxyTimeout     = 10
)

// ForwardProxy is gui-config.json's "proxy" section. Unknown keys are kept.
type ForwardProxy struct {
	UseProxy     bool   `json:"useProxy"`
	ProxyType    int    `json:"proxyType"`
	ProxyServer  string `json:"proxyServer"`
	ProxyPort    int    `json:"proxyPort"`
	ProxyTimeout int    `json:"proxyTimeout"`
	UseAuth      bool   `json:"useAuth"`
	AuthUser     string `json:"authUser"`
	AuthPwd      string `json:"authPwd"`

	extra map[string]json.RawMessage
}

type forwardFields ForwardProxy

func (p *ForwardProxy) UnmarshalJSON(b []byte) error {
	f := forwardFields{ProxyTimeout: DefaultProxyTimeout}
	if err := json.Unmarshal(b, &f); err != nil {
		return err
	}
	extra, err := unknownFields(b, &f)
	if err != nil {
		return err
	}
	*p = ForwardProxy(f)
	p.extra = extra
	return nil
}

func (p ForwardProxy) MarshalJSON() ([]byte, error) {
	return marshalWithExtra(forwardFields(p), p.extra)
}

// Validate reports problems only when the proxy is in use.
func (p ForwardProxy) Validate() []FieldError {
	if !p.UseProxy {
		return nil
	}
	var errs []FieldError
	if p.ProxyType != ProxySOCKS5 && p.ProxyType != ProxyHTTP {
		errs = append(errs, FieldError{"proxyType", "SOCKS5 or HTTP."})
	}
	if strings.TrimSpace(p.ProxyServer) == "" {
		errs = append(errs, FieldError{"proxyServer", "Required."})
	}
	if p.ProxyPort < 1 || p.ProxyPort > 65535 {
		errs = append(errs, FieldError{"proxyPort", "1–65535"})
	}
	if p.ProxyTimeout < 1 || p.ProxyTimeout > MaxProxyTimeout {
		errs = append(errs, FieldError{"proxyTimeout", "1–10 seconds."})
	}
	if p.UseAuth {
		if p.AuthUser == "" {
			errs = append(errs, FieldError{"authUser", "Required."})
		}
		if p.ProxyType == ProxyHTTP && strings.Contains(p.AuthUser, ":") {
			errs = append(errs, FieldError{"authUser", "No ':' in an HTTP user name."}) // RFC 7617
		}
		if p.ProxyType == ProxySOCKS5 { // RFC 1929: one length byte each
			if len(p.AuthUser) > 255 {
				errs = append(errs, FieldError{"authUser", "At most 255 bytes."})
			}
			if len(p.AuthPwd) > 255 {
				errs = append(errs, FieldError{"authPwd", "At most 255 bytes."})
			}
		}
	}
	return errs
}

// LogValue keeps the password out of logs.
func (p ForwardProxy) LogValue() slog.Value {
	return slog.GroupValue(
		slog.Bool("useProxy", p.UseProxy), slog.Int("type", p.ProxyType),
		slog.String("server", p.ProxyServer), slog.Int("port", p.ProxyPort),
		slog.Bool("useAuth", p.UseAuth), slog.String("user", p.AuthUser), slog.String("password", "***"),
	)
}

// TrustedPlugin is a plugin program the user allowed to run.
type TrustedPlugin struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}

// Trusted reports whether path with this hash was allowed. Windows paths and
// hex digests compare case-insensitively.
func (c *Config) Trusted(path, sha string) bool {
	p := filepath.Clean(path)
	for _, t := range c.TrustedPlugins {
		if strings.EqualFold(filepath.Clean(t.Path), p) && strings.EqualFold(t.SHA256, sha) {
			return true
		}
	}
	return false
}

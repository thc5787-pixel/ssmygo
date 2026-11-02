package config

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"slices"
	"strconv"
	"strings"
)

// Methods lists the supported encryption methods in UI order.
var Methods = []string{
	"aes-128-gcm", "aes-192-gcm", "aes-256-gcm",
	"chacha20-ietf-poly1305", "xchacha20-ietf-poly1305",
	"2022-blake3-aes-128-gcm", "2022-blake3-aes-256-gcm",
	"none", "plain",
}

// DefaultMethod is the method of a new server (original Server.DefaultMethod).
const DefaultMethod = "chacha20-ietf-poly1305"

// Server is one entry of gui-config.json "configs", field names as in the
// original Model/Server.cs. Unknown fields are preserved.
type Server struct {
	Server     string `json:"server"`
	ServerPort int    `json:"server_port"`
	Password   string `json:"password"`
	Method     string `json:"method"`
	Plugin     string `json:"plugin"`
	PluginOpts string `json:"plugin_opts"`
	PluginArgs string `json:"plugin_args"`
	Remarks    string `json:"remarks"`
	Group      string `json:"group"`
	Timeout    int    `json:"timeout"`

	extra map[string]json.RawMessage
}

type serverFields Server // no methods: avoids recursion in (Un)MarshalJSON

func (s *Server) UnmarshalJSON(b []byte) error {
	var f serverFields
	if err := json.Unmarshal(b, &f); err != nil {
		return err
	}
	extra, err := unknownFields(b, &f)
	if err != nil {
		return err
	}
	*s = Server(f)
	s.extra = extra
	return nil
}

func (s Server) MarshalJSON() ([]byte, error) {
	return marshalWithExtra(serverFields(s), s.extra)
}

// Title is the name shown in menus and lists, as the original's
// FriendlyName: "remarks (host:port)", or "host:port" without remarks (an
// IPv6 address in brackets).
func (s Server) Title() string {
	addr := net.JoinHostPort(strings.TrimSpace(s.Server), strconv.Itoa(s.ServerPort))
	if s.Remarks != "" {
		return s.Remarks + " (" + addr + ")"
	}
	return addr
}

// MaxTimeout is the largest server timeout in seconds
// (original Server.MaxServerTimeoutSec).
const MaxTimeout = 20

// FieldError is one validation problem; Field is the JSON field name.
type FieldError struct{ Field, Msg string }

func (e FieldError) Error() string { return e.Field + ": " + e.Msg }

// PSKSize returns the key size of an SS2022 method, or 0 for other methods.
func PSKSize(method string) int {
	switch method {
	case "2022-blake3-aes-128-gcm":
		return 16
	case "2022-blake3-aes-256-gcm":
		return 32
	}
	return 0
}

// Validate reports every problem with s; empty means valid.
func (s Server) Validate() []FieldError {
	var errs []FieldError
	if strings.TrimSpace(s.Server) == "" {
		errs = append(errs, FieldError{"server", "Required."})
	}
	if s.ServerPort < 1 || s.ServerPort > 65535 {
		errs = append(errs, FieldError{"server_port", "1–65535"})
	}
	if !slices.Contains(Methods, s.Method) {
		errs = append(errs, FieldError{"method", "Unsupported encryption."})
	} else if n := PSKSize(s.Method); n > 0 {
		if b, err := base64.StdEncoding.DecodeString(s.Password); err != nil || len(b) != n {
			errs = append(errs, FieldError{"password", fmt.Sprintf("Base64 key of %d bytes.", n)})
		}
	}
	if s.Timeout < 0 || s.Timeout > MaxTimeout {
		errs = append(errs, FieldError{"timeout", "0–20 seconds."})
	}
	return errs
}

// LogValue keeps passwords out of logs.
func (s Server) LogValue() slog.Value {
	return slog.GroupValue(
		slog.String("server", s.Server),
		slog.Int("port", s.ServerPort),
		slog.String("method", s.Method),
		slog.String("remarks", s.Remarks),
		slog.String("password", "***"),
	)
}

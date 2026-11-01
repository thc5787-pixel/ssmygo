package config

import (
	"bytes"
	"encoding/base64"
	"log/slog"
	"strings"
	"testing"
)

func TestValidate(t *testing.T) {
	psk16 := base64.StdEncoding.EncodeToString(make([]byte, 16))
	psk32 := base64.StdEncoding.EncodeToString(make([]byte, 32))
	ok := Server{Server: "a.com", ServerPort: 8388, Method: DefaultMethod, Password: "p", Timeout: 5}
	cases := []struct {
		name  string
		mod   func(*Server)
		field string // "" = valid
	}{
		{"valid legacy", func(s *Server) {}, ""},
		{"empty server", func(s *Server) { s.Server = " " }, "server"},
		{"port 0", func(s *Server) { s.ServerPort = 0 }, "server_port"},
		{"port 65536", func(s *Server) { s.ServerPort = 65536 }, "server_port"},
		{"2022-128 16B", func(s *Server) { s.Method, s.Password = "2022-blake3-aes-128-gcm", psk16 }, ""},
		{"2022-128 32B", func(s *Server) { s.Method, s.Password = "2022-blake3-aes-128-gcm", psk32 }, "password"},
		{"2022-256 not b64", func(s *Server) { s.Method, s.Password = "2022-blake3-aes-256-gcm", "not base64!" }, "password"},
		{"2022-256 32B", func(s *Server) { s.Method, s.Password = "2022-blake3-aes-256-gcm", psk32 }, ""},
		{"timeout -1", func(s *Server) { s.Timeout = -1 }, "timeout"},
		{"unknown method", func(s *Server) { s.Method = "rc4-md5" }, "method"},
		{"none any password", func(s *Server) { s.Method, s.Password = "none", "" }, ""},
	}
	for _, tc := range cases {
		s := ok
		tc.mod(&s)
		errs := s.Validate()
		if tc.field == "" && len(errs) != 0 {
			t.Errorf("%s: unexpected %v", tc.name, errs)
		}
		if tc.field != "" && !hasField(errs, tc.field) {
			t.Errorf("%s: want error on %s, got %v", tc.name, tc.field, errs)
		}
	}
}

func TestPSKMessage(t *testing.T) {
	s := Server{Server: "a", ServerPort: 1, Method: "2022-blake3-aes-256-gcm"}
	for _, e := range s.Validate() {
		if e.Field == "password" && e.Msg == "Base64 key of 32 bytes." {
			return
		}
	}
	t.Fatalf("got %v", s.Validate())
}

func TestMethodsOrder(t *testing.T) {
	want := "aes-128-gcm aes-192-gcm aes-256-gcm chacha20-ietf-poly1305 xchacha20-ietf-poly1305 2022-blake3-aes-128-gcm 2022-blake3-aes-256-gcm none plain"
	if got := strings.Join(Methods, " "); got != want {
		t.Fatalf("Methods = %s", got)
	}
}

func TestTitle(t *testing.T) {
	if (Server{Remarks: "Tokyo", Server: "a", ServerPort: 1}).Title() != "Tokyo" {
		t.Fatal("remarks")
	}
	if (Server{Server: "a", ServerPort: 1}).Title() != "a:1" {
		t.Fatal("host:port")
	}
}

func TestLogValueRedacts(t *testing.T) {
	var buf bytes.Buffer
	l := slog.New(slog.NewJSONHandler(&buf, nil))
	l.Info("x", "server", Server{Server: "a", ServerPort: 1, Password: "hunter2-secret", Method: "aes-256-gcm"})
	if strings.Contains(buf.String(), "hunter2") {
		t.Fatalf("password leaked: %s", buf.String())
	}
	if !strings.Contains(buf.String(), "aes-256-gcm") {
		t.Fatalf("method missing: %s", buf.String())
	}
}

package config

import (
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
)

// DefaultTimeout is the timeout of a new or imported server
// (original Server.DefaultServerTimeoutSec).
const DefaultTimeout = 5

// ParseURLs extracts every ss:// link in text (separated by any whitespace).
// Links that fail to parse are reported in errs; text without links yields
// neither servers nor errors.
func ParseURLs(text string) (servers []Server, errs []error) {
	n := 0
	for _, tok := range strings.Fields(text) {
		if len(tok) < 5 || !strings.EqualFold(tok[:5], "ss://") {
			continue
		}
		n++
		s, err := parseURL(tok)
		if err != nil {
			// Never echo the link: its userinfo is the password.
			errs = append(errs, fmt.Errorf("ss:// link %d: %w", n, err))
			continue
		}
		servers = append(servers, s)
	}
	return servers, errs
}

func parseURL(link string) (Server, error) {
	body, frag, _ := strings.Cut(link[5:], "#")
	remarks, err := url.PathUnescape(frag)
	if err != nil {
		return Server{}, err
	}
	s := Server{Remarks: remarks, Timeout: DefaultTimeout}
	if strings.Contains(body, "@") {
		err = parseSIP002(body, &s)
	} else {
		err = parseLegacy(body, &s)
	}
	if err != nil {
		return Server{}, err
	}
	s.Method = strings.ToLower(s.Method)
	if s.ServerPort < 1 || s.ServerPort > 65535 {
		return Server{}, errors.New("port out of range")
	}
	if s.Server == "" || s.Method == "" {
		return Server{}, errors.New("missing host or method")
	}
	return s, nil
}

// parseSIP002 handles ss://userinfo@host:port[/][?plugin=...]. The userinfo
// is split off by hand: standard base64 and unescaped SS2022 keys may
// contain '/', '+' and '=', which url.Parse would treat as structure.
func parseSIP002(body string, s *Server) error {
	at := strings.LastIndexByte(body, '@')
	userinfo, rest := body[:at], body[at+1:]
	if plain, err := url.PathUnescape(userinfo); err == nil && strings.Contains(plain, ":") {
		s.Method, s.Password, _ = strings.Cut(plain, ":") // SIP002 plain userinfo (SS2022)
	} else {
		dec, err := decodeBase64(userinfo)
		if err != nil {
			return errors.New("userinfo is neither method:password nor base64")
		}
		m, p, ok := strings.Cut(string(dec), ":")
		if !ok {
			return errors.New("userinfo is not method:password")
		}
		s.Method, s.Password = m, p
	}
	u, err := url.Parse("ss://" + rest)
	if err != nil {
		return errors.New("bad host or port")
	}
	s.Server = u.Hostname()
	if s.ServerPort, err = strconv.Atoi(u.Port()); err != nil {
		return errors.New("bad port")
	}
	if plugin := u.Query().Get("plugin"); plugin != "" {
		s.Plugin, s.PluginOpts, _ = strings.Cut(plugin, ";")
	}
	return nil
}

// parseLegacy handles ss://base64(method:password@host:port).
func parseLegacy(body string, s *Server) error {
	dec, err := decodeBase64(body)
	if err != nil {
		return err
	}
	at := strings.LastIndexByte(string(dec), '@')
	if at < 0 {
		return errors.New("missing @")
	}
	m, p, ok := strings.Cut(string(dec[:at]), ":")
	if !ok {
		return errors.New("missing method:password")
	}
	host, port, err := net.SplitHostPort(string(dec[at+1:]))
	if err != nil {
		return err
	}
	s.Method, s.Password, s.Server = m, p, host
	if s.ServerPort, err = strconv.Atoi(port); err != nil {
		return errors.New("bad port")
	}
	return nil
}

// decodeBase64 accepts the standard and URL alphabets, padded or not.
func decodeBase64(s string) ([]byte, error) {
	s = strings.TrimRight(s, "=")
	if b, err := base64.RawURLEncoding.DecodeString(s); err == nil {
		return b, nil
	}
	return base64.RawStdEncoding.DecodeString(s)
}

// FormatURL returns the SIP002 link of s: base64url userinfo for legacy
// methods, percent-encoded plain userinfo for SS2022.
func FormatURL(s Server) string {
	var userinfo string
	if PSKSize(s.Method) > 0 {
		userinfo = url.UserPassword(s.Method, s.Password).String()
	} else {
		userinfo = base64.RawURLEncoding.EncodeToString([]byte(s.Method + ":" + s.Password))
	}
	var b strings.Builder
	b.WriteString("ss://")
	b.WriteString(userinfo)
	b.WriteByte('@')
	b.WriteString(net.JoinHostPort(s.Server, strconv.Itoa(s.ServerPort)))
	if s.Plugin != "" {
		plugin := s.Plugin
		if s.PluginOpts != "" {
			plugin += ";" + s.PluginOpts
		}
		b.WriteString("/?plugin=")
		b.WriteString(url.QueryEscape(plugin))
	}
	if s.Remarks != "" {
		b.WriteByte('#')
		b.WriteString(url.PathEscape(s.Remarks))
	}
	return b.String()
}

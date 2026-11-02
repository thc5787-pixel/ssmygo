package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"slices"
)

// MaxSubscriptionServers caps one subscription.
const MaxSubscriptionServers = 1000

// ErrNoServers: a subscription response held no server.
var ErrNoServers = errors.New("no servers in response")

var sip008Fields = []string{"server", "server_port", "password", "method"}

// ParseSIP008 reads a SIP008 online config. Like the original client it
// accepts any JSON holding objects with server, server_port, password and
// method, wherever they are. Each server gets group; invalid ones and those
// beyond MaxSubscriptionServers are skipped and counted.
func ParseSIP008(b []byte, group string) (servers []Server, skipped int, err error) {
	d := json.NewDecoder(bytes.NewReader(b))
	d.UseNumber()
	var root any
	if err := d.Decode(&root); err != nil {
		return nil, 0, errors.New("response is not JSON")
	}
	var walk func(v any)
	walk = func(v any) {
		switch v := v.(type) {
		case []any:
			for _, e := range v {
				walk(e)
			}
		case map[string]any:
			if hasAll(v) {
				raw, _ := json.Marshal(v)
				var s Server
				if json.Unmarshal(raw, &s) != nil || len(s.Validate()) > 0 || len(servers) >= MaxSubscriptionServers {
					skipped++
					return
				}
				s.Group = group
				servers = append(servers, s)
				return
			}
			keys := make([]string, 0, len(v))
			for k := range v {
				keys = append(keys, k)
			}
			slices.Sort(keys) // map order is random; keep results stable
			for _, k := range keys {
				walk(v[k])
			}
		}
	}
	walk(root)
	if len(servers) == 0 {
		return nil, skipped, ErrNoServers
	}
	return servers, skipped, nil
}

func hasAll(m map[string]any) bool {
	for _, f := range sip008Fields {
		if _, ok := m[f]; !ok {
			return false
		}
	}
	return true
}

// SortByGroup puts the user's own servers (no group) first, then each
// subscription's servers, groups in order of first appearance — the
// original's SortByOnlineConfig.
func SortByGroup(servers []Server) []Server {
	out := make([]Server, 0, len(servers))
	var groups []string
	byGroup := map[string][]Server{}
	for _, s := range servers {
		if s.Group == "" {
			out = append(out, s)
			continue
		}
		if _, ok := byGroup[s.Group]; !ok {
			groups = append(groups, s.Group)
		}
		byGroup[s.Group] = append(byGroup[s.Group], s)
	}
	for _, g := range groups {
		out = append(out, byGroup[g]...)
	}
	return out
}

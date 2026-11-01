// Package rules turns v2fly geosite (dlc.dat) groups into shadowsocks-go
// domain sets for rule-based routing ("PAC" mode).
package rules

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"google.golang.org/protobuf/encoding/protowire"
)

// DomainType is v2fly's Domain.Type enum.
type DomainType int

const (
	Plain      DomainType = 0 // keyword
	Regex      DomainType = 1
	RootDomain DomainType = 2 // the domain and its subdomains
	Full       DomainType = 3 // exactly this domain
)

// Domain is one rule of a geosite group.
type Domain struct {
	Type  DomainType
	Value string
	Attrs []string
}

var errMalformed = errors.New("rules: malformed dlc.dat")

// ParseDLC decodes a GeoSiteList. Keys are lowercased country codes.
//
//	GeoSiteList { repeated GeoSite entry = 1; }
//	GeoSite     { string country_code = 1; repeated Domain domain = 2; }
//	Domain      { Type type = 1; string value = 2; repeated Attribute attribute = 3; }
//	Attribute   { string key = 1; oneof { bool bool_value = 2; int64 int_value = 3; } }
func ParseDLC(data []byte) (map[string][]Domain, error) { return parseDLC(data, nil) }

// parseDLC decodes only the groups in want (all when want is nil); other
// entries are skipped without allocating, which keeps a cold build small.
// country_code precedes the domains in v2fly's files, so it is known in time.
func parseDLC(data []byte, want map[string]bool) (map[string][]Domain, error) {
	out := map[string][]Domain{}
	err := eachField(data, func(num protowire.Number, typ protowire.Type, v []byte, _ uint64) error {
		if num != 1 || typ != protowire.BytesType {
			return nil
		}
		var code string
		var domains []Domain
		skip := false
		err := eachField(v, func(num protowire.Number, typ protowire.Type, v []byte, _ uint64) error {
			switch {
			case num == 1 && typ == protowire.BytesType:
				code = strings.ToLower(string(v))
				skip = want != nil && !want[code]
			case num == 2 && typ == protowire.BytesType && !skip:
				d, err := parseDomain(v)
				if err != nil {
					return err
				}
				domains = append(domains, d)
			}
			return nil
		})
		if err != nil {
			return err
		}
		if !skip {
			out[code] = append(out[code], domains...)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

func parseDomain(b []byte) (Domain, error) {
	var d Domain
	err := eachField(b, func(num protowire.Number, typ protowire.Type, v []byte, n uint64) error {
		switch {
		case num == 1 && typ == protowire.VarintType:
			d.Type = DomainType(n)
		case num == 2 && typ == protowire.BytesType:
			d.Value = string(v)
		case num == 3 && typ == protowire.BytesType:
			return eachField(v, func(num protowire.Number, typ protowire.Type, v []byte, _ uint64) error {
				if num == 1 && typ == protowire.BytesType {
					d.Attrs = append(d.Attrs, strings.ToLower(string(v)))
				}
				return nil
			})
		}
		return nil
	})
	return d, err
}

// eachField walks the top-level fields of a protobuf message. For bytes
// fields v is the payload; for varints n is the value.
func eachField(b []byte, fn func(num protowire.Number, typ protowire.Type, v []byte, n uint64) error) error {
	for len(b) > 0 {
		num, typ, l := protowire.ConsumeTag(b)
		if l < 0 {
			return errMalformed
		}
		b = b[l:]
		var v []byte
		var n uint64
		switch typ {
		case protowire.BytesType:
			v, l = protowire.ConsumeBytes(b)
		case protowire.VarintType:
			n, l = protowire.ConsumeVarint(b)
		default:
			l = protowire.ConsumeFieldValue(num, typ, b)
		}
		if l < 0 {
			return errMalformed
		}
		b = b[l:]
		if err := fn(num, typ, v, n); err != nil {
			return err
		}
	}
	return nil
}

// Select returns the domains of groups. A group is "name" or "name@attr"
// (only domains carrying attr); names are case-insensitive.
func Select(list map[string][]Domain, groups []string) ([]Domain, error) {
	var out []Domain
	for _, g := range groups {
		name, attr, hasAttr := strings.Cut(strings.ToLower(g), "@")
		ds, ok := list[name]
		if !ok {
			return nil, fmt.Errorf("rules: unknown geosite group %q", g)
		}
		for _, d := range ds {
			if !hasAttr || slices.Contains(d.Attrs, attr) {
				out = append(out, d)
			}
		}
	}
	return out, nil
}

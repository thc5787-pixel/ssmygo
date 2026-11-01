package config

import (
	"encoding/base64"
	"reflect"
	"testing"
)

func TestParseURLs(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want []Server
		nerr int
	}{
		{"sip002 legacy", "ss://Y2hhY2hhMjAtaWV0Zi1wb2x5MTMwNTpwYXNz@1.2.3.4:8388#Tokyo",
			[]Server{{Server: "1.2.3.4", ServerPort: 8388, Method: "chacha20-ietf-poly1305", Password: "pass", Remarks: "Tokyo"}}, 0},
		{"sip002 plugin", "ss://Y2hhY2hhMjAtaWV0Zi1wb2x5MTMwNTpwYXNz@h.com:443/?plugin=v2ray-plugin%3Btls%3Bhost%3Da.com#x",
			[]Server{{Server: "h.com", ServerPort: 443, Method: "chacha20-ietf-poly1305", Password: "pass", Plugin: "v2ray-plugin", PluginOpts: "tls;host=a.com", Remarks: "x"}}, 0},
		{"sip002 2022 plain userinfo", "ss://2022-blake3-aes-128-gcm:YctPZ6U7xPPcU%2Bgp3u%2B0tx%2FtRizJN9K8y%2BuKlW2qjlI%3D@[::1]:8888#k",
			[]Server{{Server: "::1", ServerPort: 8888, Method: "2022-blake3-aes-128-gcm", Password: "YctPZ6U7xPPcU+gp3u+0tx/tRizJN9K8y+uKlW2qjlI=", Remarks: "k"}}, 0},
		{"legacy whole base64", "ss://YWVzLTI1Ni1nY206cGFzc0AxMjcuMC4wLjE6ODA4OA#n",
			[]Server{{Server: "127.0.0.1", ServerPort: 8088, Method: "aes-256-gcm", Password: "pass", Remarks: "n"}}, 0},
		{"legacy padded", "ss://YWVzLTI1Ni1nY206cGFzc0AxMjcuMC4wLjE6ODA4OA==",
			[]Server{{Server: "127.0.0.1", ServerPort: 8088, Method: "aes-256-gcm", Password: "pass"}}, 0},
		{"crlf spaces chinese remarks", "  ss://Y2hhY2hhMjAtaWV0Zi1wb2x5MTMwNTpwYXNz@1.2.3.4:1#%E4%B8%9C%E4%BA%AC  \r\n\r\n ss://YWVzLTI1Ni1nY206cGFzc0AxMjcuMC4wLjE6ODA4OA \r\n",
			[]Server{
				{Server: "1.2.3.4", ServerPort: 1, Method: "chacha20-ietf-poly1305", Password: "pass", Remarks: "东京"},
				{Server: "127.0.0.1", ServerPort: 8088, Method: "aes-256-gcm", Password: "pass"},
			}, 0},
		{"uppercase method", "ss://" + base64.RawURLEncoding.EncodeToString([]byte("AES-256-GCM:p")) + "@a:1",
			[]Server{{Server: "a", ServerPort: 1, Method: "aes-256-gcm", Password: "p"}}, 0},
		{"url alphabet userinfo", "ss://" + base64.RawURLEncoding.EncodeToString([]byte("aes-128-gcm:>>>???")) + "@a:2",
			[]Server{{Server: "a", ServerPort: 2, Method: "aes-128-gcm", Password: ">>>???"}}, 0},
		{"garbage plus valid", "ss://!!!\nss://Y2hhY2hhMjAtaWV0Zi1wb2x5MTMwNTpwYXNz@1.2.3.4:8388",
			[]Server{{Server: "1.2.3.4", ServerPort: 8388, Method: "chacha20-ietf-poly1305", Password: "pass"}}, 1},
		{"no links", "hello world", nil, 0},
		{"bad port", "ss://Y2hhY2hhMjAtaWV0Zi1wb2x5MTMwNTpwYXNz@1.2.3.4:99999", nil, 1},
	}
	for _, tc := range cases {
		for i := range tc.want {
			tc.want[i].Timeout = DefaultTimeout // original default for new servers
		}
		got, errs := ParseURLs(tc.in)
		if !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%s:\n got %+v\nwant %+v", tc.name, got, tc.want)
		}
		if len(errs) != tc.nerr {
			t.Errorf("%s: errs %v, want %d", tc.name, errs, tc.nerr)
		}
	}
}

func TestFormatParseRoundTrip(t *testing.T) {
	psk := map[int]string{
		16: base64.StdEncoding.EncodeToString([]byte("0123456789abcdef")),
		32: base64.StdEncoding.EncodeToString([]byte("0123456789abcdef0123456789abcdef")),
	}
	for _, m := range Methods {
		s := Server{Server: "ex.com", ServerPort: 8388, Method: m, Password: "p@ss:w/rd#?", Remarks: "东京 1",
			Plugin: "obfs-local", PluginOpts: "obfs=http;obfs-host=a.com", Timeout: DefaultTimeout}
		if n := PSKSize(m); n > 0 {
			s.Password = psk[n]
		}
		for _, srv := range []string{"ex.com", "1.2.3.4", "::1"} {
			s.Server = srv
			got, errs := ParseURLs(FormatURL(s))
			if len(errs) != 0 || len(got) != 1 || !reflect.DeepEqual(got[0], s) {
				t.Errorf("%s %s: %s →\n got %+v %v\nwant %+v", m, srv, FormatURL(s), got, errs, s)
			}
		}
	}
}

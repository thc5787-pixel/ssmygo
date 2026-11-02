package config

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
)

const sip008Doc = `{"version":1,"servers":[
 {"id":"27b8a625-4f4b-4428-9f0f-8a2317db7c79","remarks":"Tokyo","server":"jp.example.com","server_port":8388,
  "password":"pw","method":"chacha20-ietf-poly1305","plugin":"v2ray-plugin","plugin_opts":"mode=websocket"},
 {"id":"2","remarks":"HK","server":"hk.example.com","server_port":443,"password":"pw2","method":"aes-256-gcm"}],
 "bytes_used":274877906944,"bytes_remaining":824633720832}`

func TestSIP008Official(t *testing.T) {
	s, skipped, err := ParseSIP008([]byte(sip008Doc), "https://sub.example.com/a")
	if err != nil || skipped != 0 || len(s) != 2 {
		t.Fatalf("%d servers, %d skipped, %v", len(s), skipped, err)
	}
	if s[0].Remarks != "Tokyo" || s[0].Plugin != "v2ray-plugin" || s[0].PluginOpts != "mode=websocket" ||
		s[0].Group != "https://sub.example.com/a" || s[1].Server != "hk.example.com" || s[1].ServerPort != 443 {
		t.Fatalf("%+v", s)
	}
}

// As the original's resolver: servers anywhere in the JSON.
func TestSIP008Permissive(t *testing.T) {
	doc := `{"a":{"b":[{"server":"x.example","server_port":1,"password":"p","method":"aes-128-gcm"}]},
	 "c":[[{"server":"y.example","server_port":2,"password":"p","method":"aes-128-gcm"}]]}`
	s, _, err := ParseSIP008([]byte(doc), "g")
	if err != nil || len(s) != 2 {
		t.Fatalf("%+v %v", s, err)
	}
}

func TestSIP008SkipsInvalid(t *testing.T) {
	doc := `[{"server":"ok.example","server_port":1,"password":"p","method":"aes-128-gcm"},
	 {"server":"bad.example","server_port":0,"password":"p","method":"aes-128-gcm"},
	 {"server":"old.example","server_port":1,"password":"p","method":"rc4-md5"}]`
	s, skipped, err := ParseSIP008([]byte(doc), "g")
	if err != nil || len(s) != 1 || skipped != 2 {
		t.Fatalf("%d servers, %d skipped, %v", len(s), skipped, err)
	}
}

func TestSIP008Cap(t *testing.T) {
	var b strings.Builder
	b.WriteString("[")
	for i := range 1005 {
		if i > 0 {
			b.WriteString(",")
		}
		fmt.Fprintf(&b, `{"server":"s%d.example","server_port":1,"password":"p","method":"aes-128-gcm"}`, i)
	}
	b.WriteString("]")
	s, skipped, err := ParseSIP008([]byte(b.String()), "g")
	if err != nil || len(s) != 1000 || skipped != 5 {
		t.Fatalf("%d servers, %d skipped, %v", len(s), skipped, err)
	}
}

func TestSIP008Errors(t *testing.T) {
	for _, doc := range []string{"<html>login</html>", "{}", "[]", `{"servers":[]}`} {
		if _, _, err := ParseSIP008([]byte(doc), "g"); err == nil {
			t.Errorf("%q accepted", doc)
		}
	}
}

func TestSortByGroup(t *testing.T) {
	in := []Server{{Remarks: "b1", Group: "B"}, {Remarks: "own1"}, {Remarks: "a1", Group: "A"}, {Remarks: "own2"}, {Remarks: "b2", Group: "B"}}
	var got []string
	for _, s := range SortByGroup(in) {
		got = append(got, s.Remarks)
	}
	if strings.Join(got, ",") != "own1,own2,b1,b2,a1" {
		t.Fatalf("%v", got)
	}
}

func TestOnlineConfigSourceRoundTrip(t *testing.T) {
	c, p := loadString(t, `{"configs":[],"onlineConfigSource":["https://a.example/s","https://b.example/s"]}`)
	if len(c.OnlineConfigSource) != 2 || c.OnlineConfigSource[1] != "https://b.example/s" {
		t.Fatalf("%v", c.OnlineConfigSource)
	}
	c.OnlineConfigSource = c.OnlineConfigSource[:1]
	c.Save(p)
	c2, _ := Load(p)
	if len(c2.OnlineConfigSource) != 1 {
		t.Fatalf("%v", c2.OnlineConfigSource)
	}
}

func TestOnlineConfigSourceNeverNull(t *testing.T) {
	c, p := loadString(t, `{"configs":[],"onlineConfigSource":null}`)
	c.Save(p)
	if k := topKeys(t, p); string(k["onlineConfigSource"]) != "[]" {
		t.Fatalf("saved %s", k["onlineConfigSource"])
	}
}

func topKeys(t *testing.T, path string) map[string]json.RawMessage {
	t.Helper()
	b, _ := os.ReadFile(path)
	var m map[string]json.RawMessage
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

package core

import (
	"encoding/json"
	"errors"
	"flag"
	"os"
	"path/filepath"
	"testing"

	"github.com/thc5787/ssmygo/internal/config"
	"github.com/thc5787/ssmygo/internal/rules"
)

var update = flag.Bool("update", false, "rewrite golden files")

func sampleConfig() *config.Config {
	c := config.Default()
	c.Configs = []config.Server{{Server: "ss.example.com", ServerPort: 8388, Method: "chacha20-ietf-poly1305", Password: "pw", Timeout: 5}}
	c.Enabled = true
	return c
}

var sampleRules = &rules.Result{Direct: `C:\data\cache\domainsets\direct.gob`, Proxied: `C:\data\cache\domainsets\proxied.gob`}

func TestBuildGolden(t *testing.T) {
	cases := map[string]func() (*config.Config, *rules.Result){
		"global": func() (*config.Config, *rules.Result) {
			c := sampleConfig()
			c.Global = true
			return c, sampleRules
		},
		"rules": func() (*config.Config, *rules.Result) { return sampleConfig(), sampleRules },
		"rules-preferdirect": func() (*config.Config, *rules.Result) {
			c := sampleConfig()
			c.GeositePreferDirect = true
			return c, sampleRules
		},
		"lan": func() (*config.Config, *rules.Result) {
			c := sampleConfig()
			c.ShareOverLan, c.LocalPort = true, 2080
			return c, sampleRules
		},
		"ss2022": func() (*config.Config, *rules.Result) {
			c := sampleConfig()
			c.Configs[0].Method, c.Configs[0].Password = "2022-blake3-aes-128-gcm", "AAECAwQFBgcICQoLDA0ODw=="
			c.Global = true
			return c, nil
		},
	}
	for name, mk := range cases {
		cfg, rs := mk()
		sc, err := Build(cfg, rs)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		got, _ := json.MarshalIndent(sc, "", "  ")
		path := filepath.Join("testdata", "golden", name+".json")
		if *update {
			os.MkdirAll(filepath.Dir(path), 0o755)
			os.WriteFile(path, append(got, '\n'), 0o644)
			continue
		}
		want, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if string(want) != string(got)+"\n" {
			t.Errorf("%s: got\n%s", name, got)
		}
	}
}

func TestBuildNoServer(t *testing.T) {
	c := config.Default()
	sc, err := Build(c, sampleRules)
	if !errors.Is(err, ErrNoServer) {
		t.Fatalf("err = %v", err)
	}
	if sc == nil || len(sc.Servers) != 1 || len(sc.Clients) != 1 || sc.Clients[0].Name != "direct" ||
		sc.Router.DefaultTCPClientName != "direct" {
		t.Fatalf("config %+v", sc)
	}
}

func TestBuildInvalidServerIsNoServer(t *testing.T) {
	c := sampleConfig()
	c.Configs[0].ServerPort = 0
	if _, err := Build(c, nil); !errors.Is(err, ErrNoServer) {
		t.Fatalf("err = %v", err)
	}
}

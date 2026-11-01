// Command gen writes internal/i18n/strings_{en,zh-Hans}.json from the
// original shadowsocks-windows Data/i18n.csv plus ssmygo's own strings.
//
//	go run ./internal/i18n/gen -csv <upstream>/shadowsocks-csharp/Data/i18n.csv -out internal/i18n
package main

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"flag"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// keys are the original strings P0 uses (en text as in i18n.csv).
var keys = []string{
	"System Proxy", "Disable", "PAC", "Global", "Servers", "Edit Servers...",
	"Import URL from Clipboard...", "Start on Boot", "Allow other Devices to connect", "Quit",
	"Edit Servers", "Server", "Server IP", "Server Port", "Password", "Show Password", "Encryption",
	"Plugin Program", "Plugin Options", "Need Plugin Argument", "Plugin Arguments", "Remarks",
	"Timeout(Sec)", "&Add", "&Delete", "Dupli&cate", "Move &Up", "Move D&own", "Proxy Port",
	"Portable Mode", "OK", "Cancel", "Apply", "System Proxy On: ", "System Proxy Disabled", "Shadowsocks",
}

// extra are strings the original lacks: key → {en, zh-Hans}.
var extra = map[string][2]string{
	"Group":                    {"Group", "分组"},
	"ssmygo.imported":          {"Imported %d, failed %d.", "已导入 %d 个，失败 %d 个。"},
	"ssmygo.noLinks":           {"No ss:// links found in the clipboard.", "剪贴板中没有找到 ss:// 链接。"},
	"ssmygo.portInUse":         {"Port %d is in use by another program.", "端口 %d 已被其他程序占用。"},
	"ssmygo.startFailed":       {"ssmygo could not start the proxy: %s", "ssmygo 无法启动代理：%s"},
	"ssmygo.noServer":          {"Add a server first.", "请先添加服务器。"},
	"ssmygo.cannotSave":        {"Could not save: %s", "无法保存：%s"},
	"ssmygo.pluginUnsupported": {"Plugins are not supported in this version.", "此版本暂不支持插件。"},
}

var mnemonic = regexp.MustCompile(`\s?\(&\w\)|&`)

func clean(s string) string { return mnemonic.ReplaceAllString(s, "") }

func main() {
	csvPath := flag.String("csv", "", "original Data/i18n.csv")
	out := flag.String("out", "internal/i18n", "output directory")
	flag.Parse()
	b, err := os.ReadFile(*csvPath)
	if err != nil {
		log.Fatal(err)
	}
	r := csv.NewReader(bytes.NewReader(bytes.TrimPrefix(b, []byte{0xEF, 0xBB, 0xBF})))
	r.FieldsPerRecord = -1
	rows, err := r.ReadAll()
	if err != nil {
		log.Fatal(err)
	}
	zhCol := -1
	for i, h := range rows[0] {
		if h == "zh-CN" {
			zhCol = i
		}
	}
	byKey := map[string][]string{}
	for _, row := range rows[1:] {
		if len(row) > zhCol && !strings.HasPrefix(row[0], "#") {
			byKey[row[0]] = row
		}
	}
	en, zh := map[string]string{}, map[string]string{}
	for _, k := range keys {
		row, ok := byKey[k]
		if !ok {
			log.Fatalf("key %q not in %s", k, *csvPath)
		}
		ck := clean(k)
		en[ck], zh[ck] = clean(row[0]), clean(row[zhCol])
	}
	for k, v := range extra {
		en[k], zh[k] = v[0], v[1]
	}
	for name, m := range map[string]map[string]string{"strings_en.json": en, "strings_zh-Hans.json": zh} {
		j, _ := json.MarshalIndent(m, "", "  ")
		if err := os.WriteFile(filepath.Join(*out, name), append(j, '\n'), 0o644); err != nil {
			log.Fatal(err)
		}
	}
}

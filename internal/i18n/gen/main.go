// Command gen writes internal/i18n/strings_{en,zh-Hans}.json from the
// original shadowsocks-windows Data/i18n.csv plus ssmygo's own strings.
//
//	go run ./internal/i18n/gen -csv <upstream>/shadowsocks-csharp/Data/i18n.csv -out internal/i18n
package main

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"encoding/xml"
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
	"Forward Proxy...",
	"Help", "Show Logs...", "Show Plugin Output",
	"Log Viewer", "&File", "&Open Location", "E&xit", "&View", "&Clear Logs", "Change &Font", "&Wrap Text",
	"&Top Most", "&Show Toolbar", "Inbound", "Outbound",
	"Online Config...", "Share Server Config...", "Scan QRCode from Screen...",
	"No QRCode found. Try to zoom in or move it to the center of the screen.", "Invalid QR Code content: {0}",
	"Update Local PAC from Geosite", "Edit User Rule for Geosite...", "PAC updated",
	"No updates found. Please report to Geosite if you have problems with it.",
}

// resxNames are WPF strings (Localization/Strings*.resx), stored as "resx.<name>".
var resxNames = []string{"ForwardProxy", "Type", "NoProxy", "Details", "Address", "Port", "Timeout", "CredentialsOptional", "Username",
	"updateButton_Content", "updateAllButton_Content", "copyLinkButton_Content", "removeButton_Content", "addButton_Content",
	"Copy", "ServerSharing", "OnlineConfigDelivery"}

// extra are strings the original lacks: key → {en, zh-Hans}.
var extra = map[string][2]string{
	"Group":                   {"Group", "分组"},
	"ssmygo.imported":         {"Imported %d, failed %d.", "已导入 %d 个，失败 %d 个。"},
	"ssmygo.noLinks":          {"No ss:// links found in the clipboard.", "剪贴板中没有找到 ss:// 链接。"},
	"ssmygo.portInUse":        {"Port %d is in use by another program.", "端口 %d 已被其他程序占用。"},
	"ssmygo.startFailed":      {"ssmygo could not start the proxy: %s", "ssmygo 无法启动代理：%s"},
	"ssmygo.noServer":         {"Add a server first.", "请先添加服务器。"},
	"ssmygo.cannotSave":       {"Could not save: %s", "无法保存：%s"},
	"ssmygo.pluginNotTrusted": {"Plugin %s was not run.", "未运行插件 %s。"},
	"SOCKS5":                  {"SOCKS5", "SOCKS5"},
	"HTTP":                    {"HTTP", "HTTP"},
	"Save":                    {"Save", "保存"},
	"ssmygo.trustTitle":       {"Run plugin program?", "运行插件程序？"},
	"ssmygo.trustDetail":      {"%s\n\n%s\nSHA-256 %s…\n\nRun it only if you trust where it came from.", "%s\n\n%s\nSHA-256 %s…\n\n请确认来源可信后再运行。"},
	"ssmygo.trustRun":         {"Run", "运行"},
	"ssmygo.pluginNotFound":   {"Plugin %s not found. Looked in: %s", "找不到插件 %s。已查找：%s"},
	"ssmygo.pluginStart":      {"Plugin %s did not start: %s", "插件 %s 启动失败：%s"},
	"ssmygo.pluginCrashLoop":  {"Plugin keeps crashing and was stopped.", "插件反复崩溃，已停止。"},
	"ssmygo.forwardIgnored":   {"Forward proxy: not used with this server's plugin", "前置代理：当前服务器使用插件，未启用"},
	"ssmygo.udpOff":           {"UDP: off (forward proxy in use)", "UDP：已关闭（使用前置代理）"},
	"ssmygo.logUnavailable":   {"Log file not available: %s", "日志文件不可用：%s"},
	"ssmygo.total":            {"Total", "累计"},
	"ssmygo.fontSize":         {"Size", "字号"},
	"ssmygo.badSubURL":        {"URL must start with http:// or https://", "URL 必须以 http:// 或 https:// 开头"},
	"ssmygo.dupSub":           {"Already added.", "已经添加过。"},
	"ssmygo.removeSub":        {"Remove this subscription and its %d servers?", "移除这个订阅及其 %d 台服务器？"},
	"ssmygo.selReplaced":      {"%s is no longer available; %s is selected.", "%s 已不存在，已选中 %s。"},
	"ssmygo.subsFailed":       {"%[1]d of %[2]d subscriptions failed to update.", "%[2]d 个订阅中有 %[1]d 个更新失败。"},
	"ssmygo.captureFailed":    {"Screen capture failed: %s", "截屏失败：%s"},
	"ssmygo.fromSub":          {"From subscription %s (read-only; updates replace it)", "来自订阅 %s（只读，更新时会被替换）"},
	"ssmygo.addSubTitle":      {"Add as online config (SIP008) subscription?", "添加为在线配置（SIP008）订阅？"},
	"ssmygo.addSubOK":         {"Add", "添加"},
	"ssmygo.geositeFailed":    {"Geosite update failed: %s", "Geosite 更新失败：%s"},
	"ssmygo.geositeChecksum":  {"Geosite update failed: checksum mismatch.", "Geosite 更新失败：校验失败。"},
	"ssmygo.geositeMissing":   {"Geosite update not used: it lacks the groups %s.", "未采用 Geosite 更新：缺少分组 %s。"},
	"ssmygo.geositeTooLarge":  {"Geosite update failed: the download is larger than 20 MB.", "Geosite 更新失败：下载内容超过 20 MB。"},
	"ssmygo.geositeNotDLC":    {"Geosite update failed: the download is not a geosite file.", "Geosite 更新失败：下载的不是 geosite 文件。"},
	"ssmygo.userRuleSkipped":  {"user-rule.txt: skipped %d unsupported rules (lines %s).", "user-rule.txt：已跳过 %d 条不支持的规则（第 %s 行）。"},
	"ssmygo.userRuleRead":     {"Cannot read user-rule.txt; the previous rules are kept: %v", "无法读取 user-rule.txt，保留之前的规则：%v"},
	"ssmygo.userRuleBuild":    {"Cannot apply user-rule.txt; the previous rules are kept: %v", "无法应用 user-rule.txt，保留之前的规则：%v"},
	"ssmygo.cannotOpen":       {"Cannot open %s: %v", "无法打开 %s：%v"},
}

var mnemonic = regexp.MustCompile(`\s?\(&\w\)|&`)

func clean(s string) string { return mnemonic.ReplaceAllString(s, "") }

func main() {
	csvPath := flag.String("csv", "", "original Data/i18n.csv")
	resxEn := flag.String("resx-en", "", "original Localization/Strings.resx")
	resxZh := flag.String("resx-zh", "", "original Localization/Strings.zh-Hans.resx")
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
	enR, zhR := readResx(*resxEn), readResx(*resxZh)
	for _, n := range resxNames {
		if enR[n] == "" || zhR[n] == "" {
			log.Fatalf("resx name %q missing", n)
		}
		en["resx."+n], zh["resx."+n] = enR[n], zhR[n]
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

// readResx returns name → value of a .resx file's <data> entries.
func readResx(path string) map[string]string {
	b, err := os.ReadFile(path)
	if err != nil {
		log.Fatal(err)
	}
	var doc struct {
		Data []struct {
			Name  string `xml:"name,attr"`
			Value string `xml:"value"`
		} `xml:"data"`
	}
	if err := xml.Unmarshal(b, &doc); err != nil {
		log.Fatal(err)
	}
	out := map[string]string{}
	for _, d := range doc.Data {
		out[d.Name] = d.Value
	}
	return out
}

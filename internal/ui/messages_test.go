package ui

import (
	"errors"
	"fmt"
	"testing"

	"github.com/thc5787/ssmygo/internal/controller"
	"github.com/thc5787/ssmygo/internal/core/plugin"
	"github.com/thc5787/ssmygo/internal/i18n"
)

// Deferred #4: errors users see are translated, not raw Go text.
func TestMessage(t *testing.T) {
	defer i18n.SetLocale("en")
	port := &controller.PortInUseError{Port: 1080, Err: errors.New("listen tcp 127.0.0.1:1080: bind: Only one usage")}
	cases := []struct {
		err    error
		en, zh string
	}{
		{port, "Port 1080 is in use by another program.", "端口 1080 已被其他程序占用。"},
		{fmt.Errorf("wrapped: %w", port), "Port 1080 is in use by another program.", "端口 1080 已被其他程序占用。"},
		{controller.ErrNoServer, "Add a server first.", "请先添加服务器。"},
		{&controller.SaveError{Err: errors.New("disk full")}, "Could not save: disk full", "无法保存：disk full"},
		{errors.New("something else"), "something else", "something else"},
	}
	for _, c := range cases {
		i18n.SetLocale("en")
		if got := Message(c.err); got != c.en {
			t.Errorf("en %v: %q", c.err, got)
		}
		i18n.SetLocale("zh-CN")
		if got := Message(c.err); got != c.zh {
			t.Errorf("zh %v: %q", c.err, got)
		}
	}
}

func TestMessagePluginErrors(t *testing.T) {
	defer i18n.SetLocale("en")
	nf := &plugin.NotFoundError{Name: "v2ray-plugin", Tried: []string{`C:\a\v2ray-plugin.exe`, "PATH"}}
	se := &plugin.StartError{Name: "obfs-local.exe", Tail: []string{"bad option --x"}, Err: errors.New("exited")}
	for _, c := range []struct {
		err    error
		en, zh string
	}{
		{nf, `Plugin v2ray-plugin not found. Looked in: C:\a\v2ray-plugin.exe; PATH`, `找不到插件 v2ray-plugin。已查找：C:\a\v2ray-plugin.exe; PATH`},
		{se, "Plugin obfs-local.exe did not start: bad option --x", "插件 obfs-local.exe 启动失败：bad option --x"},
		{plugin.ErrCrashLoop, "Plugin keeps crashing and was stopped.", "插件反复崩溃，已停止。"},
	} {
		i18n.SetLocale("en")
		if got := Message(c.err); got != c.en {
			t.Errorf("en: %q", got)
		}
		i18n.SetLocale("zh-CN")
		if got := Message(c.err); got != c.zh {
			t.Errorf("zh: %q", got)
		}
	}
}

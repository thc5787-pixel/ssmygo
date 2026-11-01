package ui

import (
	"errors"
	"fmt"
	"testing"

	"github.com/thc5787/ssmygo/internal/controller"
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
		{controller.ErrPluginUnsupported, "Plugins are not supported in this version.", "此版本暂不支持插件。"},
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

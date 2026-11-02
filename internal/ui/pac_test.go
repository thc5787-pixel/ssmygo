package ui

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/thc5787/ssmygo/internal/controller"
	"github.com/thc5787/ssmygo/internal/geosite"
	"github.com/thc5787/ssmygo/internal/i18n"
)

func TestMenuPAC(t *testing.T) {
	var updated, edited bool
	m := BuildMenu(controller.State{}, MenuActions{UpdateGeosite: func() { updated = true }, EditUserRule: func() { edited = true }})
	var pac []string
	for _, it := range m.Items() {
		if it.Label == "PAC" && len(it.Submenu) > 0 {
			for _, s := range it.Submenu {
				pac = append(pac, s.ID)
			}
		}
	}
	if fmt.Sprint(pac) != "[pac.update pac.userRule]" {
		t.Fatalf("PAC submenu %v", pac)
	}
	for _, id := range pac {
		it := m.ItemByID(id)
		if it.Disabled {
			t.Fatalf("%s disabled", id)
		}
		it.Click(it, nil)
	}
	if !updated || !edited {
		t.Fatalf("update %v edit %v", updated, edited)
	}
	if it := m.ItemByID("pac.update"); it.Label != "Update Local PAC from Geosite" {
		t.Fatalf("label %q", it.Label)
	}
	if it := m.ItemByID("pac.userRule"); it.Label != "Edit User Rule for Geosite..." {
		t.Fatalf("label %q", it.Label)
	}
}

// The update runs off the main thread and reports its outcome.
func TestUpdateGeositeAction(t *testing.T) {
	defer i18n.SetLocale("en")
	for _, tc := range []struct {
		changed bool
		err     error
		want    string
	}{
		{true, nil, "PAC updated"},
		{false, nil, "No updates found. Please report to Geosite if you have problems with it."},
		{false, &controller.GeositeUpdateError{Err: fmt.Errorf("%w: example.com", geosite.ErrChecksum)}, "Geosite update failed: checksum mismatch."},
	} {
		var got []string
		release := make(chan struct{})
		start := time.Now()
		done := updateGeositeAction(func(context.Context) (bool, error) { <-release; return tc.changed, tc.err }, func(m string) { got = append(got, m) })
		if d := time.Since(start); d > 50*time.Millisecond {
			t.Fatalf("blocked for %v", d)
		}
		close(release)
		<-done
		if len(got) != 1 || got[0] != tc.want {
			t.Fatalf("%v: %q", tc.err, got)
		}
	}
}

func TestEditUserRuleAction(t *testing.T) {
	var opened string
	var msgs []string
	editUserRuleAction(func() (string, error) { return `C:\data\user-rule.txt`, nil },
		func(p string) error { opened = p; return nil }, func(m string) { msgs = append(msgs, m) })
	if opened != `C:\data\user-rule.txt` || len(msgs) != 0 {
		t.Fatalf("opened %q msgs %q", opened, msgs)
	}
	opened = ""
	editUserRuleAction(func() (string, error) { return "x", errors.New("disk full") },
		func(p string) error { opened = p; return nil }, func(m string) { msgs = append(msgs, m) })
	if opened != "" || len(msgs) != 1 {
		t.Fatalf("opened %q msgs %q", opened, msgs)
	}
	editUserRuleAction(func() (string, error) { return "x", nil },
		func(string) error { return errors.New("no editor") }, func(m string) { msgs = append(msgs, m) })
	if len(msgs) != 2 {
		t.Fatalf("msgs %q", msgs)
	}
}

func TestMessageRules(t *testing.T) {
	defer i18n.SetLocale("en")
	cases := []struct {
		err    error
		en, zh string
	}{
		{&controller.GeositeUpdatedNotice{}, "PAC updated", "更新 PAC 成功"},
		{&controller.GeositeUpdateError{Err: fmt.Errorf("%w: h", geosite.ErrChecksum)}, "Geosite update failed: checksum mismatch.", "Geosite 更新失败：校验失败。"},
		{&controller.GeositeUpdateError{Err: &geosite.MissingGroupsError{Groups: []string{"cn", "x@y"}}}, "Geosite update not used: it lacks the groups cn, x@y.", "未采用 Geosite 更新：缺少分组 cn, x@y。"},
		{&controller.GeositeUpdateError{Err: fmt.Errorf("%w: h", geosite.ErrTooLarge)}, "Geosite update failed: the download is larger than 20 MB.", "Geosite 更新失败：下载内容超过 20 MB。"},
		{&controller.GeositeUpdateError{Err: geosite.ErrNotDLC}, "Geosite update failed: the download is not a geosite file.", "Geosite 更新失败：下载的不是 geosite 文件。"},
		{&controller.GeositeUpdateError{Err: fmt.Errorf("%w: github.com: HTTP 404", geosite.ErrFetch)}, "Geosite update failed: geosite download failed: github.com: HTTP 404", "Geosite 更新失败：geosite download failed: github.com: HTTP 404"},
		{&controller.UserRulesSkippedError{Lines: []int{2, 5}}, "user-rule.txt: skipped 2 unsupported rules (lines 2, 5).", "user-rule.txt：已跳过 2 条不支持的规则（第 2, 5 行）。"},
		{&controller.UserRulesReadError{Err: errors.New("denied")}, "Cannot read user-rule.txt; the previous rules are kept: denied", "无法读取 user-rule.txt，保留之前的规则：denied"},
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

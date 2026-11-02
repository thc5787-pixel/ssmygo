package controller

import (
	"testing"
	"time"

	"github.com/thc5787/ssmygo/internal/config"
	"github.com/thc5787/ssmygo/internal/traffic"
)

func fastTraffic(t *testing.T) {
	old := trafficInterval
	trafficInterval = 10 * time.Millisecond
	t.Cleanup(func() { trafficInterval = old })
}

func TestTrafficSubscription(t *testing.T) {
	fastTraffic(t)
	e := newEnv(t)
	e.writeConfig(nil)
	c := e.start()
	got := make(chan traffic.Snapshot, 100)
	off := c.SubscribeTraffic(func(s traffic.Snapshot) {
		select {
		case got <- s:
		default:
		}
	})
	defer off()
	if err := socksEcho(c.State().Port, e.target); err != nil {
		t.Fatal(err)
	}
	deadline := time.After(2 * time.Second)
	for {
		select {
		case s := <-got:
			if s.TotalUp > 0 && s.TotalDown > 0 {
				if c.Traffic().TotalUp < s.TotalUp {
					t.Fatal("Traffic() behind the published snapshot")
				}
				return
			}
		case <-deadline:
			t.Fatal("no snapshot with traffic")
		}
	}
}

func TestTrafficOffStops(t *testing.T) {
	fastTraffic(t)
	e := newEnv(t)
	e.writeConfig(nil)
	c := e.start()
	n := 0
	off := c.SubscribeTraffic(func(traffic.Snapshot) { n++ })
	time.Sleep(50 * time.Millisecond)
	off()
	time.Sleep(30 * time.Millisecond) // a tick in flight may still land
	before := n
	time.Sleep(60 * time.Millisecond)
	if n != before || before == 0 {
		t.Fatalf("calls before off %d, after %d", before, n)
	}
}

func TestSaveLogViewer(t *testing.T) {
	e := newEnv(t)
	e.writeConfig(nil)
	c := e.start()
	v := c.LogViewer()
	v.WrapText, v.Width, v.FontFamily = true, 800, "Cascadia Mono"
	if err := c.SaveLogViewer(v); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(e.deps().Paths.Config())
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.LogViewer.WrapText || cfg.LogViewer.Width != 800 || cfg.LogViewer.FontFamily != "Cascadia Mono" {
		t.Fatalf("saved %+v", cfg.LogViewer)
	}
}

func TestSetShowPluginOutput(t *testing.T) {
	e := newEnv(t)
	e.writeConfig(nil)
	c := e.start()
	if c.State().ShowPluginOutput {
		t.Fatal("default on")
	}
	if err := c.SetShowPluginOutput(true); err != nil {
		t.Fatal(err)
	}
	if !c.State().ShowPluginOutput {
		t.Fatal("state not updated")
	}
	cfg, _ := config.Load(e.deps().Paths.Config())
	if !cfg.ShowPluginOutput {
		t.Fatal("not persisted")
	}
	if c.LogPath() == "" {
		t.Fatal("no log path")
	}
}

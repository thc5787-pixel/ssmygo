package controller

import (
	"context"
	"encoding/json"
	"io"
	"testing"

	"github.com/database64128/shadowsocks-go/service"
	"github.com/database64128/shadowsocks-go/tslog"
)

func startSSGo(t *testing.T, js string) {
	t.Helper()
	var sc service.Config
	if err := json.Unmarshal([]byte(js), &sc); err != nil {
		t.Fatal(err)
	}
	l, _ := tslog.Config{Level: 8}.NewLogger(io.Discard)
	m, err := sc.NewManager(l)
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { m.Stop(); m.Close() })
}

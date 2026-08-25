package app

import (
	"context"
	"encoding/json"
	"log/slog"
	"testing"

	"github.com/Black0Bag/minibox/internal/config"
	"github.com/Black0Bag/minibox/internal/domain/llm"
	"github.com/Black0Bag/minibox/internal/domain/permission"
)

func TestAppRegistersDeviceToolsAfterSetup(t *testing.T) {
	a := newToolTestApp(t)
	for _, name := range []string{"设备_截屏", "设备_点击", "设备_信息"} {
		if _, ok := a.ToolRegistry().Get(name); !ok {
			t.Fatalf("设备工具未注册: %s", name)
		}
	}
}

func TestRegisteredDeviceReadToolRoundTrip(t *testing.T) {
	a, conn := newSimulatedDeviceApp(t)
	t.Cleanup(func() { _ = conn.Close(1000, "test done") })
	a.SetPermissionMode(permission.ModePlan)

	resultCh := make(chan struct {
		out string
		err error
	}, 1)
	go func() {
		out, err := a.toolkit.Execute(context.Background(), llm.ToolCall{
			Name:      "设备_截屏",
			Arguments: `{"device_id":"sim-device-concurrent","quality":"low"}`,
		})
		resultCh <- struct {
			out string
			err error
		}{out, err}
	}()

	var command struct {
		ID      json.RawMessage `json:"id"`
		Method  string          `json:"method"`
		Params  json.RawMessage `json:"params"`
		JSONRPC string          `json:"jsonrpc"`
	}
	readRPC(t, conn, &command)
	if command.JSONRPC != "2.0" || command.Method != "设备_截屏" {
		t.Fatalf("unexpected command=%+v", command)
	}
	writeRPC(t, conn, map[string]any{
		"jsonrpc": "2.0",
		"id":      command.ID,
		"result":  map[string]string{"screen": "tool-simulated"},
	})
	got := <-resultCh
	if got.err != nil || got.out == "" {
		t.Fatalf("tool result out=%q err=%v", got.out, got.err)
	}
}

func TestRegisteredDeviceWriteToolNeedsApproval(t *testing.T) {
	a := newToolTestApp(t)
	a.SetPermissionMode(permission.ModePlan)
	_, err := a.toolkit.Execute(context.Background(), llm.ToolCall{
		Name:      "设备_点击",
		Arguments: `{"device_id":"dev-1","x":1,"y":2}`,
	})
	if err == nil {
		t.Fatal("plan 模式设备写工具应被拒绝")
	}
}

func newToolTestApp(t *testing.T) *App {
	t.Helper()
	cfg := config.Default()
	cfg.Database.Path = t.TempDir() + "/tool-test.db"
	cfg.Database.MaxOpenConns = 1
	cfg.Database.MaxIdleConns = 1
	cfg.LLM.Providers = []config.ProviderConfig{{
		Name:    "test",
		BaseURL: "http://127.0.0.1:1/v1",
		APIKeys: []string{"test-key"},
		Models:  []config.ModelConfig{{ID: "test-model", Enabled: true}},
	}}
	cfg.LLM.DefaultProvider = "test"
	cfg.LLM.DefaultModel = "test-model"
	a, err := New(context.Background(), cfg, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = a.Close() })
	return a
}

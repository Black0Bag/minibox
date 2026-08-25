package app

import (
	"context"
	"log/slog"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"

	"github.com/Black0Bag/minibox/internal/config"
)

func newSimulatedDeviceApp(t *testing.T) (*App, *websocket.Conn) {
	t.Helper()
	dir := t.TempDir()
	cfg := config.Default()
	cfg.Database.Path = filepath.Join(dir, "minibox.db")
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

	httpServer := httptest.NewServer(a.http)
	t.Cleanup(httpServer.Close)
	wsURL := "ws" + strings.TrimPrefix(httpServer.URL, "http") + "/device/ws"
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}

	writeRPC(t, conn, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "connect",
		"params": map[string]any{
			"client":   "sim-device-concurrent",
			"protocol": "1.0",
			"auth":     a.DeviceCredential().Token(),
		},
	})
	var connectResp map[string]any
	readRPC(t, conn, &connectResp)
	connectResult, _ := connectResp["result"].(map[string]any)
	if connectResult["ok"] != true {
		t.Fatalf("connect result=%v", connectResp)
	}

	writeRPC(t, conn, map[string]any{
		"jsonrpc": "2.0",
		"id":      2,
		"method":  "device",
		"params": map[string]any{
			"method": "hello",
			"params": map[string]any{
				"id":           "sim-device-concurrent",
				"model":        "Simulated Pixel",
				"android":      "test",
				"capabilities": []string{"screen", "info"},
			},
		},
	})
	var helloResp map[string]any
	readRPC(t, conn, &helloResp)
	helloResult, _ := helloResp["result"].(map[string]any)
	if helloResult["device_id"] != "sim-device-concurrent" {
		t.Fatalf("hello result=%v", helloResp)
	}

	return a, conn
}

func writeRPC(t *testing.T, conn *websocket.Conn, value any) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := wsjson.Write(ctx, conn, value); err != nil {
		t.Fatalf("write RPC: %v", err)
	}
}

func readRPC(t *testing.T, conn *websocket.Conn, target any) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := wsjson.Read(ctx, conn, target); err != nil {
		t.Fatalf("read RPC: %v", err)
	}
}

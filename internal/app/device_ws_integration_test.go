package app

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/Black0Bag/minibox/internal/config"
)

// TestDeviceWSCommandRoundTrip verifies the real application wiring:
// authenticated WS connect -> device hello -> Hub.SendCommand -> correlated device response -> audit log.
// The peer is a local simulated device only; no Android side effect is performed.
func TestDeviceWSCommandRoundTrip(t *testing.T) {
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
	t.Cleanup(func() { _ = conn.Close(websocket.StatusNormalClosure, "test done") })

	writeRPC(t, conn, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "connect",
		"params": map[string]any{
			"client":   "sim-device-01",
			"protocol": "1.0",
			"auth":     a.DeviceCredential().Token(),
		},
	})
	var connectResp map[string]any
	readRPC(t, conn, &connectResp)
	if result, _ := connectResp["result"].(map[string]any); result["ok"] != true {
		t.Fatalf("connect result=%v, want ok=true", connectResp)
	}

	writeRPC(t, conn, map[string]any{
		"jsonrpc": "2.0",
		"id":      2,
		"method":  "device",
		"params": map[string]any{
			"method": "hello",
			"params": map[string]any{
				"id":           "sim-device-01",
				"model":        "Simulated Pixel",
				"android":      "test",
				"capabilities": []string{"screen", "input"},
			},
		},
	})
	var helloResp map[string]any
	readRPC(t, conn, &helloResp)
	if result, _ := helloResp["result"].(map[string]any); result["device_id"] != "sim-device-01" {
		t.Fatalf("hello result=%v, want device_id", helloResp)
	}

	commandResult := make(chan error, 1)
	go func() {
		params, _ := json.Marshal(map[string]any{"quality": "low"})
		result, err := a.hub.SendCommand(context.Background(), "sim-device-01", "设备_截屏", params)
		if err != nil {
			commandResult <- err
			return
		}
		if !result.OK || !strings.Contains(result.Data, "simulated-screen") {
			commandResult <- &deviceCommandTestError{result: result.Data, ok: result.OK}
			return
		}
		commandResult <- nil
	}()

	var command struct {
		JSONRPC string          `json:"jsonrpc"`
		ID      json.RawMessage `json:"id"`
		Method  string          `json:"method"`
		Params  json.RawMessage `json:"params"`
	}
	readRPC(t, conn, &command)
	if command.JSONRPC != "2.0" || command.Method != "设备_截屏" || len(command.ID) == 0 {
		t.Fatalf("unexpected command: %+v", command)
	}
	writeRPC(t, conn, map[string]any{
		"jsonrpc": "2.0",
		"id":      json.RawMessage(command.ID),
		"result":  map[string]any{"screen": "simulated-screen"},
	})

	select {
	case err := <-commandResult:
		if err != nil {
			t.Fatalf("SendCommand: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("SendCommand did not receive the simulated device response")
	}

	logs := a.hub.AuditLog()
	if len(logs) != 1 || logs[0].DeviceID != "sim-device-01" || logs[0].Status != "done" {
		t.Fatalf("audit logs=%+v, want one completed simulated-device command", logs)
	}
}

type deviceCommandTestError struct {
	result string
	ok     bool
}

func (e *deviceCommandTestError) Error() string {
	return "unexpected device command result: ok=" + strconv.FormatBool(e.ok) + " data=" + e.result
}

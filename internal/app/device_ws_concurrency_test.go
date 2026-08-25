package app

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

func TestDeviceWSConcurrentOutOfOrderResponses(t *testing.T) {
	a, conn := newSimulatedDeviceApp(t)
	t.Cleanup(func() { _ = conn.Close(websocket.StatusNormalClosure, "test done") })

	results := make(chan error, 2)
	for _, method := range []string{"设备_截屏", "设备_信息"} {
		method := method
		go func() {
			result, err := a.hub.SendCommand(context.Background(), "sim-device-concurrent", method, nil)
			if err != nil {
				results <- err
				return
			}
			if !result.OK || !strings.Contains(result.Data, method) {
				results <- fmt.Errorf("method=%s result=%+v", method, result)
				return
			}
			results <- nil
		}()
	}

	commands := make([]struct {
		ID     json.RawMessage `json:"id"`
		Method string          `json:"method"`
	}, 0, 2)
	for len(commands) < 2 {
		var command struct {
			JSONRPC string          `json:"jsonrpc"`
			ID      json.RawMessage `json:"id"`
			Method  string          `json:"method"`
		}
		readRPC(t, conn, &command)
		if command.JSONRPC != "2.0" || command.Method == "" || len(command.ID) == 0 {
			t.Fatalf("invalid concurrent command: %+v", command)
		}
		commands = append(commands, struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
		}{ID: command.ID, Method: command.Method})
	}
	if string(commands[0].ID) == string(commands[1].ID) {
		t.Fatalf("concurrent commands reused id: %s", commands[0].ID)
	}

	for i := len(commands) - 1; i >= 0; i-- {
		writeRPC(t, conn, map[string]any{
			"jsonrpc": "2.0",
			"id":      commands[i].ID,
			"result":  map[string]string{"method": commands[i].Method},
		})
	}
	for range 2 {
		select {
		case err := <-results:
			if err != nil {
				t.Fatal(err)
			}
		case <-time.After(3 * time.Second):
			t.Fatal("并发命令未完成")
		}
	}
}

package ws

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

// newTestWSClient 建立到测试 WS 服务器的连接。
func newTestWSClient(t *testing.T, srv *Server) (*websocket.Conn, *httptest.Server) {
	t.Helper()
	ts := httptest.NewServer(srv.Handler())
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(ts.URL, "http"), nil)
	if err != nil {
		ts.Close()
		t.Fatalf("WS 拨号失败: %v", err)
	}
	t.Cleanup(func() {
		_ = conn.Close(websocket.StatusNormalClosure, "")
		ts.Close()
	})
	return conn, ts
}

// sendReq 发送请求并读响应。
func sendReq(t *testing.T, conn *websocket.Conn, id int, method string, params any) (map[string]any, int, error) {
	t.Helper()
	ctx := context.Background()
	req := map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"method":  method,
	}
	if params != nil {
		req["params"] = params
	}
	if err := wsjson.Write(ctx, conn, req); err != nil {
		t.Fatalf("写请求失败: %v", err)
	}
	var resp map[string]any
	if err := wsjson.Read(ctx, conn, &resp); err != nil {
		return nil, 0, err
	}
	// 提取错误码
	if errObj, ok := resp["error"].(map[string]any); ok {
		code, _ := errObj["code"].(float64)
		return resp, int(code), nil
	}
	return resp, 0, nil
}

// TestHandshakeRequired 未握手调方法报 handshake_required。
func TestHandshakeRequired(t *testing.T) {
	srv := New(slog.New(slog.DiscardHandler))
	conn, _ := newTestWSClient(t, srv)

	_, code, err := sendReq(t, conn, 1, "device.camera.photo", nil)
	if err != nil {
		t.Fatalf("sendReq err=%v", err)
	}
	if code != codeHandshakeRequired {
		t.Errorf("错误码=%d, 期望 %d (handshake_required)", code, codeHandshakeRequired)
	}
}

// TestConnect_Handshake 握手成功后调方法。
func TestConnect_Handshake(t *testing.T) {
	srv := New(slog.New(slog.DiscardHandler))
	srv.Handle("device", func(_ context.Context, _ *Client, _ json.RawMessage) (any, error) {
		return map[string]string{"device": "ok"}, nil
	})
	conn, _ := newTestWSClient(t, srv)

	// 握手
	if _, code, err := sendReq(t, conn, 1, "connect", map[string]any{"client": "android", "protocol": "1.0"}); err != nil || code != 0 {
		t.Fatalf("握手失败: err=%v code=%d", err, code)
	}

	// 调方法
	resp, code, err := sendReq(t, conn, 2, "device.camera.photo", nil)
	if err != nil {
		t.Fatalf("调方法失败: %v", err)
	}
	if code != 0 {
		t.Errorf("方法错误码=%d, 期望 0", code)
	}
	result, _ := resp["result"].(map[string]any)
	if result["device"] != "ok" {
		t.Errorf("result=%v, 期望 device=ok", result)
	}
}

// TestHeartbeatPing 心跳不需要握手。
func TestHeartbeatPing(t *testing.T) {
	srv := New(slog.New(slog.DiscardHandler))
	conn, _ := newTestWSClient(t, srv)

	resp, code, err := sendReq(t, conn, 1, "heartbeat.ping", nil)
	if err != nil {
		t.Fatalf("ping 失败: %v", err)
	}
	if code != 0 {
		t.Errorf("ping 错误码=%d, 期望 0（心跳不需握手）", code)
	}
	result, _ := resp["result"].(map[string]any)
	if result["pong"] != true {
		t.Errorf("ping 结果=%v, 期望 pong=true", result)
	}
}

// TestUnknownMethod 未注册方法报 -32601。
func TestUnknownMethod(t *testing.T) {
	srv := New(slog.New(slog.DiscardHandler))
	conn, _ := newTestWSClient(t, srv)

	// 先握手
	_, _, _ = sendReq(t, conn, 1, "connect", nil)
	_, code, err := sendReq(t, conn, 2, "unknown.method", nil)
	if err != nil {
		t.Fatalf("sendReq err=%v", err)
	}
	if code != -32601 {
		t.Errorf("错误码=%d, 期望 -32601", code)
	}
}

// TestPrefixRoute 前缀路由：注册 device 处理 device.*。
func TestPrefixRoute(t *testing.T) {
	srv := New(slog.New(slog.DiscardHandler))
	var called string
	srv.Handle("device", func(_ context.Context, _ *Client, params json.RawMessage) (any, error) {
		var p struct {
			Sub string `json:"sub"`
		}
		_ = json.Unmarshal(params, &p)
		called = p.Sub
		return nil, nil
	})
	conn, _ := newTestWSClient(t, srv)

	_, _, _ = sendReq(t, conn, 1, "connect", nil)
	// 通知（无 id）不应被等待响应
	if err := wsjson.Write(context.Background(), conn, map[string]any{
		"jsonrpc": "2.0",
		"method":  "device.screen.capture",
		"params":  map[string]any{"sub": "capture"},
	}); err != nil {
		t.Fatal(err)
	}
	// 等待通知处理
	time.Sleep(50 * time.Millisecond)
	if called != "capture" {
		t.Errorf("前缀路由未触发: called=%q, 期望 capture", called)
	}
}

func TestNoResultResponseIncludesNullResult(t *testing.T) {
	srv := New(slog.New(slog.DiscardHandler))
	srv.Handle("device", func(_ context.Context, _ *Client, _ json.RawMessage) (any, error) {
		return nil, nil
	})
	conn, _ := newTestWSClient(t, srv)
	if _, code, err := sendReq(t, conn, 1, "connect", nil); err != nil || code != 0 {
		t.Fatalf("握手失败: err=%v code=%d", err, code)
	}
	resp, code, err := sendReq(t, conn, 2, "device.screen.capture", nil)
	if err != nil || code != 0 {
		t.Fatalf("调用失败: err=%v code=%d resp=%v", err, code, resp)
	}
	if result, ok := resp["result"]; !ok || result != nil {
		t.Fatalf("无结果请求应返回 result:null，实际: %v", resp)
	}
}
func TestUnknownMethodNotificationDoesNotReply(t *testing.T) {
	srv := New(slog.New(slog.DiscardHandler))
	conn, _ := newTestWSClient(t, srv)
	if _, code, err := sendReq(t, conn, 1, "connect", map[string]any{"client": "notify-test", "protocol": "1.0"}); err != nil || code != 0 {
		t.Fatalf("握手失败: err=%v code=%d", err, code)
	}
	if err := wsjson.Write(context.Background(), conn, map[string]any{
		"jsonrpc": "2.0",
		"method":  "unknown.method",
	}); err != nil {
		t.Fatalf("写入通知失败: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	var msg map[string]any
	if err := wsjson.Read(ctx, conn, &msg); err == nil {
		t.Fatalf("未知方法通知不应收到 JSON-RPC 响应，实际: %v", msg)
	}
}

func TestBroadcast(t *testing.T) {
	srv := New(slog.New(slog.DiscardHandler))
	conn, _ := newTestWSClient(t, srv)
	if _, code, err := sendReq(t, conn, 1, "connect", map[string]any{"client": "broadcast-test", "protocol": "1.0"}); err != nil || code != 0 {
		t.Fatalf("握手失败: err=%v code=%d", err, code)
	}
	// 等待 Handler goroutine 注册客户端到 s.clients
	time.Sleep(50 * time.Millisecond)
	srv.Broadcast(context.Background(), "system.degrade", map[string]string{"level": "L1"})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	var msg map[string]any
	if err := wsjson.Read(ctx, conn, &msg); err != nil {
		t.Fatalf("读广播失败: %v", err)
	}
	if msg["method"] != "event.push" {
		t.Errorf("广播 method=%v, 期望 event.push", msg["method"])
	}
	params, _ := msg["params"].(map[string]any)
	if params["type"] != "system.degrade" {
		t.Errorf("广播 type=%v", params["type"])
	}
}

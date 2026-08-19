package device

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

func testHub(t *testing.T) (*Hub, *websocket.Conn) {
	t.Helper()
	h := NewHub(slog.New(slog.NewTextHandler(io.Discard, nil)))

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			t.Errorf("Accept 失败: %v", err)
			return
		}
		// 模拟设备端：读取 JSON-RPC 请求并回显结果
		for {
			var req WSMessage
			if err := wsjson.Read(r.Context(), conn, &req); err != nil {
				_ = conn.Close(websocket.StatusNormalClosure, "bye")
				return
			}
			resp := map[string]any{
				"jsonrpc": "2.0",
				"id":      string(req.ID),
				"result":  `{"ok":true,"data":"点到了"}`,
			}
			if err := wsjson.Write(r.Context(), conn, resp); err != nil {
				return
			}
		}
	}))
	t.Cleanup(srv.Close)

	ctx := context.Background()
	conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http")+"/", nil)
	if err != nil {
		t.Fatalf("Dial 失败: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close(websocket.StatusNormalClosure, "test done") })

	return h, conn
}

func TestHub_设备连接与列表(t *testing.T) {
	h, conn := testHub(t)
	ctx := context.Background()

	dev, err := h.HandleConnect(ctx, conn, "dev-1", "Pixel 8", "14", []string{"screen", "input"}, map[string]bool{"accessibility": true})
	if err != nil {
		t.Fatalf("HandleConnect 失败: %v", err)
	}
	if !dev.Online || dev.ID != "dev-1" || dev.Model != "Pixel 8" {
		t.Fatalf("设备状态异常: %+v", dev)
	}

	list := h.ListDevices()
	if len(list) != 1 || list[0].ID != "dev-1" {
		t.Fatalf("ListDevices 异常: %+v", list)
	}
	if got := h.GetDevice("dev-1"); got == nil || got.ID != "dev-1" {
		t.Fatal("GetDevice 应返回设备")
	}
	if got := h.GetDevice("ghost"); got != nil {
		t.Fatal("GetDevice 未知设备应返回 nil")
	}
}

func TestHub_设备断开离线(t *testing.T) {
	h, conn := testHub(t)
	ctx := context.Background()

	_, err := h.HandleConnect(ctx, conn, "dev-1", "Pixel 8", "14", nil, nil)
	if err != nil {
		t.Fatalf("HandleConnect 失败: %v", err)
	}

	h.HandleDisconnect("dev-1")
	dev := h.GetDevice("dev-1")
	if dev == nil || dev.Online {
		t.Fatal("断开后设备应在线=false 且保留记录")
	}

	// 断开后下命令应失败（设备离线）
	_, err = h.SendCommand(ctx, "dev-1", "设备_截屏", nil)
	if err == nil || !strings.Contains(err.Error(), "设备离线") {
		t.Fatalf("离线设备应报错，得到: %v", err)
	}
}

func TestHub_移除设备(t *testing.T) {
	h, conn := testHub(t)
	ctx := context.Background()

	if _, err := h.HandleConnect(ctx, conn, "dev-1", "Pixel 8", "14", nil, nil); err != nil {
		t.Fatalf("HandleConnect 失败: %v", err)
	}
	h.RemoveDevice("dev-1")
	if got := h.GetDevice("dev-1"); got != nil {
		t.Fatal("移除后 GetDevice 应返回 nil")
	}
}

func TestHub_SendCommand_成功往返(t *testing.T) {
	h, conn := testHub(t)
	ctx := context.Background()

	if _, err := h.HandleConnect(ctx, conn, "dev-1", "Pixel 8", "14", nil, nil); err != nil {
		t.Fatalf("HandleConnect 失败: %v", err)
	}

	params, _ := json.Marshal(map[string]any{"x": 10, "y": 20})
	result, err := h.SendCommand(ctx, "dev-1", "设备_点击", params)
	if err != nil {
		t.Fatalf("SendCommand 失败: %v", err)
	}
	if !result.OK {
		t.Fatalf("结果应 OK，得到: %+v", result)
	}
	if result.Data == "" {
		t.Fatal("结果应带回执数据")
	}

	// 审计日志应已记录
	logs := h.AuditLog()
	if len(logs) != 1 {
		t.Fatalf("应有 1 条审计日志，得到 %d", len(logs))
	}
	if logs[0].Status != "done" || logs[0].Method != "设备_点击" {
		t.Fatalf("审计日志异常: %+v", logs[0])
	}
}

func TestHub_SendCommand_设备错误响应(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		var req WSMessage
		if err := wsjson.Read(r.Context(), conn, &req); err != nil {
			return
		}
		_ = wsjson.Write(r.Context(), conn, map[string]any{
			"jsonrpc": "2.0",
			"id":      string(req.ID),
			"error":   map[string]any{"code": -32000, "message": "操作失败"},
		})
		_ = conn.Close(websocket.StatusNormalClosure, "done")
	}))
	t.Cleanup(srv.Close)

	h := NewHub(slog.New(slog.NewTextHandler(io.Discard, nil)))
	ctx := context.Background()
	conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http")+"/", nil)
	if err != nil {
		t.Fatalf("Dial 失败: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close(websocket.StatusNormalClosure, "done") })

	if _, err := h.HandleConnect(ctx, conn, "dev-1", "Pixel 8", "14", nil, nil); err != nil {
		t.Fatalf("HandleConnect 失败: %v", err)
	}

	result, err := h.SendCommand(ctx, "dev-1", "设备_点击", nil)
	if err != nil {
		t.Fatalf("SendCommand 不应因设备错误而报错: %v", err)
	}
	if result.OK {
		t.Fatal("设备返回 error 时 OK 应为 false")
	}
	if !strings.Contains(result.Err, "操作失败") {
		t.Fatalf("错误信息不符: %s", result.Err)
	}
}

func TestHub_SendCommand_护栏拒绝危险操作(t *testing.T) {
	h, conn := testHub(t)
	ctx := context.Background()

	if _, err := h.HandleConnect(ctx, conn, "dev-1", "Pixel 8", "14", nil, nil); err != nil {
		t.Fatalf("HandleConnect 失败: %v", err)
	}

	// 剪贴板属敏感操作，未配置 HITL 回调 → 应被护栏拒绝
	_, err := h.SendCommand(ctx, "dev-1", "设备_剪贴板", nil)
	if err == nil || !strings.Contains(err.Error(), "人工确认") {
		t.Fatalf("敏感操作未接入 HITL 应被拒，得到: %v", err)
	}
}

func TestHub_SendCommand_未注册方法被拒(t *testing.T) {
	h, conn := testHub(t)
	ctx := context.Background()

	if _, err := h.HandleConnect(ctx, conn, "dev-1", "Pixel 8", "14", nil, nil); err != nil {
		t.Fatalf("HandleConnect 失败: %v", err)
	}

	_, err := h.SendCommand(ctx, "dev-1", "设备_不存在的操作", nil)
	if err == nil || !strings.Contains(err.Error(), "未注册") {
		t.Fatalf("未知方法应被护栏拒绝，得到: %v", err)
	}
}

func TestHub_SendCommand_离线设备报错(t *testing.T) {
	h := NewHub(slog.New(slog.NewTextHandler(io.Discard, nil)))
	_, err := h.SendCommand(context.Background(), "ghost", "设备_截屏", nil)
	if err == nil || !strings.Contains(err.Error(), "设备离线") {
		t.Fatalf("未连接设备应报离线: %v", err)
	}
}

func TestHub_EmergencyStop(t *testing.T) {
	h, conn := testHub(t)
	ctx := context.Background()

	if _, err := h.HandleConnect(ctx, conn, "dev-1", "Pixel 8", "14", nil, map[string]bool{"notification": true}); err != nil {
		t.Fatalf("HandleConnect 失败: %v", err)
	}

	if err := h.EmergencyStop("dev-1"); err != nil {
		t.Fatalf("EmergencyStop 失败: %v", err)
	}

	dev := h.GetDevice("dev-1")
	if dev == nil || dev.Online {
		t.Fatal("急停后设备应为离线")
	}
	if dev.Permissions["notification"] {
		t.Fatal("急停后权限应全部关闭")
	}

	// 已急停设备无法再下命令
	_, err := h.SendCommand(ctx, "dev-1", "设备_截屏", nil)
	if err == nil || !strings.Contains(err.Error(), "设备离线") {
		t.Fatalf("急停后应报离线: %v", err)
	}

	// 未知设备急停报错
	if err := h.EmergencyStop("ghost"); err == nil {
		t.Fatal("未知设备急停应报错")
	}
}

func TestHub_EmergencyStopAll(t *testing.T) {
	h := NewHub(slog.New(slog.NewTextHandler(io.Discard, nil)))

	// 手动构造两个设备（不依赖网络）
	h.mu.Lock()
	h.devices["dev-1"] = &Device{ID: "dev-1", Online: true, Permissions: map[string]bool{"notification": true}}
	h.devices["dev-2"] = &Device{ID: "dev-2", Online: true, Permissions: map[string]bool{"notification": true}}
	h.mu.Unlock()

	h.EmergencyStopAll()
	for _, id := range []string{"dev-1", "dev-2"} {
		dev := h.GetDevice(id)
		if dev == nil || dev.Online {
			t.Fatalf("急停后 %s 应为离线", id)
		}
	}
}

func TestHub_心跳超时判离线(t *testing.T) {
	h := NewHub(slog.New(slog.NewTextHandler(io.Discard, nil)))

	// 构造一个 LastSeen 超过 45s 阈值的在线设备
	stale := &Device{ID: "dev-1", Online: true, LastSeen: time.Now().Add(-60 * time.Second)}
	fresh := &Device{ID: "dev-2", Online: true, LastSeen: time.Now()}
	h.mu.Lock()
	h.devices["dev-1"] = stale
	h.devices["dev-2"] = fresh
	h.mu.Unlock()

	h.checkHeartbeat()

	if stale.Online {
		t.Fatal("超过 45s 未活跃的设备应判离线")
	}
	if !fresh.Online {
		t.Fatal("新活跃设备不应被误判离线")
	}
}

func TestHub_AuditLog_空与访问器(t *testing.T) {
	h := NewHub(slog.New(slog.NewTextHandler(io.Discard, nil)))
	if len(h.AuditLog()) != 0 {
		t.Fatal("新 Hub 审计日志应为空")
	}
	if h.PairingManager() == nil || h.Registry() == nil || h.Guard() == nil {
		t.Fatal("访问器不应返回 nil")
	}
}
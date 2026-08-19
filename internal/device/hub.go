package device

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"

	"github.com/Black0Bag/minibox/internal/device/guardrails"
)

// Hub 设备网关（D-01：注册/心跳/多设备/命令下发/事件订阅/安全校验）。
type Hub struct {
	mu       sync.RWMutex
	devices  map[string]*Device // deviceID → Device
	conns    map[string]*websocket.Conn // deviceID → WS 连接
	audit    *AuditLogger
	registry *Registry
	manager  *PairingManager
	guard    *guardrails.Guard
	logger   *slog.Logger
	cmdID    int64
}

// NewHub 创建设备网关。
func NewHub(logger *slog.Logger) *Hub {
	return &Hub{
		devices:  make(map[string]*Device),
		conns:    make(map[string]*websocket.Conn),
		audit:    NewAuditLogger(),
		registry: NewRegistry(),
		manager:  NewPairingManager(),
		guard:    guardrails.New(),
		logger:   logger,
	}
}

// HandleConnect 设备连接握手（D-02/D-08）。
func (h *Hub) HandleConnect(ctx context.Context, conn *websocket.Conn, deviceID, model, android string, caps []string, perms map[string]bool) (*Device, error) {
	dev := &Device{
		ID:           deviceID,
		Model:        model,
		Android:      android,
		Capabilities: caps,
		Permissions:  perms,
		Online:       true,
		ConnectedAt:  time.Now(),
		LastSeen:     time.Now(),
	}

	h.mu.Lock()
	h.devices[deviceID] = dev
	h.conns[deviceID] = conn
	h.mu.Unlock()

	h.logger.Info("设备已连接", "id", deviceID, "model", model)
	return dev, nil
}

// HandleDisconnect 设备断开（D-02：3 次未响应判离线）。
func (h *Hub) HandleDisconnect(deviceID string) {
	h.mu.Lock()
	if dev, ok := h.devices[deviceID]; ok {
		dev.Online = false
		dev.LastSeen = time.Now()
	}
	delete(h.conns, deviceID)
	h.mu.Unlock()
	h.logger.Info("设备已断开", "id", deviceID)
}

// SendCommand 下发命令到指定设备（D-05/D-06）。
// 命令先过护栏（D-17：危险动作清单 / 频率限制 / HITL）。
func (h *Hub) SendCommand(ctx context.Context, deviceID, method string, params json.RawMessage) (*CommandResult, error) {
	h.mu.RLock()
	conn, ok := h.conns[deviceID]
	dev, devOK := h.devices[deviceID]
	h.mu.RUnlock()

	if !ok || !devOK || !dev.Online {
		return nil, fmt.Errorf("设备离线: %s", deviceID)
	}

	// D-17 护栏检查
	var args map[string]any
	if len(params) > 0 {
		_ = json.Unmarshal(params, &args)
	}
	decision, reason, err := h.guard.Check(ctx, guardrails.Action{
		DeviceID: deviceID,
		Method:   method,
		Params:   args,
	})
	if err != nil {
		return nil, err
	}
	if decision == guardrails.Deny {
		return nil, fmt.Errorf("护栏拒绝: %s", reason)
	}
	if decision == guardrails.Ask {
		return nil, fmt.Errorf("护栏要求人工确认（未配置 HITL 回调）: %s", reason)
	}
	cmd := &Command{
		ID:        fmt.Sprintf("cmd_%d", time.Now().UnixNano()),
		DeviceID:  deviceID,
		Method:    method,
		Params:    params,
		Status:    "sent",
		CreatedAt: time.Now(),
	}

	// 发送 JSON-RPC 请求
	req := map[string]any{
		"jsonrpc": "2.0",
		"id":      cmd.ID,
		"method":  method,
		"params":  params,
	}
	if err := wsjson.Write(ctx, conn, req); err != nil {
		return nil, fmt.Errorf("命令发送失败: %w", err)
	}

	// 等待响应（超时 30s）
	ctx2, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	var resp struct {
		JSONRPC string          `json:"jsonrpc"`
		ID      string          `json:"id"`
		Result  json.RawMessage `json:"result,omitempty"`
		Error   *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error,omitempty"`
	}
	if err := wsjson.Read(ctx2, conn, &resp); err != nil {
		cmd.Status = "failed"
		h.audit.Log(cmd)
		return nil, fmt.Errorf("命令响应超时: %w", err)
	}

	result := &CommandResult{OK: true}
	if resp.Error != nil {
		result.OK = false
		result.Err = resp.Error.Message
		cmd.Status = "failed"
	} else {
		result.Data = string(resp.Result)
		cmd.Status = "done"
	}
	cmd.Result = result
	h.audit.Log(cmd)
	return result, nil
}

// ListDevices 列出所有设备（D-04）。
func (h *Hub) ListDevices() []*Device {
	h.mu.RLock()
	defer h.mu.RUnlock()
	out := make([]*Device, 0, len(h.devices))
	for _, d := range h.devices {
		out = append(out, d)
	}
	return out
}

// GetDevice 获取指定设备。
func (h *Hub) GetDevice(id string) *Device {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.devices[id]
}

// RemoveDevice 移除设备（解绑）。
func (h *Hub) RemoveDevice(id string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.devices, id)
	delete(h.conns, id)
}

// StartHeartbeat 启动心跳检测（D-02：15s ping，3 次未响应判离线）。
func (h *Hub) StartHeartbeat(ctx context.Context) {
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			h.checkHeartbeat()
		case <-ctx.Done():
			return
		}
	}
}

func (h *Hub) checkHeartbeat() {
	h.mu.RLock()
	defer h.mu.RUnlock()
	now := time.Now()
	for id, dev := range h.devices {
		if !dev.Online {
			continue
		}
		if now.Sub(dev.LastSeen) > 45*time.Second {
			h.logger.Warn("设备心跳超时", "id", id)
			dev.Online = false
		}
	}
}

// AuditLog 返回审计日志。
func (h *Hub) AuditLog() []*Command { return h.audit.List() }

// PairingManager 返回配对管理器。
func (h *Hub) PairingManager() *PairingManager { return h.manager }

// Registry 返回设备注册表。
func (h *Hub) Registry() *Registry { return h.registry }

// Guard 返回设备护栏（供 app 注入 HITL 回调）。
func (h *Hub) Guard() *guardrails.Guard { return h.guard }

// EmergencyStop 急停（D-13）：断开并冻结指定设备。
func (h *Hub) EmergencyStop(deviceID string) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	dev, ok := h.devices[deviceID]
	if !ok {
		return fmt.Errorf("设备不存在: %s", deviceID)
	}
	if conn, ok := h.conns[deviceID]; ok {
		_ = conn.Close(websocket.StatusNormalClosure, "紧急停止")
		delete(h.conns, deviceID)
	}
	dev.Online = false
	dev.Permissions = map[string]bool{
		"accessibility":  false,
		"mediaProjection": false,
		"notification":   false,
	}
	h.logger.Warn("设备紧急停止", "id", deviceID)
	return nil
}

// EmergencyStopAll 急停所有设备。
func (h *Hub) EmergencyStopAll() {
	h.mu.RLock()
	ids := make([]string, 0, len(h.devices))
	for id := range h.devices {
		ids = append(ids, id)
	}
	h.mu.RUnlock()
	for _, id := range ids {
		_ = h.EmergencyStop(id)
	}
}
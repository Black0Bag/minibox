package ws

import (
	"context"
	"encoding/json"
	"fmt"
)

// Method names (design doc WS method list).
const (
	// device.* — device control
	MethodDeviceScreenCapture  = "device.screen.capture"
	MethodDeviceCameraPhoto    = "device.camera.photo"
	MethodDeviceMicrophoneRec  = "device.microphone.record_start"
	MethodDeviceMicrophoneStop = "device.microphone.record_stop"
	MethodDeviceInputTap       = "device.input.tap"
	MethodDeviceInputSwipe     = "device.input.swipe"
	MethodDeviceInputText      = "device.input.text"
	MethodDeviceNotifyShow     = "device.notify.show"
	MethodDeviceTTSSpeak       = "device.tts.speak"
	MethodDeviceClipboardSet   = "device.clipboard.set"
	MethodDeviceClipboardGet   = "device.clipboard.get"
	MethodDeviceFileList       = "device.file.list"
	MethodDeviceFileRead       = "device.file.read"
	MethodDeviceFileWrite      = "device.file.write"
	MethodDeviceAppOpen        = "device.app.open"
	MethodDeviceSMSSend        = "device.sms.send"
	MethodDeviceContactList    = "device.contact.list"
	MethodDeviceSettingsGet    = "device.settings.get"
	MethodDeviceSettingsSet    = "device.settings.set"
	MethodDeviceBatteryGet     = "device.battery.get"
	MethodDeviceLocationGet    = "device.location.get"

	// browser.* — browser automation
	MethodBrowserTabOpen    = "browser.tab.open"
	MethodBrowserTabClose   = "browser.tab.close"
	MethodBrowserTabList    = "browser.tab.list"
	MethodBrowserTabExecute = "browser.tab.execute_js"
	MethodBrowserNavigate   = "browser.navigate"

	// peer.* — P2P
	MethodPeerDiscover = "peer.discover"
	MethodPeerRelay    = "peer.relay"

	// event.* — event push/ack
	MethodEventPush = "event.push"
	MethodEventAck  = "event.ack"

	// system.* — system info
	MethodSystemInfo  = "system.info"
	MethodSystemStats = "system.stats"
	MethodSystemLog   = "system.log"
)

// RegisterDefaultHandlers 注册全部内置 method 处理器。
// 这些处理器返回占位响应（actual device/browser control 由 device proxy 层覆盖）。
// 设计：WS 服务器只负责路由和协议层，具体设备操作由 device.Hub 代理转发。
func (s *Server) RegisterDefaultHandlers() {
	// device.* — 设备控制
	s.Handle("device", s.handleDeviceProxy)

	// browser.* — 浏览器自动化
	s.Handle("browser", s.handleBrowserProxy)

	// peer.* — P2P 互联
	s.Handle(MethodPeerDiscover, s.handlePeerDiscover)
	s.Handle(MethodPeerRelay, s.handlePeerRelay)

	// event.* — 事件推送
	s.Handle(MethodEventAck, s.handleEventAck)

	// system.* — 系统信息
	s.Handle(MethodSystemInfo, s.handleSystemInfo)
	s.Handle(MethodSystemStats, s.handleSystemStats)
	s.Handle(MethodSystemLog, s.handleSystemLog)
}

// handleDeviceProxy 设备代理（前缀路由：所有 device.* 方法走这里）。
// 实际设备操作由 device.Hub 代理转发到真实设备；组合根会用自己的
// handler 覆盖本占位实现（app.handleDeviceMessage）。
func (s *Server) handleDeviceProxy(_ context.Context, _ *Client, _ json.RawMessage) (any, error) {
	return map[string]any{
		"ok":      true,
		"message": "device proxy: awaiting real device implementation",
	}, nil
}

// handleBrowserProxy 浏览器代理（前缀路由：所有 browser.* 方法走这里）。
// 浏览器能力按设计放在前端（docs/browser-frontend.md），后端仅保留占位。
func (s *Server) handleBrowserProxy(_ context.Context, _ *Client, _ json.RawMessage) (any, error) {
	return map[string]any{
		"ok":      true,
		"message": "browser proxy: awaiting real browser implementation",
	}, nil
}

// handlePeerDiscover 发现对等设备。
func (s *Server) handlePeerDiscover(_ context.Context, _ *Client, _ json.RawMessage) (any, error) {
	return map[string]any{
		"peers":   []string{},
		"message": "no peers discovered",
	}, nil
}

// handlePeerRelay 中继消息到对等设备。
func (s *Server) handlePeerRelay(_ context.Context, _ *Client, params json.RawMessage) (any, error) {
	var p struct {
		PeerID string `json:"peer_id"`
		Data   any    `json:"data"`
	}
	if len(params) > 0 && string(params) != "null" {
		_ = json.Unmarshal(params, &p)
	}
	if p.PeerID == "" {
		return nil, fmt.Errorf("peer_id required")
	}
	return map[string]any{"ok": true, "peer_id": p.PeerID, "delivered": false}, nil
}

// handleEventAck 确认收到事件推送。
func (s *Server) handleEventAck(_ context.Context, _ *Client, params json.RawMessage) (any, error) {
	var p struct {
		EventID string `json:"event_id"`
	}
	if len(params) > 0 && string(params) != "null" {
		_ = json.Unmarshal(params, &p)
	}
	return map[string]any{"ok": true, "event_id": p.EventID}, nil
}

// handleSystemInfo 返回系统信息。
func (s *Server) handleSystemInfo(_ context.Context, c *Client, _ json.RawMessage) (any, error) {
	return map[string]any{
		"version":   "1.0",
		"protocol":  "1.0",
		"client_id": c.ID(),
		"methods":   []string{"device.*", "browser.*", "peer.*", "event.*", "system.*", "heartbeat.*"},
	}, nil
}

// handleSystemStats 返回系统统计信息。
func (s *Server) handleSystemStats(_ context.Context, _ *Client, _ json.RawMessage) (any, error) {
	s.mu.RLock()
	clientCount := len(s.clients)
	s.mu.RUnlock()
	return map[string]any{
		"clients": clientCount,
		"routes":  len(s.routes),
	}, nil
}

// handleSystemLog 推送系统日志（客户端订阅后服务端推送）。
func (s *Server) handleSystemLog(_ context.Context, _ *Client, _ json.RawMessage) (any, error) {
	return map[string]any{
		"ok":      true,
		"message": "log subscription acknowledged",
	}, nil
}

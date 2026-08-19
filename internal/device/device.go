// Package device 提供设备代理 Hub（D-01~D-19）。
// 设计：系统设计/03_设备代理方案.md
// 前端 APP 作为后端「眼耳口手」，通过 WebSocket 连接。
package device

import (
	"encoding/json"
	"time"
)

// Device 已注册设备。
type Device struct {
	ID           string            `json:"id"`
	Name         string            `json:"name"`
	Model        string            `json:"model"`
	Android      string            `json:"android"`       // Android 版本
	Capabilities []string          `json:"capabilities"`  // 能力列表
	Permissions  map[string]bool   `json:"permissions"`   // 权限状态
	Online       bool              `json:"online"`
	ConnectedAt  time.Time         `json:"connected_at"`
	LastSeen     time.Time         `json:"last_seen"`
	PairCode     string            `json:"-"`             // 配对码（不暴露）
	Paired       bool              `json:"paired"`
}

// Command 下发命令。
type Command struct {
	ID        string          `json:"id"`
	DeviceID  string          `json:"device_id"`
	Method    string          `json:"method"`
	Params    json.RawMessage `json:"params,omitempty"`
	Status    string          `json:"status"` // pending/sent/done/failed
	CreatedAt time.Time       `json:"created_at"`
	Result    *CommandResult  `json:"result,omitempty"`
}

// CommandResult 命令执行结果。
type CommandResult struct {
	OK   bool   `json:"ok"`
	Data string `json:"data,omitempty"`
	Err  string `json:"err,omitempty"`
}

// Event 设备主动事件。
type Event struct {
	Type  string `json:"type"`
	Event string `json:"event"`
	Data  any    `json:"data,omitempty"`
}

// WSMessage WebSocket 消息（JSON-RPC 2.0 格式）。
type WSMessage struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}
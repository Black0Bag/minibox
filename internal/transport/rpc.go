package transport

import (
	"context"
	"encoding/json"
)

// RPCError 是 JSON-RPC 远端错误的最小公共表示。
// 它位于基础 transport 包中，避免设备 Hub 依赖具体 WebSocket 实现。
type RPCError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

// RPCResponse 是一次 JSON-RPC 请求的结果。
type RPCResponse struct {
	Result json.RawMessage
	Error  *RPCError
}

// RPCRequester 发送一条带 id 的 JSON-RPC 请求并等待对应响应。
// 实现必须由单一读循环负责从底层连接读取消息，并按 id 分发响应。
type RPCRequester interface {
	SendRequest(ctx context.Context, id, method string, params json.RawMessage) (RPCResponse, error)
}

// RPCClient 是可双向使用的 JSON-RPC 客户端连接。
// Close 用于解绑或急停时主动终止设备通道。
type RPCClient interface {
	RPCRequester
	Close() error
}

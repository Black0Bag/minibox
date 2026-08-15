// Package ws 提供 WebSocket 传输层（设备代理 + 心跳 + 互联）。
// 设计（进度跟踪 20260812 第2/3项 + RemoteClaw/devicerail 实证）：
//   - 文本帧 + JSON-RPC 2.0 + connect 握手（首帧必须 connect）
//   - 未握手调方法报 handshake_required
//   - method 分组：connect/disconnect/device.*/browser.*/peer.*/heartbeat.*/event.*/system.*
//   - 错误码 -32001~-32005 应用扩展区间
//   - 无参数方法省略 params 字段
//   - 心跳 heartbeat.ping/pong
package ws

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"sync"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

// 应用错误码（QC4：-32001~-32005 区间）。
const (
	codeHandshakeRequired = -32001 // 未握手
	codeAuthFailed        = -32002 // 认证失败
	codeDeviceOffline     = -32003 // 设备离线
	codeTimeout           = -32004 // 超时
	codeRateLimited       = -32005 // 限流
)

type request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"` // 有 id = 请求；无 = 通知
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"` // 无参数省略
}

// response JSON-RPC 2.0 响应。
type response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

// rpcError JSON-RPC 错误。
type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

// client 已连接客户端（设备/前端）。
type client struct {
	id        string
	name      string
	handshake bool
}

// HandlerFunc 处理一个 method 请求。
type HandlerFunc func(ctx context.Context, c *client, params json.RawMessage) (any, error)

// Server WebSocket 传输层服务器。
type Server struct {
	logger *slog.Logger
	// method 路由表（不含 connect/disconnect 内置）
	routes map[string]HandlerFunc
	// 已连接客户端：connID → client（map 并发访问需锁，golang-concurrency）
	mu      sync.RWMutex
	clients map[*websocket.Conn]*client
}

// New 创建 WS 服务器。
func New(logger *slog.Logger) *Server {
	return &Server{
		logger:  logger,
		routes:  make(map[string]HandlerFunc),
		clients: make(map[*websocket.Conn]*client),
	}
}

// Handle 注册 method 处理器（如 "device.camera.photo"）。
// 前缀路由：注册 "device" 可处理所有 device.* 方法。
func (s *Server) Handle(method string, fn HandlerFunc) {
	s.routes[method] = fn
}

// Handler 返回 WS 升级处理器（GET /device/ws）。
func (s *Server) Handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			s.logger.Warn("WS 握手失败", "err", err)
			return
		}
		cli := &client{}
		s.mu.Lock()
		s.clients[c] = cli
		s.mu.Unlock()
		defer func() {
			_ = c.Close(websocket.StatusNormalClosure, "")
			s.mu.Lock()
			delete(s.clients, c)
			s.mu.Unlock()
		}()

		ctx := r.Context()
		for {
			// 读 JSON-RPC 消息
			var req request
			if err := wsjson.Read(ctx, c, &req); err != nil {
				// 连接关闭或读错误 → 结束
				return
			}
			// 处理消息（请求或通知）
			s.dispatch(ctx, c, cli, req)
		}
	}
}

// dispatch 分发一条 JSON-RPC 消息。
// 请求（有 id）→ 必须回 response；通知（无 id）→ 不回。
func (s *Server) dispatch(ctx context.Context, conn *websocket.Conn, cli *client, req request) {
	// 内置 method：connect/disconnect 不需要握手
	switch req.Method {
	case "connect":
		s.handleConnect(ctx, conn, cli, req)
		return
	case "disconnect":
		s.handleDisconnect(ctx, conn, cli, req)
		return
	case "heartbeat.ping":
		s.reply(ctx, conn, req, map[string]any{"pong": true})
		return
	}

	// 其他 method 需先握手（QC3）
	if !cli.handshake {
		s.replyError(ctx, conn, req, codeHandshakeRequired, "handshake_required", nil)
		return
	}

	// 路由：精确匹配或前缀
	fn, ok := s.lookup(req.Method)
	if !ok {
		s.replyError(ctx, conn, req, -32601, "method not found", nil)
		return
	}

	// 执行 handler（通知不等待结果）
	if isNotification(req) {
		// 通知：go 执行，不回响应（QC8）
		go func() {
			_, _ = fn(context.WithoutCancel(ctx), cli, req.Params)
		}()
		return
	}
	result, err := fn(ctx, cli, req.Params)
	if err != nil {
		s.replyError(ctx, conn, req, -32603, err.Error(), nil)
		return
	}
	s.reply(ctx, conn, req, result)
}

// lookup 查找 method 处理器：精确优先，否则按一级前缀。
func (s *Server) lookup(method string) (HandlerFunc, bool) {
	if fn, ok := s.routes[method]; ok {
		return fn, true
	}
	// 一级前缀：device.* → "device"
	if i := strings.IndexByte(method, '.'); i > 0 {
		if fn, ok := s.routes[method[:i]]; ok {
			return fn, true
		}
	}
	return nil, false
}

// isNotification 判断是否为通知（无 id）。
func isNotification(req request) bool {
	return len(req.ID) == 0 || string(req.ID) == "null"
}

// handleConnect 握手。
func (s *Server) handleConnect(ctx context.Context, conn *websocket.Conn, cli *client, req request) {
	var params struct {
		Client   string `json:"client"`
		Protocol string `json:"protocol"`
		Auth     string `json:"auth,omitempty"`
	}
	if len(req.Params) > 0 && string(req.Params) != "null" {
		_ = json.Unmarshal(req.Params, &params)
	}
	if params.Protocol != "" && params.Protocol != "1.0" {
		s.replyError(ctx, conn, req, codeAuthFailed, "unsupported protocol", nil)
		return
	}
	cli.handshake = true
	cli.id = params.Client
	cli.name = params.Client
	s.reply(ctx, conn, req, map[string]any{"ok": true, "protocol": "1.0"})
}

// handleDisconnect 断开。
func (s *Server) handleDisconnect(ctx context.Context, conn *websocket.Conn, cli *client, req request) {
	cli.handshake = false
	s.reply(ctx, conn, req, map[string]any{"ok": true})
	_ = conn.Close(websocket.StatusNormalClosure, "bye")
}

// reply 回成功响应。
func (s *Server) reply(ctx context.Context, conn *websocket.Conn, req request, result any) {
	resp := response{JSONRPC: "2.0", ID: req.ID, Result: result}
	_ = wsjson.Write(ctx, conn, resp)
}

// replyError 回错误响应。
func (s *Server) replyError(ctx context.Context, conn *websocket.Conn, req request, code int, msg string, data any) {
	resp := response{
		JSONRPC: "2.0",
		ID:      req.ID,
		Error:   &rpcError{Code: code, Message: msg, Data: data},
	}
	_ = wsjson.Write(ctx, conn, resp)
}

// Broadcast 向所有已握手客户端推送（event.push 通知，QC8）。
func (s *Server) Broadcast(ctx context.Context, typ string, payload any) {
	s.mu.RLock()
	conns := make([]*websocket.Conn, 0, len(s.clients))
	for conn := range s.clients {
		conns = append(conns, conn)
	}
	s.mu.RUnlock()
	for _, conn := range conns {
		_ = wsjson.Write(ctx, conn, map[string]any{
			"jsonrpc": "2.0", "method": "event.push",
			"params": map[string]any{"type": typ, "payload": payload},
		})
	}
}

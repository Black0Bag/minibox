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
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"

	"github.com/Black0Bag/minibox/internal/transport"
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
	Method  string          `json:"method,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

// rpcError JSON-RPC 错误。
type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

// Client 已连接客户端（设备/前端）。
//
// 并发约束：连接的读循环（Server.Handler）与通知处理 goroutine
// （dispatch 对无 id 请求走 go func）可能同时访问同一 Client，
// 因此 ID / Name / Handshake / Data 一律通过访问方法读写，由 metaMu 保护。
// 直接读写这些字段是 data race（CI race job 实证）。
type Client struct {
	// metaMu 保护握手状态与连接元数据（id/name/data）。
	metaMu    sync.RWMutex
	id        string
	name      string
	handshake bool
	data      any // 附加数据（设备信息等）

	conn *websocket.Conn

	// writeMu 让本连接的请求和响应按完整 JSON 消息串行写入。
	// 虽然 coder/websocket 支持并发写，此锁仍避免协议层消息在测试和日志中失序。
	writeMu sync.Mutex

	pendingMu sync.Mutex
	pending   map[string]chan rpcReply
}

// ID 返回握手时上报的客户端 ID。
func (c *Client) ID() string {
	c.metaMu.RLock()
	defer c.metaMu.RUnlock()
	return c.id
}

// Name 返回客户端名称（当前与 ID 同源）。
func (c *Client) Name() string {
	c.metaMu.RLock()
	defer c.metaMu.RUnlock()
	return c.name
}

// Handshaked 返回是否已完成 connect 握手。
func (c *Client) Handshaked() bool {
	c.metaMu.RLock()
	defer c.metaMu.RUnlock()
	return c.handshake
}

// Data 返回附加数据（设备 Hub 用来挂载 *device.Device）。
func (c *Client) Data() any {
	c.metaMu.RLock()
	defer c.metaMu.RUnlock()
	return c.data
}

// SetData 设置附加数据（设备注册后由组合根调用）。
func (c *Client) SetData(v any) {
	c.metaMu.Lock()
	defer c.metaMu.Unlock()
	c.data = v
}

// setHandshake 记录握手结果与连接元数据（仅本包内部使用）。
func (c *Client) setHandshake(ok bool, id, name string) {
	c.metaMu.Lock()
	defer c.metaMu.Unlock()
	c.handshake = ok
	if ok {
		c.id = id
		c.name = name
	}
}

type rpcReply struct {
	Result json.RawMessage
	Error  *rpcError
	Err    error
}

// SendRequest 将一条 JSON-RPC 请求发给客户端，并等待同 id 的响应。
// Client 的连接读取只能由 Server.Handler 的单一读循环执行；该方法仅注册等待者并写入请求。
func (c *Client) SendRequest(ctx context.Context, id, method string, params json.RawMessage) (transport.RPCResponse, error) {
	if c == nil || c.conn == nil {
		return transport.RPCResponse{}, fmt.Errorf("WebSocket 客户端未连接")
	}
	if id == "" {
		return transport.RPCResponse{}, fmt.Errorf("JSON-RPC 请求 id 不能为空")
	}

	waiter := make(chan rpcReply, 1)
	if err := c.registerPending(id, waiter); err != nil {
		return transport.RPCResponse{}, err
	}
	defer c.removePending(id, waiter)

	req := request{JSONRPC: "2.0", ID: json.RawMessage(strconv.Quote(id)), Method: method}
	if len(params) > 0 && string(params) != "null" {
		req.Params = append(json.RawMessage(nil), params...)
	}
	if err := c.writeJSON(ctx, req); err != nil {
		return transport.RPCResponse{}, fmt.Errorf("写入 JSON-RPC 请求: %w", err)
	}

	select {
	case reply := <-waiter:
		if reply.Err != nil {
			return transport.RPCResponse{}, reply.Err
		}
		out := transport.RPCResponse{Result: append(json.RawMessage(nil), reply.Result...)}
		if reply.Error != nil {
			out.Error = &transport.RPCError{Code: reply.Error.Code, Message: reply.Error.Message}
			if reply.Error.Data != nil {
				out.Error.Data, _ = json.Marshal(reply.Error.Data)
			}
		}
		return out, nil
	case <-ctx.Done():
		return transport.RPCResponse{}, fmt.Errorf("等待 JSON-RPC 响应: %w", ctx.Err())
	}
}

// Close 关闭客户端连接，供设备 Hub 的急停/解绑路径调用。
func (c *Client) Close() error {
	if c == nil || c.conn == nil {
		return nil
	}
	return c.conn.Close(websocket.StatusNormalClosure, "closed by server")
}

var _ transport.RPCRequester = (*Client)(nil)

func (c *Client) registerPending(id string, waiter chan rpcReply) error {
	c.pendingMu.Lock()
	defer c.pendingMu.Unlock()
	if c.pending == nil {
		c.pending = make(map[string]chan rpcReply)
	}
	if _, exists := c.pending[id]; exists {
		return fmt.Errorf("JSON-RPC 请求 id 已在等待中: %s", id)
	}
	c.pending[id] = waiter
	return nil
}

func (c *Client) removePending(id string, waiter chan rpcReply) {
	c.pendingMu.Lock()
	defer c.pendingMu.Unlock()
	if c.pending[id] == waiter {
		delete(c.pending, id)
	}
}

func (c *Client) deliverReply(id string, reply rpcReply) bool {
	c.pendingMu.Lock()
	waiter, ok := c.pending[id]
	if ok {
		delete(c.pending, id)
	}
	c.pendingMu.Unlock()
	if !ok {
		return false
	}
	waiter <- reply
	return true
}

func rpcIDKey(raw json.RawMessage) (string, bool) {
	if len(raw) == 0 || string(raw) == "null" {
		return "", false
	}
	var str string
	if err := json.Unmarshal(raw, &str); err == nil {
		return str, true
	}
	// JSON-RPC 允许数字 id；保留其规范化前的 JSON 文本作为 key。
	return string(raw), true
}

func (c *Client) failPending(err error) {
	c.pendingMu.Lock()
	pending := c.pending
	c.pending = make(map[string]chan rpcReply)
	c.pendingMu.Unlock()
	for _, waiter := range pending {
		waiter <- rpcReply{Err: fmt.Errorf("WebSocket 连接已关闭: %w", err)}
	}
}

func (c *Client) writeJSON(ctx context.Context, v any) error {
	if c == nil || c.conn == nil {
		return fmt.Errorf("WebSocket 客户端未连接")
	}
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	return wsjson.Write(ctx, c.conn, v)
}

// HandlerFunc 处理一个 method 请求。
type HandlerFunc func(ctx context.Context, c *Client, params json.RawMessage) (any, error)

// Server WebSocket 传输层服务器。
type Server struct {
	logger *slog.Logger
	// method 路由表（不含 connect/disconnect 内置）
	routes map[string]HandlerFunc
	// 已连接客户端：connID → client（map 并发访问需锁，golang-concurrency）
	mu      sync.RWMutex
	clients map[*websocket.Conn]*Client
	// CredentialCheck 可选凭据校验函数，非 nil 时 connect 必须传入有效 token。
	// 签名：candidate token → 是否有效（与 setup.DeviceCredential.Verify 匹配）。
	CredentialCheck func(candidate string) bool
}

// New 创建 WS 服务器。
func New(logger *slog.Logger) *Server {
	return &Server{
		logger:  logger,
		routes:  make(map[string]HandlerFunc),
		clients: make(map[*websocket.Conn]*Client),
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
		cli := &Client{conn: c, pending: make(map[string]chan rpcReply)}

		s.mu.Lock()
		s.clients[c] = cli
		s.mu.Unlock()
		defer func() {
			cli.failPending(fmt.Errorf("WebSocket 连接已关闭"))
			_ = c.Close(websocket.StatusNormalClosure, "")
			s.mu.Lock()
			delete(s.clients, c)
			s.mu.Unlock()
		}()

		ctx := r.Context()
		for {
			// 读 JSON-RPC 消息。coder/websocket 要求 Reader/Read 不能并发调用；
			// 因此整个连接只保留这一处读循环。
			var raw json.RawMessage
			if err := wsjson.Read(ctx, c, &raw); err != nil {
				return
			}

			// 响应必须先按 id 交给等待中的请求；没有 pending waiter 的消息
			// 才继续作为来自客户端的请求/通知处理。
			var resp response
			if err := json.Unmarshal(raw, &resp); err == nil && len(resp.ID) > 0 && (resp.Result != nil || resp.Error != nil) {
				if id, ok := rpcIDKey(resp.ID); ok && cli.deliverReply(id, rpcReply{Result: resp.Result, Error: resp.Error}) {
					continue
				}
			}

			var req request
			if err := json.Unmarshal(raw, &req); err != nil {
				return
			}
			// 已识别为响应但没有对应等待者：可能是超时后到达的迟到回执。
			// 不把它误当成 method 为空的请求再回一条错误，避免协议回声。
			if resp.JSONRPC == "2.0" && len(resp.ID) > 0 && resp.Method == "" && (resp.Result != nil || resp.Error != nil) {
				continue
			}
			s.dispatch(ctx, c, cli, req)
		}
	}
}

// dispatch 分发一条 JSON-RPC 消息。
// 请求（有 id）→ 必须回 response；通知（无 id）→ 不回。
func (s *Server) dispatch(ctx context.Context, conn *websocket.Conn, cli *Client, req request) {
	// 内置 method：connect/disconnect 不需要握手
	switch req.Method {
	case "connect":
		s.handleConnect(ctx, conn, cli, req)
		return
	case "disconnect":
		s.handleDisconnect(ctx, conn, cli, req)
		return
	case "heartbeat.ping":
		s.reply(ctx, conn, cli, req, map[string]any{"pong": true})
		return
	}

	// 其他 method 需先握手（QC3）
	if !cli.Handshaked() {
		s.replyError(ctx, conn, cli, req, codeHandshakeRequired, "handshake_required", nil)
		return
	}

	// 路由：精确匹配或前缀
	fn, ok := s.lookup(req.Method)
	if !ok {
		s.replyError(ctx, conn, cli, req, -32601, "method not found", nil)
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
		s.replyError(ctx, conn, cli, req, -32603, err.Error(), nil)
		return
	}
	s.reply(ctx, conn, cli, req, result)
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
func (s *Server) handleConnect(ctx context.Context, conn *websocket.Conn, cli *Client, req request) {
	var params struct {
		Client   string `json:"client"`
		Protocol string `json:"protocol"`
		Auth     string `json:"auth,omitempty"`
	}
	if len(req.Params) > 0 && string(req.Params) != "null" {
		_ = json.Unmarshal(req.Params, &params)
	}
	if params.Protocol != "" && params.Protocol != "1.0" {
		s.replyError(ctx, conn, cli, req, codeAuthFailed, "unsupported protocol", nil)
		return
	}
	if s.CredentialCheck != nil && !s.CredentialCheck(params.Auth) {
		s.replyError(ctx, conn, cli, req, codeAuthFailed, "invalid credential", nil)
		return
	}
	cli.setHandshake(true, params.Client, params.Client)
	s.reply(ctx, conn, cli, req, map[string]any{"ok": true, "protocol": "1.0"})
}

// handleDisconnect 断开。
func (s *Server) handleDisconnect(ctx context.Context, conn *websocket.Conn, cli *Client, req request) {
	cli.setHandshake(false, "", "")
	s.reply(ctx, conn, cli, req, map[string]any{"ok": true})
	_ = conn.Close(websocket.StatusNormalClosure, "bye")
}

// reply 回成功响应。
func (s *Server) reply(ctx context.Context, _ *websocket.Conn, cli *Client, req request, result any) {
	if isNotification(req) {
		return
	}
	resp := response{JSONRPC: "2.0", ID: req.ID, Result: json.RawMessage("null")}
	if result != nil {
		data, err := json.Marshal(result)
		if err != nil {
			s.replyError(ctx, nil, cli, req, -32603, "响应序列化失败", nil)
			return
		}
		resp.Result = data
	}
	_ = cli.writeJSON(ctx, resp)
}

// replyError 回错误响应。
func (s *Server) replyError(ctx context.Context, _ *websocket.Conn, cli *Client, req request, code int, msg string, data any) {
	if isNotification(req) {
		return
	}
	resp := response{
		JSONRPC: "2.0",
		ID:      req.ID,
		Error:   &rpcError{Code: code, Message: msg, Data: data},
	}
	_ = cli.writeJSON(ctx, resp)
}

// Broadcast 向所有已握手客户端推送（event.push 通知，QC8）。
func (s *Server) Broadcast(ctx context.Context, typ string, payload any) {
	s.mu.RLock()
	clients := make([]*Client, 0, len(s.clients))
	for _, cli := range s.clients {
		if cli.Handshaked() {
			clients = append(clients, cli)
		}
	}
	s.mu.RUnlock()
	for _, cli := range clients {
		_ = cli.writeJSON(ctx, map[string]any{
			"jsonrpc": "2.0", "method": "event.push",
			"params": map[string]any{"type": typ, "payload": payload},
		})
	}
}

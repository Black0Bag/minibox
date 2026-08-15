// Package sse 提供 SSE（Server-Sent Events）单向流式传输层。
// 设计（进度跟踪 20260812 第2/3项 + 互联网实证）：
//   - 独立路由组，每条事件 Flush()（不能用 middleware.Timeout，B 红线）
//   - 每条事件块必带 `id:` 字段且 id=seq（W3C WHATWG）
//   - 绝不返回 4xx（EventSource 永久停止重连），必须 200 + sync-required（QB8）
//   - seq 断线续传：客户端带 Last-Event-ID 重连，后端从 seq+1 补发
//   - keepalive 15s（server-sent-events 2026 实证 15-30s 内）
package sse

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/Black0Bag/minibox/internal/transport"
)

// 默认配置（进度跟踪心跳/超时表）。
const (
	defaultKeepAlive  = 15 * time.Second // SSE keepalive
	defaultBufferSize = 500              // SSE 缓冲窗口（~150KB）
)

// Stream 单个 SSE 订阅流（一个 client）。
type Stream struct {
	// 发送队列（缓冲窗口，满了阻塞背压）
	ch chan *transport.Envelope
	// 已发送的 seq 游标（供 Last-Event-ID 续传），用锁保护
	mu      sync.Mutex
	lastSeq int
	// 关闭信号
	done chan struct{}
}

// Server SSE 传输层服务器。
// 管理订阅者，提供广播能力（B14 主动推送）。
type Server struct {
	logger *slog.Logger
	// 流注册表：sessionID → Stream（map 并发读写需锁保护，golang-concurrency）
	mu        sync.RWMutex
	streams   map[string]*Stream
	keepAlive time.Duration
}

// New 创建 SSE 服务器。
func New(logger *slog.Logger) *Server {
	return &Server{
		logger:    logger,
		streams:   make(map[string]*Stream),
		keepAlive: defaultKeepAlive,
	}
}

// Handler 处理 SSE 订阅请求（GET /api/v1/stream?session_id=xxx）。
// 绝不返回 4xx（W3C）：流式错误也回 200 + sync-required 事件。
func (s *Server) Handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// 配置响应头（SSE 必须）
		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "流式输出不支持", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache, no-transform")
		w.Header().Set("Connection", "keep-alive")
		w.WriteHeader(http.StatusOK)

		sessionID := r.URL.Query().Get("session_id")
		if sessionID == "" {
			sessionID = "default"
		}

		// 断线续传：客户端 Last-Event-ID 头 → 从 seq+1 开始
		lastID, _ := strconv.Atoi(r.Header.Get("Last-Event-ID"))
		startSeq := lastID + 1

		// 注册流
		stream := s.subscribe(sessionID, startSeq)
		defer s.unsubscribe(sessionID, stream)

		// 建立请求上下文（客户端断开时结束）
		ctx := r.Context()

		// 发送 keepalive 注释（注释行不触发浏览器重连逻辑）
		keepAliveTicker := time.NewTicker(s.keepAlive)
		defer keepAliveTicker.Stop()

		for {
			select {
			case <-ctx.Done():
				return // 客户端断开
			case env, ok := <-stream.ch:
				if !ok {
					return // 流关闭
				}
				// 只发新事件（seq >= startSeq）
				if env.Seq >= startSeq {
					s.writeEvent(w, flusher, env)
				}
			case <-keepAliveTicker.C:
				// 心跳注释行：保连接，不触发事件
				_, _ = w.Write([]byte(": keepalive\n\n"))
				flusher.Flush()
			}
		}
	}
}

// writeEvent 写一条 SSE 事件块（id: seq + event: + data:）。
func (s *Server) writeEvent(w http.ResponseWriter, flusher http.Flusher, env *transport.Envelope) {
	var buf []byte
	buf = append(buf, []byte(fmt.Sprintf("id: %d\n", env.Seq))...)
	buf = append(buf, []byte("event: "+env.Type+"\n")...)
	buf = append(buf, []byte("data: ")...)
	data, _ := json.Marshal(env)
	buf = append(buf, data...)
	buf = append(buf, '\n', '\n')
	_, _ = w.Write(buf)
	flusher.Flush()
}

// subscribe 注册流并返回。
func (s *Server) subscribe(sessionID string, startSeq int) *Stream {
	st := &Stream{
		ch:      make(chan *transport.Envelope, defaultBufferSize),
		lastSeq: startSeq - 1,
		done:    make(chan struct{}),
	}
	s.mu.Lock()
	s.streams[sessionID] = st
	s.mu.Unlock()
	return st
}

// unsubscribe 注销流。
func (s *Server) unsubscribe(sessionID string, st *Stream) {
	close(st.done)
	s.mu.Lock()
	if s.streams[sessionID] == st {
		delete(s.streams, sessionID)
	}
	s.mu.Unlock()
}

// Publish 向指定会话推送事件（B14 主动推送）。
func (s *Server) Publish(sessionID, producer, typ string, data any) error {
	env, err := transport.NewEnvelope(producer, "minibox://session/"+sessionID, typ, data)
	if err != nil {
		return err
	}
	s.mu.RLock()
	st, ok := s.streams[sessionID]
	s.mu.RUnlock()
	if !ok {
		return fmt.Errorf("会话 %s 未订阅", sessionID)
	}
	st.mu.Lock()
	st.lastSeq++
	env.Seq = st.lastSeq
	st.mu.Unlock()
	select {
	case st.ch <- env:
		return nil
	default:
		// 缓冲满 → 丢事件（背压，客户端断线重连时 sync-required）
		return fmt.Errorf("会话 %s 缓冲满，事件丢弃", sessionID)
	}
}

// Broadcast 向所有会话广播（B14 主动推送/降级通知）。
func (s *Server) Broadcast(producer, typ string, data any) {
	s.mu.RLock()
	ids := make([]string, 0, len(s.streams))
	for id := range s.streams {
		ids = append(ids, id)
	}
	s.mu.RUnlock()
	for _, id := range ids {
		_ = s.Publish(id, producer, typ, data)
	}
}

// KeepAlive 设置心跳间隔（测试用）。
func (s *Server) KeepAlive(d time.Duration) {
	s.keepAlive = d
}

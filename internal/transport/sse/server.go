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
	// nextSeq 是每个会话独立的单调序号，即使暂时没有订阅者也继续递增。
	nextSeq map[string]int
	// history 保存最近事件，供 Last-Event-ID 断线续传；按 session 隔离。
	history map[string][]*transport.Envelope
}

// New 创建 SSE 服务器。
func New(logger *slog.Logger) *Server {
	return &Server{
		logger:    logger,
		streams:   make(map[string]*Stream),
		keepAlive: defaultKeepAlive,
		nextSeq:   make(map[string]int),
		history:   make(map[string][]*transport.Envelope),
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
		// 立即发送状态行和 SSE 头，避免客户端在首个业务事件前阻塞握手。
		flusher.Flush()

		sessionID := r.URL.Query().Get("session_id")
		if sessionID == "" {
			sessionID = "default"
		}

		// 断线续传：客户端 Last-Event-ID 头 → 从 seq+1 开始
		lastID, _ := strconv.Atoi(r.Header.Get("Last-Event-ID"))
		sub := s.subscribeWithReplay(sessionID, lastID)
		stream := sub.stream
		defer s.unsubscribe(sessionID, stream)

		// 历史快照与流注册在同一把锁内完成，之后只消费实时队列。
		for _, env := range sub.replay {
			s.writeEvent(w, flusher, env)
		}
		startSeq := sub.startSeq

		// 建立请求上下文（客户端断开时结束）
		ctx := r.Context()

		// 发送 keepalive 注释（注释行不触发浏览器重连逻辑）
		keepAliveTicker := time.NewTicker(s.keepAlive)
		defer keepAliveTicker.Stop()

		for {
			select {
			case <-ctx.Done():
				return // 客户端断开
			case <-stream.done:
				return // 被同 session 的新订阅替换
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

// subscribe 注册流并返回。测试和内部调用只需要流本身时使用此包装。
func (s *Server) subscribe(sessionID string, startSeq int) *Stream {
	return s.subscribeWithReplay(sessionID, startSeq-1).stream
}

type replaySubscription struct {
	stream   *Stream
	replay   []*transport.Envelope
	startSeq int
}

// subscribeWithReplay 原子地注册流并截取历史，避免历史回放与实时 Publish 之间出现重复/丢失窗口。
func (s *Server) subscribeWithReplay(sessionID string, lastID int) replaySubscription {
	startSeq := lastID + 1
	st := &Stream{
		ch:      make(chan *transport.Envelope, defaultBufferSize),
		lastSeq: lastID,
		done:    make(chan struct{}),
	}

	s.mu.Lock()
	if startSeq > s.nextSeq[sessionID] {
		s.nextSeq[sessionID] = lastID
	}
	if old := s.streams[sessionID]; old != nil {
		// 同一 session 的新连接替换旧连接；旧 Handler 会因 done/ctx 退出。
		close(old.done)
	}
	s.streams[sessionID] = st

	replay := make([]*transport.Envelope, 0, len(s.history[sessionID]))
	for _, env := range s.history[sessionID] {
		if env.Seq <= lastID {
			continue
		}
		cp := *env
		replay = append(replay, &cp)
	}
	s.mu.Unlock()

	return replaySubscription{stream: st, replay: replay, startSeq: startSeq}
}

// replay 返回指定会话中 seq 大于 lastID 的历史事件副本。
func (s *Server) replay(sessionID string, lastID int) []*transport.Envelope {
	s.mu.RLock()
	defer s.mu.RUnlock()
	history := s.history[sessionID]
	out := make([]*transport.Envelope, 0, len(history))
	for _, env := range history {
		if env.Seq <= lastID {
			continue
		}
		cp := *env
		out = append(out, &cp)
	}
	return out
}

// appendHistory 在锁内保存有限历史，超出窗口时丢弃最旧事件。
func (s *Server) appendHistoryLocked(sessionID string, env *transport.Envelope) {
	history := append(s.history[sessionID], env)
	if len(history) > defaultBufferSize {
		history = history[len(history)-defaultBufferSize:]
	}
	s.history[sessionID] = history
}

// unsubscribe 注销流。
func (s *Server) unsubscribe(sessionID string, st *Stream) {
	s.mu.Lock()
	if s.streams[sessionID] == st {
		delete(s.streams, sessionID)
		s.mu.Unlock()
		close(st.done)
		return
	}
	s.mu.Unlock()
}

// Publish 向指定会话推送事件（B14 主动推送）。
// 即使当前没有订阅者，事件也会进入有限历史缓冲，供客户端重连时回放。
func (s *Server) Publish(sessionID, producer, typ string, data any) error {
	env, err := transport.NewEnvelope(producer, "minibox://session/"+sessionID, typ, data)
	if err != nil {
		return err
	}

	s.mu.Lock()
	s.nextSeq[sessionID]++
	env.Seq = s.nextSeq[sessionID]
	s.appendHistoryLocked(sessionID, env)
	st := s.streams[sessionID]
	s.mu.Unlock()
	if st == nil {
		return nil
	}

	select {
	case st.ch <- env:
		return nil
	default:
		return fmt.Errorf("会话 %s 缓冲满，事件已写入续传历史", sessionID)
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

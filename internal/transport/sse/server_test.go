package sse

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Black0Bag/minibox/internal/transport"
)

// TestPublish_Receive 推送事件被流接收。
func TestPublish_Receive(t *testing.T) {
	srv := New(slog.New(slog.DiscardHandler))
	// 注册流（模拟 Handler 订阅）
	st := srv.subscribe("s1", 1)

	if err := srv.Publish("s1", "agent", "agent.text_message_content", map[string]string{"delta": "hi"}); err != nil {
		t.Fatalf("Publish err=%v", err)
	}

	select {
	case env := <-st.ch:
		if env.Type != "agent.text_message_content" {
			t.Errorf("type=%q, 期望 agent.text_message_content", env.Type)
		}
		if env.Seq != 1 {
			t.Errorf("seq=%d, 期望 1", env.Seq)
		}
	case <-time.After(time.Second):
		t.Fatal("事件未送达")
	}
}

// TestSeq_Sequence 多事件 seq 单调递增。
func TestSeq_Sequence(t *testing.T) {
	srv := New(slog.New(slog.DiscardHandler))
	srv.subscribe("s1", 1)
	for i := 0; i < 3; i++ {
		if err := srv.Publish("s1", "agent", "agent.x", nil); err != nil {
			t.Fatalf("Publish %d err=%v", i, err)
		}
	}
	for i := 1; i <= 3; i++ {
		env := <-srv.streams["s1"].ch
		if env.Seq != i {
			t.Fatalf("seq=%d, 期望 %d", env.Seq, i)
		}
	}
}

// TestSeq_续传 startSeq 只发 >= startSeq 的事件。
func TestSeq_续传(t *testing.T) {
	srv := New(slog.New(slog.DiscardHandler))
	srv.subscribe("s1", 2) // 模拟 Last-Event-ID=1，从 seq 2 开始
	for i := 0; i < 3; i++ {
		_ = srv.Publish("s1", "agent", "agent.x", nil)
	}
	// 应只收到 seq 2,3
	first := <-srv.streams["s1"].ch
	if first.Seq != 2 {
		t.Fatalf("续传首条 seq=%d, 期望 2", first.Seq)
	}
}

func TestReplayHistoryAfterReconnect(t *testing.T) {
	srv := New(slog.New(slog.DiscardHandler))
	first := srv.subscribe("reconnect", 1)
	for i := 0; i < 3; i++ {
		if err := srv.Publish("reconnect", "agent", "agent.history", map[string]int{"n": i}); err != nil {
			t.Fatal(err)
		}
		<-first.ch
	}
	srv.unsubscribe("reconnect", first)

	// Publish while disconnected. The event must be retained for Last-Event-ID replay.
	if err := srv.Publish("reconnect", "agent", "agent.history", map[string]int{"n": 3}); err != nil {
		t.Fatal(err)
	}
	second := srv.subscribe("reconnect", 3)
	replayed := srv.replay("reconnect", 2)
	if len(replayed) != 2 || replayed[0].Seq != 3 || replayed[1].Seq != 4 {
		t.Fatalf("回放序列异常: %+v", replayed)
	}
	if err := srv.Publish("reconnect", "agent", "agent.history", map[string]int{"n": 4}); err != nil {
		t.Fatal(err)
	}
	got := <-second.ch
	if got.Seq != 5 {
		t.Fatalf("重连后的新事件 seq=%d, want 5", got.Seq)
	}
	srv.unsubscribe("reconnect", second)
}

func TestSubscribeReplacementDoesNotDoubleClose(t *testing.T) {
	srv := New(slog.New(slog.DiscardHandler))
	first := srv.subscribe("single-client", 1)
	second := srv.subscribe("single-client", 1)

	select {
	case <-first.done:
	case <-time.After(time.Second):
		t.Fatal("新订阅应关闭旧订阅")
	}

	// 旧 Handler 的 defer 可安全调用 unsubscribe，不能误删/关闭当前订阅。
	srv.unsubscribe("single-client", first)
	if srv.streams["single-client"] != second {
		t.Fatal("旧订阅清理不应删除当前订阅")
	}
	srv.unsubscribe("single-client", second)
}
func TestHandler_SSE响应(t *testing.T) {
	srv := New(slog.New(slog.DiscardHandler))
	req := httptest.NewRequest(http.MethodGet, "/api/v1/stream?session_id=t1", nil)
	w := httptest.NewRecorder()

	// Handler 是阻塞流：先启动 goroutine，推送事件后验证写入的 body，
	// 再用带取消的 context 请求结束流（httptest 的 req.Close 不触发 ctx 取消）。
	ctx, cancel := context.WithCancel(req.Context())
	req = req.WithContext(ctx)

	done := make(chan struct{})
	go func() {
		srv.Handler()(w, req)
		close(done)
	}()

	// 等订阅建立
	time.Sleep(50 * time.Millisecond)
	_ = srv.Publish("t1", "agent", "agent.test", map[string]string{"k": "v"})
	// 等事件写入
	time.Sleep(50 * time.Millisecond)

	// 验证响应头
	if ct := w.Header().Get("Content-Type"); ct != "text/event-stream" {
		t.Errorf("Content-Type=%q, 期望 text/event-stream", ct)
	}
	if cc := w.Header().Get("Cache-Control"); cc != "no-cache, no-transform" {
		t.Errorf("Cache-Control=%q, 期望 no-cache, no-transform", cc)
	}

	// 取消 context 结束流
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("流未在 context 取消后退出")
	}
}

// TestBroadcast 广播到所有会话。
func TestBroadcast(t *testing.T) {
	srv := New(slog.New(slog.DiscardHandler))
	srv.subscribe("a", 1)
	srv.subscribe("b", 1)
	srv.Broadcast("system", "system.degrade", map[string]string{"level": "L1"})

	for _, id := range []string{"a", "b"} {
		select {
		case env := <-srv.streams[id].ch:
			if env.Type != "system.degrade" {
				t.Errorf("[%s] type=%q", id, env.Type)
			}
		case <-time.After(time.Second):
			t.Errorf("[%s] 广播未送达", id)
		}
	}
}

// TestEnvelope 信封字段。
func TestEnvelope(t *testing.T) {
	env, err := transport.NewEnvelope("agent", "src", "agent.x", nil)
	if err != nil {
		t.Fatal(err)
	}
	if env.SpecVersion != "1.0" {
		t.Errorf("spec_version=%q", env.SpecVersion)
	}
	if err := env.Validate(); err != nil {
		t.Errorf("validate: %v", err)
	}
}

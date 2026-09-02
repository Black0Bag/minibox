package app

import (
	"log/slog"
	"sync"
	"testing"
	"time"
)

// newTestHub 构造纯内存 hub（无 Agent、无持久化），用于会话状态管理测试。
func newTestHub() *sessionHub {
	return newSessionHub(nil, nil, slog.New(slog.DiscardHandler))
}

// TestSessionHub_List按更新时间倒序 回归：List 曾直接遍历 map 返回，
// 顺序随机（注释声称倒序但未实现），前端会话列表顺序不稳定。
func TestSessionHub_List按更新时间倒序(t *testing.T) {
	h := newTestHub()
	for _, id := range []string{"s_old", "s_mid", "s_new"} {
		h.createWithID(id)
	}

	// 显式设置不同更新时间（createWithID 用 time.Now()，同一纳秒内可能相同）
	base := time.Now()
	h.mu.Lock()
	h.sessions["s_old"].UpdatedAt = base.Add(-2 * time.Hour)
	h.sessions["s_mid"].UpdatedAt = base.Add(-1 * time.Hour)
	h.sessions["s_new"].UpdatedAt = base
	h.mu.Unlock()

	got := h.List()
	if len(got) != 3 {
		t.Fatalf("List 长度=%d, want 3", len(got))
	}
	for i, want := range []string{"s_new", "s_mid", "s_old"} {
		if got[i].ID != want {
			t.Errorf("got[%d].ID=%q, want %q（应按更新时间倒序）", i, got[i].ID, want)
		}
	}
}

// TestSessionHub_List同时间按ID稳定排序 保证相同更新时间下顺序可复现。
func TestSessionHub_List同时间按ID稳定排序(t *testing.T) {
	h := newTestHub()
	for _, id := range []string{"c", "a", "b"} {
		h.createWithID(id)
	}
	same := time.Now()
	h.mu.Lock()
	for _, s := range h.sessions {
		s.UpdatedAt = same
	}
	h.mu.Unlock()

	got := h.List()
	for i, want := range []string{"a", "b", "c"} {
		if got[i].ID != want {
			t.Errorf("got[%d].ID=%q, want %q（同时间应按 ID 排序）", i, got[i].ID, want)
		}
	}
}

// TestSessionHub_Get返回快照 回归：Get 曾返回内部指针，调用方修改会污染 hub 状态，
// 且与后台 driveRun 写入构成 data race。
func TestSessionHub_Get返回快照(t *testing.T) {
	h := newTestHub()
	h.createWithID("s1")
	h.appendMessage("s1", Message{Role: "user", Content: "原始内容", At: "t0"})

	snapshot, ok := h.Get("s1")
	if !ok {
		t.Fatal("Get 未找到会话")
	}
	// 修改快照不应影响 hub 内部状态
	snapshot.Title = "被篡改的标题"
	snapshot.Messages[0].Content = "被篡改的内容"
	snapshot.Messages = append(snapshot.Messages, Message{Role: "user", Content: "外部追加"})

	again, _ := h.Get("s1")
	if again.Title == "被篡改的标题" {
		t.Error("修改快照 Title 污染了 hub 内部状态")
	}
	if again.Messages[0].Content != "原始内容" {
		t.Errorf("Messages[0].Content=%q, 快照修改污染了内部状态", again.Messages[0].Content)
	}
	if len(again.Messages) != 1 {
		t.Errorf("内部消息数=%d, want 1（外部 append 不应生效）", len(again.Messages))
	}
}

// TestSessionHub_createWithID幂等 相同 ID 重复创建返回现有会话，不清空历史消息。
func TestSessionHub_createWithID幂等(t *testing.T) {
	h := newTestHub()
	h.createWithID("s1")
	h.appendMessage("s1", Message{Role: "user", Content: "已有消息", At: "t0"})

	again := h.createWithID("s1")
	if len(again.Messages) != 1 {
		t.Errorf("重复创建后消息数=%d, want 1（不得清空已有会话）", len(again.Messages))
	}
}

// TestSessionHub_Send使用请求会话ID 回归：会话不存在时原实现调 Create() 生成
// 新的随机 ID，导致用户消息写进另一个会话，前端按原 ID 拉取拿不到消息。
func TestSessionHub_Send使用请求会话ID(t *testing.T) {
	h := newTestHub() // agent 为 nil：Send 会在追加用户消息后返回“未装配”错误
	const id = "sess_from_client"

	if _, err := h.Send(t.Context(), id, "你好"); err == nil {
		t.Fatal("agent 未装配时 Send 应返回错误")
	}

	s, ok := h.Get(id)
	if !ok {
		t.Fatalf("会话 %q 不存在：用户消息被写到了别的会话", id)
	}
	if len(s.Messages) != 1 || s.Messages[0].Content != "你好" {
		t.Errorf("会话消息=%+v, 期望包含用户消息「你好」", s.Messages)
	}
}

// TestSessionHub_Rewind 回退保留指定条数，越界回退到 0。
func TestSessionHub_Rewind(t *testing.T) {
	h := newTestHub()
	h.createWithID("s1")
	for i := range 3 {
		h.appendMessage("s1", Message{Role: "user", Content: string(rune('a' + i)), At: "t"})
	}

	if err := h.Rewind("s1", 2); err != nil {
		t.Fatalf("Rewind err=%v", err)
	}
	if s, _ := h.Get("s1"); len(s.Messages) != 2 {
		t.Errorf("Rewind(2) 后消息数=%d, want 2", len(s.Messages))
	}
	if err := h.Rewind("s1", 99); err != nil { // 越界 → 清空
		t.Fatalf("Rewind err=%v", err)
	}
	if s, _ := h.Get("s1"); len(s.Messages) != 0 {
		t.Errorf("越界 Rewind 后消息数=%d, want 0", len(s.Messages))
	}
}

// TestSessionHub_并发读写 复现 driveRun goroutine 写入 + HTTP handler 读取的并发场景。
// -race 下可捕获未加锁实现的竞态（CI race job 提供该证据）。
func TestSessionHub_并发读写(t *testing.T) {
	h := newTestHub()
	for i := range 4 {
		h.createWithID(string(rune('a' + i)))
	}

	var wg sync.WaitGroup
	for i := range 4 {
		id := string(rune('a' + i))
		wg.Go(func() {
			for j := range 100 {
				h.appendMessage(id, Message{Role: "assistant", Content: "msg", At: "t"})
				if j%10 == 0 {
					h.persistMessage(id, "", "assistant", "msg") // runStore 为 nil：应直接返回
				}
			}
		})
		wg.Go(func() {
			for range 100 {
				h.List()
				h.Get(id)
			}
		})
	}
	wg.Wait()

	for i := range 4 {
		s, ok := h.Get(string(rune('a' + i)))
		if !ok {
			t.Fatalf("会话 %q 丢失", string(rune('a'+i)))
		}
		if len(s.Messages) != 100 {
			t.Errorf("会话 %q 消息数=%d, want 100", s.ID, len(s.Messages))
		}
	}
}

// TestParseDBTime 解析 conversation_log.created_at 的存储格式；非法输入不猜测。
func TestParseDBTime(t *testing.T) {
	if got, ok := parseDBTime("2026-09-02 13:45:07"); !ok || got.Year() != 2026 || got.Minute() != 45 {
		t.Errorf("解析合法时间失败: got=%v ok=%v", got, ok)
	}
	for _, bad := range []string{"", "not-a-time", "2026-09-02T13:45:07Z"} {
		if _, ok := parseDBTime(bad); ok {
			t.Errorf("parseDBTime(%q) 应返回 false", bad)
		}
	}
}

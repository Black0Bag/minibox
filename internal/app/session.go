package app

// 会话 hub：管理对话会话（阶段 1.1 对话端点后端）。
// 每次用户消息 → 启动一次 agent 运行 → 推进状态机直到 done/failed。

import (
	"context"
	"sync"
	"time"

	"github.com/Black0Bag/minibox/internal/domain/agent"
	"github.com/Black0Bag/minibox/internal/domain/llm"
	"github.com/Black0Bag/minibox/internal/domain/memory"
	"github.com/Black0Bag/minibox/internal/infrastructure/engine"
)

// sessionHub 会话管理器（内存实现，崩溃续跑留 Phase 8）。
type sessionHub struct {
	mu       sync.RWMutex
	sessions map[string]*Session
	agent    *engine.Engine
	compiler memory.Compiler
	// publish 可选事件推送器（SSE 对话流，阶段 1.3）
	publish func(sessionID, typ string, data any)
}

// Session 一个对话会话。
type Session struct {
	ID        string     `json:"id"`
	Title     string     `json:"title"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
	Messages  []Message  `json:"messages"`
	Mode      agent.Mode `json:"mode"`
}

// Message 会话消息（对外的轻量结构）。
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
	RunID   string `json:"run_id,omitempty"`
	At      string `json:"at"`
}

// newSessionHub 创建会话 hub。
func newSessionHub(a *engine.Engine, c memory.Compiler) *sessionHub {
	return &sessionHub{
		sessions: make(map[string]*Session),
		agent:    a,
		compiler: c,
	}
}

// SetPublisher 注入事件推送器（SSE 对话流）。
func (h *sessionHub) SetPublisher(p func(sessionID, typ string, data any)) {
	h.publish = p
}

// Create 创建新会话。
func (h *sessionHub) Create() *Session {
	h.mu.Lock()
	defer h.mu.Unlock()
	s := &Session{
		ID:        "sess_" + time.Now().Format("20060102150405"),
		Title:     "新对话",
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
		Messages:  []Message{},
		Mode:      agent.ModePlan,
	}
	h.sessions[s.ID] = s
	return s
}

// Get 获取会话。
func (h *sessionHub) Get(id string) (*Session, bool) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	s, ok := h.sessions[id]
	return s, ok
}

// List 列出所有会话（按更新时间倒序）。
func (h *sessionHub) List() []*Session {
	h.mu.RLock()
	defer h.mu.RUnlock()
	out := make([]*Session, 0, len(h.sessions))
	for _, s := range h.sessions {
		out = append(out, s)
	}
	return out
}

// Delete 删除会话。
func (h *sessionHub) Delete(id string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.sessions, id)
}

// Send 发送用户消息，驱动 agent 运行直到结束。
// 返回最终回答与运行信息。
func (h *sessionHub) Send(ctx context.Context, sessionID, text string) (string, error) {
	s, ok := h.Get(sessionID)
	if !ok {
		s = h.Create()
	}

	// 用户消息入会话
	s.Messages = append(s.Messages, Message{
		Role:    "user",
		Content: text,
		At:      time.Now().Format(time.RFC3339),
	})
	if h.publish != nil {
		h.publish(sessionID, "agent.message.user", map[string]string{"content": text})
	}

	if h.agent == nil {
		s.UpdatedAt = time.Now()
		return "Agent 引擎未装配", nil
	}

	// 历史消息（转 llm.Message）
	var history []llm.Message
	for _, m := range s.Messages {
		role := llm.RoleUser
		if m.Role == "assistant" {
			role = llm.RoleAssistant
		}
		history = append(history, llm.Message{Role: role, Content: m.Content})
	}

	run, err := h.agent.Start(ctx, agent.Request{
		SessionID: sessionID,
		Message:   text,
		Mode:      s.Mode,
		History:   history[:len(history)-1], // 去重最后一条（Start 会再 append）
	})
	if err != nil {
		return "", err
	}

	// 推进状态机直到终结
	for run.State == agent.StatePlanning || run.State == agent.StateActing {
		if h.publish != nil {
			h.publish(sessionID, "agent.state", map[string]string{
				"run_id": run.ID, "state": string(run.State),
			})
		}
		run, err = h.agent.Step(ctx, run.ID)
		if err != nil {
			return "", err
		}
		// 需要批准时默认拒绝并让 LLM 换方案（避免卡死）
		if run.State == agent.StateAwaitingApproval {
			run, _ = h.agent.Approve(ctx, run.ID, false)
		}
		if run.State == agent.StateFailed {
			break
		}
	}

	answer := run.Answer
	if run.State == agent.StateFailed {
		answer = "（运行失败）" + run.Error
	}
	if answer == "" {
		answer = "（无回答）"
	}

	s.Messages = append(s.Messages, Message{
		Role:    "assistant",
		Content: answer,
		RunID:   run.ID,
		At:      time.Now().Format(time.RFC3339),
	})
	s.UpdatedAt = time.Now()
	if h.publish != nil {
		h.publish(sessionID, "agent.message.assistant", map[string]string{
			"run_id": run.ID, "content": answer,
		})
	}
	return answer, nil
}

// Rewind 回退到指定消息数（rewind 端点）。
func (h *sessionHub) Rewind(sessionID string, keep int) error {
	s, ok := h.Get(sessionID)
	if !ok {
		return nil
	}
	if keep < 0 || keep > len(s.Messages) {
		keep = 0
	}
	s.Messages = s.Messages[:keep]
	s.UpdatedAt = time.Now()
	return nil
}

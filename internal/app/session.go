package app

// 会话 hub：管理对话会话（阶段 1.1 对话端点后端）。
// 每次用户消息 → 启动一次 agent 运行 → 推进状态机直到 done/failed。

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/Black0Bag/minibox/internal/domain/agent"
	"github.com/Black0Bag/minibox/internal/domain/llm"
	"github.com/Black0Bag/minibox/internal/domain/memory"
	"github.com/Black0Bag/minibox/internal/infrastructure/engine"
	"github.com/Black0Bag/minibox/internal/infrastructure/storage"
)

// sessionHub 会话管理器（内存实现，崩溃续跑留 Phase 8）。
type sessionHub struct {
	mu          sync.RWMutex
	sessions    map[string]*Session
	pendingRuns map[string]string // run_id → session_id（等待审批的运行）
	agent       *engine.Engine
	compiler    memory.Compiler
	runStore    *storage.RunStore // 会话持久化（nil = 纯内存模式）
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
		sessions:    make(map[string]*Session),
		pendingRuns: make(map[string]string),
		agent:       a,
		compiler:    c,
	}
}

// SetRunStore 注入运行持久化存储（会话持久化）。
func (h *sessionHub) SetRunStore(rs *storage.RunStore) {
	h.runStore = rs
}

// RestoreSessions 启动时从数据库恢复最近会话。
func (h *sessionHub) RestoreSessions(logger *slog.Logger) {
	if h.runStore == nil {
		return
	}
	sessions, err := h.runStore.LoadRecentSessions(50)
	if err != nil {
		if logger != nil {
			logger.Warn("恢复会话失败", "err", err)
		}
		return
	}

	h.mu.Lock()
	defer h.mu.Unlock()
	for sessionID, msgs := range sessions {
		s := &Session{
			ID:        sessionID,
			Title:     "历史对话",
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
			Mode:      agent.ModePlan,
			Messages:  make([]Message, 0, len(msgs)),
		}
		for _, m := range msgs {
			s.Messages = append(s.Messages, Message{
				Role:    m.Role,
				Content: m.Content,
				RunID:   m.RunID,
				At:      m.At,
			})
		}
		h.sessions[sessionID] = s
	}
	if logger != nil && len(sessions) > 0 {
		logger.Info("会话持久化恢复完成", "sessions", len(sessions))
	}
}

// persistMessage 将消息写入 conversation_log（runStore 非 nil 时）。
func (h *sessionHub) persistMessage(sessionID, runID, role, content string) {
	if h.runStore == nil {
		return
	}
	if err := h.runStore.LogConversation(sessionID, runID, role, content, 0, nil); err != nil && h.publish != nil {
		// 持久化失败不阻断流程，只记日志（publish 为 nil 时跳过）
		_ = err
	}
}
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

// Send 发送用户消息，启动一次 Agent 运行（异步）。
// 返回 run_id；结果通过 SSE 推送。
// 审批等待时暂停驱动，由 SubmitApproval 外部恢复。
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
	h.persistMessage(sessionID, "", "user", text)
	if h.publish != nil {
		h.publish(sessionID, "agent.message.user", map[string]string{"content": text})
	}

	if h.agent == nil {
		s.UpdatedAt = time.Now()
		return "", fmt.Errorf("Agent 引擎未装配")
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

	// 注册到 pending runs（供审批 API 查找）
	h.mu.Lock()
	h.pendingRuns[run.ID] = sessionID
	h.mu.Unlock()

	// 发布运行开始事件（SSE）
	if h.publish != nil {
		h.publish(sessionID, engine.EventRunStarted, map[string]string{
			"run_id": run.ID, "session_id": sessionID,
		})
	}

	// 异步驱动状态机
	go h.driveRun(context.Background(), sessionID, run)

	return run.ID, nil
}

// driveRun 异步推进 Agent 状态机直到终结或等待审批。
func (h *sessionHub) driveRun(ctx context.Context, sessionID string, run *agent.Run) {
	for run.State == agent.StatePlanning || run.State == agent.StateActing {
		if h.publish != nil {
			h.publish(sessionID, engine.EventStepStarted, map[string]any{
				"run_id": run.ID, "step": run.Steps, "state": string(run.State),
			})
		}
		run, err := h.agent.Step(ctx, run.ID)
		if err != nil {
			break
		}
		if h.publish != nil {
			h.publish(sessionID, engine.EventStepFinished, map[string]any{
				"run_id": run.ID, "step": run.Steps, "state": string(run.State),
			})
		}

		// 等待审批：暂停驱动，发布事件等外部提交
		if run.State == agent.StateAwaitingApproval {
			toolName := ""
			if run.PendingTool != nil {
				toolName = run.PendingTool.Name
			}
			if h.publish != nil {
				h.publish(sessionID, engine.EventApprovalRequested, map[string]string{
					"run_id": run.ID, "tool_name": toolName,
				})
			}
			return // 暂停，等 SubmitApproval 恢复
		}

		if run.State == agent.StateFailed || run.State == agent.StateDone {
			break
		}
	}
	h.finishRun(sessionID, run)
}

// finishRun 发布完成/失败事件并更新会话。
func (h *sessionHub) finishRun(sessionID string, run *agent.Run) {
	if h.publish != nil {
		h.publish(sessionID, engine.EventRunFinished, map[string]any{
			"run_id": run.ID, "state": string(run.State), "steps": run.Steps,
		})
	}

	s, ok := h.Get(sessionID)
	if !ok {
		return
	}

	if run.State == agent.StateFailed {
		if h.publish != nil {
			h.publish(sessionID, "agent.run_failed", map[string]string{
				"run_id": run.ID, "error": run.Error,
			})
		}
		return
	}

	if run.Answer != "" {
		s.Messages = append(s.Messages, Message{
			Role:    "assistant",
			Content: run.Answer,
			RunID:   run.ID,
			At:      time.Now().Format(time.RFC3339),
		})
		h.persistMessage(sessionID, run.ID, "assistant", run.Answer)
	}
	s.UpdatedAt = time.Now()
	if h.publish != nil && run.Answer != "" {
		h.publish(sessionID, "agent.message.assistant", map[string]string{
			"run_id": run.ID, "content": run.Answer,
		})
	}
}

// SubmitApproval 提交审批决定并恢复驱动。
func (h *sessionHub) SubmitApproval(ctx context.Context, runID string, approved bool) error {
	h.mu.RLock()
	sessionID, ok := h.pendingRuns[runID]
	h.mu.RUnlock()
	if !ok {
		return fmt.Errorf("运行不存在或不在等待审批状态: %s", runID)
	}

	run, err := h.agent.Approve(ctx, runID, approved)
	if err != nil {
		return err
	}

	// 清理 pending 记录
	h.mu.Lock()
	delete(h.pendingRuns, runID)
	h.mu.Unlock()

	// 发布审批结果事件
	if h.publish != nil {
		h.publish(sessionID, "agent.approval_result", map[string]any{
			"run_id": runID, "approved": approved, "state": string(run.State),
		})
	}

	// 如果批准了，恢复异步驱动
	if approved && run.State == agent.StateActing {
		go h.driveRun(ctx, sessionID, run)
	} else if !approved {
		// 拒绝后回到 planning，继续驱动
		go h.driveRun(ctx, sessionID, run)
	}

	return nil
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

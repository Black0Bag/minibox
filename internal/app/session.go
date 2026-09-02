package app

// 会话 hub：管理对话会话（阶段 1.1 对话端点后端）。
// 每次用户消息 → 启动一次 agent 运行 → 推进状态机直到 done/failed。
//
// 并发约束（重要）：Send 会启动 driveRun goroutine 异步推进状态机，该 goroutine
// 追加助手消息并刷新更新时间；同时 HTTP handler 可能正在读取/序列化同一会话。
// 因此所有会话状态读写都必须持 h.mu，且对外一律交出快照副本（clone），
// 避免序列化期间与后台写入并发（data race）。

import (
	"cmp"
	"context"
	"fmt"
	"log/slog"
	"slices"
	"sync"
	"time"

	"github.com/Black0Bag/minibox/internal/domain/agent"
	"github.com/Black0Bag/minibox/internal/domain/llm"
	"github.com/Black0Bag/minibox/internal/domain/memory"
	"github.com/Black0Bag/minibox/internal/infrastructure/engine"
	"github.com/Black0Bag/minibox/internal/infrastructure/storage"
)

// dbTimeLayout conversation_log.created_at 的存储格式（SQLite datetime('now')）。
const dbTimeLayout = "2006-01-02 15:04:05"

// sessionHub 会话管理器（内存实现 + conversation_log 持久化）。
type sessionHub struct {
	mu          sync.RWMutex
	sessions    map[string]*Session
	pendingRuns map[string]string // run_id → session_id（等待审批的运行）
	agent       *engine.Engine
	compiler    memory.Compiler
	runStore    *storage.RunStore // 会话持久化（nil = 纯内存模式）
	logger      *slog.Logger
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

// clone 返回会话快照副本（Message 为纯值类型，浅拷贝切片即安全）。
// 调用方需持 h.mu。
func (s *Session) clone() *Session {
	cp := *s
	cp.Messages = slices.Clone(s.Messages)
	return &cp
}

// Message 会话消息（对外的轻量结构）。
// At 统一 RFC3339：新消息直接生成，历史消息从库中 created_at 转换，
// 前端只需处理一种时间格式。
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
	RunID   string `json:"run_id,omitempty"`
	At      string `json:"at"`
}

// newSessionHub 创建会话 hub。logger 为空时回落 slog.Default()。
func newSessionHub(a *engine.Engine, c memory.Compiler, logger *slog.Logger) *sessionHub {
	if logger == nil {
		logger = slog.Default()
	}
	return &sessionHub{
		sessions:    make(map[string]*Session),
		pendingRuns: make(map[string]string),
		agent:       a,
		compiler:    c,
		logger:      logger,
	}
}

// SetRunStore 注入运行持久化存储（会话持久化）。
func (h *sessionHub) SetRunStore(rs *storage.RunStore) {
	h.runStore = rs
}

// parseDBTime 解析 conversation_log.created_at；失败返回 false（不猜测时间）。
func parseDBTime(s string) (time.Time, bool) {
	t, err := time.Parse(dbTimeLayout, s)
	if err != nil {
		return time.Time{}, false
	}
	return t, true
}

// RestoreSessions 启动时从数据库恢复最近会话。
// 会话时间取自消息真实入库时间（首条=创建、末条=更新），解析失败才回落当前时间；
// 否则历史会话会被标成「刚刚活跃」并错误地排到列表最前。
func (h *sessionHub) RestoreSessions() {
	if h.runStore == nil {
		return
	}
	sessions, err := h.runStore.LoadRecentSessions(50)
	if err != nil {
		h.logger.Warn("恢复会话失败", "err", err)
		return
	}

	h.mu.Lock()
	defer h.mu.Unlock()
	for sessionID, msgs := range sessions {
		now := time.Now()
		s := &Session{
			ID:        sessionID,
			Title:     "历史对话",
			CreatedAt: now,
			UpdatedAt: now,
			Mode:      agent.ModePlan,
			Messages:  make([]Message, 0, len(msgs)),
		}
		for _, m := range msgs {
			at := m.At
			if t, ok := parseDBTime(m.At); ok {
				at = t.Format(time.RFC3339)
			}
			s.Messages = append(s.Messages, Message{
				Role:    m.Role,
				Content: m.Content,
				RunID:   m.RunID,
				At:      at,
			})
		}
		// LoadRecentSessions 按 created_at 正序返回：首条最早、末条最新。
		if len(msgs) > 0 {
			if t, ok := parseDBTime(msgs[0].At); ok {
				s.CreatedAt = t
			}
			if t, ok := parseDBTime(msgs[len(msgs)-1].At); ok {
				s.UpdatedAt = t
			}
		}
		h.sessions[sessionID] = s
	}
	if len(sessions) > 0 {
		h.logger.Info("会话持久化恢复完成", "sessions", len(sessions))
	}
}

// persistMessage 将消息写入 conversation_log（runStore 非 nil 时）。
// 失败不阻断对话流程，但必须留痕：静默丢弃会让重启后消息凭空消失且无法排查。
func (h *sessionHub) persistMessage(sessionID, runID, role, content string) {
	if h.runStore == nil {
		return
	}
	if err := h.runStore.LogConversation(sessionID, runID, role, content, 0, nil); err != nil {
		h.logger.Warn("会话消息持久化失败",
			"session_id", sessionID, "run_id", runID, "role", role, "err", err)
	}
}

func (h *sessionHub) SetPublisher(p func(sessionID, typ string, data any)) {
	h.publish = p
}

// Create 创建新会话，返回快照副本。
func (h *sessionHub) Create() *Session {
	return h.createWithID("sess_" + time.Now().Format("20060102150405"))
}

// createWithID 用指定 ID 创建会话（已存在则直接返回现有会话快照）。
func (h *sessionHub) createWithID(id string) *Session {
	h.mu.Lock()
	defer h.mu.Unlock()
	if existing, ok := h.sessions[id]; ok {
		return existing.clone()
	}
	now := time.Now()
	s := &Session{
		ID:        id,
		Title:     "新对话",
		CreatedAt: now,
		UpdatedAt: now,
		Messages:  []Message{},
		Mode:      agent.ModePlan,
	}
	h.sessions[id] = s
	return s.clone()
}

// Get 获取会话快照副本。
func (h *sessionHub) Get(id string) (*Session, bool) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	s, ok := h.sessions[id]
	if !ok {
		return nil, false
	}
	return s.clone(), true
}

// List 列出所有会话快照（按更新时间倒序，最近活跃在前）。
func (h *sessionHub) List() []*Session {
	h.mu.RLock()
	out := make([]*Session, 0, len(h.sessions))
	for _, s := range h.sessions {
		out = append(out, s.clone())
	}
	h.mu.RUnlock()

	slices.SortFunc(out, func(a, b *Session) int {
		if c := b.UpdatedAt.Compare(a.UpdatedAt); c != 0 {
			return c
		}
		return cmp.Compare(a.ID, b.ID) // 时间相同时按 ID 稳定排序
	})
	return out
}

// Delete 删除会话。
func (h *sessionHub) Delete(id string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.sessions, id)
}

// appendMessage 追加一条消息并刷新更新时间；会话不存在返回 false。
func (h *sessionHub) appendMessage(sessionID string, msg Message) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	s, ok := h.sessions[sessionID]
	if !ok {
		return false
	}
	s.Messages = append(s.Messages, msg)
	s.UpdatedAt = time.Now()
	return true
}

// historyAndMode 读取会话当前消息历史与模式（供 Agent 启动用）。
func (h *sessionHub) historyAndMode(sessionID string) ([]llm.Message, agent.Mode, bool) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	s, ok := h.sessions[sessionID]
	if !ok {
		return nil, "", false
	}
	history := make([]llm.Message, 0, len(s.Messages))
	for _, m := range s.Messages {
		role := llm.RoleUser
		if m.Role == "assistant" {
			role = llm.RoleAssistant
		}
		history = append(history, llm.Message{Role: role, Content: m.Content})
	}
	return history, s.Mode, true
}

// Send 发送用户消息，启动一次 Agent 运行（异步）。
// 返回 run_id；结果通过 SSE 推送。
// 审批等待时暂停驱动，由 SubmitApproval 外部恢复。
func (h *sessionHub) Send(ctx context.Context, sessionID, text string) (string, error) {
	// 会话不存在时按请求的 ID 创建，保证持久化/SSE/驱动都用同一个 ID
	// （原实现新建随机 ID 会导致后续消息写到另一个会话）。
	if _, ok := h.Get(sessionID); !ok {
		h.createWithID(sessionID)
	}

	h.appendMessage(sessionID, Message{
		Role:    "user",
		Content: text,
		At:      time.Now().Format(time.RFC3339),
	})
	h.persistMessage(sessionID, "", "user", text)
	if h.publish != nil {
		h.publish(sessionID, "agent.message.user", map[string]string{"content": text})
	}

	if h.agent == nil {
		return "", fmt.Errorf("Agent 引擎未装配")
	}

	history, mode, ok := h.historyAndMode(sessionID)
	if !ok {
		return "", fmt.Errorf("会话不存在: %s", sessionID)
	}

	run, err := h.agent.Start(ctx, agent.Request{
		SessionID: sessionID,
		Message:   text,
		Mode:      mode,
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

	// 异步驱动状态机：脱离请求 ctx 的取消（HTTP 响应返回后运行仍需继续），
	// 但保留 ctx 上的 trace 等值（context.WithoutCancel）。
	go h.driveRun(context.WithoutCancel(ctx), sessionID, run)

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
		// 原实现用 := 在循环内新建 run，外层循环条件始终读旧状态；
		// 这里显式赋值给外层 run，状态机才真正推进。
		next, err := h.agent.Step(ctx, run.ID)
		if err != nil {
			h.logger.Warn("Agent 单步推进失败", "session_id", sessionID, "run_id", run.ID, "err", err)
			break
		}
		run = next
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
	// 运行终结，清理 pending 记录（避免已结束的 run 长期占用审批入口）
	h.mu.Lock()
	delete(h.pendingRuns, run.ID)
	h.mu.Unlock()

	if h.publish != nil {
		h.publish(sessionID, engine.EventRunFinished, map[string]any{
			"run_id": run.ID, "state": string(run.State), "steps": run.Steps,
		})
	}

	if run.State == agent.StateFailed {
		if h.publish != nil {
			h.publish(sessionID, "agent.run_failed", map[string]string{
				"run_id": run.ID, "error": run.Error,
			})
		}
		return
	}

	if run.Answer == "" {
		return
	}
	if !h.appendMessage(sessionID, Message{
		Role:    "assistant",
		Content: run.Answer,
		RunID:   run.ID,
		At:      time.Now().Format(time.RFC3339),
	}) {
		h.logger.Warn("助手消息无法写入：会话已不存在", "session_id", sessionID, "run_id", run.ID)
		return
	}
	h.persistMessage(sessionID, run.ID, "assistant", run.Answer)
	if h.publish != nil {
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

	// 批准 → 恢复执行工具；拒绝 → 回到 planning 换方案；两者都继续异步驱动。
	go h.driveRun(context.WithoutCancel(ctx), sessionID, run)

	return nil
}

// Rewind 回退到指定消息数（rewind 端点）。
func (h *sessionHub) Rewind(sessionID string, keep int) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	s, ok := h.sessions[sessionID]
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

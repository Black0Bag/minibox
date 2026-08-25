package storage

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/Black0Bag/minibox/internal/domain/agent"
	"github.com/Black0Bag/minibox/internal/domain/llm"
)

// RunStore Agent 运行持久化（B5 崩溃续跑）。
// 每次状态变更后落盘，重启后 Resume 从 DB 恢复。
type RunStore struct {
	db *sql.DB
}

// NewRunStore 创建运行存储。
func NewRunStore(db *sql.DB) *RunStore {
	return &RunStore{db: db}
}

// SaveRun 持久化（或更新）一个 Run。
func (s *RunStore) SaveRun(run *agent.Run) error {
	messages, err := json.Marshal(run.Messages)
	if err != nil {
		return fmt.Errorf("序列化 messages 失败: %w", err)
	}
	pendingTool := ""
	if run.PendingTool != nil {
		b, err := json.Marshal(run.PendingTool)
		if err != nil {
			return fmt.Errorf("序列化 pending_tool 失败: %w", err)
		}
		pendingTool = string(b)
	}
	plan := ""
	if run.Plan != nil {
		b, err := json.Marshal(run.Plan)
		if err != nil {
			return fmt.Errorf("序列化 plan 失败: %w", err)
		}
		plan = string(b)
	}
	seenCalls, err := json.Marshal(run.SeenCalls)
	if err != nil {
		return fmt.Errorf("序列化 seen_calls 失败: %w", err)
	}

	_, err = s.db.Exec(`INSERT INTO runs (id, session_id, state, mode, steps, tokens_spent, messages, pending_tool, plan, seen_calls, answer, error, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			state=excluded.state, mode=excluded.mode, steps=excluded.steps,
			tokens_spent=excluded.tokens_spent, messages=excluded.messages,
			pending_tool=excluded.pending_tool, plan=excluded.plan,
			seen_calls=excluded.seen_calls, answer=excluded.answer,
			error=excluded.error, updated_at=excluded.updated_at`,
		run.ID, run.SessionID, string(run.State), string(run.Mode),
		run.Steps, run.TokensSpent, string(messages), pendingTool, plan,
		string(seenCalls), run.Answer, run.Error,
		run.CreatedAt.Format("2006-01-02 15:04:05"),
		run.UpdatedAt.Format("2006-01-02 15:04:05"),
	)
	if err != nil {
		return fmt.Errorf("保存 run 失败: %w", err)
	}
	return nil
}

// LoadRun 从 DB 恢复一个 Run（崩溃续跑）。
func (s *RunStore) LoadRun(runID string) (*agent.Run, error) {
	var (
		state, mode       string
		messagesJSON      string
		pendingToolJSON   string
		planJSON          string
		seenCallsJSON     string
		createdAt, updatedAt string
	)
	run := &agent.Run{}
	err := s.db.QueryRow(`SELECT id, session_id, state, mode, steps, tokens_spent,
		messages, pending_tool, plan, seen_calls, answer, error, created_at, updated_at
		FROM runs WHERE id = ?`, runID).Scan(
		&run.ID, &run.SessionID, &state, &mode, &run.Steps, &run.TokensSpent,
		&messagesJSON, &pendingToolJSON, &planJSON, &seenCallsJSON,
		&run.Answer, &run.Error, &createdAt, &updatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("加载 run %s 失败: %w", runID, err)
	}
	run.State = agent.State(state)
	run.Mode = agent.Mode(mode)

	if err := json.Unmarshal([]byte(messagesJSON), &run.Messages); err != nil {
		return nil, fmt.Errorf("反序列化 messages 失败: %w", err)
	}
	if pendingToolJSON != "" {
		var tc llm.ToolCall
		if err := json.Unmarshal([]byte(pendingToolJSON), &tc); err == nil {
			run.PendingTool = &tc
		}
	}
	if planJSON != "" {
		var p agent.Plan
		if err := json.Unmarshal([]byte(planJSON), &p); err == nil {
			run.Plan = &p
		}
	}
	if err := json.Unmarshal([]byte(seenCallsJSON), &run.SeenCalls); err != nil {
		return nil, fmt.Errorf("反序列化 seen_calls 失败: %w", err)
	}

	run.CreatedAt = parseTime(createdAt)
	run.UpdatedAt = parseTime(updatedAt)
	return run, nil
}

// ListPendingRuns 列出所有未完成的 Run（崩溃续跑用）。
func (s *RunStore) ListPendingRuns() ([]*agent.Run, error) {
	rows, err := s.db.Query(`SELECT id FROM runs WHERE state NOT IN ('done', 'failed') ORDER BY created_at`)
	if err != nil {
		return nil, fmt.Errorf("列出未完成 run 失败: %w", err)
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			_ = rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	_ = rows.Close()

	var runs []*agent.Run
	for _, id := range ids {
		run, err := s.LoadRun(id)
		if err != nil {
			continue
		}
		runs = append(runs, run)
	}
	return runs, nil
}

// DeleteRun 删除一个 Run。
func (s *RunStore) DeleteRun(runID string) error {
	_, err := s.db.Exec(`DELETE FROM runs WHERE id = ?`, runID)
	return err
}

// LogConversation 记录对话消息到 conversation_log（TTL 90 天）。
func (s *RunStore) LogConversation(sessionID, runID, role, content string, tokens int, metadata any) error {
	metaJSON := "{}"
	if metadata != nil {
		b, err := json.Marshal(metadata)
		if err == nil {
			metaJSON = string(b)
		}
	}
	_, err := s.db.Exec(`INSERT INTO conversation_log (session_id, run_id, role, content, tokens, metadata) VALUES (?, ?, ?, ?, ?, ?)`,
		sessionID, runID, role, content, tokens, metaJSON)
	return err
}

// CleanupOldLogs 清理 90 天前的对话日志（cron 定时调用）。
func (s *RunStore) CleanupOldLogs(days int) (int64, error) {
	if days <= 0 {
		days = 90
	}
	res, err := s.db.Exec(fmt.Sprintf(`DELETE FROM conversation_log WHERE created_at < datetime('now', '-%d days')`, days))
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return n, nil
}

// parseTime 解析 SQLite datetime 字符串。
func parseTime(s string) time.Time {
	t, err := time.Parse("2006-01-02 15:04:05", s)
	if err != nil {
		return time.Now() // 解析失败用当前时间兜底
	}
	return t
}

// SessionMessage 会话消息的轻量表示（从 conversation_log 恢复用）。
type SessionMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
	RunID   string `json:"run_id,omitempty"`
	At      string `json:"at"`
}

// LoadRecentSessions 从 conversation_log 中恢复最近 N 条会话的消息。
// 返回 map[session_id][]SessionMessage，按时间正序。
func (s *RunStore) LoadRecentSessions(limit int) (map[string][]SessionMessage, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := s.db.Query(`SELECT session_id, run_id, role, content, created_at
		FROM conversation_log
		WHERE session_id IN (
			SELECT DISTINCT session_id FROM conversation_log ORDER BY created_at DESC LIMIT ?
		)
		ORDER BY created_at ASC`, limit)
	if err != nil {
		return nil, fmt.Errorf("加载最近会话失败: %w", err)
	}
	defer rows.Close()

	result := make(map[string][]SessionMessage)
	for rows.Next() {
		var sessionID, runID, role, content, createdAt string
		if err := rows.Scan(&sessionID, &runID, &role, &content, &createdAt); err != nil {
			return nil, fmt.Errorf("扫描会话消息失败: %w", err)
		}
		result[sessionID] = append(result[sessionID], SessionMessage{
			Role:    role,
			Content: content,
			RunID:   runID,
			At:      createdAt,
		})
	}
	return result, rows.Err()
}
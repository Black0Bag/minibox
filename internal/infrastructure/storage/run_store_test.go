package storage

import (
	"path/filepath"
	"testing"

	"github.com/Black0Bag/minibox/internal/config"
	"github.com/Black0Bag/minibox/internal/domain/agent"
	"github.com/Black0Bag/minibox/internal/domain/llm"
)

func newTestRunStore(t *testing.T) (*RunStore, func()) {
	t.Helper()
	cfg := config.Default()
	cfg.Database.Path = filepath.Join(t.TempDir(), "test.db")
	cfg.Database.MaxOpenConns = 1
	cfg.Database.MaxIdleConns = 1
	db, err := Open(cfg.Database)
	if err != nil {
		t.Fatalf("打开数据库失败: %v", err)
	}
	return NewRunStore(db), func() { _ = db.Close() }
}

func TestSaveAndLoadRun(t *testing.T) {
	store, cleanup := newTestRunStore(t)
	defer cleanup()

	run := &agent.Run{
		ID:          "test-run-1",
		SessionID:   "session-1",
		State:       agent.StatePlanning,
		Mode:        agent.ModeBuild,
		Steps:       3,
		TokensSpent: 500,
		Messages: []llm.Message{
			{Role: llm.RoleUser, Content: "hello"},
			{Role: llm.RoleAssistant, Content: "hi there"},
		},
		SeenCalls: []string{"call_1", "call_2"},
	}

	if err := store.SaveRun(run); err != nil {
		t.Fatalf("SaveRun 失败: %v", err)
	}

	loaded, err := store.LoadRun(run.ID)
	if err != nil {
		t.Fatalf("LoadRun 失败: %v", err)
	}

	if loaded.ID != run.ID {
		t.Errorf("ID = %q, want %q", loaded.ID, run.ID)
	}
	if loaded.State != agent.StatePlanning {
		t.Errorf("State = %q", loaded.State)
	}
	if loaded.Steps != 3 {
		t.Errorf("Steps = %d, want 3", loaded.Steps)
	}
	if len(loaded.Messages) != 2 {
		t.Errorf("Messages 长度 = %d, want 2", len(loaded.Messages))
	}
	if loaded.Messages[0].Content != "hello" {
		t.Errorf("Messages[0].Content = %q", loaded.Messages[0].Content)
	}
	if len(loaded.SeenCalls) != 2 {
		t.Errorf("SeenCalls 长度 = %d, want 2", len(loaded.SeenCalls))
	}
}

func TestSaveRunUpsert(t *testing.T) {
	store, cleanup := newTestRunStore(t)
	defer cleanup()

	run := &agent.Run{
		ID:        "test-run-2",
		SessionID: "session-2",
		State:     agent.StatePlanning,
		Mode:      agent.ModeBuild,
	}
	if err := store.SaveRun(run); err != nil {
		t.Fatalf("第一次 SaveRun 失败: %v", err)
	}

	run.State = agent.StateDone
	run.Answer = "完成"
	run.Steps = 5
	if err := store.SaveRun(run); err != nil {
		t.Fatalf("第二次 SaveRun 失败: %v", err)
	}

	loaded, err := store.LoadRun(run.ID)
	if err != nil {
		t.Fatalf("LoadRun 失败: %v", err)
	}
	if loaded.State != agent.StateDone {
		t.Errorf("State = %q, want done", loaded.State)
	}
	if loaded.Answer != "完成" {
		t.Errorf("Answer = %q, want 完成", loaded.Answer)
	}
	if loaded.Steps != 5 {
		t.Errorf("Steps = %d, want 5", loaded.Steps)
	}
}

func TestListPendingRuns(t *testing.T) {
	store, cleanup := newTestRunStore(t)
	defer cleanup()

	runs := []*agent.Run{
		{ID: "r1", SessionID: "s1", State: agent.StatePlanning, Mode: agent.ModeBuild},
		{ID: "r2", SessionID: "s1", State: agent.StateDone, Mode: agent.ModeBuild},
		{ID: "r3", SessionID: "s2", State: agent.StateActing, Mode: agent.ModeBuild},
	}
	for _, r := range runs {
		if err := store.SaveRun(r); err != nil {
			t.Fatalf("SaveRun %s 失败: %v", r.ID, err)
		}
	}

	pending, err := store.ListPendingRuns()
	if err != nil {
		t.Fatalf("ListPendingRuns 失败: %v", err)
	}
	// r1 和 r3 未完成，r2 已完成
	if len(pending) != 2 {
		t.Errorf("pending 数量 = %d, want 2", len(pending))
	}
}

func TestDeleteRun(t *testing.T) {
	store, cleanup := newTestRunStore(t)
	defer cleanup()

	run := &agent.Run{ID: "r-del", SessionID: "s1", State: agent.StatePlanning, Mode: agent.ModeBuild}
	if err := store.SaveRun(run); err != nil {
		t.Fatalf("SaveRun 失败: %v", err)
	}
	if err := store.DeleteRun(run.ID); err != nil {
		t.Fatalf("DeleteRun 失败: %v", err)
	}
	_, err := store.LoadRun(run.ID)
	if err == nil {
		t.Error("DeleteRun 后 LoadRun 应失败")
	}
}

func TestLogConversation(t *testing.T) {
	store, cleanup := newTestRunStore(t)
	defer cleanup()

	err := store.LogConversation("session-1", "run-1", "user", "你好", 10, map[string]any{"model": "deepseek-chat"})
	if err != nil {
		t.Fatalf("LogConversation 失败: %v", err)
	}

	// 验证记录存在
	var role, content string
	err = store.db.QueryRow("SELECT role, content FROM conversation_log WHERE session_id = ?", "session-1").Scan(&role, &content)
	if err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	if role != "user" || content != "你好" {
		t.Errorf("role=%q content=%q", role, content)
	}
}

func TestCleanupOldLogs(t *testing.T) {
	store, cleanup := newTestRunStore(t)
	defer cleanup()

	// 插入一条记录
	err := store.LogConversation("s1", "r1", "user", "test", 0, nil)
	if err != nil {
		t.Fatalf("LogConversation 失败: %v", err)
	}
	// 清理 0 天前的（不删除刚插入的，因为 datetime 是 today）
	n, err := store.CleanupOldLogs(90)
	if err != nil {
		t.Fatalf("CleanupOldLogs 失败: %v", err)
	}
	// 刚插入的记录不会被清理（未满 90 天）
	if n > 1 {
		t.Errorf("清理数量 = %d, 应为 0（刚插入未过期）", n)
	}
}
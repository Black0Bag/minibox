package agent

import (
	"testing"
	"time"

	"github.com/Black0Bag/minibox/internal/domain/llm"
)

func TestStateConstants(t *testing.T) {
	cases := []struct {
		name string
		got  State
		want string
	}{
		{"planning", StatePlanning, "planning"},
		{"acting", StateActing, "acting"},
		{"awaiting_approval", StateAwaitingApproval, "awaiting_approval"},
		{"awaiting_input", StateAwaitingInput, "awaiting_input"},
		{"done", StateDone, "done"},
		{"failed", StateFailed, "failed"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if string(tc.got) != tc.want {
				t.Errorf("got %q, want %q", tc.got, tc.want)
			}
		})
	}
}

func TestModeConstants(t *testing.T) {
	if string(ModePlan) != "plan" {
		t.Errorf("ModePlan = %q, want %q", ModePlan, "plan")
	}
	if string(ModeBuild) != "build" {
		t.Errorf("ModeBuild = %q, want %q", ModeBuild, "build")
	}
}

func TestMaxSteps(t *testing.T) {
	if MaxSteps != 20 {
		t.Errorf("MaxSteps = %d, want 20", MaxSteps)
	}
}

func TestTodoStatusConstants(t *testing.T) {
	cases := []struct {
		name string
		got  TodoStatus
		want string
	}{
		{"pending", TodoPending, "pending"},
		{"in_progress", TodoInProgress, "in_progress"},
		{"completed", TodoCompleted, "completed"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if string(tc.got) != tc.want {
				t.Errorf("got %q, want %q", tc.got, tc.want)
			}
		})
	}
}

func TestRunStruct(t *testing.T) {
	now := time.Now()
	r := Run{
		ID:          "run-1",
		SessionID:   "sess-1",
		State:       StatePlanning,
		Mode:        ModePlan,
		Steps:       0,
		TokensSpent: 100,
		Messages:    []llm.Message{{Role: "user", Content: "hello"}},
		SeenCalls:   []string{"call-1"},
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	if r.ID != "run-1" {
		t.Errorf("ID = %q", r.ID)
	}
	if r.State != StatePlanning {
		t.Errorf("State = %q", r.State)
	}
	if r.Mode != ModePlan {
		t.Errorf("Mode = %q", r.Mode)
	}
	if len(r.Messages) != 1 {
		t.Errorf("Messages len = %d", len(r.Messages))
	}
	if len(r.SeenCalls) != 1 {
		t.Errorf("SeenCalls len = %d", len(r.SeenCalls))
	}
	if r.PendingTool != nil {
		t.Error("PendingTool should be nil")
	}
	if r.Plan != nil {
		t.Error("Plan should be nil")
	}
}

func TestTodoItemStruct(t *testing.T) {
	now := time.Now()
	item := TodoItem{
		ID:        "todo-1",
		Content:   "Write tests",
		Status:    TodoPending,
		CreatedAt: now,
	}

	if item.ID != "todo-1" {
		t.Errorf("ID = %q", item.ID)
	}
	if item.Status != TodoPending {
		t.Errorf("Status = %q", item.Status)
	}
	if !item.CreatedAt.Equal(now) {
		t.Error("CreatedAt mismatch")
	}
	if !item.StartedAt.IsZero() {
		t.Error("StartedAt should be zero")
	}
}

func TestConfigDefaults(t *testing.T) {
	cfg := Config{
		MaxSteps:      MaxSteps,
		MaxTokens:     100000,
		Mode:          ModePlan,
		RequirePlan:   true,
		ToolOutputCap: 8000,
	}

	if cfg.MaxSteps != 20 {
		t.Errorf("MaxSteps = %d, want 20", cfg.MaxSteps)
	}
	if cfg.MaxTokens != 100000 {
		t.Errorf("MaxTokens = %d", cfg.MaxTokens)
	}
	if cfg.Mode != ModePlan {
		t.Errorf("Mode = %q", cfg.Mode)
	}
	if !cfg.RequirePlan {
		t.Error("RequirePlan should be true")
	}
	if cfg.ToolOutputCap != 8000 {
		t.Errorf("ToolOutputCap = %d", cfg.ToolOutputCap)
	}
}

func TestPlanStruct(t *testing.T) {
	p := Plan{
		Recorded: true,
		Content:  "Step 1: Do X\nStep 2: Do Y",
	}

	if !p.Recorded {
		t.Error("Recorded should be true")
	}
	if p.Content == "" {
		t.Error("Content should not be empty")
	}
}

func TestRequestStruct(t *testing.T) {
	req := Request{
		SessionID: "sess-1",
		Message:   "test message",
		Mode:      ModeBuild,
	}

	if req.SessionID != "sess-1" {
		t.Errorf("SessionID = %q", req.SessionID)
	}
	if req.Message != "test message" {
		t.Errorf("Message = %q", req.Message)
	}
	if req.Mode != ModeBuild {
		t.Errorf("Mode = %q", req.Mode)
	}
}

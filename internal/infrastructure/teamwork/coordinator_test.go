package teamwork

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/Black0Bag/minibox/internal/domain/llm"
	"github.com/Black0Bag/minibox/internal/domain/teamwork"
)

// mockLLM 固定响应的假 LLM。
type mockLLM struct{ content string }

func (m *mockLLM) Complete(_ context.Context, req llm.Request) (*llm.Response, error) {
	if len(req.Messages) < 2 {
		return nil, fmt.Errorf("messages too short")
	}
	return &llm.Response{Content: m.content}, nil
}

func newTestCoordinator() *Coordinator {
	sched := teamwork.NewScheduler(nil, nil)
	return NewCoordinator(nil, sched, &mockLLM{content: "方案 A：稳扎稳打"})
}

func TestTriage(t *testing.T) {
	c := newTestCoordinator()
	triage, err := c.Triage(context.Background(), "帮我写一段 Go 代码并调试")
	if err != nil {
		t.Fatalf("Triage failed: %v", err)
	}
	if len(triage.Recommended) == 0 {
		t.Fatal("无推荐团队")
	}
	if triage.Recommended[0].Team.ID != "engineering" {
		t.Errorf("推荐团队 = %s, want engineering", triage.Recommended[0].Team.ID)
	}
}

func TestStartProject(t *testing.T) {
	c := newTestCoordinator()
	p, err := c.StartProject(context.Background(), "sess-1", "实现一个 REST API", "engineering", nil)
	if err != nil {
		t.Fatalf("StartProject failed: %v", err)
	}
	if p.Status != "confirmed" {
		t.Errorf("status = %s, want confirmed", p.Status)
	}
	if p.Discussion == nil {
		t.Fatal("Discussion is nil")
	}
	if len(p.Members) != 4 {
		t.Errorf("members = %d, want 4", len(p.Members))
	}
	if p.Members[0].Level != teamwork.Level2 {
		t.Errorf("lead level = %v, want Level2", p.Members[0].Level)
	}
}

func TestRunDiscussion(t *testing.T) {
	c := newTestCoordinator()
	_, _ = c.StartProject(context.Background(), "sess-2", "设计数据库 schema", "engineering", nil)

	props, err := c.RunDiscussion(context.Background(), "sess-2", "第一轮")
	if err != nil {
		t.Fatalf("RunDiscussion failed: %v", err)
	}
	if len(props) != 3 {
		t.Errorf("proposals = %d, want 3", len(props))
	}
	for _, prop := range props {
		if !strings.Contains(prop.Content, "方案 A") {
			t.Errorf("proposal content = %q, want mock", prop.Content)
		}
	}
}

func TestConclude(t *testing.T) {
	c := newTestCoordinator()
	p, _ := c.StartProject(context.Background(), "sess-3", "写一份市场报告", "research", nil)
	_, _ = c.RunDiscussion(context.Background(), "sess-3", "")

	p, err := c.Conclude(context.Background(), "sess-3", "采用研究主管框架定稿")
	if err != nil {
		t.Fatalf("Conclude failed: %v", err)
	}
	if p.Status != "done" {
		t.Errorf("status = %s, want done", p.Status)
	}
	if p.Verdict != "采用研究主管框架定稿" {
		t.Errorf("verdict = %q", p.Verdict)
	}
	if p.Discussion.Verdict == "" {
		t.Error("discussion verdict should be set")
	}
	for _, m := range p.Members {
		if m.Profile != nil && m.Profile.Score() <= 0 {
			t.Errorf("member %s profile score should be >0 after completion", m.ID)
		}
	}
}

func TestStartProject_UnknownTeam(t *testing.T) {
	c := newTestCoordinator()
	_, err := c.StartProject(context.Background(), "sess-4", "q", "nonexistent", nil)
	if err == nil {
		t.Error("unknown team should error")
	}
}

func TestProject_NotFound(t *testing.T) {
	c := newTestCoordinator()
	_, err := c.RunDiscussion(context.Background(), "nope", "")
	if err == nil {
		t.Error("missing project should error")
	}
}

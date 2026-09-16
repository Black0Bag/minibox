package subagent

import (
	"testing"
	"time"
)

func TestModeConstants(t *testing.T) {
	cases := []struct {
		name string
		got  Mode
		want string
	}{
		{"single", ModeSingle, "single"},
		{"parallel", ModeParallel, "parallel"},
		{"chain", ModeChain, "chain"},
		{"background", ModeBackground, "background"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if string(tc.got) != tc.want {
				t.Errorf("got %q, want %q", tc.got, tc.want)
			}
		})
	}
}

func TestTaskStruct(t *testing.T) {
	task := Task{
		AgentID:   "agent-1",
		Objective: "Search the web",
		MaxTokens: 1000,
	}

	if task.AgentID != "agent-1" {
		t.Errorf("AgentID = %q", task.AgentID)
	}
	if task.Objective != "Search the web" {
		t.Errorf("Objective = %q", task.Objective)
	}
	if task.MaxTokens != 1000 {
		t.Errorf("MaxTokens = %d", task.MaxTokens)
	}
}

func TestResultStruct(t *testing.T) {
	r := Result{
		AgentID: "agent-1",
		Content: "result text",
		Tokens:  50,
		Latency: 100 * time.Millisecond,
	}

	if r.AgentID != "agent-1" {
		t.Errorf("AgentID = %q", r.AgentID)
	}
	if r.Content != "result text" {
		t.Errorf("Content = %q", r.Content)
	}
	if r.Tokens != 50 {
		t.Errorf("Tokens = %d", r.Tokens)
	}
	if r.Error != "" {
		t.Errorf("Error = %q, want empty", r.Error)
	}
}

func TestResultWithError(t *testing.T) {
	r := Result{
		AgentID: "agent-2",
		Error:   "timeout",
	}

	if r.Error != "timeout" {
		t.Errorf("Error = %q", r.Error)
	}
	if r.Content != "" {
		t.Errorf("Content = %q, want empty on error", r.Content)
	}
}

func TestConfigDefaults(t *testing.T) {
	cfg := Config{
		MaxConcurrent: 4,
		AgentTimeout:  30 * time.Second,
		TokenBudget:   10000,
	}

	if cfg.MaxConcurrent != 4 {
		t.Errorf("MaxConcurrent = %d", cfg.MaxConcurrent)
	}
	if cfg.AgentTimeout != 30*time.Second {
		t.Errorf("AgentTimeout = %v", cfg.AgentTimeout)
	}
	if cfg.TokenBudget != 10000 {
		t.Errorf("TokenBudget = %d", cfg.TokenBudget)
	}
}

func TestRunReportStruct(t *testing.T) {
	report := RunReport{
		Results: []Result{
			{AgentID: "a1", Content: "r1"},
			{AgentID: "a2", Content: "r2"},
		},
		Failures:    []Failure{{AgentID: "a3", Error: "err"}},
		TokensSpent: 200,
		Elapsed:     500 * time.Millisecond,
	}

	if len(report.Results) != 2 {
		t.Errorf("Results len = %d", len(report.Results))
	}
	if len(report.Failures) != 1 {
		t.Errorf("Failures len = %d", len(report.Failures))
	}
	if report.TokensSpent != 200 {
		t.Errorf("TokensSpent = %d", report.TokensSpent)
	}
}

func TestFailureStruct(t *testing.T) {
	f := Failure{
		AgentID: "agent-fail",
		Error:   "context deadline exceeded",
	}

	if f.AgentID != "agent-fail" {
		t.Errorf("AgentID = %q", f.AgentID)
	}
	if f.Error != "context deadline exceeded" {
		t.Errorf("Error = %q", f.Error)
	}
}

func TestAgentConfigStruct(t *testing.T) {
	cfg := AgentConfig{
		ID:           "sub-1",
		Name:         "Researcher",
		SystemPrompt: "You are a researcher",
		Model:        "gpt-4",
		Skills:       []string{"web_search"},
		Tools:        []string{"browser"},
		MaxTurns:     5,
	}

	if cfg.ID != "sub-1" {
		t.Errorf("ID = %q", cfg.ID)
	}
	if cfg.Name != "Researcher" {
		t.Errorf("Name = %q", cfg.Name)
	}
	if cfg.Model != "gpt-4" {
		t.Errorf("Model = %q", cfg.Model)
	}
	if len(cfg.Skills) != 1 {
		t.Errorf("Skills len = %d", len(cfg.Skills))
	}
	if cfg.MaxTurns != 5 {
		t.Errorf("MaxTurns = %d", cfg.MaxTurns)
	}
}

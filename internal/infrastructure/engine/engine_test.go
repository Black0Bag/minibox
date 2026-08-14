package engine

import (
	"context"
	"testing"

	"github.com/Black0Bag/minibox/internal/domain/agent"
	"github.com/Black0Bag/minibox/internal/domain/llm"
)

// mockLLM 测试用假 LLM。
type mockLLM struct {
	responses []*llm.Response
	calls     int
}

func (m *mockLLM) Name() string { return "mock" }

func (m *mockLLM) Complete(_ context.Context, _ llm.Request) (*llm.Response, error) {
	if m.calls < len(m.responses) {
		resp := m.responses[m.calls]
		m.calls++
		return resp, nil
	}
	// 默认：最终答案
	m.calls++
	return &llm.Response{Content: "最终答案", Usage: llm.Usage{TotalTokens: 10}}, nil
}

func (m *mockLLM) Stream(_ context.Context, _ llm.Request) (<-chan llm.StreamEvent, error) {
	ch := make(chan llm.StreamEvent, 1)
	ch <- llm.StreamEvent{Type: llm.StreamDone}
	close(ch)
	return ch, nil
}

func (m *mockLLM) Models(_ context.Context) ([]llm.ModelInfo, error) {
	return nil, nil
}

var _ llm.Provider = (*mockLLM)(nil)

// mockTools 测试用假工具执行器。
type mockTools struct {
	executed []string
	approve  map[string]bool
}

func (m *mockTools) Execute(_ context.Context, call llm.ToolCall) (string, error) {
	m.executed = append(m.executed, call.Name)
	return "工具执行成功", nil
}

func (m *mockTools) RequiresApproval(_ context.Context, call llm.ToolCall) bool {
	if m.approve == nil {
		return false
	}
	return m.approve[call.Name]
}

var _ agent.ToolExecutor = (*mockTools)(nil)

// testLogger 测试 logger（静默）。
type testLogger struct{}

func (testLogger) Info(_ string, _ ...any)  {}
func (testLogger) Warn(_ string, _ ...any)  {}
func (testLogger) Error(_ string, _ ...any) {}

// TestEngineSimpleAnswer 验证简单问答（无工具调用）。
func TestEngineSimpleAnswer(t *testing.T) {
	e := NewEngine(
		&mockLLM{responses: []*llm.Response{{Content: "你好！"}}},
		&mockTools{},
		agent.Config{MaxSteps: 10},
		testLogger{},
	)

	run, err := e.Start(context.Background(), agent.Request{Message: "hi"})
	if err != nil {
		t.Fatalf("Start 失败: %v", err)
	}

	// Step 一次应到达 DONE
	run, err = e.Step(context.Background(), run.ID)
	if err != nil {
		t.Fatalf("Step 失败: %v", err)
	}
	if run.State != agent.StateDone {
		t.Errorf("应到达 DONE，实际 %s", run.State)
	}
	if run.Answer != "你好！" {
		t.Errorf("答案错误: %s", run.Answer)
	}
}

// TestEngineToolCall 验证工具调用流程（PLANNING→ACTING→PLANNING→DONE）。
func TestEngineToolCall(t *testing.T) {
	tools := &mockTools{}
	e := NewEngine(
		&mockLLM{responses: []*llm.Response{
			{ToolCalls: []llm.ToolCall{{ID: "call1", Name: "search", Arguments: `{"q":"test"}`}}},
			{Content: "找到了", Usage: llm.Usage{TotalTokens: 5}},
		}},
		tools,
		agent.Config{MaxSteps: 10},
		testLogger{},
	)

	run, _ := e.Start(context.Background(), agent.Request{Message: "搜索"})

	// Step 1: PLANNING → ACTING（发现工具调用）
	run, _ = e.Step(context.Background(), run.ID)
	if run.State != agent.StateActing {
		t.Fatalf("Step1 应到 ACTING，实际 %s", run.State)
	}

	// Step 2: ACTING → PLANNING（执行工具）
	run, _ = e.Step(context.Background(), run.ID)
	if run.State != agent.StatePlanning {
		t.Fatalf("Step2 应回 PLANNING，实际 %s", run.State)
	}
	if len(tools.executed) != 1 || tools.executed[0] != "search" {
		t.Errorf("工具未执行: %v", tools.executed)
	}

	// Step 3: PLANNING → DONE（最终答案）
	run, _ = e.Step(context.Background(), run.ID)
	if run.State != agent.StateDone {
		t.Errorf("Step3 应到 DONE，实际 %s", run.State)
	}
}

// TestEngineApproval 验证人类批准流程。
func TestEngineApproval(t *testing.T) {
	tools := &mockTools{approve: map[string]bool{"danger": true}}
	e := NewEngine(
		&mockLLM{responses: []*llm.Response{
			{ToolCalls: []llm.ToolCall{{ID: "c1", Name: "danger", Arguments: "{}"}}},
			{Content: "完成"},
		}},
		tools,
		agent.Config{MaxSteps: 10},
		testLogger{},
	)

	run, _ := e.Start(context.Background(), agent.Request{Message: "危险操作"})

	// 需要批准 → AWAITING_APPROVAL
	run, _ = e.Step(context.Background(), run.ID)
	if run.State != agent.StateAwaitingApproval {
		t.Fatalf("应到 AWAITING_APPROVAL，实际 %s", run.State)
	}

	// 批准 → ACTING
	run, err := e.Approve(context.Background(), run.ID, true)
	if err != nil {
		t.Fatalf("Approve 失败: %v", err)
	}
	if run.State != agent.StateActing {
		t.Errorf("批准后应到 ACTING，实际 %s", run.State)
	}
}

// TestEngineBudgetExhausted 验证步数预算。
func TestEngineBudgetExhausted(t *testing.T) {
	e := NewEngine(
		&mockLLM{responses: []*llm.Response{
			{ToolCalls: []llm.ToolCall{{ID: "c1", Name: "search", Arguments: "{}"}}},
			{ToolCalls: []llm.ToolCall{{ID: "c2", Name: "search", Arguments: "{}"}}},
		}},
		&mockTools{},
		agent.Config{MaxSteps: 1},
		testLogger{},
	)

	run, _ := e.Start(context.Background(), agent.Request{Message: "x"})
	run, _ = e.Step(context.Background(), run.ID) // step 1 = maxsteps

	// 下一步应因预算耗尽失败
	run, _ = e.Step(context.Background(), run.ID)
	if run.State != agent.StateFailed {
		t.Errorf("应到 FAILED（预算耗尽），实际 %s", run.State)
	}
}

// TestNormalizer 验证归一化映射。
func TestNormalizer(t *testing.T) {
	n := NewNormalizer("msg1")

	evs := n.Map(llm.StreamEvent{Type: llm.StreamTextDelta, Text: "你好"})
	if len(evs) != 1 || evs[0].Type != "agent.text_message_content" || evs[0].Delta != "你好" {
		t.Errorf("文本映射错误: %+v", evs)
	}

	evs = n.Map(llm.StreamEvent{Type: llm.StreamThinkingDelta, Thinking: "思考中"})
	if len(evs) != 1 || evs[0].Type != "agent.reasoning_delta" {
		t.Errorf("思考映射错误: %+v", evs)
	}

	evs = n.Map(llm.StreamEvent{Type: llm.StreamDone})
	if len(evs) != 1 || evs[0].Type != "agent.text_message_end" {
		t.Errorf("结束映射错误: %+v", evs)
	}
}

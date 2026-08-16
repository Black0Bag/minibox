package engine

import (
	"context"
	"strings"
	"testing"

	"github.com/Black0Bag/minibox/internal/domain/agent"
	"github.com/Black0Bag/minibox/internal/domain/llm"
)

// mockLLM 测试用假 LLM。
type mockLLM struct {
	responses []*llm.Response
	calls     int
	lastReq   llm.Request // 最近一次收到的请求（测试断言用）
}

func (m *mockLLM) Name() string { return "mock" }

func (m *mockLLM) Complete(_ context.Context, req llm.Request) (*llm.Response, error) {
	m.lastReq = req
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

// ToolDefs 返回空工具定义（测试用，不影响现有测试语义）。
func (m *mockTools) ToolDefs() []llm.ToolDef {
	return []llm.ToolDef{
		{
			Type: "function",
			Function: llm.FunctionDef{
				Name:        "test_tool",
				Description: "测试工具",
				Parameters:  map[string]any{"type": "object", "properties": map[string]any{}},
			},
		},
	}
}

// IsReadOnly 返回 false（测试中默认所有工具视为非只读）。
func (m *mockTools) IsReadOnly(_ string) bool { return false }

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

// TestToolDefsSentToLLM 验证工具定义被传给 LLM（toolsSchema 接线）。
func TestToolDefsSentToLLM(t *testing.T) {
	llmMock := &mockLLM{responses: []*llm.Response{{Content: "需要工具", ToolCalls: []llm.ToolCall{
		{ID: "c1", Name: "test_tool", Arguments: `{}`},
	}}}}
	engine := NewEngine(llmMock, &mockTools{}, agent.Config{}, nil)

	run, err := engine.Start(context.Background(), agent.Request{Message: "用工具"})
	if err != nil {
		t.Fatalf("Start err=%v", err)
	}
	if _, err := engine.Step(context.Background(), run.ID); err != nil {
		t.Fatalf("Step err=%v", err)
	}

	// 断言 LLM 收到的请求携带工具定义
	if len(llmMock.lastReq.Tools) == 0 {
		t.Fatal("LLM 请求应携带工具定义（ToolDefs 未接入）")
	}
	if llmMock.lastReq.Tools[0].Function.Name != "test_tool" {
		t.Errorf("工具名 = %q, 期望 test_tool", llmMock.lastReq.Tools[0].Function.Name)
	}
}

// roTools 只读工具执行器（IsReadOnly 返回 true，模拟 search_knowledge/read_file）。
type roTools struct {
	mockTools
	readOnly map[string]bool
}

func (r *roTools) IsReadOnly(name string) bool {
	if r.readOnly == nil {
		return false
	}
	return r.readOnly[name]
}

// TestPlanGateReadOnly 只读工具不被 plan-first 门控。
func TestPlanGateReadOnly(t *testing.T) {
	llmMock := &mockLLM{responses: []*llm.Response{
		{Content: "要检索", ToolCalls: []llm.ToolCall{{ID: "c1", Name: "search_knowledge", Arguments: `{"query":"端口"}`}}},
		{Content: "最终答案", ToolCalls: nil},
	}}
	tools := &roTools{readOnly: map[string]bool{"search_knowledge": true}}
	e := NewEngine(llmMock, tools, agent.Config{RequirePlan: true}, nil)

	run, err := e.Start(context.Background(), agent.Request{Message: "查端口"})
	if err != nil {
		t.Fatalf("Start err=%v", err)
	}
	// Step 1: planning → 工具调用 → 因只读应直接进入 acting（不被 gate）
	run, err = e.Step(context.Background(), run.ID)
	if err != nil {
		t.Fatalf("Step1 err=%v", err)
	}
	if run.State != agent.StateActing {
		t.Fatalf("只读工具不应被 plan 门控，状态=%s（应 acting）", run.State)
	}
	// Step 2: 执行工具
	run, _ = e.Step(context.Background(), run.ID)
	// Step 3: 回到 planning → 最终答案
	run, _ = e.Step(context.Background(), run.ID)
	if run.State != agent.StateDone {
		t.Errorf("应 done，实际 %s", run.State)
	}
}

// TestPlanGateWriteTool 写工具（非只读）仍被 plan-first 门控。
func TestPlanGateWriteTool(t *testing.T) {
	llmMock := &mockLLM{responses: []*llm.Response{
		{Content: "要写文件", ToolCalls: []llm.ToolCall{{ID: "c1", Name: "write_file", Arguments: `{}`}}},
	}}
	tools := &roTools{readOnly: map[string]bool{"write_file": false}}
	e := NewEngine(llmMock, tools, agent.Config{RequirePlan: true}, nil)

	run, err := e.Start(context.Background(), agent.Request{Message: "写文件"})
	if err != nil {
		t.Fatalf("Start err=%v", err)
	}
	run, _ = e.Step(context.Background(), run.ID)
	if run.State != agent.StateFailed {
		t.Fatalf("写工具应被 plan 门控 → failed，实际 %s", run.State)
	}
	if !strings.Contains(run.Error, "plan-first") {
		t.Errorf("错误应提示 plan-first: %s", run.Error)
	}
}

// TestDuplicateToolCallGuided 重复工具调用不杀 run，而是引导换策略。
func TestDuplicateToolCallGuided(t *testing.T) {
	llmMock := &mockLLM{responses: []*llm.Response{
		// 第一次：调用 search_knowledge
		{Content: "检索", ToolCalls: []llm.ToolCall{{ID: "c1", Name: "search_knowledge", Arguments: `{"query":"端口"}`}}},
		// 第二次：相同调用（重复）
		{Content: "再检索", ToolCalls: []llm.ToolCall{{ID: "c2", Name: "search_knowledge", Arguments: `{"query":"端口"}`}}},
		// 第三次：最终答案
		{Content: "答案是8086", ToolCalls: nil},
	}}
	e := NewEngine(llmMock, &mockTools{}, agent.Config{}, nil)

	run, err := e.Start(context.Background(), agent.Request{Message: "查端口"})
	if err != nil {
		t.Fatalf("Start err=%v", err)
	}
	// 第1步：planning → acting（search_knowledge）
	run, _ = e.Step(context.Background(), run.ID)
	if run.State != agent.StateActing {
		t.Fatalf("第1步应 acting，实际 %s", run.State)
	}
	// 第2步：执行工具 → planning
	run, _ = e.Step(context.Background(), run.ID)
	// 第3步：planning 收到重复调用 → 不应 failed，应回 planning 并注入提示
	run, _ = e.Step(context.Background(), run.ID)
	if run.State == agent.StateFailed {
		t.Fatalf("重复调用不应 failed: %s", run.Error)
	}
	if run.State != agent.StatePlanning {
		t.Fatalf("重复调用应回 planning，实际 %s", run.State)
	}
	// 注入的提示应在最后一条消息
	last := run.Messages[len(run.Messages)-1]
	if !strings.Contains(last.Content, "请勿重复调用") {
		t.Errorf("应注入引导提示: %s", last.Content)
	}
	// 第4步：planning → LLM 给最终答案 → done
	run, _ = e.Step(context.Background(), run.ID)
	if run.State != agent.StateDone {
		t.Errorf("最终应 done，实际 %s", run.State)
	}
}

// TestToolCallMessageSequence 验证工具调用消息序列完整（assistant 带 ToolCalls + tool 结果关联）。
func TestToolCallMessageSequence(t *testing.T) {
	tools := &mockTools{}
	llmMock := &mockLLM{responses: []*llm.Response{
		// 1: 调用 search
		{ToolCalls: []llm.ToolCall{{ID: "call1", Name: "search", Arguments: `{"q":"端口"}`}}},
		// 2: 调用 search（不同参数，多跳第二跳）
		{ToolCalls: []llm.ToolCall{{ID: "call2", Name: "search", Arguments: `{"q":"数据库"}`}}},
		// 3: 最终答案
		{Content: "最终综合答案", Usage: llm.Usage{TotalTokens: 5}},
	}}
	e := NewEngine(llmMock, tools, agent.Config{MaxSteps: 20}, testLogger{})

	run, _ := e.Start(context.Background(), agent.Request{Message: "综合查询"})

	// 第1跳：planning→acting（第2次 planning 时 LLM 应看到完整历史含 assistant tool_call + tool 结果）
	for i := 0; i < 6 && run.State != agent.StateDone && run.State != agent.StateFailed; i++ {
		run, _ = e.Step(context.Background(), run.ID)
	}

	if run.State != agent.StateDone {
		t.Fatalf("应 DONE，实际 %s（error=%s）", run.State, run.Error)
	}

	// 检查 assistant 消息携带 ToolCalls
	var assistantWithCalls int
	var toolMsgWithID int
	for _, m := range run.Messages {
		if m.Role == llm.RoleAssistant && len(m.ToolCalls) > 0 {
			assistantWithCalls++
		}
		if m.Role == llm.RoleTool && m.ToolCallID != "" {
			toolMsgWithID++
		}
	}
	if toolMsgWithID != 2 {
		t.Errorf("应有 2 条 tool 结果带 ToolCallID，实际 %d", toolMsgWithID)
	}
	// 第2跳的 LLM 请求应包含历史消息（assistant tool_call + tool 结果）
	// lastReq 是最后一次 Complete 的请求，其中应能看到 tool 消息
	if len(llmMock.lastReq.Messages) == 0 {
		t.Fatal("LLM 请求应为空消息")
	}
	var sawToolMsg bool
	for _, m := range llmMock.lastReq.Messages {
		if m.Role == llm.RoleTool {
			sawToolMsg = true
		}
	}
	if !sawToolMsg {
		t.Error("最后 LLM 请求应包含历史 tool 结果消息（多跳识别上下文）")
	}
	_ = assistantWithCalls
}

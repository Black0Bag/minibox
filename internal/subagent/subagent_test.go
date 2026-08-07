package subagent

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Black0Bag/minibox/internal/llm"
	"github.com/Black0Bag/minibox/internal/tools"
)

// fakeRunner 记录并发执行情况，验证 Fan-out/Fan-in 与并发上限
type fakeRunner struct {
	mu        sync.Mutex
	active    int
	maxActive int
	called    []string
}

func (f *fakeRunner) Run(_ context.Context, task Task) (Result, error) {
	f.mu.Lock()
	f.active++
	if f.active > f.maxActive {
		f.maxActive = f.active
	}
	f.called = append(f.called, task.ID)
	f.mu.Unlock()

	time.Sleep(30 * time.Millisecond)

	f.mu.Lock()
	f.active--
	f.mu.Unlock()
	return Result{TaskID: task.ID, Output: "ok-" + task.ID}, nil
}

func TestEngineFanOutFanIn(t *testing.T) {
	e := New(&fakeRunner{}, 2, 2)
	ctx := context.Background()
	var tasks []Task
	for i := 0; i < 8; i++ {
		tasks = append(tasks, Task{ID: "t" + string(rune('a'+i)), Name: "任务", Depth: 0})
	}
	results, err := e.Run(ctx, tasks)
	if err != nil {
		t.Fatalf("Run 返回错误: %v", err)
	}
	if len(results) != 8 {
		t.Fatalf("应收集 8 个结果，得到 %d", len(results))
	}
	for _, r := range results {
		if r.Err != nil {
			t.Errorf("结果 %s 不应有错误: %v", r.TaskID, r.Err)
		}
		if !strings.HasPrefix(r.Output, "ok-") {
			t.Errorf("结果 %s 输出异常: %q", r.TaskID, r.Output)
		}
	}
}

func TestEngineFanOutConcurrencyLimit(t *testing.T) {
	// 并发上限 3，验证最大并发不超过该值
	runner := &fakeRunner{}
	e := New(runner, 3, 2)
	tasks := make([]Task, 6)
	for i := range tasks {
		tasks[i] = Task{ID: "c" + string(rune('0'+i)), Depth: 0}
	}
	_, err := e.Run(context.Background(), tasks)
	if err != nil {
		t.Fatalf("Run 返回错误: %v", err)
	}
	if runner.maxActive > 3 {
		t.Errorf("最大并发 %d 超过上限 3", runner.maxActive)
	}
	if len(runner.called) != 6 {
		t.Errorf("应执行 6 个任务，实际 %d", len(runner.called))
	}
}

func TestEngineDepthLimit(t *testing.T) {
	runner := &fakeRunner{}
	e := New(runner, 4, 2) // maxDepth=2：允许深度 0、1
	tasks := []Task{
		{ID: "ok1", Depth: 0},
		{ID: "ok2", Depth: 1},
		{ID: "bad", Depth: 2},
		{ID: "bad2", Depth: 5},
	}
	results, _ := e.Run(context.Background(), tasks)
	if len(results) != 4 {
		t.Fatalf("应返回 4 个结果，得到 %d", len(results))
	}
	got := map[string]error{}
	for _, r := range results {
		got[r.TaskID] = r.Err
	}
	if got["ok1"] != nil || got["ok2"] != nil {
		t.Errorf("深度 0/1 应正常执行: %v", got)
	}
	if !errors.Is(got["bad"], ErrDepthExceeded) {
		t.Errorf("深度 2 应被拒绝为 ErrDepthExceeded，得到 %v", got["bad"])
	}
	if !errors.Is(got["bad2"], ErrDepthExceeded) {
		t.Errorf("深度 5 应被拒绝为 ErrDepthExceeded，得到 %v", got["bad2"])
	}
}

func TestEngineContextCancellation(t *testing.T) {
	runner := &fakeRunner{}
	e := New(runner, 4, 2)
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // 立即取消
	tasks := []Task{{ID: "x", Depth: 0}}
	results, err := e.Run(ctx, tasks)
	if err == nil {
		t.Fatal("context 已取消，Run 应返回错误")
	}
	for _, r := range results {
		if r.Err == nil {
			t.Errorf("context 取消后结果应含错误: %+v", r)
		}
	}
}

// fakeChat 按顺序返回预设 LLM 输出
type fakeChat struct {
	outputs []string
}

func (c *fakeChat) Chat(_ context.Context, _ []llm.Message, _ llm.Options) (string, error) {
	if len(c.outputs) == 0 {
		return "最终答案", nil
	}
	o := c.outputs[0]
	c.outputs = c.outputs[1:]
	return o, nil
}

// echoTool 测试工具
type echoTool struct {
	mu      sync.Mutex
	calledN int
}

func (e *echoTool) Name() string        { return "回显" }
func (e *echoTool) Description() string { return "测试工具，回显参数" }
func (e *echoTool) Run(_ context.Context, args string) (string, error) {
	e.mu.Lock()
	e.calledN++
	e.mu.Unlock()
	return "回显:" + args, nil
}

func TestAgentRunnerWhitelist(t *testing.T) {
	dir := t.TempDir()
	reg := tools.NewRegistry()
	echo := &echoTool{}
	reg.Register(echo)
	reg.Register(tools.TimeTool{})

	chat := &fakeChat{outputs: []string{
		`<tool name="回显" args="你好"></tool>`,
		`<tool name="当前时间" args=""></tool>`,
		"结果如下",
	}}
	runner := NewAgentRunner(chat.Chat, reg, dir)
	task := Task{
		ID:      "w1",
		Name:    "白名单测试",
		Goal:    "请用回显工具处理你好，然后回答",
		Tools:   []string{"回显"}, // 白名单只允许「回显」
		MaxIter: 4,
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	res, err := runner.Run(ctx, task)
	if err != nil {
		t.Fatalf("Run 返回错误: %v", err)
	}
	if res.Err != nil {
		t.Fatalf("结果错误: %v", res.Err)
	}
	if !strings.Contains(res.Output, "结果如下") {
		t.Errorf("输出异常: %q", res.Output)
	}
	if echo.calledN != 1 {
		t.Errorf("白名单工具应被调用 1 次，实际 %d", echo.calledN)
	}
	// 白名单校验：子集说明中不应含「当前时间」
	sub := reg.Subset(task.Tools)
	specs := sub.LLMSpecs()
	if strings.Contains(specs, "当前时间") {
		t.Error("白名单外的工具不应出现在 LLM 说明中")
	}
	if !strings.Contains(specs, "回显") {
		t.Error("白名单内工具应出现在 LLM 说明中")
	}
}

func TestAgentRunnerSideLog(t *testing.T) {
	dir := t.TempDir()
	reg := tools.NewRegistry()
	reg.Register(tools.TimeTool{})
	chat := &fakeChat{outputs: []string{"回答内容"}}
	runner := NewAgentRunner(chat.Chat, reg, dir)
	task := Task{ID: "s1", Name: "侧链日志测试", Goal: "你好", MaxIter: 2}
	res, err := runner.Run(context.Background(), task)
	if err != nil || res.Err != nil {
		t.Fatalf("Run 失败: err=%v resErr=%v", err, res.Err)
	}
	logPath := filepath.Join(dir, "s1.log")
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("侧链日志文件不存在: %v", err)
	}
	logText := string(data)
	if !strings.Contains(logText, "s1") {
		t.Errorf("侧链日志应含任务 ID: %s", logText)
	}
	if res.LogFile != logPath {
		t.Errorf("Result.LogFile 应为 %s，得到 %s", logPath, res.LogFile)
	}
}

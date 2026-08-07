package subagent

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/Black0Bag/minibox/internal/llm"
	"github.com/Black0Bag/minibox/internal/tools"
)

// 错误定义
var (
	// ErrDepthExceeded 超出最大嵌套深度（T-05：先 2 层）
	ErrDepthExceeded = errors.New("超出 subagent 最大嵌套深度")
)

// 默认配置
const (
	DefaultMaxConcurrent = 3 // Fan-out 并发上限（errgroup.SetLimit）
	DefaultMaxDepth      = 2 // 嵌套深度上限（先 2 层：主 agent=0，subagent=1）
	DefaultMaxIter       = 4 // 每个 subagent 内部工具循环迭代上限
	DefaultTimeout       = 2 * time.Minute
)

// Task subagent 任务定义（T-05 四项隔离中的「上下文隔离」载体）
type Task struct {
	ID       string   `json:"id"`       // 任务 ID（唯一）
	Name     string   `json:"name"`     // 任务名称
	Goal     string   `json:"goal"`     // 目标指令
	Tools    []string `json:"tools"`    // 工具白名单（空 = 不限制）
	Depth    int      `json:"depth"`    // 嵌套深度（根任务为 0）
	MaxIter  int      `json:"max_iter"` // 内部工具循环迭代上限（0 = 默认）
	Timeout  int      `json:"timeout_s,omitempty"` // 超时秒数（0 = 默认）
	Messages []llm.Message `json:"messages,omitempty"` // 附加上下文（可选）
}

// Result subagent 执行结果（Fan-in 汇总）
type Result struct {
	TaskID   string        `json:"task_id"`
	Name     string        `json:"name"`
	Output   string        `json:"output"`
	Err      error         `json:"-"` // 错误（不直接序列化，API 层转换）
	LogFile  string        `json:"log_file,omitempty"` // 侧链日志路径
	Duration time.Duration `json:"duration_ms"` // 耗时
}

// ErrorString 序列化用：返回错误文本
func (r *Result) ErrorString() string {
	if r.Err == nil {
		return ""
	}
	return r.Err.Error()
}

// Runner subagent 执行器接口（可注入测试替身）
type Runner interface {
	Run(ctx context.Context, task Task) (Result, error)
}

// ChatFunc LLM 对话函数（抽象出 *llm.Client，便于测试替身与多供应商）
type ChatFunc func(ctx context.Context, messages []llm.Message, opts llm.Options) (string, error)

// Engine subagent 引擎：Fan-out 并发派发 + Fan-in 结果汇总
type Engine struct {
	runner       Runner
	maxConcurrent int // errgroup.SetLimit 并发上限
	maxDepth     int // 嵌套深度上限
}

// New 创建 subagent 引擎
func New(runner Runner, maxConcurrent, maxDepth int) *Engine {
	if maxConcurrent <= 0 {
		maxConcurrent = DefaultMaxConcurrent
	}
	if maxDepth <= 0 {
		maxDepth = DefaultMaxDepth
	}
	return &Engine{runner: runner, maxConcurrent: maxConcurrent, maxDepth: maxDepth}
}

// Run Fan-out/Fan-in：并发执行全部任务并汇总结果。
// 深度超限的任务直接标记 ErrDepthExceeded，不启动 goroutine。
func (e *Engine) Run(ctx context.Context, tasks []Task) ([]Result, error) {
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(e.maxConcurrent)
	results := make([]Result, len(tasks))

	for i := range tasks {
		i := i
		g.Go(func() error {
			t := tasks[i]
			// context 已取消（父取消或兄弟失败触发）：标记为取消
			if err := gctx.Err(); err != nil {
				results[i] = Result{TaskID: t.ID, Name: t.Name, Err: err}
				return err
			}
			if t.Depth >= e.maxDepth {
				results[i] = Result{TaskID: t.ID, Name: t.Name, Err: ErrDepthExceeded}
				return nil
			}
			res, err := e.runner.Run(gctx, t)
			if err != nil {
				res.TaskID = t.ID
				res.Name = t.Name
				if res.Err == nil {
					res.Err = err
				}
			}
			results[i] = res
			return nil
		})
	}

	if err := g.Wait(); err != nil {
		return results, err
	}
	return results, nil
}

// ============ agentRunner：真实执行器 ============

// agentRunner 基于 AgentLoop 的真实 subagent 执行器
// 隔离四要素（T-05）：goroutine（由 Engine 提供）+ context 隔离（任务级超时）+ 侧链日志 + 工具白名单
type agentRunner struct {
	chat    ChatFunc
	tools   *tools.Registry
	sideDir string // 侧链日志目录
}

// NewAgentRunner 创建 agentRunner
func NewAgentRunner(chat ChatFunc, reg *tools.Registry, sideDir string) Runner {
	return &agentRunner{chat: chat, tools: reg, sideDir: sideDir}
}

// Run 执行单个 subagent 任务
func (r *agentRunner) Run(ctx context.Context, task Task) (Result, error) {
	start := time.Now()
	// 白名单：构造受限工具子集（T-05）
	reg := r.tools.Subset(task.Tools)

	// 侧链日志：独立文件
	logPath := filepath.Join(r.sideDir, task.ID+".log")
	sideLog, closer, err := openSideLog(logPath)
	if err != nil {
		return Result{TaskID: task.ID, Name: task.Name, Err: err}, err
	}
	defer closer()

	sideLog.Info("subagent 任务启动", "id", task.ID, "name", task.Name, "goal", task.Goal, "depth", task.Depth, "whitelist", task.Tools)

	// 任务级 context 隔离（超时）
	timeout := time.Duration(task.Timeout) * time.Second
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	tctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// 执行 AgentLoop（内部工具循环）
	out, loopErr := runLoop(tctx, r.chat, reg, task, sideLog)

	elapsed := time.Since(start)
	res := Result{TaskID: task.ID, Name: task.Name, Output: out, LogFile: logPath, Duration: elapsed}
	if loopErr != nil {
		res.Err = loopErr
		sideLog.Error("subagent 任务失败", "err", loopErr.Error(), "elapsed_ms", elapsed.Milliseconds())
		return res, nil // 任务失败不算引擎级错误，返回给上层
	}
	sideLog.Info("subagent 任务完成", "elapsed_ms", elapsed.Milliseconds(), "output_len", len(out))
	return res, nil
}

// runLoop subagent 内部 AgentLoop（LLM → 工具决策 → 执行 → 观察）
func runLoop(ctx context.Context, chat ChatFunc, reg *tools.Registry, task Task, sideLog *slog.Logger) (string, error) {
	maxIter := task.MaxIter
	if maxIter <= 0 {
		maxIter = DefaultMaxIter
	}
	messages := make([]llm.Message, 0, len(task.Messages)+1)
	messages = append(messages, llm.Message{Role: "system", Content: task.Goal})
	messages = append(messages, task.Messages...)

	resp := ""
	for i := 0; i < maxIter; i++ {
		msgs := make([]llm.Message, 0, len(messages)+1)
		msgs = append(msgs, messages...)
		msgs = append(msgs, llm.Message{Role: "system", Content: reg.LLMSpecs()})

		out, err := chat(ctx, msgs, llm.Options{MaxTokens: 2000, Temperature: 0.5})
		if err != nil {
			return "", fmt.Errorf("LLM 调用: %w", err)
		}
		resp = out
		sideLog.Info("LLM 输出", "iter", i+1, "output", truncate(out, 500))

		name, args, ok := tools.ParseToolCall(out)
		if !ok {
			return tools.CleanToolMarkers(out), nil
		}
		result, err := reg.Run(ctx, name, args)
		if err != nil {
			result = "工具执行错误：" + err.Error()
		}
		sideLog.Info("工具执行", "name", name, "args", args, "result", truncate(result, 300))
		messages = append(messages,
			llm.Message{Role: "assistant", Content: out},
			llm.Message{Role: "user", Content: "工具「" + name + "」返回结果：" + result + "\n请直接给出最终答案，绝对不要再输出任何 <tool> 工具调用标记。"},
		)
	}
	return resp, nil
}

// truncate 截断过长的日志内容
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

// openSideLog 打开侧链日志（slog text 格式，含时间戳）
func openSideLog(path string) (*slog.Logger, func(), error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, nil, fmt.Errorf("创建侧链日志目录: %w", err)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, nil, fmt.Errorf("打开侧链日志 %s: %w", path, err)
	}
	lg := slog.New(slog.NewTextHandler(f, &slog.HandlerOptions{Level: slog.LevelDebug}))
	return lg, func() { _ = f.Close() }, nil
}

package engine

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/Black0Bag/minibox/internal/domain/subagent"
)

// Orchestrator subagent 调度器实现（Orchestrator-Worker）。
type Orchestrator struct {
	agents map[string]subagent.Agent
	cfg    subagent.Config
	logger interface {
		Warn(msg string, args ...any)
	}
}

// NewOrchestrator 创建调度器。
func NewOrchestrator(cfg subagent.Config, logger interface {
	Warn(msg string, args ...any)
}) *Orchestrator {
	if cfg.MaxConcurrent == 0 {
		cfg.MaxConcurrent = 4
	}
	if cfg.AgentTimeout == 0 {
		cfg.AgentTimeout = 5 * time.Minute
	}
	return &Orchestrator{
		agents: make(map[string]subagent.Agent),
		cfg:    cfg,
		logger: logger,
	}
}

// Register 注册 subagent。
func (o *Orchestrator) Register(a subagent.Agent) {
	o.agents[a.ID()] = a
}

// Dispatch 把计划扇出并发执行并合成结果。
// 关键陷阱（BackendBytes 2026）：
//   1. subagent 失败 record 到 slice，return nil（不杀兄弟）
//   2. 预算 atomic reserve-and-reconcile 在 worker 内
//   3. panic 隔离
//   4. 结果排序 pre-size results[index]
func (o *Orchestrator) Dispatch(ctx context.Context, plan []subagent.Task) (subagent.RunReport, error) {
	start := time.Now()

	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(o.cfg.MaxConcurrent)

	var (
		mu       sync.Mutex
		results  = make([]subagent.Result, len(plan))
		failures []subagent.Failure
		reserved atomic.Int64 // 预留预算
		spent    atomic.Int64 // 实际消耗
	)

	for i, task := range plan {
		i, task := i, task // capture

		g.Go(func() error {
			agent, ok := o.agents[task.AgentID]
			if !ok {
				mu.Lock()
				failures = append(failures, subagent.Failure{AgentID: task.AgentID, Error: "subagent 未注册"})
				mu.Unlock()
				return nil // 不杀兄弟
			}

			// 预算原子预留（worker 内检查，不能 dispatch loop 检查）
			if !o.reserveBudget(&reserved, task.MaxTokens) {
				mu.Lock()
				failures = append(failures, subagent.Failure{
					AgentID: task.AgentID,
					Error:   fmt.Sprintf("预算超限: %d", o.cfg.TokenBudget),
				})
				mu.Unlock()
				return nil
			}

			// 每个 subagent 独立超时（从 group context 派生）
			actx, cancel := context.WithTimeout(gctx, o.cfg.AgentTimeout)
			defer cancel()

			res, err := o.runWithRecover(actx, agent, task)
			if err != nil {
				reserved.Add(-int64(task.MaxTokens)) // 退款
				mu.Lock()
				failures = append(failures, subagent.Failure{AgentID: task.AgentID, Error: err.Error()})
				mu.Unlock()
				return nil
			}

			// 预留调整到实际消耗
			reserved.Add(int64(res.Tokens - task.MaxTokens))
			spent.Add(int64(res.Tokens))

			results[i] = res // pre-size，保证 decomposition order
			return nil
		})
	}

	// Wait 只在父 context 取消时返回 error
	if err := g.Wait(); err != nil {
		return subagent.RunReport{}, fmt.Errorf("调度取消: %w", err)
	}

	// 过滤空结果
	var nonEmpty []subagent.Result
	for _, r := range results {
		if r.AgentID != "" {
			nonEmpty = append(nonEmpty, r)
		}
	}
	sort.Slice(nonEmpty, func(a, b int) bool { return nonEmpty[a].AgentID < nonEmpty[b].AgentID })

	return subagent.RunReport{
		Results:     nonEmpty,
		Failures:    failures,
		TokensSpent: int(spent.Load()),
		Elapsed:     time.Since(start),
	}, nil
}

// RunSingle 单 subagent 执行。
func (o *Orchestrator) RunSingle(ctx context.Context, t subagent.Task) (subagent.Result, error) {
	a, ok := o.agents[t.AgentID]
	if !ok {
		return subagent.Result{}, fmt.Errorf("subagent 未注册: %s", t.AgentID)
	}
	actx, cancel := context.WithTimeout(ctx, o.cfg.AgentTimeout)
	defer cancel()
	return o.runWithRecover(actx, a, t)
}

// RunChain 串行执行（前一个结果给下一个）。
func (o *Orchestrator) RunChain(ctx context.Context, tasks []subagent.Task) (subagent.RunReport, error) {
	start := time.Now()
	var results []subagent.Result
	var failures []subagent.Failure
	prev := ""

	for _, t := range tasks {
		if prev != "" {
			t.Objective = t.Objective + "\n\n上下文（来自上一步）:\n" + prev
		}
		res, err := o.RunSingle(ctx, t)
		if err != nil {
			failures = append(failures, subagent.Failure{AgentID: t.AgentID, Error: err.Error()})
			continue
		}
		if res.Error != "" {
			failures = append(failures, subagent.Failure{AgentID: t.AgentID, Error: res.Error})
			continue
		}
		results = append(results, res)
		prev = res.Content
	}

	return subagent.RunReport{
		Results:  results,
		Failures: failures,
		Elapsed:  time.Since(start),
	}, nil
}

// RunBackground 后台异步执行（立即返回结果通道）。
func (o *Orchestrator) RunBackground(ctx context.Context, t subagent.Task) (<-chan subagent.Result, error) {
	if _, ok := o.agents[t.AgentID]; !ok {
		return nil, fmt.Errorf("subagent 未注册: %s", t.AgentID)
	}
	ch := make(chan subagent.Result, 1)
	go func() {
		res, err := o.RunSingle(context.Background(), t)
		if err != nil {
			res = subagent.Result{AgentID: t.AgentID, Error: err.Error()}
		}
		ch <- res
		close(ch)
	}()
	return ch, nil
}

// reserveBudget 原子预留预算（compare-and-add）。
func (o *Orchestrator) reserveBudget(reserved *atomic.Int64, amount int) bool {
	if o.cfg.TokenBudget == 0 {
		return true // 无预算限制
	}
	for {
		cur := reserved.Load()
		if cur+int64(amount) > int64(o.cfg.TokenBudget) {
			return false
		}
		if reserved.CompareAndSwap(cur, cur+int64(amount)) {
			return true
		}
	}
}

// runWithRecover 带 panic 恢复地执行 subagent。
func (o *Orchestrator) runWithRecover(ctx context.Context, a subagent.Agent, t subagent.Task) (res subagent.Result, err error) {
	defer func() {
		if r := recover(); r != nil {
			res = subagent.Result{AgentID: t.AgentID}
			err = fmt.Errorf("subagent panic: %v", r)
		}
	}()
	res, err = a.Run(ctx, t)
	return res, err
}

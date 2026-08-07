package heartbeat

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Black0Bag/minibox/internal/fsutil"
)

// 状态
const (
	StatusOn  = "on"
	StatusOff = "off"
)

// 最小间隔（防滥用，默认 5 分钟）
const MinInterval = 60 * 5

// Task 心跳任务（按需订阅，非全量轮询；F2：必须绑定用户预设需求边界）
type Task struct {
	ID         string `json:"id"`
	Desc       string `json:"desc"`
	Prompt     string `json:"prompt"`     // 执行提示（给 LLM/agent）
	Boundaries string `json:"boundaries"` // 用户预设需求边界（安全约束）
	Interval   int    `json:"interval"`   // 执行间隔（秒）
	Status     string `json:"status"`
	CreatedAt  string `json:"created_at"`
	LastRun    string `json:"last_run,omitempty"`
	lastRunAt  time.Time // 内部时间（不序列化）
}

// Executor 心跳执行器（由上层注入：可调 LLM/agent 执行）
type Executor interface {
	Execute(ctx context.Context, task Task) (string, error)
}

// Engine 心跳任务引擎（F2：可主动操作，但必须绑定用户预设需求边界）
type Engine struct {
	mu      sync.Mutex
	dir     string
	tasks   map[string]*Task
	exec    Executor
	loaded  bool
}

// NewEngine 创建心跳引擎
func NewEngine(dir string) *Engine {
	e := &Engine{dir: dir, tasks: map[string]*Task{}}
	_ = e.load()
	return e
}

// SetExecutor 注入执行器
func (e *Engine) SetExecutor(exec Executor) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.exec = exec
}

// Create 创建心跳任务（订阅）
func (e *Engine) Create(t Task) (*Task, error) {
	if strings.TrimSpace(t.Desc) == "" {
		return nil, errors.New("任务描述不能为空")
	}
	if t.Interval < MinInterval {
		return nil, fmt.Errorf("间隔至少 %d 秒（当前 %d）", MinInterval, t.Interval)
	}
	if t.Status == "" {
		t.Status = StatusOn
	}
	t.ID = fsutil.NewID()
	t.CreatedAt = time.Now().Format("2006-01-02 15:04:05")
	t.lastRunAt = time.Now()
	e.mu.Lock()
	defer e.mu.Unlock()
	e.tasks[t.ID] = &t
	if err := e.saveLocked(); err != nil {
		return nil, err
	}
	return &t, nil
}

// Get 获取任务
func (e *Engine) Get(id string) (*Task, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	t, ok := e.tasks[id]
	if !ok {
		return nil, errors.New("任务不存在")
	}
	c := *t
	return &c, nil
}

// List 列出全部任务
func (e *Engine) List() ([]Task, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := make([]Task, 0, len(e.tasks))
	for _, t := range e.tasks {
		out = append(out, *t)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt > out[j].CreatedAt })
	return out, nil
}

// Unsubscribe 退订任务
func (e *Engine) Unsubscribe(id string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	t, ok := e.tasks[id]
	if !ok {
		return errors.New("任务不存在")
	}
	t.Status = StatusOff
	return e.saveLocked()
}

// DueTasks 返回到期需执行的任务（按间隔判定）
func (e *Engine) DueTasks(now time.Time) ([]Task, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	var due []Task
	for _, t := range e.tasks {
		if t.Status != StatusOn {
			continue
		}
		interval := time.Duration(t.Interval) * time.Second
		if now.Sub(t.lastRunAt) >= interval {
			due = append(due, *t)
		}
	}
	return due, nil
}

// RunDue 执行全部到期任务
func (e *Engine) RunDue(ctx context.Context, now time.Time) (int, error) {
	due, err := e.DueTasks(now)
	if err != nil {
		return 0, err
	}
	n := 0
	for i := range due {
		if err := ctx.Err(); err != nil {
			return n, err
		}
		t := due[i]
		e.mu.Lock()
		exec := e.exec
		e.mu.Unlock()
		if exec != nil {
			if _, err := exec.Execute(ctx, t); err != nil {
				return n, err
			}
		}
		// 更新 lastRun
		e.mu.Lock()
		if cur, ok := e.tasks[t.ID]; ok {
			cur.lastRunAt = now
			cur.LastRun = now.Format("2006-01-02 15:04:05")
			_ = e.saveLocked()
		}
		e.mu.Unlock()
		n++
	}
	return n, nil
}

// Start 后台心跳循环
func (e *Engine) Start(ctx context.Context, tick time.Duration, exec Executor) {
	e.SetExecutor(exec)
	go func() {
		ticker := time.NewTicker(tick)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case now := <-ticker.C:
				_, _ = e.RunDue(ctx, now)
			}
		}
	}()
}

// ForceRun 立即执行指定任务（无视到期状态）
func (e *Engine) ForceRun(id string) error {
	e.mu.Lock()
	t, ok := e.tasks[id]
	e.mu.Unlock()
	if !ok {
		return errors.New("任务不存在")
	}
	e.mu.Lock()
	exec := e.exec
	e.mu.Unlock()
	if exec != nil {
		ctx := context.Background()
		if _, err := exec.Execute(ctx, *t); err != nil {
			return err
		}
	}
	e.mu.Lock()
	if cur, ok := e.tasks[id]; ok {
		cur.lastRunAt = time.Now()
		cur.LastRun = time.Now().Format("2006-01-02 15:04:05")
		_ = e.saveLocked()
	}
	e.mu.Unlock()
	return nil
}

// SetExecutorTmp 替换执行器（API 即时执行用）
func (e *Engine) SetExecutorTmp(exec Executor) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.exec = exec
	return nil
}

// setLastRun 测试辅助：调整 lastRunAt
func (e *Engine) setLastRun(id string, t time.Time) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	tk, ok := e.tasks[id]
	if !ok {
		return errors.New("任务不存在")
	}
	tk.lastRunAt = t
	tk.LastRun = t.Format("2006-01-02 15:04:05")
	return e.saveLocked()
}

// ============ 持久化 ============

func (e *Engine) path() string { return filepath.Join(e.dir, "heartbeat.json") }

func (e *Engine) saveLocked() error {
	if err := os.MkdirAll(e.dir, 0o755); err != nil {
		return err
	}
	data, _ := json.MarshalIndent(e.tasks, "", "  ")
	return os.WriteFile(e.path(), data, 0o600)
}

func (e *Engine) load() error {
	data, err := os.ReadFile(e.path())
	if err != nil {
		return nil
	}
	var tasks map[string]*Task
	if err := json.Unmarshal(data, &tasks); err != nil {
		return fmt.Errorf("解析心跳任务: %w", err)
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	e.tasks = tasks
	// 重启后 lastRunAt 初始化为当前时间（避免立即触发）
	for _, t := range tasks {
		t.lastRunAt = time.Now()
	}
	e.loaded = true
	return nil
}

package todolist

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/Black0Bag/minibox/internal/fsutil"
)

// 状态常量
const (
	// 步骤状态
	StepPending = "pending" // 待执行
	StepRunning = "running" // 执行中
	StepDone    = "done"    // 完成
	StepFailed  = "failed"  // 失败
	// 计划状态
	PlanPending = "pending" // 待执行
	PlanRunning = "running" // 执行中
	PlanDone    = "done"    // 全部完成
	PlanFailed  = "failed"  // 失败（可回滚后重跑）
)

// Step 计划步骤
type Step struct {
	ID      int    `json:"id"`
	Desc    string `json:"desc"` // 步骤描述（做什么）
	Status  string `json:"status"`
	Output  string `json:"output,omitempty"`
	Err     string `json:"error,omitempty"`
	Created string `json:"created_at"`
}

// Plan 长程任务计划（to-do-list 长程驱动器）
type Plan struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	Goal      string `json:"goal"` // 总目标
	Status    string `json:"status"`
	Steps     []Step `json:"steps"`
	Current   int    `json:"current"` // 当前执行到的步骤下标（续跑断点）
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

// StepFunc 单步执行函数（由调用方注入：可用 LLM/subagent 执行）
type StepFunc func(ctx context.Context, plan *Plan, idx int) (string, error)

// Driver 长程任务驱动器：计划持久化 + 校验 + 逐步执行 + 回滚 + 中断续跑
type Driver struct {
	dir string // 计划文件目录
}

// NewDriver 创建驱动器（dir 为计划持久化目录）
func NewDriver(dir string) *Driver {
	return &Driver{dir: dir}
}

// ErrNoSteps 计划无步骤
var ErrNoSteps = errors.New("计划至少需要一步")

// Create 创建计划（含校验）。steps 仅使用 Desc 字段，ID/Status 由驱动分配。
func (d *Driver) Create(title, goal string, steps []Step) (*Plan, error) {
	if strings.TrimSpace(goal) == "" {
		return nil, errors.New("计划目标不能为空")
	}
	if len(steps) == 0 {
		return nil, ErrNoSteps
	}
	for i := range steps {
		if strings.TrimSpace(steps[i].Desc) == "" {
			return nil, fmt.Errorf("步骤 %d 描述不能为空", i+1)
		}
	}
	now := time.Now().Format("2006-01-02 15:04:05")
	p := &Plan{
		ID:        fsutil.NewID(),
		Title:     title,
		Goal:      goal,
		Status:    PlanPending,
		Current:   0,
		CreatedAt: now,
		UpdatedAt: now,
		Steps:     make([]Step, 0, len(steps)),
	}
	for i, s := range steps {
		p.Steps = append(p.Steps, Step{
			ID:      i + 1,
			Desc:    strings.TrimSpace(s.Desc),
			Status:  StepPending,
			Created: now,
		})
	}
	if err := d.Save(p); err != nil {
		return nil, err
	}
	return p, nil
}

// path 返回计划文件路径（ID 白名单校验防目录穿越）
func (d *Driver) path(id string) (string, error) {
	if id == "" || strings.ContainsAny(id, `/\`) || strings.Contains(id, "..") {
		return "", fmt.Errorf("非法计划 ID %q", id)
	}
	return filepath.Join(d.dir, id+".json"), nil
}

// Save 持久化计划
func (d *Driver) Save(p *Plan) error {
	if p == nil || p.ID == "" {
		return errors.New("计划无效")
	}
	path, err := d.path(p.ID)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(d.dir, 0o755); err != nil {
		return fmt.Errorf("创建计划目录: %w", err)
	}
	p.UpdatedAt = time.Now().Format("2006-01-02 15:04:05")
	data, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return fmt.Errorf("序列化计划: %w", err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("写入计划 %s: %w", p.ID, err)
	}
	return nil
}

// Get 加载计划（中断续跑入口）
func (d *Driver) Get(id string) (*Plan, error) {
	path, err := d.path(id)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var p Plan
	if err := json.Unmarshal(data, &p); err != nil {
		return nil, fmt.Errorf("解析计划 %s: %w", id, err)
	}
	return &p, nil
}

// List 列出全部计划（按创建时间倒序）
func (d *Driver) List() ([]Plan, error) {
	entries, err := os.ReadDir(d.dir)
	if err != nil {
		if os.IsNotExist(err) {
			return []Plan{}, nil
		}
		return nil, fmt.Errorf("读取计划目录: %w", err)
	}
	var plans []Plan
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		p, err := d.Get(strings.TrimSuffix(e.Name(), ".json"))
		if err != nil {
			continue
		}
		plans = append(plans, *p)
	}
	sort.Slice(plans, func(i, j int) bool { return plans[i].CreatedAt > plans[j].CreatedAt })
	return plans, nil
}

// Delete 删除计划
func (d *Driver) Delete(id string) error {
	path, err := d.path(id)
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("计划 %s 不存在", id)
		}
		return err
	}
	return nil
}

// nextPending 返回下一个待执行步骤下标；全部完成返回 -1
func (p *Plan) nextPending() int {
	for i := range p.Steps {
		if p.Steps[i].Status == StepPending || p.Steps[i].Status == StepFailed {
			return i
		}
	}
	return -1
}

// RunAll 从断点开始执行全部剩余步骤（中断续跑）。
// 已 done 的计划直接返回 nil。
func (d *Driver) RunAll(ctx context.Context, id string, fn StepFunc) error {
	p, err := d.Get(id)
	if err != nil {
		return err
	}
	if p.Status == PlanDone {
		return nil
	}
	p.Status = PlanRunning
	if err := d.Save(p); err != nil {
		return err
	}
	for {
		idx := p.nextPending()
		if idx < 0 {
			break
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		p.Current = idx
		p.Steps[idx].Status = StepRunning
		_ = d.Save(p)
		out, runErr := fn(ctx, p, idx)
		if runErr != nil {
			p.Steps[idx].Status = StepFailed
			p.Steps[idx].Err = runErr.Error()
			p.Status = PlanFailed
			_ = d.Save(p)
			return fmt.Errorf("步骤 %d（%s）失败: %w", idx+1, p.Steps[idx].Desc, runErr)
		}
		p.Steps[idx].Status = StepDone
		p.Steps[idx].Output = out
		p.Steps[idx].Err = ""
		_ = d.Save(p)
	}
	p.Status = PlanDone
	return d.Save(p)
}

// Rollback 回滚到指定步骤（下标 idx，0 起）。该步骤及之后全部重置为 pending。
func (d *Driver) Rollback(id string, idx int) error {
	p, err := d.Get(id)
	if err != nil {
		return err
	}
	if idx < 0 || idx >= len(p.Steps) {
		return fmt.Errorf("回滚下标 %d 越界（共 %d 步）", idx, len(p.Steps))
	}
	for i := idx; i < len(p.Steps); i++ {
		p.Steps[i].Status = StepPending
		p.Steps[i].Err = ""
	}
	p.Status = PlanPending
	p.Current = idx
	return d.Save(p)
}

// ResumeAt 获取从指定断点继续执行（等价于 RunAll，但明确语义供 API 使用）
func (d *Driver) ResumeAt(ctx context.Context, id string, fn StepFunc) error {
	return d.RunAll(ctx, id, fn)
}

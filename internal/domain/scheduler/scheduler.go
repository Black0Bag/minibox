// Package scheduler 定义调度中枢接口（B17，cronicle/libtnb-cron/GoForj 实证）。
// 设计（进度跟踪 20260812 子块D）：
//   - 单二进制用 in-process 调度（robfig/cron），不用分布式
//   - schedule/alarm/calendar 三分类
//   - agent 定时任务带预算：max_turns/wallclock/max_tokens/budget_usd
//   - 结果写知识库；调度也走 Router（Q4B 红线）
package scheduler

import (
	"context"
	"time"
)

// TaskType 任务类型（B17 三分类）。
type TaskType string

const (
	// TypeSchedule 周期性计划任务。
	TypeSchedule TaskType = "schedule"
	// TypeAlarm 一次性闹钟任务。
	TypeAlarm TaskType = "alarm"
	// TypeCalendar 日历事件任务。
	TypeCalendar TaskType = "calendar"
)

// Budget 任务预算（Q4B：定时任务必须有界）。
type Budget struct {
	MaxTurns  int           // 最大步数
	MaxTokens int           // 最大 token
	MaxWall   time.Duration // 最大墙钟时间
	MaxUSD    float64       // 最大费用（美元）
}

// Task 一个定时任务。
type Task struct {
	ID        string     `json:"id"`
	Type      TaskType   `json:"type"`
	Name      string     `json:"name"`         // 任务名（展示）
	Spec      string     `json:"spec"`         // cron 表达式（schedule/calendar）
	At        *time.Time `json:"at,omitempty"` // 闹钟时间（alarm 单次）
	Prompt    string     `json:"prompt"`       // 触发时给 agent 的指令
	Budget    Budget     `json:"budget"`       // 预算上限
	Enabled   bool       `json:"enabled"`
	CreatedAt time.Time  `json:"created_at"`
}

// Runner 任务执行器（组合根注入，连接 Agent 引擎）。
// 调度器触发任务时调用，返回执行结果供写知识库。
type Runner interface {
	Run(ctx context.Context, task Task) (result string, tokens int, err error)
}

// Scheduler 调度中枢接口。
type Scheduler interface {
	// Add 注册任务（返回任务 ID）。
	Add(task Task) (string, error)
	// Remove 移除任务。
	Remove(id string) error
	// List 列出所有任务。
	List() []Task
	// Start 启动调度（阻塞，监听 ctx 取消）。
	Start(ctx context.Context) error
	// Stop 停止调度。
	Stop()
}

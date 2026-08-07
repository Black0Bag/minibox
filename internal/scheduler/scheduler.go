package scheduler

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

// 任务类型（三分类）
const (
	TypeAlarm    = "alarm"    // 闹钟/定时提醒
	TypeSchedule = "schedule" // 定时任务
	TypeCalendar = "calendar" // 日历事件
)

// 状态
const (
	StatusPending   = "pending"
	StatusDone      = "done"
	StatusCancelled = "cancelled"
)

// Task 调度任务
type Task struct {
	ID        string `json:"id"`
	Type      string `json:"type"`
	Time      string `json:"time"`  // HH:MM（每日重复）
	Date      string `json:"date,omitempty"` // YYYY-MM-DD（单次）
	Desc      string `json:"desc"`
	Payload   string `json:"payload,omitempty"`
	Status    string `json:"status"`
	CreatedAt string `json:"created_at"`
	DoneAt    string `json:"done_at,omitempty"`
}

// KBSink 结果写知识库的接口（结果写知识库，路线图 Phase 4）
type KBSink interface {
	Write(zone, title, content string)
}

// Scheduler 调度中枢：schedule/alarm/calendar 三分类，结果写知识库
type Scheduler struct {
	mu     sync.Mutex
	dir    string
	tasks  map[string]*Task
	kb     KBSink
	loaded bool
}

// NewScheduler 创建调度中枢（dir 为持久化目录）
func NewScheduler(dir string) *Scheduler {
	s := &Scheduler{dir: dir, tasks: map[string]*Task{}}
	_ = s.load()
	return s
}

// SetKBSink 注入知识库写入器（结果沉淀）
func (s *Scheduler) SetKBSink(kb KBSink) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.kb = kb
}

// ErrInvalidTask 无效任务
var ErrInvalidTask = errors.New("无效任务")

// Create 创建任务
func (s *Scheduler) Create(t Task) (*Task, error) {
	switch t.Type {
	case TypeAlarm, TypeSchedule, TypeCalendar:
	default:
		return nil, fmt.Errorf("%w: 类型 %q 非法", ErrInvalidTask, t.Type)
	}
	if t.Time == "" && t.Date == "" {
		return nil, fmt.Errorf("%w: 时间不能为空", ErrInvalidTask)
	}
	if strings.TrimSpace(t.Desc) == "" {
		return nil, fmt.Errorf("%w: 描述不能为空", ErrInvalidTask)
	}
	if t.Status == "" {
		t.Status = StatusPending
	}
	t.ID = fsutil.NewID()
	t.CreatedAt = time.Now().Format("2006-01-02 15:04:05")
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tasks[t.ID] = &t
	if err := s.saveLocked(); err != nil {
		return nil, err
	}
	return &t, nil
}

// Get 获取任务
func (s *Scheduler) Get(id string) (*Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.tasks[id]
	if !ok {
		return nil, errors.New("任务不存在")
	}
	c := *t
	return &c, nil
}

// List 列出全部任务
func (s *Scheduler) List() ([]Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Task, 0, len(s.tasks))
	for _, t := range s.tasks {
		out = append(out, *t)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt > out[j].CreatedAt })
	return out, nil
}

// DueTasks 返回当前到期且待执行的任务（按类型/描述排序稳定）
func (s *Scheduler) DueTasks(now time.Time) ([]Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cur := now.Format("15:04")
	today := now.Format("2006-01-02")
	var due []Task
	for _, t := range s.tasks {
		if t.Status != StatusPending {
			continue
		}
		// 单次任务：日期匹配且时间到期
		if t.Date != "" {
			if t.Date == today && t.Time <= cur {
				due = append(due, *t)
			}
			continue
		}
		// 每日任务：仅比较时间
		if t.Time <= cur {
			due = append(due, *t)
		}
	}
	return due, nil
}

// MarkDone 标记任务完成
func (s *Scheduler) MarkDone(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.tasks[id]
	if !ok {
		return errors.New("任务不存在")
	}
	t.Status = StatusDone
	t.DoneAt = time.Now().Format("2006-01-02 15:04:05")
	return s.saveLocked()
}

// Cancel 取消任务
func (s *Scheduler) Cancel(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.tasks[id]
	if !ok {
		return errors.New("任务不存在")
	}
	t.Status = StatusCancelled
	return s.saveLocked()
}

// RunDue 执行全部到期任务：结果写知识库（route：schedule/alarm/calendar 沉淀）
func (s *Scheduler) RunDue(ctx context.Context, now time.Time) (int, error) {
	due, err := s.DueTasks(now)
	if err != nil {
		return 0, err
	}
	n := 0
	for i := range due {
		if err := ctx.Err(); err != nil {
			return n, err
		}
		t := due[i]
		s.mu.Lock()
		kb := s.kb
		s.mu.Unlock()
		if kb != nil {
			kb.Write("store", "调度-"+t.Desc, t.Payload)
		}
		if err := s.MarkDone(t.ID); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}

// Start 后台调度循环（每 tick 执行一次到期任务）
func (s *Scheduler) Start(ctx context.Context, tick time.Duration, kb KBSink) {
	s.SetKBSink(kb)
	go func() {
		ticker := time.NewTicker(tick)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case now := <-ticker.C:
				_, _ = s.RunDue(ctx, now)
			}
		}
	}()
}

// ============ 持久化 ============

func (s *Scheduler) path() string { return filepath.Join(s.dir, "scheduler.json") }

func (s *Scheduler) saveLocked() error {
	if err := os.MkdirAll(s.dir, 0o755); err != nil {
		return err
	}
	data, _ := json.MarshalIndent(s.tasks, "", "  ")
	return os.WriteFile(s.path(), data, 0o600)
}

func (s *Scheduler) load() error {
	data, err := os.ReadFile(s.path())
	if err != nil {
		return nil // 首次无文件
	}
	var tasks map[string]*Task
	if err := json.Unmarshal(data, &tasks); err != nil {
		return fmt.Errorf("解析调度任务: %w", err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tasks = tasks
	s.loaded = true
	return nil
}

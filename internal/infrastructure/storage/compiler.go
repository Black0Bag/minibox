package storage

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"sync"
	"time"

	"github.com/Black0Bag/minibox/internal/domain/memory"
)

// SQLiteCompiler 编译管道实现（B12）。
// 状态机：PENDING→PROCESSING→READY/FAILED，单调不后退（SmartSearch 实证）。
// 当前骨架：异步提交 + 状态查询 + 死信重试框架。LLM 提炼/embedding 在 Phase 3 后续接入。
type SQLiteCompiler struct {
	db        *sql.DB
	store     memory.Store
	mu        sync.Mutex
	jobs      map[string]*memory.CompileJob // 内存作业表（暂用，Phase 3 落表）
}

// NewCompiler 创建编译管道。
func NewCompiler(db *sql.DB, store memory.Store) *SQLiteCompiler {
	return &SQLiteCompiler{
		db:    db,
		store: store,
		jobs:  make(map[string]*memory.CompileJob),
	}
}

// Compile 提交编译作业（异步）。
// 当前骨架：立即标记 PENDING，后台 goroutine 处理（Phase 3 后续接 LLM 提炼+embedding）。
func (c *SQLiteCompiler) Compile(ctx context.Context, source string, opts memory.CompileOptions) (*memory.CompileJob, error) {
	job := &memory.CompileJob{
		ID:        newJobID(),
		Source:    source,
		Status:    memory.JobPending,
		CreatedAt: time.Now().Format("2006-01-02 15:04:05"),
		UpdatedAt: time.Now().Format("2006-01-02 15:04:05"),
	}

	c.mu.Lock()
	c.jobs[job.ID] = job
	c.mu.Unlock()

	// 异步启动处理
	go c.process(job.ID, source, opts)

	return job, nil
}

// process 后台处理编译作业（状态机推进）。
// 当前：直接把 source 当文本写入 kb_store（占位），后续接 LLM 提炼+切块+embedding。
func (c *SQLiteCompiler) process(jobID, source string, opts memory.CompileOptions) {
	c.updateStatus(jobID, memory.JobProcessing, 0, 1, "")

	// 骨架：源文本直接入库（Phase 3 后续替换为完整管道）
	err := c.store.Upsert(context.Background(), memory.Entry{
		Content:      source,
		Source:       source,
		SourceHash:   hashSource(source),
		Importance:   0.5,
	})
	if err != nil {
		c.updateStatus(jobID, memory.JobFailed, 0, 1, err.Error())
		return
	}

	c.updateStatus(jobID, memory.JobReady, 1, 1, "")
}

// GetJob 查询作业状态。
func (c *SQLiteCompiler) GetJob(ctx context.Context, id string) (*memory.CompileJob, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	job, ok := c.jobs[id]
	if !ok {
		return nil, fmt.Errorf("作业不存在: %s", id)
	}
	// 返回副本，避免并发修改
	cp := *job
	return &cp, nil
}

// ListJobs 列出作业。
func (c *SQLiteCompiler) ListJobs(ctx context.Context, limit int) ([]memory.CompileJob, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	var jobs []memory.CompileJob
	for _, j := range c.jobs {
		jobs = append(jobs, *j)
		if len(jobs) >= limit && limit > 0 {
			break
		}
	}
	return jobs, nil
}

// Retry 重试失败作业（死信恢复）。
func (c *SQLiteCompiler) Retry(ctx context.Context, id string) (*memory.CompileJob, error) {
	c.mu.Lock()
	job, ok := c.jobs[id]
	c.mu.Unlock()
	if !ok {
		return nil, fmt.Errorf("作业不存在: %s", id)
	}
	if job.Status != memory.JobFailed {
		return nil, fmt.Errorf("作业非失败状态，无法重试: %s", job.Status)
	}

	// 重置为 pending 重新处理
	go c.process(id, job.Source, memory.CompileOptions{})
	return c.GetJob(ctx, id)
}

// updateStatus 更新作业状态。
func (c *SQLiteCompiler) updateStatus(id string, status memory.JobStatus, progress, total int, errMsg string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if job, ok := c.jobs[id]; ok {
		job.Status = status
		job.Progress = progress
		job.Total = total
		job.Error = errMsg
		job.UpdatedAt = time.Now().Format("2006-01-02 15:04:05")
	}
}

// newJobID 生成作业 ID。
func newJobID() string {
	b := make([]byte, 8)
	rand.Read(b)
	return "job_" + hex.EncodeToString(b)
}

// hashSource 生成源内容哈希（幂等摄入用）。
func hashSource(s string) string {
	// 简单哈希（Phase 3 后续用 SHA-256）
	return fmt.Sprintf("src_%x", len(s))
}

var _ = sql.ErrNoRows

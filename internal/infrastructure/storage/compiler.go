package storage

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/Black0Bag/minibox/internal/domain/memory"
)

// KnowledgeExtractor 将原始输入提炼为可直接入库的结构化知识条目。
// 实现由 app 层注入，可使用功能级 LLM 路由；失败时编译器安全降级为原文切块。
type KnowledgeExtractor interface {
	Extract(ctx context.Context, source string) ([]memory.Entry, error)
}

// SQLiteCompiler 编译管道实现（B12）。
// 状态机：PENDING→PROCESSING→READY/FAILED，单调不后退（SmartSearch 实证）。
// 流程：解析→切块→embedding（若配置）→入库（Store.Embed 写向量）。
// embedding 不可用时降级纯文本入库（向量检索自动降级 FTS5→LIKE）。
type SQLiteCompiler struct {
	db        *sql.DB
	store     memory.Store
	mu        sync.Mutex
	jobs      map[string]*memory.CompileJob // 内存作业表（暂用，Phase 3 落表）
	embedder  Embedder                      // 可选 embedding 客户端
	extractor KnowledgeExtractor            // 可选 LLM 结构化提炼器
}

// Embedder 文本向量化接口（组合根注入）。
// 例：infrastructure/llm.EmbeddingClient + 模型名封装。
type Embedder interface {
	// EmbedBatch 批量生成向量（长度与 texts 对应）。
	EmbedBatch(ctx context.Context, texts []string) ([][]float32, error)
}

// NewCompiler 创建编译管道。
func NewCompiler(db *sql.DB, store memory.Store) *SQLiteCompiler {
	return &SQLiteCompiler{
		db:    db,
		store: store,
		jobs:  make(map[string]*memory.CompileJob),
	}
}

// SetKnowledgeExtractor 注入可选的 LLM 结构化提炼器。
func (c *SQLiteCompiler) SetKnowledgeExtractor(e KnowledgeExtractor) {
	c.extractor = e
}

// SetEmbedder 注入 embedding 客户端（阶段 2.1 接通向量检索）。
func (c *SQLiteCompiler) SetEmbedder(e Embedder) {
	c.embedder = e
}

// Compile 提交编译作业（异步）。
// 当前骨架：立即标记 PENDING，后台 goroutine 处理（Phase 3 后续接 LLM 提炼+embedding）。
// 后台上下文用 context.WithoutCancel 派生：编译作业必须脱离请求生命周期（golang-context）。
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

	// 异步启动处理：脱离请求取消链，作业不会被客户端断开打断（G118 修复）
	jobCtx := context.WithoutCancel(ctx)
	go c.process(jobCtx, job.ID, source, opts)

	return job, nil
}

// process 后台处理编译作业（状态机推进）。
// 流程：切块 → 逐块入库 → embedding 可用时写入向量（Store.Embed）。
// 每 chunk 独立 hash（hashSource(chunk)），确保 GetIDByHash 返回正确行、向量不覆盖。
func (c *SQLiteCompiler) process(ctx context.Context, jobID, source string, opts memory.CompileOptions) {
	entries := make([]memory.Entry, 0)
	for _, chunk := range chunkSource(source, opts.MaxChunkTokens) {
		entries = append(entries, memory.Entry{Content: chunk, Source: source, Importance: 0.5})
	}
	if c.extractor != nil {
		structured, err := c.extractor.Extract(ctx, source)
		if err == nil && len(structured) > 0 {
			entries = entries[:0]
			for _, entry := range structured {
				if strings.TrimSpace(entry.Content) == "" {
					continue
				}
				entry.Source = source
				entries = append(entries, entry)
			}
			if len(entries) == 0 {
				for _, chunk := range chunkSource(source, opts.MaxChunkTokens) {
					entries = append(entries, memory.Entry{Content: chunk, Source: source, Importance: 0.5})
				}
			}
		}
	}
	c.updateStatus(jobID, memory.JobProcessing, 0, len(entries), "")

	for i, entry := range entries {
		chunkHash := hashSource(entry.Content)
		entry.SourceHash = chunkHash
		if entry.Importance <= 0 {
			entry.Importance = 0.5
		}
		if err := c.store.Upsert(ctx, entry); err != nil {
			c.updateStatus(jobID, memory.JobFailed, i, len(entries), err.Error())
			return
		}

		// 向量化：embedding 可用时写向量；不可用则跳过（检索自动降级）
		if c.embedder != nil {
			id, err := c.store.GetIDByHash(ctx, chunkHash)
			if err == nil && id > 0 {
				vecs, err := c.embedder.EmbedBatch(ctx, []string{entry.Content})
				if err != nil {
					c.updateStatus(jobID, memory.JobProcessing, i, len(entries), "向量生成失败: "+err.Error())
				} else if len(vecs) == 1 {
					if err := c.store.Embed(ctx, id, memory.TierStore, vecs[0]); err != nil {
						c.updateStatus(jobID, memory.JobProcessing, i, len(entries), "向量写入失败: "+err.Error())
					}
				}
			}
		}

		c.updateStatus(jobID, memory.JobProcessing, i+1, len(entries), "")
	}

	c.updateStatus(jobID, memory.JobReady, len(entries), len(entries), "")
}

// chunkSource 源文本切块（按 opts.MaxChunkTokens 估算，默认每块约 40 行）。
func chunkSource(source string, maxTokens int) []string {
	if maxTokens <= 0 {
		maxTokens = 3000
	}
	// 简单实现：按段落切块（\n\n），再按长度合并（生产可换语义切块）
	paragraphs := splitParagraphs(source)
	var chunks []string
	var current []string
	currentLen := 0
	for _, p := range paragraphs {
		pLen := len(p) / 4 // 估算 token（中文约 4 字符/token 宽松）
		if currentLen+pLen > maxTokens && len(current) > 0 {
			chunks = append(chunks, joinChunk(current))
			current = nil
			currentLen = 0
		}
		current = append(current, p)
		currentLen += pLen
	}
	if len(current) > 0 {
		chunks = append(chunks, joinChunk(current))
	}
	if len(chunks) == 0 {
		chunks = []string{source}
	}
	return chunks
}

// splitParagraphs 按空行切段。
func splitParagraphs(source string) []string {
	var out []string
	start := 0
	for i := 0; i < len(source); i++ {
		if i > 0 && source[i] == '\n' && source[i-1] == '\n' {
			if seg := source[start:i]; seg != "" {
				out = append(out, seg)
			}
			start = i + 1
		}
	}
	if seg := source[start:]; seg != "" {
		out = append(out, seg)
	}
	if len(out) == 0 {
		out = []string{source}
	}
	return out
}

// joinChunk 合并段落为一条 chunk。
func joinChunk(parts []string) string {
	if len(parts) == 0 {
		return ""
	}
	out := parts[0]
	for _, p := range parts[1:] {
		out += "\n\n" + p
	}
	return out
}

// GetJob 查询作业状态。
func (c *SQLiteCompiler) GetJob(_ context.Context, id string) (*memory.CompileJob, error) {
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
func (c *SQLiteCompiler) ListJobs(_ context.Context, limit int) ([]memory.CompileJob, error) {
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

	// 重置为 pending 重新处理（脱离请求取消链）
	go c.process(context.WithoutCancel(ctx), id, job.Source, memory.CompileOptions{})
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

// newJobID 生成作业 ID（crypto/rand，抗碰撞）。
func newJobID() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		// crypto/rand 失败极罕见；退回时间戳保证唯一
		return fmt.Sprintf("job_%d", time.Now().UnixNano())
	}
	return "job_" + hex.EncodeToString(b)
}

// hashSource 生成源内容哈希（幂等摄入 + 每 chunk 唯一标识，SHA-256）。
func hashSource(s string) string {
	sum := sha256.Sum256([]byte(s))
	return "src_" + hex.EncodeToString(sum[:8])
}

var _ = sql.ErrNoRows

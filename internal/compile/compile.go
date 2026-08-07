package compile

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/Black0Bag/minibox/internal/llm"
	"github.com/Black0Bag/minibox/internal/memory"
)

// Task 编译任务
type Task struct {
	ID         int64  `json:"id"`
	SourceType string `json:"source_type"` // url / text
	Content    string `json:"content"`
	Status     string `json:"status"` // pending/running/done/failed
	ResultID   int64  `json:"result_id,omitempty"`
	ErrorMsg   string `json:"error_msg,omitempty"`
	CreatedAt  string `json:"created_at"`
	UpdatedAt  string `json:"updated_at"`
}

// Compiler 编译管道：LLM 提炼 → 结构化入库（T-06：限流/退避/断点续编）
type Compiler struct {
	db      *sql.DB
	store   *memory.Store
	llm     *llm.Client
	queue   chan int64
	running bool
}

// New 创建编译管道
func NewCompiler(store *memory.Store, client *llm.Client) *Compiler {
	return &Compiler{
		db:    store.DB(),
		store: store,
		llm:   client,
		queue: make(chan int64, 100),
	}
}

// Start 启动 worker（启动时重入队未完成任务 = 断点续编）
func (c *Compiler) Start() {
	c.running = true
	c.resumePending()
	go c.run()
}

// Submit 提交编译任务
func (c *Compiler) Submit(sourceType, content string) (int64, error) {
	if sourceType != "text" && sourceType != "url" {
		sourceType = "text"
	}
	now := time.Now().Format("2006-01-02 15:04:05")
	res, err := c.db.Exec(`INSERT INTO compile_tasks(source_type,content,status,created_at,updated_at)
		VALUES(?,?,?,?,?)`, sourceType, content, "pending", now, now)
	if err != nil {
		return 0, fmt.Errorf("创建编译任务: %w", err)
	}
	id, _ := res.LastInsertId()
	c.queue <- id
	slog.Info("编译任务已提交", "id", id, "type", sourceType)
	return id, nil
}

// Get 查询任务
func (c *Compiler) Get(id int64) (*Task, error) {
	var t Task
	var rid sql.NullInt64
	err := c.db.QueryRow(`SELECT id,source_type,content,status,result_id,error_msg,created_at,updated_at
		FROM compile_tasks WHERE id=?`, id).
		Scan(&t.ID, &t.SourceType, &t.Content, &t.Status, &rid, &t.ErrorMsg, &t.CreatedAt, &t.UpdatedAt)
	t.ResultID = rid.Int64
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("编译任务 %d 不存在", id)
	}
	if err != nil {
		return nil, fmt.Errorf("查询编译任务: %w", err)
	}
	return &t, nil
}

// List 任务列表
func (c *Compiler) List(limit int) ([]Task, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := c.db.Query(`SELECT id,source_type,content,status,result_id,error_msg,created_at,updated_at
		FROM compile_tasks ORDER BY id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, fmt.Errorf("列出编译任务: %w", err)
	}
	defer func() { _ = rows.Close() }()
	tasks := []Task{}
	for rows.Next() {
		var t Task
		var rid sql.NullInt64
		if err := rows.Scan(&t.ID, &t.SourceType, &t.Content, &t.Status, &rid, &t.ErrorMsg, &t.CreatedAt, &t.UpdatedAt); err != nil {
			return nil, err
		}
		t.ResultID = rid.Int64
		tasks = append(tasks, t)
	}
	return tasks, rows.Err()
}

// resumePending 断点续编：重启时把 pending/running 重新入队
func (c *Compiler) resumePending() {
	rows, err := c.db.Query(`SELECT id FROM compile_tasks WHERE status IN ('pending','running') ORDER BY id`)
	if err != nil {
		slog.Warn("恢复编译任务失败", "err", err)
		return
	}
	defer func() { _ = rows.Close() }()
	var ids []int64
	for rows.Next() {
		var id int64
		_ = rows.Scan(&id)
		ids = append(ids, id)
	}
	for _, id := range ids {
		select {
		case c.queue <- id:
		default:
		}
	}
	slog.Info("编译任务续编", "count", len(ids))
}

// run worker 串行处理
func (c *Compiler) run() {
	for id := range c.queue {
		c.process(id)
	}
}

func (c *Compiler) process(id int64) {
	t, err := c.Get(id)
	if err != nil {
		slog.Error("编译任务不存在", "id", id)
		return
	}
	now := time.Now().Format("2006-01-02 15:04:05")
	_, _ = c.db.Exec(`UPDATE compile_tasks SET status='running', updated_at=? WHERE id=?`, now, id)
	slog.Info("开始编译", "id", id)

	// LLM 提炼
	result, err := c.distill(context.Background(), t.Content)
	if err != nil {
		c.fail(id, err)
		return
	}
	// 入库（存储区）
	entryID, err := c.store.CreateEntry(memory.ZoneStore, "编译", result.Title, result.Summary, result.Tags, "compile")
	if err != nil {
		c.fail(id, err)
		return
	}
	_, _ = c.db.Exec(`UPDATE compile_tasks SET status='done', result_id=?, updated_at=? WHERE id=?`, entryID, now, id)
	slog.Info("编译完成", "id", id, "entry", entryID)
}

func (c *Compiler) fail(id int64, err error) {
	now := time.Now().Format("2006-01-02 15:04:05")
	msg := err.Error()
	if len(msg) > 500 {
		msg = msg[:500]
	}
	_, _ = c.db.Exec(`UPDATE compile_tasks SET status='failed', error_msg=?, updated_at=? WHERE id=?`, msg, now, id)
	slog.Error("编译失败", "id", id, "err", err)
}

// distillResult LLM 提炼结果
type distillResult struct {
	Title   string   `json:"title"`
	Summary string   `json:"summary"`
	Tags    []string `json:"tags"`
}

// distill 用 LLM 提炼文本为结构化知识
func (c *Compiler) distill(ctx context.Context, content string) (*distillResult, error) {
	if len(content) > 8000 {
		content = content[:8000]
	}
	prompt := `你是 minibox 知识库编译助手。分析以下内容，仅返回一个 JSON 对象（不要任何其他文字、不要 markdown 代码块）：
{"title":"简短标题","summary":"50-100 字中文摘要","tags":["标签1","标签2"]}

内容：
` + content

	out, err := c.llm.Chat(ctx, []llm.Message{
		{Role: "system", Content: "你是严谨的知识编译助手，只输出 JSON。"},
		{Role: "user", Content: prompt},
	}, llm.Options{MaxTokens: 2000, Temperature: 0.2})
	if err != nil {
		return nil, fmt.Errorf("LLM 提炼失败: %w", err)
	}
	out = strings.TrimSpace(out)
	out = strings.TrimPrefix(out, "```json")
	out = strings.TrimSuffix(out, "```")
	out = strings.TrimSpace(out)

	var res distillResult
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		return nil, fmt.Errorf("解析提炼结果: %w", err)
	}
	if res.Title == "" {
		res.Title = "编译条目"
	}
	if res.Summary == "" {
		res.Summary = content
		if len(res.Summary) > 200 {
			res.Summary = res.Summary[:200]
		}
	}
	if len(res.Tags) > 10 {
		res.Tags = res.Tags[:10]
	}
	return &res, nil
}

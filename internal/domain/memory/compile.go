package memory

import "context"

// JobStatus 编译管道作业状态（SmartSearch 实证：单调不后退）。
type JobStatus string

const (
	// JobPending 待处理。
	JobPending JobStatus = "pending"
	// JobProcessing 处理中。
	JobProcessing JobStatus = "processing"
	// JobReady 就绪（已完成编译）。
	JobReady JobStatus = "ready"
	// JobFailed 失败。
	JobFailed JobStatus = "failed"
)

// CompileJob 编译作业。
type CompileJob struct {
	ID         string    `json:"id"`
	Source     string    `json:"source"`  // url / text / image
	Status     JobStatus `json:"status"`
	Progress   int       `json:"progress"`    // 已完成 chunk 数
	Total      int       `json:"total"`       // 总 chunk 数
	Error      string    `json:"error,omitempty"`
	CreatedAt  string    `json:"created_at"`
	UpdatedAt  string    `json:"updated_at"`
}

// CompileOptions 编译选项。
type CompileOptions struct {
	// RequireApproval 高风险内容是否需人工审（Bedrock 实证，默认关）。
	RequireApproval bool
	// MaxChunkTokens 单条 chunk 最大 token 数。
	MaxChunkTokens int
	// BatchSize embedding 批量大小。
	BatchSize int
}

// Compiler 编译管道接口（B12）。
// 流程：解析→切块→LLM 提炼→embedding→入库。
// 状态机：PENDING→PROCESSING→READY/FAILED。
type Compiler interface {
	// Compile 提交编译作业（异步）。
	Compile(ctx context.Context, source string, opts CompileOptions) (*CompileJob, error)

	// GetJob 查询作业状态。
	GetJob(ctx context.Context, id string) (*CompileJob, error)

	// ListJobs 列出作业。
	ListJobs(ctx context.Context, limit int) ([]CompileJob, error)

	// Retry 重试失败作业（死信恢复）。
	Retry(ctx context.Context, id string) (*CompileJob, error)
}

// DistillOptions 蒸馏选项（B13）。
type DistillOptions struct {
	// MinEvidence 最小证据数。
	MinEvidence int
	// ImportanceThreshold 重要性阈值。
	ImportanceThreshold float64
	// WindowDays 统计窗口（天）。
	WindowDays int
}

// DistillResult 蒸馏结果。
type DistillResult struct {
	Extracted int `json:"extracted"` // 提取的偏好数
}

// Distiller 概率蒸馏接口（B13）。
// 结果写入 kb_preferences（key/value/probability/evidence_count）。
type Distiller interface {
	// Distill 执行蒸馏。
	Distill(ctx context.Context, opts DistillOptions) (*DistillResult, error)

	// ListPreferences 列出偏好。
	ListPreferences(ctx context.Context, limit int) ([]Preference, error)
}

// Preference 偏好条目。
type Preference struct {
	Key          string  `json:"key"`
	Value        string  `json:"value"`
	Probability  float64 `json:"probability"`
	EvidenceCnt  int     `json:"evidence_count"`
	LastHitAt    string  `json:"last_hit_at,omitempty"`
}

// Projection 上下文组装结果（投影层）。
// 稳定前缀（缓存命中）+ 动态检索 + 会话摘要。
type Projection struct {
	Pinned    []string  `json:"pinned"`     // 稳定前缀：system prompt + 角色卡 + 世界书
	Retrieved []Hit     `json:"retrieved"`  // 动态检索结果（放稳定内容后）
	Recent    []string  `json:"recent"`     // 最近对话
	Summary   string    `json:"summary"`    // 会话摘要
	Usage     TokenUsage `json:"usage"`     // token 预算使用
}

// TokenUsage token 预算使用。
type TokenUsage struct {
	Pinned    int `json:"pinned"`
	Retrieved int `json:"retrieved"`
	Recent    int `json:"recent"`
	Total     int `json:"total"`
	Budget    int `json:"budget"` // 总预算
}

// Assembler 上下文组装接口（投影层）。
// 依据：Zylos 2026 "上下文窗口是投影不是存储"；稳定前缀命中缓存。
type Assembler interface {
	// Assemble 组装当前推理所需的上下文投影。
	Assemble(ctx context.Context, current string, budget int) (*Projection, error)
}

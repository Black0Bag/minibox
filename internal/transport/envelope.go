// Package transport 传输层：REST / SSE / WebSocket 三通道 + 统一信封。
// 设计（进度跟踪 20260812 第3项接口协议细化）：
//   - 三通道共用统一信封（B20），字段见 Envelope
//   - event_id 用 ULID（26 字符时间排序，与 SSE seq 续传语义一致）
//   - Idempotency-Key 用 UUIDv7（RFC 9562，B-tree 友好）
//   - trace_id 对齐 W3C trace-id（32hex）
package transport

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"time"

	"github.com/google/uuid"
	"github.com/oklog/ulid/v2"
)

// SpecVersion 信封版式号（破坏性变更时 bump，旧前端提示升级）。
const SpecVersion = "1.0"

// timestampLayout 时间戳格式（B22：YYYY-MM-DD HH:MM:SS 正则锁死）。
const timestampLayout = "2006-01-02 15:04:05"

// timestampRE 时间戳校验正则（QB: 锁死格式）。
var timestampRE = regexp.MustCompile(`^\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}$`)

// Producer 生产者枚举（QB：限枚举防扩散）。
type Producer string

const (
	// ProducerAgent 主 agent。
	ProducerAgent Producer = "agent"
	// ProducerSystem 系统。
	ProducerSystem Producer = "system"
	// ProducerDevice 设备。
	ProducerDevice Producer = "device"
	// ProducerScheduler 调度器。
	ProducerScheduler Producer = "scheduler"
	// ProducerUser 用户。
	ProducerUser Producer = "user"
)

// Envelope 统一业务信封（三通道共用，B20）。
// 字段 9 个：spec_version/event_id/trace_id/seq/timestamp/producer/source/type/data。
// 注意：seq 仅 SSE/WS 必有，REST 无（条件必填）。
type Envelope struct {
	SpecVersion string          `json:"spec_version"`
	EventID     string          `json:"event_id"`      // ULID，幂等去重
	TraceID     string          `json:"trace_id"`      // 32hex，对齐 W3C
	Seq         int             `json:"seq,omitempty"` // SSE/WS 单调递增，续传游标
	Timestamp   string          `json:"timestamp"`     // "YYYY-MM-DD HH:MM:SS"
	Producer    string          `json:"producer"`      // 枚举：agent/system/device/scheduler/user/subagent.<id>
	Source      string          `json:"source"`        // 细粒度 URI，如 minibox://session/x
	Type        string          `json:"type"`          // api.*/agent.*/device.* 等
	Data        json.RawMessage `json:"data"`          // 负载，type-specific
}

// NewEnvelope 创建信封（自动生成 event_id/trace_id/timestamp）。
func NewEnvelope(producer, source, typ string, data any) (*Envelope, error) {
	raw, err := json.Marshal(data)
	if err != nil {
		return nil, fmt.Errorf("信封 data 序列化失败: %w", err)
	}
	return &Envelope{
		SpecVersion: SpecVersion,
		EventID:     NewULID(),
		TraceID:     NewTraceID(),
		Timestamp:   time.Now().Format(timestampLayout),
		Producer:    producer,
		Source:      source,
		Type:        typ,
		Data:        raw,
	}, nil
}

// NewULID 生成 ULID（26 字符，时间排序，单调，UUIDv4 兼容熵）。
func NewULID() string {
	entropy := ulid.Monotonic(rand.Reader, 0)
	return ulid.MustNew(ulid.Timestamp(time.Now()), entropy).String()
}

// NewTraceID 生成 32hex trace-id（对齐 W3C trace-id）。
func NewTraceID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		// crypto/rand 失败极罕见；退回 UUIDv4 hex
		return uuid.NewString()
	}
	return hex.EncodeToString(b)
}

// NewIdempotencyKey 生成 UUIDv7（幂等键，RFC 9562，B-tree 友好）。
func NewIdempotencyKey() string {
	id, err := uuid.NewV7()
	if err != nil {
		return uuid.NewString() // 极端情况回退 UUIDv4
	}
	return id.String()
}

// Validate 校验信封字段合法性。
// 返回错误描述；空返回 nil 表示合法。
func (e *Envelope) Validate() error {
	if e.SpecVersion == "" {
		return fmt.Errorf("信封缺 spec_version")
	}
	if e.EventID == "" {
		return fmt.Errorf("信封缺 event_id")
	}
	if e.TraceID == "" {
		return fmt.Errorf("信封缺 trace_id")
	}
	if e.Timestamp != "" && !timestampRE.MatchString(e.Timestamp) {
		return fmt.Errorf("timestamp 格式非法（应为 YYYY-MM-DD HH:MM:SS）: %q", e.Timestamp)
	}
	if e.Producer == "" {
		return fmt.Errorf("信封缺 producer")
	}
	if e.Type == "" {
		return fmt.Errorf("信封缺 type")
	}
	if e.Data == nil {
		return fmt.Errorf("信封缺 data")
	}
	return nil
}

// UnmarshalData 解析 data 负载到目标结构。
func (e *Envelope) UnmarshalData(v any) error {
	if len(e.Data) == 0 || string(e.Data) == "null" {
		return nil
	}
	return json.Unmarshal(e.Data, v)
}

// ValidateUUIDv7 校验幂等键是否为合法 UUIDv7。
func ValidateUUIDv7(key string) error {
	id, err := uuid.Parse(key)
	if err != nil {
		return fmt.Errorf("幂等键非法: %w", err)
	}
	// UUIDv7 版本号 = 7
	if id.Version() != 7 {
		return fmt.Errorf("幂等键非 UUIDv7 版本（当前 v%d）", id.Version())
	}
	return nil
}

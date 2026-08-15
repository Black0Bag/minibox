package transport

import (
	"encoding/json"
	"testing"
	"time"
)

// TestNewEnvelope 创建信封 + 字段自动生成。
func TestNewEnvelope(t *testing.T) {
	env, err := NewEnvelope("agent", "minibox://session/1", "agent.text_message_content", map[string]string{"delta": "你好"})
	if err != nil {
		t.Fatalf("NewEnvelope err=%v", err)
	}

	if env.SpecVersion != "1.0" {
		t.Errorf("SpecVersion=%q, 期望 1.0", env.SpecVersion)
	}
	// event_id 应为 26 字符 ULID
	if len(env.EventID) != 26 {
		t.Errorf("event_id 应 26 字符 ULID，实际 %q(%d)", env.EventID, len(env.EventID))
	}
	// trace_id 应为 32 hex
	if len(env.TraceID) != 32 {
		t.Errorf("trace_id 应 32 hex，实际 %q(%d)", env.TraceID, len(env.TraceID))
	}
	// timestamp 应匹配 YYYY-MM-DD HH:MM:SS
	if !timestampRE.MatchString(env.Timestamp) {
		t.Errorf("timestamp 格式非法: %q", env.Timestamp)
	}
	if env.Producer != "agent" || env.Type != "agent.text_message_content" {
		t.Errorf("producer/type 不对: %q/%q", env.Producer, env.Type)
	}

	// 校验通过
	if err := env.Validate(); err != nil {
		t.Errorf("Validate 应通过: %v", err)
	}
}

// TestValidate 校验失败场景。
func TestValidate(t *testing.T) {
	env, _ := NewEnvelope("system", "src", "type", "data")
	cases := []struct {
		name string
		mut  func(*Envelope)
	}{
		{"缺 spec_version", func(e *Envelope) { e.SpecVersion = "" }},
		{"缺 event_id", func(e *Envelope) { e.EventID = "" }},
		{"缺 trace_id", func(e *Envelope) { e.TraceID = "" }},
		{"缺 producer", func(e *Envelope) { e.Producer = "" }},
		{"缺 type", func(e *Envelope) { e.Type = "" }},
		{"缺 data", func(e *Envelope) { e.Data = nil }},
		{"timestamp 非法", func(e *Envelope) { e.Timestamp = "bad" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cp := *env
			tc.mut(&cp)
			if err := cp.Validate(); err == nil {
				t.Errorf("应校验失败: %s", tc.name)
			}
		})
	}
}

// TestUnmarshalData 解析 data。
func TestUnmarshalData(t *testing.T) {
	env, _ := NewEnvelope("agent", "src", "api.test", map[string]string{"k": "v"})
	var got map[string]string
	if err := env.UnmarshalData(&got); err != nil {
		t.Fatalf("UnmarshalData err=%v", err)
	}
	if got["k"] != "v" {
		t.Errorf("data 解析错误: %v", got)
	}
}

// TestNewTraceID 32hex。
func TestNewTraceID(t *testing.T) {
	id := NewTraceID()
	if len(id) != 32 {
		t.Errorf("trace_id 应 32 字符，实际 %d", len(id))
	}
	for _, c := range id {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			t.Errorf("trace_id 含非 hex 字符: %q", c)
			break
		}
	}
}

// TestNewIdempotencyKey_UUIDv7 幂等键为 UUIDv7。
func TestNewIdempotencyKey_UUIDv7(t *testing.T) {
	key := NewIdempotencyKey()
	if err := ValidateUUIDv7(key); err != nil {
		t.Fatalf("幂等键非 UUIDv7: %v", err)
	}
	// 两次应不同
	if key == NewIdempotencyKey() {
		t.Error("两次幂等键应不同")
	}
}

// TestValidateUUIDv7_非法 非 v7 拒绝。
func TestValidateUUIDv7_非法(t *testing.T) {
	// 传一个 v4 UUID
	v4 := "123e4567-e89b-12d3-a456-426614174000"
	if err := ValidateUUIDv7(v4); err == nil {
		t.Error("v4 UUID 应被拒绝（非 v7）")
	}
	if err := ValidateUUIDv7("not-a-uuid"); err == nil {
		t.Error("非法字符串应被拒绝")
	}
}

// TestNewULID_时间排序 早创建的 ULID 字典序更小。
func TestNewULID_时间排序(t *testing.T) {
	a := NewULID()
	time.Sleep(2 * time.Millisecond)
	b := NewULID()
	if a >= b {
		t.Errorf("ULID 应时间排序（a<b），实际 a=%s b=%s", a, b)
	}
}

// TestEnvelopeJSON 信封 JSON 往返。
func TestEnvelopeJSON(t *testing.T) {
	env, _ := NewEnvelope("agent", "src", "api.test", map[string]int{"n": 1})
	b, err := json.Marshal(env)
	if err != nil {
		t.Fatalf("Marshal err=%v", err)
	}
	var back Envelope
	if err := json.Unmarshal(b, &back); err != nil {
		t.Fatalf("Unmarshal err=%v", err)
	}
	if back.EventID != env.EventID || back.TraceID != env.TraceID || back.SpecVersion != env.SpecVersion {
		t.Errorf("JSON 往返不一致: %+v vs %+v", back, env)
	}
}

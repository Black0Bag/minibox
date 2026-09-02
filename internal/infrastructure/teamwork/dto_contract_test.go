package teamwork

import (
	"context"
	"encoding/json"
	"maps"
	"slices"
	"testing"
)

// TestDTO契约_Project 冻结 POST /api/v1/teamwork/projects 与
// /projects/{id}/conclude 的返回体字段名（Android DTO 直接对齐）。
// 回归：Project 原本无 json tag，序列化出 ID/Question/Team/... 大驼峰，
// 而内层 Team/RoleCard 却是 snake_case，同一响应两种命名风格。
func TestDTO契约_Project(t *testing.T) {
	c := newTestCoordinator()
	p, err := c.StartProject(context.Background(), "dto-1", "设计一个登录接口", "engineering", nil)
	if err != nil {
		t.Fatalf("StartProject err=%v", err)
	}

	raw, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("反序列化失败: %v（json=%s）", err, raw)
	}

	got := slices.Sorted(maps.Keys(m))
	want := slices.Sorted(slices.Values([]string{
		"id", "question", "team", "members", "discussion", "status", "created_at",
	})) // verdict 未裁决时省略
	if !slices.Equal(got, want) {
		t.Errorf("Project JSON 字段=%v, 期望=%v", got, want)
	}

	for _, key := range got {
		if c := key[0]; c >= 'A' && c <= 'Z' {
			t.Errorf("Project 存在大驼峰字段 %q，违反 lower_snake_case 契约", key)
		}
	}

	// 结案后 verdict 出现。
	if _, err := c.RunDiscussion(context.Background(), "dto-1", ""); err != nil {
		t.Fatalf("RunDiscussion err=%v", err)
	}
	done, err := c.Conclude(context.Background(), "dto-1", "采用方案 A")
	if err != nil {
		t.Fatalf("Conclude err=%v", err)
	}
	raw, _ = json.Marshal(done)
	var withVerdict struct {
		Verdict string `json:"verdict"`
		Status  string `json:"status"`
	}
	if err := json.Unmarshal(raw, &withVerdict); err != nil {
		t.Fatalf("反序列化失败: %v", err)
	}
	if withVerdict.Verdict != "采用方案 A" {
		t.Errorf("verdict=%q, 期望「采用方案 A」", withVerdict.Verdict)
	}
	if withVerdict.Status != "done" {
		t.Errorf("status=%q, 期望 done", withVerdict.Status)
	}
}

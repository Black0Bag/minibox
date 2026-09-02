package teamwork

import (
	"encoding/json"
	"maps"
	"slices"
	"testing"
	"time"
)

// jsonKeys 返回一个结构体序列化后的顶层 JSON 字段名（已排序）。
func jsonKeys(t *testing.T, v any) []string {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("反序列化为 map 失败: %v（json=%s）", err, raw)
	}
	return slices.Sorted(maps.Keys(m))
}

// assertJSONKeys 断言顶层字段名集合与期望完全一致。
func assertJSONKeys(t *testing.T, name string, v any, want []string) {
	t.Helper()
	got := jsonKeys(t, v)
	if !slices.Equal(got, slices.Sorted(slices.Values(want))) {
		t.Errorf("%s JSON 字段=%v, 期望=%v", name, got, slices.Sorted(slices.Values(want)))
	}
}

// TestDTO契约_Discussion 冻结 Discussion 的 REST 字段名（Android DTO 直接对齐）。
// 回归：这些字段原本无 json tag，序列化出大驼峰（Question/LeadID…），
// 与 rules.md「REST JSON 字段使用 lower_snake_case」冲突。
func TestDTO契约_Discussion(t *testing.T) {
	d := NewDiscussion("需求", "lead_1")
	if _, err := d.SubmitProposal("m1", "方案"); err != nil {
		t.Fatalf("SubmitProposal err=%v", err)
	}
	if err := d.Conclude("裁决"); err != nil {
		t.Fatalf("Conclude err=%v", err)
	}
	assertJSONKeys(t, "Discussion", d, []string{
		"question", "lead_id", "round", "max_rounds",
		"proposals", "verdict", "done", "started_at",
	})

	// proposals 的 key 是轮次的字符串形式（Go 整数键 map 的固定行为）。
	raw, err := json.Marshal(d)
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}
	var probe struct {
		Proposals map[string][]Proposal `json:"proposals"`
	}
	if err := json.Unmarshal(raw, &probe); err != nil {
		t.Fatalf("proposals 应能按字符串键解析: %v", err)
	}
	if len(probe.Proposals["1"]) != 1 {
		t.Errorf(`proposals["1"] 长度=%d, 期望 1（json=%s）`, len(probe.Proposals["1"]), raw)
	}

	// sync.Mutex 不得出现在输出中（未导出字段天然被忽略，此处防回归）。
	var all map[string]json.RawMessage
	_ = json.Unmarshal(raw, &all)
	if _, bad := all["mu"]; bad {
		t.Error("内部锁字段 mu 泄漏到 JSON 输出")
	}
}

// TestDTO契约_Proposal_Critique 提案与批评的字段名。
func TestDTO契约_Proposal_Critique(t *testing.T) {
	assertJSONKeys(t, "Proposal", Proposal{Round: 1, MemberID: "m", Content: "c"},
		[]string{"round", "member_id", "content"}) // critiques 为空时省略
	assertJSONKeys(t, "Proposal（含批评）", Proposal{
		Round: 1, MemberID: "m", Content: "c",
		Critiques: []Critique{{MemberID: "x", Content: "y"}},
	}, []string{"round", "member_id", "content", "critiques"})
	assertJSONKeys(t, "Critique", Critique{MemberID: "m", Content: "c"},
		[]string{"member_id", "content"})
}

// TestDTO契约_Triage 分诊端点（POST /teamwork/triage）返回体字段名。
func TestDTO契约_Triage(t *testing.T) {
	team := Team{ID: "engineering", Name: "编程团队"}
	tri := &Triage{
		Recommended: []TeamRecommendation{{Team: &team, Reason: "关键词匹配"}},
		NeedClarify: false,
	}
	assertJSONKeys(t, "Triage", tri, []string{"recommended", "need_clarify"})
	assertJSONKeys(t, "TeamRecommendation", tri.Recommended[0], []string{"team", "reason"})
}

// TestDTO契约_DispatchOutcome 调度结果字段名；空字段省略。
func TestDTO契约_DispatchOutcome(t *testing.T) {
	assertJSONKeys(t, "DispatchOutcome（空）", &DispatchOutcome{}, nil)
	assertJSONKeys(t, "DispatchOutcome（含讨论与计划）", &DispatchOutcome{
		Triage:     &Triage{},
		Plan:       &DispatchPlan{Groups: [][]string{{"a"}}},
		Discussion: NewDiscussion("q", "l"),
	}, []string{"triage", "plan", "discussion"})
}

// TestDTO契约_HR HR 审批相关结构字段名。
func TestDTO契约_HR(t *testing.T) {
	assertJSONKeys(t, "StaffingRequest",
		StaffingRequest{TaskID: "t", Reason: "r", RoleCardID: "rc", Permanent: true},
		[]string{"task_id", "reason", "role_card_id", "permanent"})
	assertJSONKeys(t, "StaffingDecision",
		StaffingDecision{Level: ApprovalHR, Approved: true, Note: "n"},
		[]string{"level", "approved", "note"})
	assertJSONKeys(t, "HRAssistant", NewHRAssistant(nil),
		[]string{"staffed_this_task", "role_card_library", "decisions"})
}

// TestDTO契约_CircuitBreaker 熔断器字段名（timeout 为纳秒整数）。
func TestDTO契约_CircuitBreaker(t *testing.T) {
	cb := NewCircuitBreaker(CircuitConfig{Timeout: 90 * time.Minute})
	cb.RecordFailure()
	assertJSONKeys(t, "CircuitBreaker", cb, []string{
		"consecutive_failures", "infinite_loops", "qa_fail_rounds",
		"started_at", "timeout_ns", "demoted_members", "tripped",
	}) // trip_reason 未熔断时省略

	var probe struct {
		TimeoutNS int64 `json:"timeout_ns"`
	}
	raw, _ := json.Marshal(cb)
	if err := json.Unmarshal(raw, &probe); err != nil {
		t.Fatalf("解析 timeout_ns 失败: %v", err)
	}
	if probe.TimeoutNS != int64(90*time.Minute) {
		t.Errorf("timeout_ns=%d, 期望 %d（纳秒）", probe.TimeoutNS, int64(90*time.Minute))
	}
}

// TestDTO契约_Profile 信任档案字段名（7 维评分 + 统计计数）。
func TestDTO契约_Profile(t *testing.T) {
	assertJSONKeys(t, "Profile", NewProfile("m1"), []string{
		"member_id", "success_rate", "reasoning_consistency", "tool_accuracy",
		"compliance", "settlement_rate", "maintenance_decay",
		"completed", "assigned", "tool_calls_total", "tool_calls_ok",
		"violations", "settled_normal", "settled_crash", "settled_waste",
		"conversations_since_assign", "updated_at",
	})
}

// TestDTO契约_Team_Member_RoleCard 团队/成员/角色卡字段名（原本已符合，防回归）。
func TestDTO契约_Team_Member_RoleCard(t *testing.T) {
	assertJSONKeys(t, "Team", Team{ID: "e", Name: "n", Description: "d"},
		[]string{"id", "name", "description", "keywords", "lead_role", "member_roles"})
	assertJSONKeys(t, "RoleCard", NewRoleCard("id", "名", "核心", "约束", nil, "输出"),
		[]string{"id", "name", "core", "constraints", "tools", "output"})
	assertJSONKeys(t, "Member", NewMember("m1", RoleCard{}, Level3),
		[]string{"id", "role", "level", "profile", "permanent"})
}

// TestDTO契约_全域无大驼峰字段 兜底：遍历本域主要 DTO，
// 断言不存在以大写字母开头的 JSON 字段名（lower_snake_case 红线）。
func TestDTO契约_全域无大驼峰字段(t *testing.T) {
	samples := map[string]any{
		"Discussion":      NewDiscussion("q", "l"),
		"Triage":          &Triage{Recommended: []TeamRecommendation{{Team: &Team{}, Reason: "r"}}},
		"DispatchOutcome": &DispatchOutcome{Triage: &Triage{}, Plan: &DispatchPlan{}, Discussion: NewDiscussion("q", "l")},
		"HRAssistant":     NewHRAssistant(nil),
		"CircuitBreaker":  NewCircuitBreaker(CircuitConfig{}),
		"Profile":         NewProfile("m"),
		"TaskDependency":  TaskDependency{ID: "t", DependsOn: []string{"a"}},
		"StaffingRequest": StaffingRequest{TaskID: "t", Reason: "r"},
		"Team":            Team{ID: "e"},
		"Member":          NewMember("m", RoleCard{}, Level3),
	}
	for name, v := range samples {
		for _, key := range jsonKeys(t, v) {
			if key == "" {
				continue
			}
			if c := key[0]; c >= 'A' && c <= 'Z' {
				t.Errorf("%s 存在大驼峰 JSON 字段 %q，违反 lower_snake_case 契约", name, key)
			}
		}
	}
}

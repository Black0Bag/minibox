package teamwork

import (
	"context"
	"testing"
)

func TestNewDiscussion(t *testing.T) {
	d := NewDiscussion("帮我写一个登录接口", "lead_1")
	if d.LeadID != "lead_1" {
		t.Fatalf("组长 ID 错误: %s", d.LeadID)
	}
	if d.MaxRounds != 5 {
		t.Fatalf("轮次上限应为 5，实际 %d", d.MaxRounds)
	}
	if d.Round != 0 {
		t.Fatalf("初始轮次应为 0，实际 %d", d.Round)
	}
}

func TestDiscussionSubmitAndCritique(t *testing.T) {
	d := NewDiscussion("帮我写一个登录接口", "lead_1")
	p, err := d.SubmitProposal("arch_1", "使用 JWT 方案")
	if err != nil {
		t.Fatalf("提交提案失败: %v", err)
	}
	if p.Round != 1 {
		t.Fatalf("第一轮提案轮次应为 1，实际 %d", p.Round)
	}
	if err := d.AddCritique(1, 0, "coder_1", "JWT 无状态难吊销"); err != nil {
		t.Fatalf("添加批评失败: %v", err)
	}
	if got := d.Proposals[1][0].Critiques; len(got) != 1 || got[0].Content != "JWT 无状态难吊销" {
		t.Fatalf("批评记录错误: %+v", got)
	}
}

func TestDiscussionMaxRounds(t *testing.T) {
	d := NewDiscussion("任务", "lead")
	// 从第 0 轮推进到第 5 轮。
	for i := 0; i < 5; i++ {
		if !d.NextRound() {
			t.Fatalf("第 %d 轮推进失败", i+1)
		}
	}
	// 第 5 轮已到上限，不能再推进。
	if d.NextRound() {
		t.Fatal("第 5 轮后不应再推进")
	}
}

func TestDiscussionConcludeAndDrift(t *testing.T) {
	d := NewDiscussion("帮我写一个登录接口", "lead")
	if _, err := d.SubmitProposal("a", "用 JWT"); err != nil {
		t.Fatalf("提交提案失败: %v", err)
	}
	if err := d.Conclude("采用 JWT 方案实现登录接口"); err != nil {
		t.Fatalf("裁决失败: %v", err)
	}
	if !d.Done {
		t.Fatal("裁决后应标记完成")
	}
	// 问题锁：裁决回应了「登录」「接口」等关键词，不应偏移。
	if drift := d.DriftCheck(); drift != "" {
		t.Fatalf("不应偏移: %s", drift)
	}
}

func TestDiscussionDriftDetection(t *testing.T) {
	d := NewDiscussion("帮我写一个登录接口", "lead")
	_ = d.Conclude("我们改做会员积分系统") // 完全跑题
	if drift := d.DriftCheck(); drift == "" {
		t.Fatal("跑题裁决应被检测到偏移")
	}
}

func TestPlanDispatch(t *testing.T) {
	tasks := []TaskDependency{
		{ID: "c"},
		{ID: "a"},
		{ID: "b", DependsOn: []string{"a"}},
		{ID: "d", DependsOn: []string{"b", "c"}},
	}
	plan, err := PlanDispatch(tasks)
	if err != nil {
		t.Fatalf("调度失败: %v", err)
	}
	// 期望分层：a,c 同层并行 → b → d。
	if len(plan.Groups) != 3 {
		t.Fatalf("应有 3 层，实际 %d: %+v", len(plan.Groups), plan.Groups)
	}
	// 第一层含 a,c。
	layer0 := plan.Groups[0]
	if len(layer0) != 2 || layer0[0] != "a" || layer0[1] != "c" {
		t.Fatalf("第一层错误: %+v", layer0)
	}
	if plan.Groups[1][0] != "b" {
		t.Fatalf("第二层应为 b: %+v", plan.Groups[1])
	}
	if plan.Groups[2][0] != "d" {
		t.Fatalf("第三层应为 d: %+v", plan.Groups[2])
	}
}

func TestPlanDispatchCycle(t *testing.T) {
	tasks := []TaskDependency{
		{ID: "a", DependsOn: []string{"b"}},
		{ID: "b", DependsOn: []string{"a"}},
	}
	if _, err := PlanDispatch(tasks); err == nil {
		t.Fatal("循环依赖应报错")
	}
}

func TestPlanDispatchMissingDep(t *testing.T) {
	tasks := []TaskDependency{
		{ID: "a", DependsOn: []string{"nonexistent"}},
	}
	if _, err := PlanDispatch(tasks); err == nil {
		t.Fatal("依赖不存在应报错")
	}
}

func TestAlarmFor(t *testing.T) {
	cases := []struct {
		count int
		want  StaffingAlarm
	}{
		{1, AlarmNone},
		{2, AlarmYellow},
		{3, AlarmRed},
		{5, AlarmRed},
	}
	for _, c := range cases {
		if got := AlarmFor(c.count); got != c.want {
			t.Fatalf("AlarmFor(%d)=%v，期望 %v", c.count, got, c.want)
		}
	}
}

func TestHRReview(t *testing.T) {
	rc := NewRoleCard("rc_1", "测试角色", "核心", "约束", []string{"read_knowledge"}, "输出")
	hr := NewHRAssistant([]RoleCard{rc})

	// 第 1 次增派：1 人，HR 自批，无用户审批。
	decisions, alarm, err := hr.Review(StaffingRequest{TaskID: "t1", Reason: "缺人", RoleCardID: "rc_1"})
	if err != nil {
		t.Fatalf("审批失败: %v", err)
	}
	if alarm != AlarmNone {
		t.Fatalf("第 1 次应无预警，实际 %v", alarm)
	}
	if len(decisions) != 2 { // 组长 + HR
		t.Fatalf("第 1 次应 2 层决策，实际 %d", len(decisions))
	}

	// 第 2 次：2 人，黄色预警。
	if _, alarm, _ = hr.Review(StaffingRequest{TaskID: "t1", Reason: "仍缺人"}); alarm != AlarmYellow {
		t.Fatalf("第 2 次应黄色预警，实际 %v", alarm)
	}

	// 第 3 次：3 人，红色预警 + 用户审批层。
	decisions, alarm, _ = hr.Review(StaffingRequest{TaskID: "t1", Reason: "再缺人"})
	if alarm != AlarmRed {
		t.Fatalf("第 3 次应红色预警，实际 %v", alarm)
	}
	if decisions[len(decisions)-1].Level != ApprovalUser {
		t.Fatalf("红色预警应含用户审批层")
	}
}

func TestCircuitBreaker(t *testing.T) {
	cb := NewCircuitBreaker(CircuitConfig{})
	if cb.IsTripped() {
		t.Fatal("初始不应熔断")
	}
	// 连续失败 3 次触发。
	cb.RecordFailure()
	cb.RecordFailure()
	if cb.IsTripped() {
		t.Fatal("2 次失败不应熔断")
	}
	cb.RecordFailure()
	if !cb.IsTripped() {
		t.Fatal("3 次失败应熔断")
	}
	if cb.Reason() == "" {
		t.Fatal("熔断应有原因")
	}
	// 重置后恢复。
	cb.Reset()
	if cb.IsTripped() {
		t.Fatal("重置后不应熔断")
	}
}

func TestCircuitBreakerQA(t *testing.T) {
	cb := NewCircuitBreaker(CircuitConfig{})
	cb.RecordQAFail()
	cb.RecordQAFail()
	if cb.IsTripped() {
		t.Fatal("2 轮质检未过不应熔断")
	}
	cb.RecordQAFail()
	if !cb.IsTripped() {
		t.Fatal("连续 3 轮质检未过应熔断")
	}
}

func TestCircuitBreakerDemotion(t *testing.T) {
	cb := NewCircuitBreaker(CircuitConfig{})
	cb.RecordDemotion()
	if cb.IsTripped() {
		t.Fatal("1 名成员降级不应熔断")
	}
	cb.RecordDemotion()
	if !cb.IsTripped() {
		t.Fatal("2 名成员降级应熔断")
	}
}

func TestSchedulerTriage(t *testing.T) {
	s := NewScheduler(nil, nil)
	// 明确需求：编程。
	tr, err := s.TriageTask(context.Background(), "帮我写一个登录接口的代码")
	if err != nil {
		t.Fatalf("分诊失败: %v", err)
	}
	if tr.NeedClarify {
		t.Fatal("明确需求不应要求澄清")
	}
	if len(tr.Recommended) != 1 {
		t.Fatalf("明确需求应推荐 1 团队，实际 %d", len(tr.Recommended))
	}
	if tr.Recommended[0].Team.ID != "engineering" {
		t.Fatalf("编程需求应推荐编程团队，实际 %s", tr.Recommended[0].Team.ID)
	}
}

func TestSchedulerTriageEmpty(t *testing.T) {
	s := NewScheduler(nil, nil)
	if _, err := s.TriageTask(context.Background(), ""); err == nil {
		t.Fatal("空需求应报错")
	}
}

func TestSchedulerStartProject(t *testing.T) {
	s := NewScheduler(nil, nil)
	out, err := s.StartProject(context.Background(), "开发一个功能", "lead_1",
		[]TaskDependency{{ID: "a"}, {ID: "b", DependsOn: []string{"a"}}})
	if err != nil {
		t.Fatalf("启动项目失败: %v", err)
	}
	if out.Discussion == nil {
		t.Fatal("应创建讨论")
	}
	if out.Discussion.LeadID != "lead_1" {
		t.Fatalf("组长应为 lead_1")
	}
	if out.Plan == nil || len(out.Plan.Groups) != 2 {
		t.Fatalf("应有 2 层调度计划: %+v", out.Plan)
	}
}

func TestRoleCardValidate(t *testing.T) {
	rc := NewRoleCard("id", "name", "core", "constraint", []string{"tool"}, "output")
	if err := rc.Validate(); err != nil {
		t.Fatalf("合法角色卡不应报错: %v", err)
	}
	bad := NewRoleCard("", "", "", "", nil, "")
	if err := bad.Validate(); err == nil {
		t.Fatal("缺字段角色卡应报错")
	}
}

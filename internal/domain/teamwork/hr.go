package teamwork

import (
	"fmt"
	"sync"
)

// ApprovalLevel 审批层级（三层审批）。
type ApprovalLevel int

const (
	// ApprovalLead 组长审批：评估是否真缺人。
	ApprovalLead ApprovalLevel = iota + 1
	// ApprovalHR HR 审批：专业匹配、临时/永久、模板库有无。
	ApprovalHR
	// ApprovalUser 用户审批：专业性太强或超标才上报。
	ApprovalUser
)

// String 审批层级名。
func (l ApprovalLevel) String() string {
	switch l {
	case ApprovalLead:
		return "组长"
	case ApprovalHR:
		return "HR"
	case ApprovalUser:
		return "用户"
	default:
		return "未知"
	}
}

// StaffingRequest 增派申请。
type StaffingRequest struct {
	// TaskID 关联任务。
	TaskID string
	// Reason 增派理由（组长填写：为什么缺人）。
	Reason string
	// RoleCardID 需要的角色卡 ID（模板库命中则非空）。
	RoleCardID string
	// Permanent 是否申请永久入职（否则临时组队）。
	Permanent bool
}

// StaffingDecision 审批决策。
type StaffingDecision struct {
	Level ApprovalLevel `json:"level"`
	// Approved 是否批准。
	Approved bool `json:"approved"`
	// Note 审批意见。
	Note string `json:"note,omitempty"`
}

// StaffingAlarm 增派预警级别。
type StaffingAlarm int

const (
	// AlarmNone 无预警：1 人增派 HR 自批。
	AlarmNone StaffingAlarm = iota
	// AlarmYellow 黄色预警：2 人增派，系统预警。
	AlarmYellow
	// AlarmRed 红色预警：3 人及以上，必须用户确认。
	AlarmRed
)

// String 预警级别名。
func (a StaffingAlarm) String() string {
	switch a {
	case AlarmNone:
		return "无预警"
	case AlarmYellow:
		return "黄色预警"
	case AlarmRed:
		return "红色预警"
	default:
		return "未知"
	}
}

// AlarmFor 根据单次任务增派人数返回预警级别。
// 设计：1 人 HR 自批、2 人系统预警黄色、3 人及以上必须用户确认。
func AlarmFor(count int) StaffingAlarm {
	switch {
	case count >= 3:
		return AlarmRed
	case count == 2:
		return AlarmYellow
	default:
		return AlarmNone
	}
}

// HRAssistant HR 审批助手（三层审批链路）。
// 设计：成员→组长→HR→用户（专业性太强或超标才上报）。
type HRAssistant struct {
	mu sync.Mutex

	// StaffedThisTask 当前任务已增派人数（预警计数）。
	StaffedThisTask map[string]int

	// RoleCardLibrary 模板库（角色卡 ID 索引）。
	RoleCardLibrary map[string]RoleCard

	// Decisions 审批记录。
	Decisions []StaffingDecision
}

// NewHRAssistant 创建 HR 助手。
func NewHRAssistant(library []RoleCard) *HRAssistant {
	lib := make(map[string]RoleCard, len(library))
	for _, rc := range library {
		lib[rc.ID] = rc
	}
	return &HRAssistant{
		StaffedThisTask: make(map[string]int),
		RoleCardLibrary: lib,
	}
}

// Review 走三层审批链路，返回最终决策 + 预警级别。
// 流程：组长评估 → HR 审核 → 需用户时标记 ApprovalUser。
func (h *HRAssistant) Review(req StaffingRequest) ([]StaffingDecision, StaffingAlarm, error) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if req.TaskID == "" {
		return nil, AlarmNone, fmt.Errorf("增派申请缺任务 ID")
	}
	if req.Reason == "" {
		return nil, AlarmNone, fmt.Errorf("增派申请缺理由")
	}

	var decisions []StaffingDecision

	// 第 1 层：组长审批（评估是否真缺人）。
	lead := StaffingDecision{Level: ApprovalLead, Approved: true, Note: "组长确认缺人"}
	decisions = append(decisions, lead)

	// 第 2 层：HR 审批（专业匹配 + 模板库命中）。
	hr := StaffingDecision{Level: ApprovalHR, Approved: true}
	if req.RoleCardID != "" {
		if _, ok := h.RoleCardLibrary[req.RoleCardID]; ok {
			hr.Note = "模板库命中: " + req.RoleCardID
		} else {
			hr.Note = "模板库未命中，现场生成"
		}
	} else {
		hr.Note = "现场生成角色卡"
	}
	decisions = append(decisions, hr)

	// 增派预警计数。
	h.StaffedThisTask[req.TaskID]++
	count := h.StaffedThisTask[req.TaskID]
	alarm := AlarmFor(count)

	// 第 3 层：用户审批（红色预警或专业性太强才上报）。
	if alarm == AlarmRed {
		user := StaffingDecision{Level: ApprovalUser, Approved: false, Note: fmt.Sprintf("第 %d 次增派，需用户确认", count)}
		decisions = append(decisions, user)
	}

	h.Decisions = append(h.Decisions, decisions...)
	return decisions, alarm, nil
}

// ResolveUser 用户对红色预警的最终裁决。
func (h *HRAssistant) ResolveUser(approved bool, note string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.Decisions = append(h.Decisions, StaffingDecision{Level: ApprovalUser, Approved: approved, Note: note})
}

// StaffCount 当前任务已增派人数。
func (h *HRAssistant) StaffCount(taskID string) int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.StaffedThisTask[taskID]
}

package permission

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/Black0Bag/minibox/internal/domain/tools"
)

func mkReq(name string, meta tools.Metadata, mode Mode, args string) Request {
	return Request{
		ToolName: name,
		Metadata: meta,
		Mode:     mode,
		Args:     json.RawMessage(args),
	}
}

var readMeta = tools.Metadata{ReadOnly: true, RiskTier: "low"}
var writeMeta = tools.Metadata{Destructive: true, RiskTier: "high"}
var writeMidMeta = tools.Metadata{Destructive: true, RiskTier: "medium"}

// TestForbiddenPolicy 禁止表。
func TestForbiddenPolicy(t *testing.T) {
	p := NewForbiddenPolicy()
	req := mkReq("set_permissions", readMeta, ModeYolo, "")
	if d, _, _ := p.Check(context.Background(), req); d != DecisionDeny {
		t.Fatalf("set_permissions 应 deny, got %v", d)
	}
	req = mkReq("read_file", readMeta, ModeYolo, "")
	if d, _, _ := p.Check(context.Background(), req); d != DecisionAllow {
		t.Fatalf("read_file 应 allow, got %v", d)
	}
}

// TestModePolicy_Plan 读放写禁。
func TestModePolicy_Plan(t *testing.T) {
	p := NewModePolicy()
	if d, _, _ := p.Check(context.Background(), mkReq("read", readMeta, ModePlan, "")); d != DecisionAllow {
		t.Fatalf("plan 读应 allow, got %v", d)
	}
	if d, _, _ := p.Check(context.Background(), mkReq("write", writeMeta, ModePlan, "")); d != DecisionDeny {
		t.Fatalf("plan 写应 deny, got %v", d)
	}
}

// TestModePolicy_Ask 非只读问。
func TestModePolicy_Ask(t *testing.T) {
	p := NewModePolicy()
	if d, _, _ := p.Check(context.Background(), mkReq("read", readMeta, ModeAsk, "")); d != DecisionAllow {
		t.Fatalf("ask 读应 allow, got %v", d)
	}
	if d, _, _ := p.Check(context.Background(), mkReq("write", writeMeta, ModeAsk, "")); d != DecisionAsk {
		t.Fatalf("ask 写应 ask, got %v", d)
	}
}

// TestModePolicy_AcceptEdits 高危问。
func TestModePolicy_AcceptEdits(t *testing.T) {
	p := NewModePolicy()
	if d, _, _ := p.Check(context.Background(), mkReq("write", writeMeta, ModeAcceptEdits, "")); d != DecisionAsk {
		t.Fatalf("accept_edits 高危应 ask, got %v", d)
	}
	if d, _, _ := p.Check(context.Background(), mkReq("write", writeMidMeta, ModeAcceptEdits, "")); d != DecisionAllow {
		t.Fatalf("accept_edits 中危写应 allow, got %v", d)
	}
}

// TestMetadataPolicy 破坏性高风险 ask。
func TestMetadataPolicy(t *testing.T) {
	p := NewMetadataPolicy()
	if d, _, _ := p.Check(context.Background(), mkReq("read", readMeta, ModeYolo, "")); d != DecisionAllow {
		t.Fatalf("只读应 allow, got %v", d)
	}
	if d, _, _ := p.Check(context.Background(), mkReq("write", writeMeta, ModeYolo, "")); d != DecisionAsk {
		t.Fatalf("破坏性高风险应 ask, got %v", d)
	}
}

// TestApprovalRequiredPolicy 声明需批准。
func TestApprovalRequiredPolicy(t *testing.T) {
	p := NewApprovalRequiredPolicy()
	meta := tools.Metadata{RequiresApproval: true}
	if d, _, _ := p.Check(context.Background(), mkReq("x", meta, ModeYolo, "")); d != DecisionAsk {
		t.Fatalf("声明需批准应 ask, got %v", d)
	}
	if d, _, _ := p.Check(context.Background(), mkReq("x", readMeta, ModeYolo, "")); d != DecisionAllow {
		t.Fatalf("未声明应 allow, got %v", d)
	}
}

// TestArgsValidationPolicy 参数校验。
func TestArgsValidationPolicy(t *testing.T) {
	p := NewArgsValidationPolicy()
	if d, _, _ := p.Check(context.Background(), mkReq("x", readMeta, ModeYolo, "")); d != DecisionAllow {
		t.Fatalf("空参应 allow, got %v", d)
	}
	if d, _, _ := p.Check(context.Background(), mkReq("x", readMeta, ModeYolo, `{"a":1}`)); d != DecisionAllow {
		t.Fatalf("对象参应 allow, got %v", d)
	}
	if d, _, _ := p.Check(context.Background(), mkReq("x", readMeta, ModeYolo, `"not-object"`)); d != DecisionDeny {
		t.Fatalf("非对象参应 deny, got %v", d)
	}
}

// TestStringPolicy 白名单。
func TestStringPolicy(t *testing.T) {
	p := NewStringPolicy("read_file", "search_files")
	if d, _, _ := p.Check(context.Background(), mkReq("read_file", readMeta, ModeYolo, "")); d != DecisionAllow {
		t.Fatalf("白名单内应 allow, got %v", d)
	}
	if d, _, _ := p.Check(context.Background(), mkReq("shell", writeMeta, ModeYolo, "")); d != DecisionDeny {
		t.Fatalf("白名单外应 deny, got %v", d)
	}
}

// TestPolicyChain_Order 门控链顺序：禁止表最先。
func TestPolicyChain_Order(t *testing.T) {
	chain := NewChain(
		NewForbiddenPolicy(), // 禁止表
		NewModePolicy(),      // 模式
		NewMetadataPolicy(),  // 风险
	)

	// 禁止表 + yolo → 仍被禁止表拦
	if d, _, _ := chain.Check(context.Background(), mkReq("set_permissions", writeMeta, ModeYolo, "")); d != DecisionDeny {
		t.Fatalf("禁止表应最先 deny, got %v", d)
	}

	// plan + 写 → 模式拦
	if d, _, _ := chain.Check(context.Background(), mkReq("write", writeMeta, ModePlan, "")); d != DecisionDeny {
		t.Fatalf("plan 写应 deny, got %v", d)
	}

	// yolo + 破坏性高风险 → 元数据拦（ask）
	if d, _, _ := chain.Check(context.Background(), mkReq("write", writeMeta, ModeYolo, "")); d != DecisionAsk {
		t.Fatalf("破坏性应 ask, got %v", d)
	}

	// yolo + 只读 → 全放
	if d, _, _ := chain.Check(context.Background(), mkReq("read", readMeta, ModeYolo, "")); d != DecisionAllow {
		t.Fatalf("只读 yolo 应 allow, got %v", d)
	}
}

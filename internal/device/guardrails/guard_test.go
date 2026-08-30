package guardrails

import (
	"context"
	"errors"
	"testing"
)

func TestNew_默认规则表覆盖全部18工具(t *testing.T) {
	g := New()
	// 18 个工具逐一必须有一条规则，否则安全读操作会被"未注册的方法"拒绝
	all := []string{
		"设备_截屏", "设备_界面层级", "设备_通知列表", "设备_剪贴板", "设备_信息", "设备_前台应用",
		"设备_点击", "设备_滑动", "设备_长按", "设备_输入文字", "设备_按键", "设备_打开应用", "设备_滚动查找",
		"设备_朗读", "设备_提示",
		"设备_通知", "设备_听写", "设备_监听",
	}
	for _, m := range all {
		decision, _, _ := g.Check(context.Background(), Action{DeviceID: "d1", Method: m})
		if decision == Deny {
			t.Errorf("工具 %s 被护栏拒绝，规则表缺失", m)
		}
	}
}

func TestCheck_未知方法保守拒绝(t *testing.T) {
	g := New()
	decision, reason, err := g.Check(context.Background(), Action{DeviceID: "d1", Method: "设备_不存在"})
	if err != nil {
		t.Fatalf("Check 不应返回 error: %v", err)
	}
	if decision != Deny {
		t.Fatalf("未知方法应 Deny，得到 %s", decision)
	}
	if reason == "" {
		t.Fatal("Deny 应附带原因")
	}
}

func TestCheck_安全读操作直接放行(t *testing.T) {
	g := New()
	decision, reason, err := g.Check(context.Background(), Action{
		DeviceID: "d1",
		Method:   "设备_截屏",
		Params:   map[string]any{"quality": "low"},
	})
	if err != nil {
		t.Fatalf("Check 不应返回 error: %v", err)
	}
	if decision != Allow {
		t.Fatalf("截屏应 Allow，得到 %s (%s)", decision, reason)
	}
}

func TestCheck_敏感操作未配置HITL返回Ask(t *testing.T) {
	g := New()
	decision, reason, err := g.Check(context.Background(), Action{DeviceID: "d1", Method: "设备_输入文字"})
	if err != nil {
		t.Fatalf("Check 不应返回 error: %v", err)
	}
	if decision != Ask {
		t.Fatalf("敏感操作无回调应 Ask，得到 %s", decision)
	}
	if reason == "" {
		t.Fatal("Ask 应附带原因")
	}
}

func TestCheck_HITL放行与拒绝(t *testing.T) {
	g := New()
	g.SetHITL(func(_ context.Context, _ Action) (bool, error) {
		return true, nil
	})
	decision, _, err := g.Check(context.Background(), Action{DeviceID: "d1", Method: "设备_输入文字"})
	if err != nil {
		t.Fatalf("Check 不应返回 error: %v", err)
	}
	if decision != Allow {
		t.Fatalf("HITL 放行后应 Allow，得到 %s", decision)
	}

	g.SetHITL(func(_ context.Context, _ Action) (bool, error) {
		return false, nil
	})
	decision, reason, err := g.Check(context.Background(), Action{DeviceID: "d1", Method: "设备_输入文字"})
	if err != nil {
		t.Fatalf("Check 不应返回 error: %v", err)
	}
	if decision != Deny {
		t.Fatalf("HITL 拒绝后应 Deny，得到 %s", decision)
	}
	if reason != "用户拒绝该操作" {
		t.Fatalf("拒绝原因不匹配: %s", reason)
	}
}

func TestCheck_HITL错误返回Deny(t *testing.T) {
	g := New()
	g.SetHITL(func(_ context.Context, _ Action) (bool, error) {
		return false, errors.New("确认服务不可用")
	})
	decision, _, err := g.Check(context.Background(), Action{DeviceID: "d1", Method: "设备_输入文字"})
	if err != nil {
		t.Fatalf("Check 不应返回 error: %v", err)
	}
	if decision != Deny {
		t.Fatalf("HITL 错误应 Deny，得到 %s", decision)
	}
}

func TestCheck_频率限制滑动窗口(t *testing.T) {
	g := New()
	g.SetRules([]DangerRule{
		{Method: "设备_滑动", RequireAsk: false, RateLimit: 3},
	})

	for i := 0; i < 3; i++ {
		decision, _, _ := g.Check(context.Background(), Action{DeviceID: "d1", Method: "设备_滑动"})
		if decision != Allow {
			t.Fatalf("第 %d 次调用应 Allow，得到 %s", i+1, decision)
		}
	}
	decision, reason, _ := g.Check(context.Background(), Action{DeviceID: "d1", Method: "设备_滑动"})
	if decision != Deny {
		t.Fatalf("第 4 次调用应 Deny，得到 %s", decision)
	}
	if reason == "" {
		t.Fatal("频率拒绝应附带原因")
	}
}

func TestCheck_频率限制按设备隔离(t *testing.T) {
	g := New()
	g.SetRules([]DangerRule{
		{Method: "设备_滑动", RequireAsk: false, RateLimit: 1},
	})

	decision, _, _ := g.Check(context.Background(), Action{DeviceID: "d1", Method: "设备_滑动"})
	if decision != Allow {
		t.Fatalf("设备 d1 第 1 次应 Allow，得到 %s", decision)
	}
	decision, _, _ = g.Check(context.Background(), Action{DeviceID: "d2", Method: "设备_滑动"})
	if decision != Allow {
		t.Fatalf("设备 d2 独立配额应 Allow，得到 %s", decision)
	}
}

func TestSetRules_全量覆盖默认规则(t *testing.T) {
	g := New()
	g.SetRules([]DangerRule{{Method: "设备_点击", RequireAsk: false, RateLimit: 0}})

	// 默认规则里的 设备_输入文字 应被覆盖移除 → 未知方法 Deny
	decision, _, _ := g.Check(context.Background(), Action{DeviceID: "d1", Method: "设备_输入文字"})
	if decision != Deny {
		t.Fatalf("覆盖后默认方法应 Deny，得到 %s", decision)
	}
	decision, _, _ = g.Check(context.Background(), Action{DeviceID: "d1", Method: "设备_点击"})
	if decision != Allow {
		t.Fatalf("新规则方法应 Allow，得到 %s", decision)
	}
}

func TestCheck_RateLimit为0不限制(t *testing.T) {
	g := New()
	g.SetRules([]DangerRule{{Method: "设备_朗读", RequireAsk: false, RateLimit: 0}})

	for i := 0; i < 100; i++ {
		decision, _, _ := g.Check(context.Background(), Action{DeviceID: "d1", Method: "设备_朗读"})
		if decision != Allow {
			t.Fatalf("不限频方法第 %d 次应 Allow，得到 %s", i+1, decision)
		}
	}
}

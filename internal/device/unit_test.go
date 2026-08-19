package device

import (
	"strings"
	"testing"
	"time"
)

func TestGenerateCode_六位数字(t *testing.T) {
	pm := NewPairingManager()
	code, err := pm.GenerateCode("dev-1")
	if err != nil {
		t.Fatalf("GenerateCode 失败: %v", err)
	}
	if len(code) != 6 {
		t.Fatalf("配对码应为 6 位，得到 %q", code)
	}
	for _, c := range code {
		if c < '0' || c > '9' {
			t.Fatalf("配对码含非数字字符: %q", code)
		}
	}
}

func TestGenerateCode_每次生成不同(t *testing.T) {
	pm := NewPairingManager()
	code1, _ := pm.GenerateCode("dev-1")
	code2, _ := pm.GenerateCode("dev-1")
	if code1 == code2 {
		t.Fatalf("两次生成配对码不应相同: %s", code1)
	}
}

func TestVerifyCode_成功配对(t *testing.T) {
	pm := NewPairingManager()
	code, err := pm.GenerateCode("dev-1")
	if err != nil {
		t.Fatalf("GenerateCode 失败: %v", err)
	}
	id, ok := pm.VerifyCode(code)
	if !ok {
		t.Fatal("有效配对码应校验通过")
	}
	if id != "dev-1" {
		t.Fatalf("应返回 dev-1，得到 %s", id)
	}
}

func TestVerifyCode_一次性消费(t *testing.T) {
	pm := NewPairingManager()
	code, _ := pm.GenerateCode("dev-1")
	if _, ok := pm.VerifyCode(code); !ok {
		t.Fatal("首次校验应通过")
	}
	if _, ok := pm.VerifyCode(code); ok {
		t.Fatal("配对码一次性，二次校验应失败")
	}
}

func TestVerifyCode_无效码(t *testing.T) {
	pm := NewPairingManager()
	if _, ok := pm.VerifyCode("000000"); ok {
		t.Fatal("未生成的码应校验失败")
	}
}

func TestVerifyCode_过期码(t *testing.T) {
	pm := NewPairingManager()
	code, _ := pm.GenerateCode("dev-1")
	// 直接把过期时间拨回过去（内部结构直改，白盒测试）
	pm.mu.Lock()
	if entry, ok := pm.codes["dev-1"]; ok {
		entry.ExpiresAt = time.Now().Add(-time.Minute)
		pm.codes["dev-1"] = entry
	}
	pm.mu.Unlock()

	if _, ok := pm.VerifyCode(code); ok {
		t.Fatal("过期配对码应校验失败")
	}
}

func TestRemoveCode_失效(t *testing.T) {
	pm := NewPairingManager()
	code, _ := pm.GenerateCode("dev-1")
	pm.RemoveCode("dev-1")
	if _, ok := pm.VerifyCode(code); ok {
		t.Fatal("移除后配对码应失效")
	}
}

func TestRegistry_别名解析(t *testing.T) {
	r := NewRegistry()
	r.SetAlias("dev-abc-123", "客厅手机")

	if got := r.Resolve("客厅手机"); got != "dev-abc-123" {
		t.Fatalf("别名应解析到设备 ID，得到 %s", got)
	}
	if got := r.Resolve("dev-abc-123"); got != "dev-abc-123" {
		t.Fatalf("设备 ID 应原样返回，得到 %s", got)
	}
	if got := r.Resolve("不存在的名字"); got != "不存在的名字" {
		t.Fatalf("未知名字应原样返回，得到 %s", got)
	}
}

func TestRegistry_别名覆盖与移除(t *testing.T) {
	r := NewRegistry()
	r.SetAlias("dev-1", "卧室")
	r.SetAlias("dev-2", "卧室")
	if got := r.Resolve("卧室"); got != "dev-2" {
		t.Fatalf("重复别名应取最后设置者，得到 %s", got)
	}

	r.RemoveAlias("卧室")
	if got := r.Resolve("卧室"); got != "卧室" {
		t.Fatalf("移除后应原样返回，得到 %s", got)
	}
}

func TestAuditLogger_记录与列表(t *testing.T) {
	al := NewAuditLogger()
	cmd1 := &Command{ID: "cmd_1", DeviceID: "dev-1", Method: "设备_截屏", Status: "done"}
	cmd2 := &Command{ID: "cmd_2", DeviceID: "dev-1", Method: "设备_点击", Status: "failed"}

	al.Log(cmd1)
	al.Log(cmd2)

	logs := al.List()
	if len(logs) != 2 {
		t.Fatalf("应有 2 条日志，得到 %d", len(logs))
	}
	if logs[0].ID != cmd1.ID || logs[0].Method != cmd1.Method || logs[0].Status != cmd1.Status {
		t.Fatal("日志顺序或内容不符")
	}
	if logs[1].ID != cmd2.ID || logs[1].Method != cmd2.Method || logs[1].Status != cmd2.Status {
		t.Fatal("日志顺序或内容不符")
	}

	// 列表拷贝不共享底层
	logs[0].Status = "mutated"
	if cmd1.Status != "done" {
		t.Fatal("List 应返回深拷贝，修改副本不应影响原记录")
	}
}

func TestAuditLogger_CleanOlderThan(t *testing.T) {
	al := NewAuditLogger()
	old := &Command{ID: "old", CreatedAt: time.Now().Add(-2 * time.Hour)}
	recent := &Command{ID: "recent", CreatedAt: time.Now()}
	al.Log(old)
	al.Log(recent)

	al.CleanOlderThan(time.Hour)

	logs := al.List()
	if len(logs) != 1 {
		t.Fatalf("清理后应剩 1 条，得到 %d", len(logs))
	}
	if logs[0].ID != "recent" {
		t.Fatalf("应仅保留 recent，得到 %s", logs[0].ID)
	}
}

func TestDeviceTools_18个工具契约(t *testing.T) {
	if len(DeviceTools) != 18 {
		t.Fatalf("应恰好 18 个设备工具，得到 %d", len(DeviceTools))
	}

	seen := make(map[string]bool)
	for _, tool := range DeviceTools {
		if tool.Name == "" || tool.Description == "" || tool.Category == "" {
			t.Fatalf("工具 %+v 缺必备字段", tool)
		}
		if seen[tool.Name] {
			t.Fatalf("工具名重复: %s", tool.Name)
		}
		seen[tool.Name] = true

		switch tool.Category {
		case "眼", "手", "口", "耳":
		default:
			t.Fatalf("工具 %s 分类非法: %s", tool.Name, tool.Category)
		}
	}
}

func TestDeviceTools_眼手口耳计数(t *testing.T) {
	counts := map[string]int{"眼": 0, "手": 0, "口": 0, "耳": 0}
	for _, tool := range DeviceTools {
		counts[tool.Category]++
	}
	if counts["眼"] != 6 || counts["手"] != 7 || counts["口"] != 2 || counts["耳"] != 3 {
		t.Fatalf("分类计数不符: %+v（期望 眼6/手7/口2/耳3）", counts)
	}
}

func TestDeviceTools_参数契约(t *testing.T) {
	requiredCases := map[string][]string{
		"设备_点击":       {"x", "y"},
		"设备_滑动":       {"x1", "y1", "x2", "y2"},
		"设备_输入文字":     {"text"},
		"设备_按键":       {"key"},
		"设备_打开应用":     {"package_name"},
		"设备_滚动查找":     {"text"},
		"设备_朗读":       {"text"},
		"设备_提示":       {"message"},
		"设备_通知":       {"title", "text"},
		"设备_监听":       {"events"},
	}

	found := map[string]ToolDef{}
	for _, tool := range DeviceTools {
		found[tool.Name] = tool
	}

	for name, want := range requiredCases {
		tool, ok := found[name]
		if !ok {
			t.Fatalf("缺少工具 %s", name)
		}
		params, ok := tool.Parameters.(map[string]any)
		if !ok {
			t.Fatalf("工具 %s 参数应为 object", name)
		}
		req, ok := params["required"].([]string)
		if !ok {
			t.Fatalf("工具 %s 缺少 required 数组", name)
		}
		for _, r := range want {
			has := false
			for _, got := range req {
				if got == r {
					has = true
					break
				}
			}
			if !has {
				t.Fatalf("工具 %s 的 required 缺少 %s（实际 %v）", name, r, req)
			}
		}
	}
}

func TestDeviceTools_工具名前缀(t *testing.T) {
	for _, tool := range DeviceTools {
		if !strings.HasPrefix(tool.Name, "设备_") {
			t.Fatalf("工具名应带 设备_ 前缀: %s", tool.Name)
		}
	}
}
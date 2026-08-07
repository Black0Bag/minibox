package device

import (
	"strings"
	"testing"
	"time"
)

func TestRegisterWithPairingCode(t *testing.T) {
	h := NewHub(t.TempDir())
	code, err := h.NewPairingCode("测试设备")
	if err != nil {
		t.Fatalf("生成配对码失败: %v", err)
	}
	if len(code) < 6 {
		t.Errorf("配对码过短: %q", code)
	}

	// 正确配对码注册
	dev, err := h.Register(code, "phone-1", "安卓", []string{"无障碍", "浏览器"})
	if err != nil {
		t.Fatalf("注册失败: %v", err)
	}
	if dev.ID == "" || dev.Name == "phone-1" {
		t.Errorf("设备注册异常: %+v", dev)
	}
	if dev.Status != "在线" {
		t.Errorf("注册后应在线: %s", dev.Status)
	}
	// 能力声明
	if len(dev.Capabilities) != 2 {
		t.Errorf("能力声明异常: %v", dev.Capabilities)
	}
}

func TestRegisterWrongCode(t *testing.T) {
	h := NewHub(t.TempDir())
	if _, err := h.Register("WRONG-CODE", "phone-2", "安卓", nil); err == nil {
		t.Error("错误配对码应注册失败")
	}
}

func TestPairingCodeSingleUse(t *testing.T) {
	h := NewHub(t.TempDir())
	code, _ := h.NewPairingCode("设备A")
	if _, err := h.Register(code, "d1", "安卓", nil); err != nil {
		t.Fatalf("首次注册失败: %v", err)
	}
	// 配对码应一次性
	if _, err := h.Register(code, "d2", "安卓", nil); err == nil {
		t.Error("配对码应一次性使用")
	}
}

func TestHeartbeatAndOffline(t *testing.T) {
	h := NewHub(t.TempDir())
	code, _ := h.NewPairingCode("设备B")
	dev, _ := h.Register(code, "d3", "安卓", nil)
	if err := h.Heartbeat(dev.ID); err != nil {
		t.Fatalf("心跳失败: %v", err)
	}
	// 未过期前应在线
	ok, _ := h.CheckOnline(dev.ID)
	if !ok {
		t.Error("心跳后应在线")
	}
	// 超时后标记离线（时间短阈值）
	if err := h.MarkOffline(dev.ID); err != nil {
		t.Fatalf("离线标记失败: %v", err)
	}
	ok, _ = h.CheckOnline(dev.ID)
	if ok {
		t.Error("标记离线后不应在线")
	}
}

func TestListAndUnregister(t *testing.T) {
	h := NewHub(t.TempDir())
	code1, _ := h.NewPairingCode("设备1")
	_, _ = h.Register(code1, "d-a", "安卓", nil)
	code2, _ := h.NewPairingCode("设备2")
	_, _ = h.Register(code2, "d-b", "PC", nil)

	list, err := h.List()
	if err != nil {
		t.Fatalf("列表失败: %v", err)
	}
	if len(list) != 2 {
		t.Errorf("应有 2 设备，得到 %d", len(list))
	}
	if err := h.Unregister("d-a"); err != nil {
		t.Fatalf("注销失败: %v", err)
	}
	list, _ = h.List()
	if len(list) != 1 {
		t.Errorf("注销后应 1 设备，得到 %d", len(list))
	}
}

func TestGet(t *testing.T) {
	h := NewHub(t.TempDir())
	code, _ := h.NewPairingCode("设备C")
	dev, _ := h.Register(code, "d-c", "安卓", nil)
	got, err := h.Get(dev.ID)
	if err != nil {
		t.Fatalf("Get 失败: %v", err)
	}
	if got.ID != dev.ID {
		t.Errorf("Get 返回错误设备: %s != %s", got.ID, dev.ID)
	}
	if _, err := h.Get("nonexist"); err == nil {
		t.Error("不存在的设备应报错")
	}
}

func TestPersistence(t *testing.T) {
	dir := t.TempDir()
	h := NewHub(dir)
	code, _ := h.NewPairingCode("设备P")
	dev, _ := h.Register(code, "d-p", "安卓", nil)
	// 重新加载（模拟重启）
	h2 := NewHub(dir)
	got, err := h2.Get(dev.ID)
	if err != nil {
		t.Fatalf("重启后加载失败: %v", err)
	}
	if got.ID != dev.ID {
		t.Errorf("Get 返回错误设备: %s != %s", got.ID, dev.ID)
	}
	if got.Name != "设备P" {
		t.Errorf("Name 应来自配对名: %s", got.Name)
	}
}

func TestAudit(t *testing.T) {
	h := NewHub(t.TempDir())
	code, _ := h.NewPairingCode("设备A")
	_, _ = h.Register(code, "d-a", "安卓", nil)
	_ = h.Heartbeat("d-a")
	log, err := h.AuditLog(10)
	if err != nil {
		t.Fatalf("审计日志失败: %v", err)
	}
	if len(log) == 0 {
		t.Fatal("应有审计记录")
	}
	joined := strings.Join(log, "\n")
	if !strings.Contains(joined, "注册") && !strings.Contains(joined, "心跳") {
		t.Errorf("审计应含注册/心跳动作: %s", joined)
	}
}

func TestDefaultOfflineTTL(t *testing.T) {
	// 心跳 TTL 默认 90 秒
	h := NewHub(t.TempDir())
	code, _ := h.NewPairingCode("设备T")
	dev, _ := h.Register(code, "d-t", "安卓", nil)
	_ = h.Heartbeat(dev.ID)
	// 模拟超过 TTL：直接改时间
	dev.lastSeen = time.Now().Add(-91 * time.Second)
	ok, _ := h.CheckOnline(dev.ID)
	if ok {
		t.Error("超过 TTL 应离线")
	}
}

package upgrade

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestNewManagerDefaults(t *testing.T) {
	m := NewManager("", 0)
	if m.DownloadDir() == "" {
		t.Error("下载目录不应为空")
	}
}

func TestCheckHealthHealthy(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"状态":"ok"}`))
	}))
	defer srv.Close()

	m := NewManager(srv.URL, 0)
	ok, err := m.CheckHealth(context.Background())
	if err != nil || !ok {
		t.Fatalf("应健康: ok=%v err=%v", ok, err)
	}
	if atomic.LoadInt32(&calls) == 0 {
		t.Error("应发起健康检查请求")
	}
}

func TestCheckHealthUnhealthy(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()
	m := NewManager(srv.URL, 0)
	ok, err := m.CheckHealth(context.Background())
	if err != nil {
		t.Fatalf("健康检查不应报错: %v", err)
	}
	if ok {
		t.Error("503 应判定不健康")
	}
}

func TestCheckHealthTimeout(t *testing.T) {
	// 慢响应服务器（超过超时）
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	m := NewManager(srv.URL, 0)
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	ok, err := m.CheckHealth(ctx)
	if err == nil {
		t.Fatal("超时应报错")
	}
	if ok {
		t.Error("超时后不应判定健康")
	}
}

func TestApplyShutdownHook(t *testing.T) {
	var hookCalled int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hookCalled, 1)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("升级完成"))
	}))
	defer srv.Close()
	m := NewManager(srv.URL, 0)
	// hook 返回升级完成提示
	m.SetUpgradeHook(func() error { return nil })
	// 真实检查：这里只测 watchdog 状态记录
	m.MarkUpgraded()
	if !m.LastUpgrade().Upgraded {
		t.Error("应记录已升级")
	}
	if !strings.Contains(m.LastUpgrade().Version, "0") && !strings.Contains(m.LastUpgrade().Version, "dev") {
		t.Errorf("版本信息异常: %s", m.LastUpgrade().Version)
	}
}

func TestWatchdogCycle(t *testing.T) {
	// 健康服务器：watchdog 不应触发恢复
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()
	m := NewManager(srv.URL, 0)
	var restarts int32
	m.SetRestartHook(func() { atomic.AddInt32(&restarts, 1) })
	// 手动执行一次 watchdog 检查
	m.CheckOnce(context.Background())
	if atomic.LoadInt32(&restarts) != 0 {
		t.Errorf("健康时不应重启，得到 %d 次重启", restarts)
	}
}

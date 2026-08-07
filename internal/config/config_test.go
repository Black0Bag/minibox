package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefault(t *testing.T) {
	cfg := Default()
	if cfg.Server.Port != 8086 {
		t.Errorf("默认端口应为 8086，得到 %d", cfg.Server.Port)
	}
	if cfg.Logging.Level != "info" {
		t.Errorf("默认日志级别应为 info，得到 %s", cfg.Logging.Level)
	}
	if cfg.DataDir != "data" {
		t.Errorf("默认数据目录应为 data，得到 %s", cfg.DataDir)
	}
}

func TestLoadBytes(t *testing.T) {
	data := []byte(`
server:
  host: 127.0.0.1
  port: 9000
logging:
  level: debug
providers:
  - name: deepseek
    base_url: https://api.example.com/v1
    api_key: sk-test
    model: test-model
    enabled: true
`)
	cfg, err := LoadBytes(data)
	if err != nil {
		t.Fatalf("LoadBytes 失败: %v", err)
	}
	if cfg.Server.Port != 9000 {
		t.Errorf("端口应为 9000，得到 %d", cfg.Server.Port)
	}
	if cfg.Server.Host != "127.0.0.1" {
		t.Errorf("host 应为 127.0.0.1，得到 %s", cfg.Server.Host)
	}
	if cfg.Logging.Level != "debug" {
		t.Errorf("日志级别应为 debug，得到 %s", cfg.Logging.Level)
	}
	if len(cfg.Providers) != 1 {
		t.Fatalf("应有 1 个供应商，得到 %d", len(cfg.Providers))
	}
	if cfg.Providers[0].Name != "deepseek" {
		t.Errorf("供应商名应为 deepseek，得到 %s", cfg.Providers[0].Name)
	}
}

func TestLoadPartialDefaults(t *testing.T) {
	// 部分配置应补默认值
	data := []byte("server:\n  port: 9090\n")
	cfg, err := LoadBytes(data)
	if err != nil {
		t.Fatalf("LoadBytes 失败: %v", err)
	}
	if cfg.Server.Port != 9090 {
		t.Errorf("端口应为 9090，得到 %d", cfg.Server.Port)
	}
	if cfg.Server.Host != "0.0.0.0" {
		t.Errorf("host 应补默认 0.0.0.0，得到 %s", cfg.Server.Host)
	}
	if cfg.Logging.Level != "info" {
		t.Errorf("日志级别应补默认 info，得到 %s", cfg.Logging.Level)
	}
}

func TestLoadFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	data := []byte("server:\n  port: 7000\nlogging:\n  level: debug\n")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("写入测试配置失败: %v", err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load 失败: %v", err)
	}
	if cfg.Server.Port != 7000 {
		t.Errorf("端口应为 7000，得到 %d", cfg.Server.Port)
	}
}

func TestLoadMissingFileReturnsDefault(t *testing.T) {
	cfg, err := Load(filepath.Join(t.TempDir(), "不存在.yaml"))
	if err != nil {
		t.Fatalf("文件不存在应返回默认配置而非错误: %v", err)
	}
	if cfg.Server.Port != 8086 {
		t.Errorf("默认端口应为 8086，得到 %d", cfg.Server.Port)
	}
}

func TestSaveAndPerm(t *testing.T) {
	path := filepath.Join(t.TempDir(), "out.yaml")
	cfg := Default()
	cfg.Server.Port = 1234
	if err := cfg.Save(path); err != nil {
		t.Fatalf("Save 失败: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat 失败: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("配置文件权限应为 0600，得到 %v", info.Mode().Perm())
	}
	// 回读验证
	got, err := Load(path)
	if err != nil {
		t.Fatalf("回读失败: %v", err)
	}
	if got.Server.Port != 1234 {
		t.Errorf("回读端口应为 1234，得到 %d", got.Server.Port)
	}
}

func TestFindProvider(t *testing.T) {
	cfg := Default()
	cfg.Providers = []Provider{
		{Name: "a", BaseURL: "u1"},
		{Name: "b", BaseURL: "u2"},
	}
	p, ok := cfg.FindProvider("b")
	if !ok || p.BaseURL != "u2" {
		t.Errorf("FindProvider(b) 失败: ok=%v p=%+v", ok, p)
	}
	if _, ok := cfg.FindProvider("c"); ok {
		t.Error("不存在的供应商应返回 false")
	}
}

func TestAddr(t *testing.T) {
	cfg := Default()
	cfg.Server.Host = "127.0.0.1"
	cfg.Server.Port = 9090
	if got := cfg.Addr(); got != "127.0.0.1:9090" {
		t.Errorf("Addr 应为 127.0.0.1:9090，得到 %s", got)
	}
}

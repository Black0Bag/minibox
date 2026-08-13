package config

import (
	"testing"
)

func TestLoadDefault(t *testing.T) {
	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load 错误: %v", err)
	}
	if cfg.Server.Port != 8086 {
		t.Errorf("端口错误: %d, 期望 8086", cfg.Server.Port)
	}
	if cfg.Server.Listen != "127.0.0.1" {
		t.Errorf("监听地址错误: %s", cfg.Server.Listen)
	}
	if cfg.Logging.Format != "text" {
		t.Errorf("日志格式错误: %s", cfg.Logging.Format)
	}
	if cfg.Logging.Output != "stdout" {
		t.Errorf("日志输出错误: %s", cfg.Logging.Output)
	}
	if cfg.Logging.Level != "info" {
		t.Errorf("日志级别错误: %s", cfg.Logging.Level)
	}
	t.Logf("Server=%+v", cfg.Server)
	t.Logf("Logging=%+v", cfg.Logging)
}

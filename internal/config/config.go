package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"

	"github.com/Black0Bag/minibox/internal/logging"
)

// ServerConfig 服务配置（N-14：端口 8086 可配）
type ServerConfig struct {
	Host string `yaml:"host"` // 监听地址，默认 0.0.0.0
	Port int    `yaml:"port"` // 监听端口，默认 8086
}

// Provider LLM 供应商配置（多供应商接入）
type Provider struct {
	Name    string `yaml:"name" json:"name"`         // 供应商标识，如 deepseek
	BaseURL string `yaml:"base_url" json:"base_url"` // API 地址
	APIKey  string `yaml:"api_key" json:"api_key"`   // 密钥（N-05：明文 YAML，文件权限 0600）
	Model   string `yaml:"model" json:"model"`       // 默认模型
	Enabled bool   `yaml:"enabled" json:"enabled"`   // 是否启用
}

// EmbeddingConfig 向量 embedding 配置（sqlite-vec 向量检索，N-11）
type EmbeddingConfig struct {
	Enabled bool   `yaml:"enabled"`  // 是否启用向量检索
	BaseURL string `yaml:"base_url"` // OpenAI 兼容 embedding API 地址
	APIKey  string `yaml:"api_key"`  // 密钥
	Model   string `yaml:"model"`    // embedding 模型
	Dim     int    `yaml:"dim"`      // 向量维度（换模型需重建向量表）
}

// Config 顶层配置
type Config struct {
	Server    ServerConfig    `yaml:"server"`
	Logging   logging.Config  `yaml:"logging"`
	Providers []Provider      `yaml:"providers"`
	Embedding EmbeddingConfig `yaml:"embedding"`
	DataDir   string          `yaml:"data_dir"` // 数据目录（数据库/日志）
}

// Default 返回默认配置
func Default() Config {
	return Config{
		Server:  ServerConfig{Host: "0.0.0.0", Port: 8086},
		Logging: logging.Default(),
		Providers: []Provider{},
		DataDir: "data",
	}
}

// Load 从 YAML 文件加载配置，未提供的字段使用默认值
func Load(path string) (*Config, error) {
	cfg := Default()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &cfg, nil // 文件不存在则用默认值（首次启动向导将创建）
		}
		return nil, fmt.Errorf("读取配置文件 %s: %w", path, err)
	}
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("解析配置文件 %s: %w", path, err)
	}
	cfg.applyDefaults()
	return &cfg, nil
}

// LoadBytes 从字节加载（测试用）
func LoadBytes(data []byte) (*Config, error) {
	cfg := Default()
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("解析配置: %w", err)
	}
	cfg.applyDefaults()
	return &cfg, nil
}

// applyDefaults 填充零值字段为默认值
func (c *Config) applyDefaults() {
	if c.Server.Port == 0 {
		c.Server.Port = 8086
	}
	if c.Server.Host == "" {
		c.Server.Host = "0.0.0.0"
	}
	if c.Logging.Level == "" {
		c.Logging.Level = "info"
	}
	if c.Logging.Format == "" {
		c.Logging.Format = "text"
	}
	if c.DataDir == "" {
		c.DataDir = "data"
	}
}

// Save 将配置写入 YAML 文件（权限 0600，符合 N-05）
func (c *Config) Save(path string) error {
	data, err := yaml.Marshal(c)
	if err != nil {
		return fmt.Errorf("序列化配置: %w", err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("写入配置文件 %s: %w", path, err)
	}
	return nil
}

// Addr 返回监听地址
func (c *Config) Addr() string {
	return fmt.Sprintf("%s:%d", c.Server.Host, c.Server.Port)
}

// Provider 按名称查找供应商
func (c *Config) FindProvider(name string) (*Provider, bool) {
	for i := range c.Providers {
		if c.Providers[i].Name == name {
			return &c.Providers[i], true
		}
	}
	return nil, false
}

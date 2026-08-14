// Package config 提供配置加载（koanf + YAML + 默认值）。
// 设计：koanf 多 provider 合并，Go struct 作默认值来源（2026 选型不用 Viper）。
package config

import (
	"fmt"
	"os"
	"time"

	"github.com/knadh/koanf/parsers/yaml"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/v2"
)

// Config 是 minibox 后端根配置结构。
// 所有配置通过 koanf 从默认值 + 配置文件合并而来。
type Config struct {
	Server   ServerConfig   `koanf:"server"`
	Database DatabaseConfig `koanf:"database"`
	Logging  LoggingConfig  `koanf:"logging"`
	LLM      LLMConfig      `koanf:"llm"`
	Memory   MemoryConfig   `koanf:"memory"`
}

// ServerConfig 服务端配置。
type ServerConfig struct {
	// Port 监听端口，默认 8086（PRD 红线）。
	Port int `koanf:"port"`
	// Listen 监听地址，默认 127.0.0.1（安全默认，OpenClaw 教训）。
	// 设为 0.0.0.0 时自动双栈监听 IPv4 + IPv6。
	Listen string `koanf:"listen"`
	// IPv6 设为 true 时同步监听 [::]（配合 Listen=0.0.0.0 使用）。
	IPv6 bool `koanf:"ipv6"`
	// ReadTimeout HTTP 读超时。
	ReadTimeout time.Duration `koanf:"read_timeout"`
	// WriteTimeout HTTP 写超时。
	WriteTimeout time.Duration `koanf:"write_timeout"`
	// IdleTimeout 空闲连接超时。
	IdleTimeout time.Duration `koanf:"idle_timeout"`
	// ShutdownTimeout 优雅停机排水预算。
	ShutdownTimeout time.Duration `koanf:"shutdown_timeout"`
}

// DatabaseConfig 数据库配置（SQLite 单文件）。
type DatabaseConfig struct {
	// Path 数据库文件路径，相对项目运行时目录。
	Path string `koanf:"path"`
	// MaxOpenConns 最大打开连接数（SQLite 单写者，1 足够）。
	MaxOpenConns int `koanf:"max_open_conns"`
	// MaxIdleConns 最大空闲连接数。
	MaxIdleConns int `koanf:"max_idle_conns"`
	// BusyTimeout SQLite 忙等待超时。
	BusyTimeout time.Duration `koanf:"busy_timeout"`
}

// LoggingConfig 日志配置（log/slog）。
type LoggingConfig struct {
	// Level 日志级别：debug / info / warn / error。
	Level string `koanf:"level"`
	// Format 输出格式：text（本地）/ json（生产）。
	Format string `koanf:"format"`
	// Output 输出目标：stdout / stderr / 文件路径。
	Output string `koanf:"output"`
	// MaxSizeMB 日志文件单文件最大 MB（轮转）。
	MaxSizeMB int `koanf:"max_size_mb"`
	// MaxBackups 保留的日志备份数。
	MaxBackups int `koanf:"max_backups"`
	// MaxAgeDays 日志最大保留天数。
	MaxAgeDays int `koanf:"max_age_days"`
}

// LLMConfig 大模型配置（Phase 2）。
type LLMConfig struct {
	// Providers 供应商列表（每个含 endpoint/api_key/model 等）。
	Providers []ProviderConfig `koanf:"providers"`
	// DefaultProvider 默认供应商标识。
	DefaultProvider string `koanf:"default_provider"`
	// DefaultModel 默认模型。
	DefaultModel string `koanf:"default_model"`
	// Timeout LLM 调用超时。
	Timeout time.Duration `koanf:"timeout"`
	// MaxRetries 重试次数。
	MaxRetries int `koanf:"max_retries"`
}

// ProviderConfig 单个 LLM 供应商配置。
type ProviderConfig struct {
	// Name 供应商标识（如 openai-compatible / deepseek / glm）。
	Name string `koanf:"name"`
	// BaseURL API 基础地址（OpenAI 兼容，如 https://api.deepseek.com/v1）。
	BaseURL string `koanf:"base_url"`
	// APIKeys 多 key 列表（key 池轮询，B4）。
	APIKeys []string `koanf:"api_keys"`
	// Models 该供应商可用的模型列表。
	Models []ModelConfig `koanf:"models"`
	// Timeout 单供应商超时（覆盖全局）。
	Timeout time.Duration `koanf:"timeout"`
}

// ModelConfig 单个模型配置。
type ModelConfig struct {
	// ID 模型 ID（如 deepseek-chat）。
	ID string `koanf:"id"`
	// Enabled 是否启用。
	Enabled bool `koanf:"enabled"`
	// ContextLength 上下文长度（自动识别可覆盖）。
	ContextLength int `koanf:"context_length"`
	// MaxOutputTokens 单次输出上限。
	MaxOutputTokens int `koanf:"max_output_tokens"`
	// SupportsThinking 是否支持思考。
	SupportsThinking bool `koanf:"supports_thinking"`
	// RPM 每分钟请求限流。
	RPM int `koanf:"rpm"`
	// TPM 每分钟 token 限流。
	TPM int `koanf:"tpm"`
}

// MemoryConfig 知识库/记忆配置（Phase 3 使用，先占位）。
type MemoryConfig struct {
	// CompileBatchSize 编译管道 embedding 批量大小（实测 32 最优）。
	CompileBatchSize int `koanf:"compile_batch_size"`
	// MaxTokens 单条 chunk 最大 token 数。
	MaxTokens int `koanf:"max_tokens"`
}

// Default 返回默认配置（安全默认值优先）。
func Default() Config {
	return Config{
		Server: ServerConfig{
			Port:            8086,
			Listen:          "127.0.0.1",
			IPv6:            false,
			ReadTimeout:     30 * time.Second,
			WriteTimeout:    30 * time.Second,
			IdleTimeout:     120 * time.Second,
			ShutdownTimeout: 15 * time.Second,
		},
		Database: DatabaseConfig{
			Path:         "data/minibox.db",
			MaxOpenConns: 1,
			MaxIdleConns: 1,
			BusyTimeout:  5 * time.Second,
		},
		Logging: LoggingConfig{
			Level:      "info",
			Format:     "text",
			Output:     "stdout",
			MaxSizeMB:  10,
			MaxBackups: 3,
			MaxAgeDays: 7,
		},
		LLM: LLMConfig{
			DefaultProvider: "openai-compatible",
			DefaultModel:    "",
			Timeout:         120 * time.Second,
			MaxRetries:      3,
		},
		Memory: MemoryConfig{
			CompileBatchSize: 32,
			MaxTokens:        3000,
		},
	}
}

// Load 加载配置：默认值 + 配置文件覆盖。
// 配置通过 koanf 合并：先内嵌默认 Config，再用文件覆盖。
// path 为空时仅返回默认值。
func Load(path string) (Config, error) {
	cfg := Default()

	k := koanf.New(".")

	// 步骤1：加载默认值（从 Go struct）
	if err := k.Load(structProvider{cfg}, nil); err != nil {
		return cfg, fmt.Errorf("加载默认配置失败: %w", err)
	}

	// 步骤2：加载配置文件覆盖
	if path != "" {
		if _, err := os.Stat(path); err == nil {
			if err := k.Load(file.Provider(path), yaml.Parser()); err != nil {
				return cfg, fmt.Errorf("加载配置文件 %s 失败: %w", path, err)
			}
		}
	}

	// 步骤3：反序列化到 struct
	if err := k.Unmarshal("", &cfg); err != nil {
		return cfg, fmt.Errorf("解析配置失败: %w", err)
	}
	return cfg, nil
}

// WriteDefault 把默认配置写入 yaml 文件（首次启动自动生成配置用）。
func WriteDefault(path string) error {
	cfg := Default()
	k := koanf.New(".")
	if err := k.Load(structProvider{cfg}, nil); err != nil {
		return err
	}
	out, err := k.Marshal(yaml.Parser())
	if err != nil {
		return err
	}
	return os.WriteFile(path, out, 0o644)
}

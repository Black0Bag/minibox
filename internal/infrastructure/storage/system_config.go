package storage

import (
	"database/sql"
	"encoding/json"
	"fmt"
)

// SystemConfigEntry 表示一条系统配置记录。
type SystemConfigEntry struct {
	Key         string
	Value       string
	Description string
}

// SystemConfigStore 提供 system_config 表的读写访问。
type SystemConfigStore struct {
	db *sql.DB
}

// NewSystemConfigStore 创建系统配置存储。
func NewSystemConfigStore(db *sql.DB) *SystemConfigStore {
	return &SystemConfigStore{db: db}
}

// Get 读取指定键的配置值，不存在时返回空串。
func (s *SystemConfigStore) Get(key string) (string, error) {
	var value string
	err := s.db.QueryRow("SELECT value FROM system_config WHERE key = ?", key).Scan(&value)
	if err == sql.ErrNoRows {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("读取 system_config 失败: %w", err)
	}
	return value, nil
}

// Set 写入（或覆盖）指定键的配置值。
func (s *SystemConfigStore) Set(key, value string) error {
	_, err := s.db.Exec(
		`INSERT INTO system_config (key, value, description) VALUES (?, ?, '')
		 ON CONFLICT(key) DO UPDATE SET value = excluded.value`,
		key, value,
	)
	if err != nil {
		return fmt.Errorf("写入 system_config 失败: %w", err)
	}
	return nil
}

// GetJSON 读取配置并按 JSON 反序列化到 dest。
func (s *SystemConfigStore) GetJSON(key string, dest any) error {
	raw, err := s.Get(key)
	if err != nil || raw == "" {
		return err
	}
	return json.Unmarshal([]byte(raw), dest)
}

// SetJSON 将 val 序列化为 JSON 后写入配置。
func (s *SystemConfigStore) SetJSON(key string, val any) error {
	b, err := json.Marshal(val)
	if err != nil {
		return fmt.Errorf("JSON 序列化失败: %w", err)
	}
	return s.Set(key, string(b))
}

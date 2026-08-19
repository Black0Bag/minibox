package storage

import (
	"database/sql"
	"encoding/json"
	"fmt"
)

type SystemConfigEntry struct {
	Key         string
	Value       string
	Description string
}

type SystemConfigStore struct {
	db *sql.DB
}

func NewSystemConfigStore(db *sql.DB) *SystemConfigStore {
	return &SystemConfigStore{db: db}
}

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

func (s *SystemConfigStore) GetJSON(key string, dest any) error {
	raw, err := s.Get(key)
	if err != nil || raw == "" {
		return err
	}
	return json.Unmarshal([]byte(raw), dest)
}

func (s *SystemConfigStore) SetJSON(key string, val any) error {
	b, err := json.Marshal(val)
	if err != nil {
		return fmt.Errorf("JSON 序列化失败: %w", err)
	}
	return s.Set(key, string(b))
}
package app

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
)

// loadOrGenerateToken 从文件加载 Token，如果文件不存在则生成一个新的 Token 并保存。
// 文件权限为 0600（仅当前用户可读写）。
func loadOrGenerateToken(tokenFile string) (string, error) {
	// 确保目录存在
	dir := filepath.Dir(tokenFile)
	if err := os.MkdirAll(dir, 0750); err != nil {
		return "", fmt.Errorf("创建 token 目录失败: %w", err)
	}

	// 尝试读取已有 token
	data, err := os.ReadFile(tokenFile)
	if err == nil {
		token := string(data)
		if len(token) > 0 {
			return token, nil
		}
	}

	// 生成新 token（32 字节 = 64 位 hex）
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", fmt.Errorf("生成 token 失败: %w", err)
	}
	token := hex.EncodeToString(bytes)

	// 写入文件（0600）
	if err := os.WriteFile(tokenFile, []byte(token), 0600); err != nil {
		return "", fmt.Errorf("保存 token 失败: %w", err)
	}

	return token, nil
}

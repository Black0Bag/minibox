package setup

// 设备凭据：首次启动自动生成随机令牌，供 WS 设备通道握手鉴权。
// 安全要点：crypto/rand（不可预测）、常时比较（防时序侧信道）、0600 文件（N-05）。

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// credentialLen 凭据字节数（32 字节 → 64 hex 字符，约 256 bit 熵）。
const credentialLen = 32

// DeviceCredential 设备凭据。
type DeviceCredential struct {
	token string // 原始令牌（进程内存中保留，供后续校验用）
}

// GenerateDeviceCredential 生成新设备凭据。
func GenerateDeviceCredential() (*DeviceCredential, error) {
	buf := make([]byte, credentialLen)
	if _, err := rand.Read(buf); err != nil {
		return nil, fmt.Errorf("生成设备凭据失败: %w", err)
	}
	return &DeviceCredential{token: hex.EncodeToString(buf)}, nil
}

// Token 返回凭据字符串（供展示/写入配置）。
func (d *DeviceCredential) Token() string {
	if d == nil {
		return ""
	}
	return d.token
}

// Verify 校验令牌（常时比较，防时序侧信道）。
func (d *DeviceCredential) Verify(candidate string) bool {
	if d == nil || candidate == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(d.token), []byte(candidate)) == 1
}

// LoadOrCreate 从文件加载凭据；文件不存在则生成并写入（0600，目录不存在自动创建）。
// 校验失败返回错误（文件损坏时拒绝静默覆盖——fail-closed）。
func LoadOrCreate(path string) (*DeviceCredential, error) {
	data, err := os.ReadFile(path) // #nosec G304 -- path 由组合根配置注入（data 目录），非不可信输入
	if err == nil {
		token := strings.TrimSpace(string(data))
		if len(token) != credentialLen*2 {
			return nil, fmt.Errorf("设备凭据文件损坏: %s（长度异常）", path)
		}
		return &DeviceCredential{token: token}, nil
	}
	if !os.IsNotExist(err) {
		return nil, fmt.Errorf("读取设备凭据失败: %w", err)
	}

	cred, err := GenerateDeviceCredential()
	if err != nil {
		return nil, err
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return nil, fmt.Errorf("创建凭据目录失败: %w", err)
	}
	// G306 已处理：0600 权限（只有本用户可读写设备凭据）
	if err := os.WriteFile(path, []byte(cred.token+"\n"), 0o600); err != nil {
		return nil, fmt.Errorf("写入设备凭据失败: %w", err)
	}
	return cred, nil
}

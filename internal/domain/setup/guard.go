package setup

// 敏感路径拦截：agent 只读工具（read_file 等）的路径黑名单。
// 设计：沙箱（fsutil.PathValidator）管"能去哪"，本清单管"绝不能碰哪"（纵深防御），
// 命中黑名单一律拒绝——无论沙箱 root 如何配置（OpenClaw 教训：配置错也不能读密钥）。

import (
	"path/filepath"
	"strings"
)

// defaultSensitive 默认敏感路径清单（绝对路径/前缀/通配）。
var defaultSensitive = []string{
	// 系统关键目录
	"/etc",
	"/root",
	"/proc",
	"/sys",
	"/boot",
	"/dev",
	"/run",
	// 密钥与凭据
	".ssh",
	".gnupg",
	".docker",
	".aws",
	".config/gh",
	".config/gcloud",
	".config/opencode",
	// 私钥/证书/密码文件（任意目录）
	".pem",
	".key",
	".p12",
	".pfx",
	"id_rsa",
	"id_ed25519",
	"credentials.json",
	"credential.json",
	".netrc",
	".pgpass",
	// 本系统运行凭据
	"device.cred",
	"device.credential",
}

// PathGuard 敏感路径拦截器。
type PathGuard struct {
	patterns []string // 绝对路径按前缀匹配，其余按文件名包含匹配
}

// NewPathGuard 创建拦截器（自定义清单）。
func NewPathGuard(patterns ...string) *PathGuard {
	return &PathGuard{patterns: patterns}
}

// DefaultPathGuard 默认清单拦截器。
func DefaultPathGuard() *PathGuard {
	return NewPathGuard(defaultSensitive...)
}

// IsSensitive 判断路径是否命中黑名单。
// 规则：pattern 以 / 开头 → 路径前缀匹配；否则 → 忽略大小写做通配包含匹配。
func (g *PathGuard) IsSensitive(path string) bool {
	if g == nil {
		return false
	}
	clean := filepath.Clean(path)
	lower := strings.ToLower(clean)
	for _, p := range g.patterns {
		if strings.HasPrefix(p, "/") {
			if clean == p || strings.HasPrefix(clean, p+string(filepath.Separator)) {
				return true
			}
			continue
		}
		needle := strings.ToLower(p)
		if needle == "" || strings.Contains(lower, needle) {
			return true
		}
	}
	return false
}

// Check 拦截动作：命中返回错误（fail-closed）。
func (g *PathGuard) Check(path string) error {
	if g.IsSensitive(path) {
		return &SensitivePathError{Path: path}
	}
	return nil
}

// SensitivePathError 敏感路径访问被拒的错误。
type SensitivePathError struct {
	Path string
}

// Error 实现 error。
func (e *SensitivePathError) Error() string {
	return "敏感路径访问被拒: " + e.Path
}

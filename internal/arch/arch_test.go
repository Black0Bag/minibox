// Package arch 提供架构守护测试（模块 40：跨模块边界，go list -deps 实证）。
// 分层约束（组合根 app 是唯一能看全层的地方）：
//   - domain：纯净业务层，不依赖 infrastructure/transport/app，不碰数据库/HTTP 驱动
//   - platform：通用平台设施，不依赖 domain/infrastructure
//   - infrastructure：可依赖 domain + platform
//   - transport：可依赖 domain + platform（信封走 domain）
//   - app：组合根，允许依赖所有层（唯一特例）
//
// 用 go list -deps 取传递依赖闭包，grep 违规（零新增依赖，CI 可跑）。
package arch

import (
	"os/exec"
	"strings"
	"testing"
)

// deps 返回包路径的传递依赖闭包（go list -deps）。
func deps(t *testing.T, pkgPattern string) []string {
	t.Helper()
	cmd := exec.Command("go", "list", "-deps", "-f", "{{.ImportPath}}", pkgPattern)
	cmd.Dir = repoRoot(t)
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("go list -deps %s 失败: %v", pkgPattern, err)
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	var res []string
	for _, l := range lines {
		if l != "" {
			res = append(res, l)
		}
	}
	return res
}

// repoRoot 返回 minibox 模块根目录（找 go.mod）。
func repoRoot(t *testing.T) string {
	t.Helper()
	cmd := exec.Command("go", "env", "GOMOD")
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("go env GOMOD 失败: %v", err)
	}
	mod := strings.TrimSpace(string(out))
	// GOMOD 返回 go.mod 的绝对路径，取其目录
	idx := strings.LastIndex(mod, "/")
	if idx < 0 {
		t.Fatalf("无法解析 go.mod 路径: %s", mod)
	}
	return mod[:idx]
}

// TestDomainLayerIsolation domain 层不得依赖 infrastructure/transport/app，
// 不得直接依赖数据库与 HTTP 驱动（值对象 JSON 序列化除外）。
func TestDomainLayerIsolation(t *testing.T) {
	got := deps(t, "./internal/domain/...")

	forbidden := []string{
		"github.com/Black0Bag/minibox/internal/infrastructure",
		"github.com/Black0Bag/minibox/internal/transport",
		"github.com/Black0Bag/minibox/internal/app",
		"database/sql",
		"net/http",
	}
	var violations []string
	for _, dep := range got {
		for _, f := range forbidden {
			if strings.HasPrefix(dep, f) {
				violations = append(violations, dep)
			}
		}
	}
	if len(violations) > 0 {
		t.Errorf("domain 层违规依赖: %v", violations)
	}
}

// TestPlatformLayerIsolation platform 层不得依赖 domain/infrastructure/transport/app。
func TestPlatformLayerIsolation(t *testing.T) {
	got := deps(t, "./internal/platform/...")

	forbidden := []string{
		"github.com/Black0Bag/minibox/internal/domain",
		"github.com/Black0Bag/minibox/internal/infrastructure",
		"github.com/Black0Bag/minibox/internal/transport",
		"github.com/Black0Bag/minibox/internal/app",
	}
	var violations []string
	for _, dep := range got {
		for _, f := range forbidden {
			if strings.HasPrefix(dep, f) {
				violations = append(violations, dep)
			}
		}
	}
	if len(violations) > 0 {
		t.Errorf("platform 层违规依赖: %v", violations)
	}
}

// TestInfrastructureDependsOnlyOnDomainPlatform infrastructure 不得依赖 transport/app。
func TestInfrastructureDependsOnlyOnDomainPlatform(t *testing.T) {
	got := deps(t, "./internal/infrastructure/...")

	forbidden := []string{
		"github.com/Black0Bag/minibox/internal/transport",
		"github.com/Black0Bag/minibox/internal/app",
	}
	var violations []string
	for _, dep := range got {
		for _, f := range forbidden {
			if strings.HasPrefix(dep, f) {
				violations = append(violations, dep)
			}
		}
	}
	if len(violations) > 0 {
		t.Errorf("infrastructure 层违规依赖 transport/app: %v", violations)
	}
}

// TestTransportDependsOnlyOnDomainPlatform transport 不得依赖 infrastructure/app。
func TestTransportDependsOnlyOnDomainPlatform(t *testing.T) {
	got := deps(t, "./internal/transport/...")

	forbidden := []string{
		"github.com/Black0Bag/minibox/internal/infrastructure",
		"github.com/Black0Bag/minibox/internal/app",
	}
	var violations []string
	for _, dep := range got {
		for _, f := range forbidden {
			if strings.HasPrefix(dep, f) {
				violations = append(violations, dep)
			}
		}
	}
	if len(violations) > 0 {
		t.Errorf("transport 层违规依赖 infrastructure/app: %v", violations)
	}
}

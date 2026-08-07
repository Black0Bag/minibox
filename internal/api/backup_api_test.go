package api

import (
	"net/http"
	"strings"
	"testing"
)

const (
	backupPath = "/api/v1/%E5%A4%87%E4%BB%BD" // 备份
	upgPath    = "/api/v1/%E5%8D%87%E7%BA%A7" // 升级
)

func TestBackupAPI(t *testing.T) {
	s := testServer(t, nil)
	// 手动备份（知识库已初始化）
	rec := doReq(t, s, http.MethodPost, backupPath+"/", "")
	if rec.Code != http.StatusCreated {
		t.Fatalf("备份应为 201，得到 %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "快照") {
		t.Errorf("响应应含快照名: %s", rec.Body.String())
	}
	// 列表
	rec = doReq(t, s, http.MethodGet, backupPath+"/", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("列表应为 200，得到 %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "snapshot-") {
		t.Errorf("列表应含快照: %s", rec.Body.String())
	}
	// 记录
	rec = doReq(t, s, http.MethodGet, backupPath+"/%E8%AE%B0%E5%BD%95", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("记录应为 200，得到 %d", rec.Code)
	}
}

func TestUpgradeAPI(t *testing.T) {
	s := testServer(t, nil)
	// 状态
	rec := doReq(t, s, http.MethodGet, upgPath+"/", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("状态应为 200，得到 %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "健康检查地址") {
		t.Errorf("状态应含健康检查地址: %s", rec.Body.String())
	}
	// 健康检查（无 healthURL 时默认健康）
	rec = doReq(t, s, http.MethodPost, upgPath+"/%E6%A3%80%E6%9F%A5", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("检查应为 200，得到 %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"健康":true`) {
		t.Errorf("无 URL 应默认健康: %s", rec.Body.String())
	}
	// 应用升级（未配置钩子 → 502）
	rec = doReq(t, s, http.MethodPost, upgPath+"/%E5%BA%94%E7%94%A8", "")
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("无钩子应用应 502，得到 %d: %s", rec.Code, rec.Body.String())
	}
}

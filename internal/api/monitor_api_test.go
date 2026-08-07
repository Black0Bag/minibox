package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

const devicePath = "/api/v1/%E8%AE%BE%E5%A4%87" // 设备 URL 编码

func TestMonitorEndpoint(t *testing.T) {
	rec := doReq(t, testServer(t, nil), http.MethodGet, "/api/v1/%E7%9B%91%E6%8E%A7", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("监控应为 200，得到 %d: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, "num_cpu") && !strings.Contains(body, "cpu") {
		t.Errorf("监控应含 CPU 指标: %s", body)
	}
	if !strings.Contains(body, "降级级别") {
		t.Errorf("监控应含降级级别: %s", body)
	}
}

func TestDegradeEndpoint(t *testing.T) {
	rec := doReq(t, testServer(t, nil), http.MethodGet, "/api/v1/%E9%99%8D%E7%BA%A7", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("降级应为 200，得到 %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "正常") {
		t.Errorf("初始应正常: %s", rec.Body.String())
	}
}

func TestDeviceLifecycleAPI(t *testing.T) {
	s := testServer(t, nil)

	// 生成配对码
	rec := doReq(t, s, http.MethodPost, devicePath+"/%E9%85%8D%E5%AF%B9%E7%A0%81", `{"name":"手机A"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("配对码应为 201，得到 %d: %s", rec.Code, rec.Body.String())
	}
	var codeResp struct {
		Code string `json:"配对码"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &codeResp)
	if codeResp.Code == "" {
		t.Fatal("配对码不应为空")
	}

	// 用配对码注册
	rec = doReq(t, s, http.MethodPost, devicePath+"/",
		`{"code":"`+codeResp.Code+`","id":"phone-1","type":"安卓","capabilities":["无障碍","浏览器"]}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("注册应为 201，得到 %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "phone-1") {
		t.Errorf("注册响应应含设备: %s", rec.Body.String())
	}

	// 错误配对码 → 401
	rec = doReq(t, s, http.MethodPost, devicePath+"/", `{"code":"WRONG","id":"x"}`)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("错误配对码应 401，得到 %d", rec.Code)
	}

	// 列表
	rec = doReq(t, s, http.MethodGet, devicePath+"/", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("列表应为 200，得到 %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "手机A") {
		t.Errorf("列表应含手机A: %s", rec.Body.String())
	}

	// 心跳
	rec = doReq(t, s, http.MethodPost, devicePath+"/phone-1/%E5%BF%83%E8%B7%B3", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("心跳应为 200，得到 %d", rec.Code)
	}

	// 获取
	rec = doReq(t, s, http.MethodGet, devicePath+"/phone-1", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("获取应为 200，得到 %d", rec.Code)
	}

	// 审计
	rec = doReq(t, s, http.MethodGet, devicePath+"/%E5%AE%A1%E8%AE%A1", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("审计应为 200，得到 %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "注册") {
		t.Errorf("审计应含注册记录: %s", rec.Body.String())
	}

	// 注销
	rec = doReq(t, s, http.MethodDelete, devicePath+"/phone-1", "")
	if rec.Code != http.StatusNoContent {
		t.Fatalf("注销应为 204，得到 %d", rec.Code)
	}
	rec = doReq(t, s, http.MethodGet, devicePath+"/phone-1", "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("注销后应 404，得到 %d", rec.Code)
	}
}

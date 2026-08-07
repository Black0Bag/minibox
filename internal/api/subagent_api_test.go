package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

const (
	subagentPath = "/api/v1/subagent"
	planPath     = "/api/v1/%E8%AE%A1%E5%88%92" // 计划 URL 编码
)

// TestSubagentDispatchValidation subagent 派发参数校验
func TestSubagentDispatchValidation(t *testing.T) {
	s := testServer(t, nil)
	// 空 tasks → 400
	rec := doReq(t, s, http.MethodPost, subagentPath+"/", `{"tasks":[]}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("空 tasks 应为 400，得到 %d", rec.Code)
	}
	// 非法 JSON → 400
	rec = doReq(t, s, http.MethodPost, subagentPath+"/", `not-json`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("非法 JSON 应为 400，得到 %d", rec.Code)
	}
	// 未配置 LLM 的测试 server 应返回 503
	// 注意：testServer 无 provider，subEngine 为 nil
	rec = doReq(t, s, http.MethodPost, subagentPath+"/", `{"tasks":[{"id":"a","goal":"测试"}]}`)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("未配置 LLM 应 503，得到 %d: %s", rec.Code, rec.Body.String())
	}
}

// TestSubagentLogNotFound 侧链日志不存在 → 404
func TestSubagentLogNotFound(t *testing.T) {
	rec := doReq(t, testServer(t, nil), http.MethodGet, subagentPath+"/nope/日志", "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("应为 404，得到 %d", rec.Code)
	}
}

// TestPlanCreateValidation 计划创建参数校验
func TestPlanCreateValidation(t *testing.T) {
	s := testServer(t, nil)
	// goal 为空 → 400
	rec := doReq(t, s, http.MethodPost, planPath+"/", `{"goal":""}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("空 goal 应为 400，得到 %d: %s", rec.Code, rec.Body.String())
	}
	// steps 为空 → 400
	rec = doReq(t, s, http.MethodPost, planPath+"/", `{"goal":"目标"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("无步骤应为 400，得到 %d", rec.Code)
	}
	// 步骤 desc 为空 → 400
	rec = doReq(t, s, http.MethodPost, planPath+"/", `{"goal":"目标","steps":[{"desc":""}]}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("空步骤 desc 应为 400，得到 %d", rec.Code)
	}
}

// TestPlanLifecycle 计划全生命周期：创建 → 获取 → 列表 → 执行 → 回滚 → 删除
func TestPlanLifecycle(t *testing.T) {
	s := testServer(t, nil)

	// 创建
	rec := doReq(t, s, http.MethodPost, planPath+"/",
		`{"title":"测试计划","goal":"完成演示","steps":[{"desc":"第一步"},{"desc":"第二步"}]}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("创建应为 201，得到 %d: %s", rec.Code, rec.Body.String())
	}
	var created struct {
		ID     string `json:"id"`
		Status string `json:"status"`
		Steps  []struct {
			ID     int    `json:"id"`
			Status string `json:"status"`
		} `json:"steps"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &created)
	if created.ID == "" {
		t.Fatal("创建应返回 id")
	}
	if created.Status != "pending" {
		t.Errorf("初始状态应为 pending，得到 %s", created.Status)
	}
	if len(created.Steps) != 2 {
		t.Errorf("应有 2 步，得到 %d", len(created.Steps))
	}
	if created.Steps[0].Status != "pending" {
		t.Errorf("初始步骤应 pending，得到 %s", created.Steps[0].Status)
	}

	// 获取
	rec = doReq(t, s, http.MethodGet, planPath+"/"+created.ID, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("获取应为 200，得到 %d", rec.Code)
	}

	// 列表
	rec = doReq(t, s, http.MethodGet, planPath+"/", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("列表应为 200，得到 %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "测试计划") {
		t.Errorf("列表应含测试计划: %s", rec.Body.String())
	}

	// 获取不存在 → 404
	rec = doReq(t, s, http.MethodGet, planPath+"/nonexist", "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("不存在计划应 404，得到 %d", rec.Code)
	}

	// 执行（无 LLM 配置 → 503）
	rec = doReq(t, s, http.MethodPost, planPath+"/"+created.ID+"/%E6%89%A7%E8%A1%8C", "")
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("无 LLM 执行应 503，得到 %d: %s", rec.Code, rec.Body.String())
	}

	// 回滚越界 → 400
	rec = doReq(t, s, http.MethodPost, planPath+"/"+created.ID+"/%E5%9B%9E%E6%BB%9A", `{"index":9}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("越界回滚应 400，得到 %d: %s", rec.Code, rec.Body.String())
	}

	// 删除
	rec = doReq(t, s, http.MethodDelete, planPath+"/"+created.ID, "")
	if rec.Code != http.StatusNoContent {
		t.Fatalf("删除应为 204，得到 %d", rec.Code)
	}
	// 删除后 404
	rec = doReq(t, s, http.MethodGet, planPath+"/"+created.ID, "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("删除后应 404，得到 %d", rec.Code)
	}
}

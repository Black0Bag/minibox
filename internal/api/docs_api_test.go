package api

import (
	"net/http"
	"strings"
	"testing"
)

func TestAPIDocEndpoint(t *testing.T) {
	rec := doReq(t, testServer(t, nil), http.MethodGet, "/api/v1/%E6%96%87%E6%A1%A3", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("文档应为 200，得到 %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "minibox 后端 API 文档") {
		t.Errorf("文档应含标题: %s", body)
	}
	if !strings.Contains(body, "/api/v1/知识库") {
		t.Errorf("文档应含知识库路由: %s", body)
	}
}

func TestAPIDocMarkdownEndpoint(t *testing.T) {
	rec := doReq(t, testServer(t, nil), http.MethodGet, "/api/v1/%E6%96%87%E6%A1%A3.md", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("Markdown 文档应为 200，得到 %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "| GET |") {
		t.Errorf("Markdown 应含路由表格: %s", rec.Body.String())
	}
}

func TestStatusFirstRunHint(t *testing.T) {
	// 无供应商 → 提示向导
	rec := doReq(t, testServer(t, nil), http.MethodGet, "/api/v1/%E7%8A%B6%E6%80%81", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("状态应为 200，得到 %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "首次启动提示") {
		t.Errorf("状态应含首次启动提示: %s", rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "LLM就绪") {
		t.Errorf("状态应含 LLM就绪: %s", rec.Body.String())
	}
}

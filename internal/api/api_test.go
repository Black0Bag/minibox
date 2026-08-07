package api

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Black0Bag/minibox/internal/config"
	"github.com/Black0Bag/minibox/internal/logging"
	"github.com/Black0Bag/minibox/internal/memory"
)

func testServer(t *testing.T, cfg *config.Config) *Server {
	t.Helper()
	if cfg == nil {
		cfg = &config.Config{}
		cfg.Server.Port = 8086
	}
	logger := slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil))
	store, err := memory.Open(t.TempDir())
	if err != nil {
		t.Fatalf("初始化知识库失败: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return NewServer(cfg, logger, "test-version", store, nil)
}

func doReq(t *testing.T, s *Server, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	var rd *bytes.Reader
	if body == "" {
		rd = bytes.NewReader(nil)
	} else {
		rd = bytes.NewReader([]byte(body))
	}
	req := httptest.NewRequest(method, path, rd)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.Router().ServeHTTP(rec, req)
	return rec
}

func TestHealthz(t *testing.T) {
	rec := doReq(t, testServer(t, nil), http.MethodGet, "/api/v1/healthz", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("healthz 应为 200，得到 %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `"状态":"ok"`) {
		t.Errorf("healthz 响应异常: %s", rec.Body.String())
	}
}

func TestStatus(t *testing.T) {
	rec := doReq(t, testServer(t, nil), http.MethodGet, "/api/v1/状态", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("状态应为 200，得到 %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "运行中") {
		t.Errorf("状态响应缺少「运行中」: %s", rec.Body.String())
	}
}

func TestVersion(t *testing.T) {
	rec := doReq(t, testServer(t, nil), http.MethodGet, "/api/v1/版本", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("版本应为 200，得到 %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "test-version") {
		t.Errorf("版本响应异常: %s", rec.Body.String())
	}
}

func TestLogsEndpoint(t *testing.T) {
	// 初始化日志（需要内存缓冲）
	lg := logging.Default()
	lg.Level = "debug"
	_, _ = logging.Init(lg)
	_, _ = logging.SetLevel("debug")
	slog.Debug("调试abc")
	slog.Error("错误xyz", "key", "sk-dJc97HDfRL4zwdb5cipRoOD6pTgdS8")

	s := testServer(t, nil)
	rec := doReq(t, s, http.MethodGet, "/api/v1/logs?level=error&limit=50", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("logs 应为 200，得到 %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "错误xyz") {
		t.Errorf("logs 应含 error 级消息: %s", body)
	}
	// 脱敏检查：不应出现明文 key
	if strings.Contains(body, "sk-dJc97") {
		t.Error("logs 响应不应出现明文 key")
	}
	if !strings.Contains(body, "sk-***") {
		t.Error("logs 响应应脱敏为 sk-***")
	}
}

func TestProviderCRUD(t *testing.T) {
	s := testServer(t, nil)
	// 新增
	rec := doReq(t, s, http.MethodPost, "/api/v1/供应商/", `{"name":"deepseek","base_url":"https://api.example.com/v1","api_key":"sk-dJc97HDfRL4zwdb5cipRoOD6pTgdS8","model":"m1","enabled":true}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("新增应为 201，得到 %d: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if strings.Contains(body, "sk-dJc97") {
		t.Errorf("新增响应 key 应脱敏: %s", body)
	}
	if !strings.Contains(body, "sk****") {
		t.Errorf("key 应脱敏为 sk**** 形式: %s", body)
	}

	// 列表
	rec = doReq(t, s, http.MethodGet, "/api/v1/供应商/", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("列表应为 200，得到 %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "deepseek") {
		t.Errorf("列表应含 deepseek: %s", rec.Body.String())
	}

	// 获取
	rec = doReq(t, s, http.MethodGet, "/api/v1/供应商/deepseek", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("获取应为 200，得到 %d", rec.Code)
	}

	// 更新
	rec = doReq(t, s, http.MethodPut, "/api/v1/供应商/deepseek", `{"model":"m2"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("更新应为 200，得到 %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "m2") {
		t.Errorf("更新后 model 应为 m2: %s", rec.Body.String())
	}

	// 重复新增 → 409
	rec = doReq(t, s, http.MethodPost, "/api/v1/供应商/", `{"name":"deepseek","base_url":"x"}`)
	if rec.Code != http.StatusConflict {
		t.Fatalf("重复新增应为 409，得到 %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "CONFLICT") {
		t.Errorf("409 响应应含 CONFLICT code: %s", rec.Body.String())
	}

	// 删除
	rec = doReq(t, s, http.MethodDelete, "/api/v1/供应商/deepseek", "")
	if rec.Code != http.StatusNoContent {
		t.Fatalf("删除应为 204，得到 %d", rec.Code)
	}

	// 删除后 404
	rec = doReq(t, s, http.MethodGet, "/api/v1/供应商/deepseek", "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("删除后应为 404，得到 %d", rec.Code)
	}
}

func TestProviderNotFound(t *testing.T) {
	rec := doReq(t, testServer(t, nil), http.MethodGet, "/api/v1/供应商/nonexist", "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("应为 404，得到 %d", rec.Code)
	}
	var p Problem
	_ = json.Unmarshal(rec.Body.Bytes(), &p)
	if p.Code != "NOT_FOUND" {
		t.Errorf("错误码应为 NOT_FOUND，得到 %s", p.Code)
	}
	if p.Status != 404 {
		t.Errorf("status 应为 404，得到 %d", p.Status)
	}
}

func TestInvalidProviderAdd(t *testing.T) {
	rec := doReq(t, testServer(t, nil), http.MethodPost, "/api/v1/供应商/", `{"name":""}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("空 name 应为 400，得到 %d", rec.Code)
	}
	rec = doReq(t, testServer(t, nil), http.MethodPost, "/api/v1/供应商/", `not-json`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("非法 JSON 应为 400，得到 %d", rec.Code)
	}
}

func TestMaskKey(t *testing.T) {
	got := maskKey("sk-dJc97HDfRL4zwdb5")
	if got != "sk****wdb5" {
		t.Errorf("maskKey 结果错误: %q", got)
	}
	short := maskKey("abc")
	if short != "***" {
		t.Errorf("短 key 应显示 ***: %q", short)
	}
}

// ============ 知识库 API 测试 ============

const kbPath = "/api/v1/%E7%9F%A5%E8%AF%86%E5%BA%93" // 知识库 URL 编码

func TestKnowledgeBaseAPI(t *testing.T) {
	s := testServer(t, nil)
	// 写入
	rec := doReq(t, s, http.MethodPost, kbPath+"/", `{"zone":"store","type":"text","title":"测试","content":"minibox 的 logme 留痕系统开发记录","tags":["项目"],"source":"s1"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("写入应为 201，得到 %d: %s", rec.Code, rec.Body.String())
	}
	var created struct {
		ID   int64  `json:"id"`
		Zone string `json:"zone"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &created)
	if created.ID == 0 || created.Zone != "store" {
		t.Errorf("写入响应异常: %s", rec.Body.String())
	}

	// 中文搜索
	rec = doReq(t, s, http.MethodGet, kbPath+"/%E6%90%9C%E7%B4%A2?q=%E7%95%99%E7%97%95%E7%B3%BB%E7%BB%9F", "") // /搜索?q=留痕系统
	if rec.Code != http.StatusOK {
		t.Fatalf("搜索应为 200，得到 %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "logme") {
		t.Errorf("搜索应命中 logme 条目: %s", rec.Body.String())
	}

	// 读取
	rec = doReq(t, s, http.MethodGet, kbPath+"/1", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("读取应为 200，得到 %d", rec.Code)
	}

	// 列表
	rec = doReq(t, s, http.MethodGet, kbPath+"/?zone=store", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("列表应为 200，得到 %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `"总数":1`) {
		t.Errorf("列表总数应为 1: %s", rec.Body.String())
	}

	// 更新
	rec = doReq(t, s, http.MethodPut, kbPath+"/1", `{"title":"新标题","content":"更新后的内容","tags":["a"]}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("更新应为 200，得到 %d", rec.Code)
	}

	// 删除
	rec = doReq(t, s, http.MethodDelete, kbPath+"/1", "")
	if rec.Code != http.StatusNoContent {
		t.Fatalf("删除应为 204，得到 %d", rec.Code)
	}

	// 删除后读取 404
	rec = doReq(t, s, http.MethodGet, kbPath+"/1", "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("删除后应为 404，得到 %d", rec.Code)
	}
}

func TestKnowledgeBaseValidation(t *testing.T) {
	s := testServer(t, nil)
	// content 为空 → 400
	rec := doReq(t, s, http.MethodPost, kbPath+"/", `{"content":""}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("空 content 应为 400，得到 %d", rec.Code)
	}
	// zone 非法 → 400
	rec = doReq(t, s, http.MethodPost, kbPath+"/", `{"content":"x","zone":"bad"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("非法 zone 应为 400，得到 %d", rec.Code)
	}
}

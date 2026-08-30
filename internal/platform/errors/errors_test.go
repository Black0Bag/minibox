package errors

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestWriteProblemDetail(t *testing.T) {
	pd := ProblemDetail{
		Type:     "https://minibox.dev/errors/not_found",
		Title:    "资源不存在",
		Status:   404,
		Detail:   "会话不存在: abc123",
		Instance: "/api/v1/conversations/abc123",
	}
	rr := httptest.NewRecorder()
	Write(rr, pd)

	if ct := rr.Header().Get("Content-Type"); ct != "application/problem+json; charset=utf-8" {
		t.Errorf("Content-Type = %q, want application/problem+json", ct)
	}
	if rr.Code != 404 {
		t.Errorf("Status = %d, want 404", rr.Code)
	}

	var m map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &m); err != nil {
		t.Fatalf("解析响应失败: %v", err)
	}
	if m["type"] != "https://minibox.dev/errors/not_found" {
		t.Errorf("type = %v", m["type"])
	}
	if m["title"] != "资源不存在" {
		t.Errorf("title = %v", m["title"])
	}
	if m["detail"] != "会话不存在: abc123" {
		t.Errorf("detail = %v", m["detail"])
	}
	if m["instance"] != "/api/v1/conversations/abc123" {
		t.Errorf("instance = %v", m["instance"])
	}
}

func TestDefaultTypeAboutBlank(t *testing.T) {
	pd := ProblemDetail{Status: 400, Title: "test", Detail: "msg"}
	rr := httptest.NewRecorder()
	Write(rr, pd)

	var m map[string]any
	_ = json.Unmarshal(rr.Body.Bytes(), &m)
	if m["type"] != "about:blank" {
		t.Errorf("type 应默认为 about:blank, got %v", m["type"])
	}
}

func TestWithInstance(t *testing.T) {
	pd := New(404, "not_found", "资源不存在", "test")
	pd = pd.WithInstance("/api/v1/test")
	if pd.Instance != "/api/v1/test" {
		t.Errorf("Instance = %q", pd.Instance)
	}
}

func TestWithMember(t *testing.T) {
	pd := New(429, "rate_limited", "限流", "请求过快")
	pd = pd.WithMember("retry_after", 30)
	rr := httptest.NewRecorder()
	Write(rr, pd)

	var m map[string]any
	_ = json.Unmarshal(rr.Body.Bytes(), &m)
	if m["retry_after"] != float64(30) {
		t.Errorf("retry_after = %v", m["retry_after"])
	}
}

func TestConstructors(t *testing.T) {
	cases := []struct {
		fn     func(string) ProblemDetail
		status int
		typ    string
	}{
		{NotFound, http.StatusNotFound, "not_found"},
		{BadRequest, http.StatusBadRequest, "bad_request"},
		{Internal, http.StatusInternalServerError, "internal_error"},
		{ServiceUnavailable, http.StatusServiceUnavailable, "service_unavailable"},
		{Unauthorized, http.StatusUnauthorized, "unauthorized"},
		{Forbidden, http.StatusForbidden, "forbidden"},
		{Conflict, http.StatusConflict, "conflict"},
	}
	for _, c := range cases {
		pd := c.fn("test")
		if pd.Status != c.status {
			t.Errorf("%s Status = %d, want %d", c.typ, pd.Status, c.status)
		}
		if pd.Type != c.typ {
			t.Errorf("%s Type = %q, want %q", c.typ, pd.Type, c.typ)
		}
	}
}

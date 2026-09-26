package app

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/Black0Bag/minibox/internal/config"
	"github.com/Black0Bag/minibox/internal/infrastructure/storage"
)

// newKBApp 构造带空知识库的 App（常规测试标签，CI 必跑）。
func newKBApp(t *testing.T) *App {
	t.Helper()
	cfg := config.Default()
	cfg.Database.Path = filepath.Join(t.TempDir(), "kb.db")
	cfg.Database.MaxOpenConns = 1
	cfg.Database.MaxIdleConns = 1
	db, err := storage.Open(cfg.Database)
	if err != nil {
		t.Fatalf("打开知识库失败: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	tokenizer, err := storage.NewJiebaTokenizer()
	if err != nil {
		t.Fatalf("创建分词器失败: %v", err)
	}
	a := mockApp()
	a.memory = storage.NewSQLiteStore(db, tokenizer, 0)
	return a
}

// TestKBList空库返回数组 回归：空库时 data.entries 曾是 null（Go nil slice 序列化），
// 严格 JSON 客户端解码直接崩溃（f4-integration 联调缺陷 Bug#1）。
// api.md §3 契约：entries 恒为数组。
func TestKBList空库返回数组(t *testing.T) {
	a := newKBApp(t)

	r := chi.NewRouter()
	r.Get("/api/v1/kb/store", a.handleKBList)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/kb/store?offset=0&limit=20", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("GET /kb/store => %d, want 200; body=%s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	if !bytes.Contains([]byte(body), []byte(`"entries":[]`)) {
		t.Errorf(`空库必须返回 "entries":[]，实际: %s`, body)
	}
	if bytes.Contains([]byte(body), []byte(`"entries":null`)) {
		t.Errorf(`空库返回 null 违反 api.md §3 契约: %s`, body)
	}
}

// TestKBSearch无命中返回数组 回归：无命中时 data.hits 曾是 null（同 Bug#1 同类）。
// api.md §3 契约：hits 恒为数组。
func TestKBSearch无命中返回数组(t *testing.T) {
	a := newKBApp(t)

	r := chi.NewRouter()
	r.Post("/api/v1/kb/search", a.handleKBSearch)

	req := httptest.NewRequest(
		http.MethodPost, "/api/v1/kb/search",
		bytes.NewBufferString(`{"query":"不存在的词_9527xyz","top_k":5}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("POST /kb/search => %d, want 200; body=%s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	if !bytes.Contains([]byte(body), []byte(`"hits":[]`)) {
		t.Errorf(`无命中必须返回 "hits":[]，实际: %s`, body)
	}
	if bytes.Contains([]byte(body), []byte(`"hits":null`)) {
		t.Errorf(`无命中返回 null 违反 api.md §3 契约: %s`, body)
	}
}

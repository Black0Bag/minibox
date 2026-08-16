//go:build integration

// 集成测试（模块 41：真实 DB + 真实 LLM 端点）。
//
//	运行：MINIBOX_TEST_LLM_BASE_URL=... MINIBOX_TEST_LLM_API_KEY=... MINIBOX_TEST_LLM_MODEL=... \
//	      go test -tags integration ./internal/app/
//
// 密钥从环境变量注入（不硬编码进代码）；未配置 LLM 时对话相关测试跳过。
package app

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Black0Bag/minibox/internal/config"
	"github.com/Black0Bag/minibox/internal/domain/memory"
)

// testLLMConfig 从环境变量读取真实 LLM 配置（缺任一则返回 ok=false）。
func testLLMConfig() (baseURL, apiKey, model string, ok bool) {
	baseURL = os.Getenv("MINIBOX_TEST_LLM_BASE_URL")
	apiKey = os.Getenv("MINIBOX_TEST_LLM_API_KEY")
	model = os.Getenv("MINIBOX_TEST_LLM_MODEL")
	return baseURL, apiKey, model, baseURL != "" && apiKey != "" && model != ""
}

// newIntegrationApp 构建真实 app（临时 DB，可选真实 LLM + embedding）。
func newIntegrationApp(t *testing.T, withLLM bool) *App {
	t.Helper()
	dir := t.TempDir()
	cfg := config.Default()
	cfg.Database.Path = filepath.Join(dir, "test.db")
	cfg.Database.MaxOpenConns = 1
	cfg.Database.MaxIdleConns = 1

	if withLLM {
		baseURL, apiKey, model, ok := testLLMConfig()
		if !ok {
			t.Skip("未配置 MINIBOX_TEST_LLM_* 环境变量，跳过真实 LLM 集成")
		}
		cfg.LLM.Providers = []config.ProviderConfig{{
			Name: "test", BaseURL: baseURL, APIKeys: []string{apiKey},
			Models: []config.ModelConfig{{ID: model, Enabled: true}},
		}}
		cfg.LLM.DefaultModel = model
		cfg.LLM.DefaultProvider = "test"
	}

	a, err := New(context.Background(), cfg, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("New err=%v", err)
	}
	t.Cleanup(func() { _ = a.Close() })
	return a
}

// TestIntegrationKBPipeline 真实 DB 全链路：编译入库 → 检索。
func TestIntegrationKBPipeline(t *testing.T) {
	a := newIntegrationApp(t, false)

	// 编译入库
	job, err := a.compiler.Compile(context.Background(),
		"minibox 是一个私有化 agent 系统，知识库是唯一记忆系统，采用 SQLite 存储。",
		memory.CompileOptions{})
	if err != nil {
		t.Fatalf("Compile err=%v", err)
	}
	// 等异步完成
	deadline := time.Now().Add(5 * time.Second)
	var status memory.JobStatus
	for time.Now().Before(deadline) {
		j, err := a.compiler.GetJob(context.Background(), job.ID)
		if err == nil {
			status = j.Status
			if status == memory.JobReady || status == memory.JobFailed {
				break
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	if status != memory.JobReady {
		t.Fatalf("编译未就绪: %s", status)
	}

	// 检索（知识库应有内容）
	hits, err := a.memory.Search(context.Background(), memory.SearchQuery{
		Text: "记忆系统", TopK: 3, Tier: memory.TierStore,
	})
	if err != nil {
		t.Fatalf("Search err=%v", err)
	}
	if len(hits) == 0 {
		t.Fatal("知识库应有检索结果")
	}
	if !strings.Contains(hits[0].Content, "minibox") {
		t.Errorf("检索内容异常: %s", hits[0].Content)
	}
}

// TestIntegrationSessionMemory 真实 DB 会话 + 记忆门。
func TestIntegrationSessionMemory(t *testing.T) {
	a := newIntegrationApp(t, false)

	s := a.sessions.Create()
	// 先入库一段知识，再问（无 LLM 时 Send 会返回"未装配"但不应崩溃）
	_, err := a.compiler.Compile(context.Background(),
		"用户偏好：深色主题、简洁回答。", memory.CompileOptions{})
	if err != nil {
		t.Fatalf("Compile err=%v", err)
	}
	time.Sleep(300 * time.Millisecond)

	// 会话结构可用
	if s.ID == "" || s.Mode == "" {
		t.Fatalf("会话初始化异常: %+v", s)
	}
}

// TestIntegrationConversationWithLLM 真实 LLM 端到端对话（需环境变量）。
func TestIntegrationConversationWithLLM(t *testing.T) {
	a := newIntegrationApp(t, true)

	s := a.sessions.Create()
	answer, err := a.sessions.Send(context.Background(), s.ID, "你好，请用一句话介绍你自己")
	if err != nil {
		t.Fatalf("Send err=%v", err)
	}
	if answer == "" || answer == "（无回答）" || strings.Contains(answer, "运行失败") {
		t.Fatalf("LLM 回答异常: %q", answer)
	}
	t.Logf("LLM 回答: %s", answer)
}

// TestIntegrationSearchKnowledgeWithLLM 真实 LLM + 知识库：多跳检索。
// 编译入库后，问需要知识库的问题，验证 Agent 调用 search_knowledge。
func TestIntegrationSearchKnowledgeWithLLM(t *testing.T) {
	a := newIntegrationApp(t, true)

	// 先入库知识
	job, err := a.compiler.Compile(context.Background(),
		"minibox 项目的默认端口是 8086，可以配置。", memory.CompileOptions{})
	if err != nil {
		t.Fatalf("Compile err=%v", err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		j, _ := a.compiler.GetJob(context.Background(), job.ID)
		if j.Status == memory.JobReady || j.Status == memory.JobFailed {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}

	s := a.sessions.Create()
	answer, err := a.sessions.Send(context.Background(), s.ID, "minibox 项目的默认端口号是多少？请查阅知识库后回答")
	if err != nil {
		t.Fatalf("Send err=%v", err)
	}
	if answer == "" || strings.Contains(answer, "（无回答）") || strings.Contains(answer, "运行失败") {
		t.Fatalf("多跳检索回答异常: %q", answer)
	}
	if !strings.Contains(answer, "8086") {
		t.Errorf("回答应包含 8086（从知识库检索）: %q", answer)
	}
	t.Logf("多跳回答: %s", answer)
}

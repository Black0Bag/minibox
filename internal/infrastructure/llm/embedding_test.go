package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestEmbeddingClientUsesV1EndpointAndValidatesResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/embeddings" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Fatalf("authorization = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"index":0,"embedding":[1,2]},{"index":1,"embedding":[3,4]}]}`))
	}))
	defer server.Close()
	got, err := NewEmbeddingClient(server.URL, "test-key", time.Second, 2).EmbedBatch(context.Background(), "model", []string{"a", "b"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[1][0] != 3 {
		t.Fatalf("unexpected vectors: %#v", got)
	}
}

func TestEmbeddingEndpointNormalization(t *testing.T) {
	cases := map[string]string{
		"service root": "https://example.test/v1/embeddings",
		"v1 root":      "https://example.test/v1/embeddings",
		"endpoint":     "https://example.test/v1/embeddings",
	}
	inputs := map[string]string{
		"service root": "https://example.test",
		"v1 root":      "https://example.test/v1/",
		"endpoint":     "https://example.test/v1/embeddings/",
	}
	for name, want := range cases {
		if got := embeddingEndpoint(inputs[name]); got != want {
			t.Errorf("%s: endpoint=%q, want %q", name, got, want)
		}
	}
}

func TestEmbeddingClientOmitsDimensionsForBGEM3(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request map[string]any
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatalf("解析请求失败: %v", err)
		}
		if _, ok := request["dimensions"]; ok {
			t.Fatalf("BGE-M3 请求不应发送 dimensions: %#v", request)
		}
		if request["input_type"] != "query" {
			t.Fatalf("input_type = %v, want query", request["input_type"])
		}
		_, _ = w.Write([]byte(`{"data":[{"index":0,"embedding":[1,2]}]}`))
	}))
	defer server.Close()

	client := NewEmbeddingClient(server.URL, "", time.Second, 2)
	if _, err := client.EmbedQuery(context.Background(), "BAAI/bge-m3", "查询文本"); err != nil {
		t.Fatalf("BGE-M3 embedding 失败: %v", err)
	}
}

func TestEmbeddingClientSendsDimensionsForOtherConfiguredModels(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request map[string]any
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatalf("解析请求失败: %v", err)
		}
		if request["dimensions"] != float64(2) {
			t.Fatalf("dimensions = %v, want 2", request["dimensions"])
		}
		_, _ = w.Write([]byte(`{"data":[{"index":0,"embedding":[1,2]}]}`))
	}))
	defer server.Close()

	client := NewEmbeddingClient(server.URL, "", time.Second, 2)
	if _, err := client.Embed(context.Background(), "custom/dynamic-embed", "文本"); err != nil {
		t.Fatalf("带 dimensions embedding 失败: %v", err)
	}
}
func TestEmbeddingClientRetriesRateLimit(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if calls.Add(1) < 3 {
			http.Error(w, "limited", http.StatusTooManyRequests)
			return
		}
		_, _ = w.Write([]byte(`{"data":[{"index":0,"embedding":[1]}]}`))
	}))
	defer server.Close()
	client := NewEmbeddingClient(server.URL+"/v1", "", time.Second, 1)
	if _, err := client.Embed(context.Background(), "model", "a"); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 3 {
		t.Fatalf("calls = %d, want 3", calls.Load())
	}
}

func TestEmbeddingClientRejectsNonRetryableAndBadDimensions(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("bad") != "" {
			_, _ = w.Write([]byte(`{"data":[{"index":0,"embedding":[1]}]}`))
			return
		}
		http.Error(w, "bad key", http.StatusUnauthorized)
	}))
	defer server.Close()
	_, err := NewEmbeddingClient(server.URL, "", time.Second, 1).Embed(context.Background(), "model", "a")
	if err == nil || !strings.Contains(err.Error(), "status=401") {
		t.Fatalf("error = %v", err)
	}
	_, err = NewEmbeddingClient(server.URL+"?bad=1", "", time.Second, 2).Embed(context.Background(), "model", "a")
	if err == nil || !strings.Contains(err.Error(), "维度异常") {
		t.Fatalf("error = %v", err)
	}
}

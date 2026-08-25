package sse

import (
	"bufio"
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestHandlerLastEventIDReplaysEventsAfterReconnect(t *testing.T) {
	srv := New(nil)
	httpServer := httptest.NewServer(srv.Handler())
	defer httpServer.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	firstReq, err := http.NewRequestWithContext(ctx, http.MethodGet, httpServer.URL+"?session_id=http-reconnect", nil)
	if err != nil {
		t.Fatal(err)
	}
	firstResp, err := http.DefaultClient.Do(firstReq)
	if err != nil {
		t.Fatalf("首次 SSE 连接失败: %v", err)
	}
	if firstResp.StatusCode != http.StatusOK || !strings.HasPrefix(firstResp.Header.Get("Content-Type"), "text/event-stream") {
		firstResp.Body.Close()
		t.Fatalf("首次 SSE 响应异常: status=%d content-type=%q", firstResp.StatusCode, firstResp.Header.Get("Content-Type"))
	}

	waitForStream(t, srv, "http-reconnect")
	if err := srv.Publish("http-reconnect", "agent", "agent.first", map[string]string{"value": "first"}); err != nil {
		firstResp.Body.Close()
		t.Fatal(err)
	}
	firstIDs := readSSEIDs(t, firstResp.Body, 1)
	firstID := firstIDs[0]
	if firstID != 1 {
		firstResp.Body.Close()
		t.Fatalf("首次事件 id=%d, want 1", firstID)
	}
	_ = firstResp.Body.Close()
	waitForNoStream(t, srv, "http-reconnect")

	// 断线期间产生的事件必须进入历史缓冲。
	if err := srv.Publish("http-reconnect", "agent", "agent.missed", map[string]string{"value": "missed-1"}); err != nil {
		t.Fatal(err)
	}
	if err := srv.Publish("http-reconnect", "agent", "agent.missed", map[string]string{"value": "missed-2"}); err != nil {
		t.Fatal(err)
	}

	reconnectReq, err := http.NewRequestWithContext(ctx, http.MethodGet, httpServer.URL+"?session_id=http-reconnect", nil)
	if err != nil {
		t.Fatal(err)
	}
	reconnectReq.Header.Set("Last-Event-ID", strconv.Itoa(firstID))
	reconnectResp, err := http.DefaultClient.Do(reconnectReq)
	if err != nil {
		t.Fatalf("重连失败: %v", err)
	}
	defer reconnectResp.Body.Close()
	if reconnectResp.StatusCode != http.StatusOK {
		t.Fatalf("重连状态码=%d, want 200", reconnectResp.StatusCode)
	}
	replayedIDs := readSSEIDs(t, reconnectResp.Body, 2)
	if replayedIDs[0] != 2 {
		t.Fatalf("重连第一条回放 id=%d, want 2", replayedIDs[0])
	}
	if replayedIDs[1] != 3 {
		t.Fatalf("重连第二条回放 id=%d, want 3", replayedIDs[1])
	}
}

func waitForStream(t *testing.T, srv *Server, sessionID string) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		srv.mu.RLock()
		stream := srv.streams[sessionID]
		srv.mu.RUnlock()
		if stream != nil {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("SSE stream %q 未建立", sessionID)
}

func waitForNoStream(t *testing.T, srv *Server, sessionID string) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		srv.mu.RLock()
		stream := srv.streams[sessionID]
		srv.mu.RUnlock()
		if stream == nil {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("SSE stream %q 未在连接关闭后注销", sessionID)
}

func readSSEIDs(t *testing.T, body interface{ Read([]byte) (int, error) }, count int) []int {
	t.Helper()
	scanner := bufio.NewScanner(body)
	ids := make([]int, 0, count)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "id: ") {
			continue
		}
		id, err := strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(line, "id: ")))
		if err != nil {
			t.Fatalf("非法 SSE id: %q", line)
		}
		ids = append(ids, id)
		if len(ids) == count {
			return ids
		}
	}
	t.Fatalf("SSE 流提前结束: %v", scanner.Err())
	return ids
}

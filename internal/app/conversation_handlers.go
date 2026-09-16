package app

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
)

// --- 对话域 ---

// handleConversationCreate 创建会话。
func (a *App) handleConversationCreate(w http.ResponseWriter, r *http.Request) {
	s := a.sessions.Create()
	a.respondOK(w, r, "api.conversations.create", s)
}

// handleConversationList 列出会话。
func (a *App) handleConversationList(w http.ResponseWriter, r *http.Request) {
	a.respondOK(w, r, "api.conversations.list", a.sessions.List())
}

// handleConversationGet 获取单个会话。
func (a *App) handleConversationGet(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	s, ok := a.sessions.Get(id)
	if !ok {
		a.respondErr(w, r, http.StatusNotFound, "not_found", "会话不存在: "+id)
		return
	}
	a.respondOK(w, r, "api.conversations.get", s)
}

// conversationSendReq 发送消息请求体。
type conversationSendReq struct {
	Message string `json:"message"`
}

// handleConversationSend 发送用户消息，驱动 agent。
func (a *App) handleConversationSend(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if _, ok := a.sessions.Get(id); !ok {
		a.respondErr(w, r, http.StatusNotFound, "not_found", "会话不存在: "+id)
		return
	}
	var req conversationSendReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Message == "" {
		a.respondErr(w, r, http.StatusBadRequest, "bad_request", "message 必填")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Minute)
	defer cancel()
	answer, err := a.sessions.Send(ctx, id, req.Message)
	if err != nil {
		a.respondErr(w, r, http.StatusInternalServerError, "agent_error", err.Error())
		return
	}
	a.respondOK(w, r, "api.conversations.send", map[string]string{"answer": answer})
}

// conversationRewindReq 回退请求体。
type conversationRewindReq struct {
	Keep int `json:"keep"`
}

// handleConversationRewind 回退会话到指定消息数。
func (a *App) handleConversationRewind(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var req conversationRewindReq
	_ = json.NewDecoder(r.Body).Decode(&req)
	if err := a.sessions.Rewind(id, req.Keep); err != nil {
		a.respondErr(w, r, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	a.respondOK(w, r, "api.conversations.rewind", map[string]bool{"ok": true})
}

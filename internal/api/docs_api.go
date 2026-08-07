package api

import (
	"net/http"

	"github.com/Black0Bag/minibox/internal/docs"
)

// handleAPIDoc API 文档（GET /文档，JSON）
func (s *Server) handleAPIDoc(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, docs.Build(s.version))
}

// handleAPIDocMarkdown API 文档（GET /文档.md，Markdown，浏览器友好）
func (s *Server) handleAPIDocMarkdown(w http.ResponseWriter, r *http.Request) {
	md := docs.Build(s.version).Markdown()
	w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(md))
}

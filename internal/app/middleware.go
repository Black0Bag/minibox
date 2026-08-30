package app

import (
	"net/http"
	"strings"
)

// AuthMiddleware 返回一个 chi 中间件，用于验证 Bearer Token。
// 白名单路径：/health、/ready 不需要认证。
// 其他所有请求必须携带有效的 Authorization: Bearer <token> 头。
func AuthMiddleware(expectedToken string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// 白名单：健康检查不需要认证
			if r.URL.Path == "/api/v1/health" || r.URL.Path == "/api/v1/ready" {
				next.ServeHTTP(w, r)
				return
			}

			// 获取 Authorization 头
			authHeader := r.Header.Get("Authorization")
			if authHeader == "" {
				http.Error(w, `{"error":"missing authorization header"}`, http.StatusUnauthorized)
				return
			}

			// 必须是 Bearer 格式
			if !strings.HasPrefix(authHeader, "Bearer ") {
				http.Error(w, `{"error":"invalid authorization format, expected Bearer token"}`, http.StatusUnauthorized)
				return
			}

			// 提取 token
			token := strings.TrimPrefix(authHeader, "Bearer ")
			if token == "" {
				http.Error(w, `{"error":"empty token"}`, http.StatusUnauthorized)
				return
			}

			// 校验 token
			if token != expectedToken {
				http.Error(w, `{"error":"invalid token"}`, http.StatusUnauthorized)
				return
			}

			// 认证通过，继续
			next.ServeHTTP(w, r)
		})
	}
}

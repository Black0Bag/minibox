// 认证中间件（Bearer Token）。
//
// 规范依据（2026-09-02 互联网校准）：
//   - RFC 9110 §11.1：auth-scheme 是 case-insensitive token，
//     因此 "Bearer" / "bearer" / "BEARER" 都必须接受
//   - RFC 9110 §15.5.2：生成 401 响应 MUST 携带 WWW-Authenticate 挑战头
//   - RFC 6750 §3：无凭据时只回 realm，不带 error；
//     凭据存在但无效时补 error="invalid_token"
//   - RFC 7807：错误体统一用 application/problem+json（与本仓其他错误一致）
//   - crypto/subtle：token 比较用常量时间，避免 == 短路泄漏时序
package http

import (
	"crypto/subtle"
	"net/http"
	"strings"

	plerrors "github.com/Black0Bag/minibox/internal/platform/errors"
)

// authRealm WWW-Authenticate 挑战的 realm 值。
const authRealm = "minibox"

// bearerPrefix Bearer scheme 与凭据之间的分隔（大小写不敏感匹配后使用）。
const bearerScheme = "bearer"

// AuthMiddleware 返回校验 Bearer Token 的 chi 中间件。
//
// expectedToken 为空时返回直通中间件（不启用认证）。
// publicPaths 为免认证白名单（精确匹配请求路径）。
//
// 认证失败统一回 RFC 7807 problem+json + WWW-Authenticate 挑战头，
// 与其余 REST 错误响应格式一致，前端可用同一个错误解析器处理。
func AuthMiddleware(expectedToken string, publicPaths ...string) func(http.Handler) http.Handler {
	if expectedToken == "" {
		return func(next http.Handler) http.Handler { return next }
	}

	public := make(map[string]struct{}, len(publicPaths))
	for _, p := range publicPaths {
		public[p] = struct{}{}
	}
	expected := []byte(expectedToken)

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if _, ok := public[r.URL.Path]; ok {
				next.ServeHTTP(w, r)
				return
			}

			token, ok := bearerToken(r.Header.Get("Authorization"))
			if !ok {
				// RFC 6750 §3：请求未携带任何认证信息时不暴露 error 码。
				writeAuthChallenge(w, r.URL.Path, "", "缺少 Authorization: Bearer <token> 请求头")
				return
			}
			// 常量时间比较：长度不等时 ConstantTimeCompare 直接返回 0，
			// 相同长度下比较耗时不随匹配前缀长度变化。
			if subtle.ConstantTimeCompare([]byte(token), expected) != 1 {
				writeAuthChallenge(w, r.URL.Path, "invalid_token", "Token 无效")
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// bearerToken 从 Authorization 头提取 Bearer 凭据。
// scheme 大小写不敏感（RFC 9110 §11.1）；返回 false 表示不是可用的 Bearer 凭据。
func bearerToken(header string) (string, bool) {
	scheme, credentials, found := strings.Cut(strings.TrimSpace(header), " ")
	if !found || !strings.EqualFold(scheme, bearerScheme) {
		return "", false
	}
	credentials = strings.TrimSpace(credentials)
	if credentials == "" {
		return "", false
	}
	return credentials, true
}

// writeAuthChallenge 写 401 响应：WWW-Authenticate 挑战头 + RFC 7807 错误体。
// errCode 为空表示请求未携带凭据（RFC 6750 §3 要求此时不带 error 参数）。
func writeAuthChallenge(w http.ResponseWriter, instance, errCode, detail string) {
	challenge := `Bearer realm="` + authRealm + `"`
	if errCode != "" {
		challenge += `, error="` + errCode + `"`
	}
	w.Header().Set("WWW-Authenticate", challenge)
	plerrors.Write(w, plerrors.Unauthorized(detail).WithInstance(instance))
}

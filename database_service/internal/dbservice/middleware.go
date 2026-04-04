package dbservice

import (
	"net/http"
	"strings"

	"educationagent/database_service_go/internal/auth"
)

func skipAuthPath(path string) bool {
	switch path {
	case "/healthz":
		return true
	case "/openapi.json", "/favicon.ico":
		return true
	default:
	}
	if strings.HasPrefix(path, "/docs") || strings.HasPrefix(path, "/redoc") {
		return true
	}
	if strings.HasPrefix(path, "/internal/") {
		return false
	}
	if !strings.HasPrefix(path, "/api/v1/") {
		return true
	}
	return false
}

// AuthMiddleware — 与 Python PPTAuthMiddleware 一致：/api/v1 与 /internal 需鉴权（未跳过时）。
func AuthMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		if skipAuthPath(path) {
			ctx := auth.WithContext(r.Context(), &auth.Context{IsInternal: true})
			next.ServeHTTP(w, r.WithContext(ctx))
			return
		}
		// /internal 与 /api/v1
		authz := r.Header.Get("Authorization")
		xik := r.Header.Get("X-Internal-Key")
		if xik == "" {
			xik = r.Header.Get("x-internal-key")
		}
		ac, code, msg := auth.Authenticate(authz, xik)
		if code != 0 {
			writeErr(w, code, msg)
			return
		}
		ctx := auth.WithContext(r.Context(), ac)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

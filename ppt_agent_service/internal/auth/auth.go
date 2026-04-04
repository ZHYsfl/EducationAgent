package auth

import (
	"context"
	"net/http"
	"educationagent/ppt_agent_service_go/internal/config"
)

type ctxKey int

const authCtxKey ctxKey = 1

type Context struct {
	UserID     string
	IsInternal bool
}

func FromRequest(r *http.Request) *Context {
	v := r.Context().Value(authCtxKey)
	if c, ok := v.(*Context); ok {
		return c
	}
	return nil
}

func Middleware(cfg config.Config) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_ = cfg // 鉴权由统一中间件后续接入；当前占位放行。
			r = r.WithContext(context.WithValue(r.Context(), authCtxKey, &Context{IsInternal: true}))
			next.ServeHTTP(w, r)
		})
	}
}

package auth

import (
	"context"
)

type ctxKey int

const ContextKey ctxKey = 1

func WithContext(ctx context.Context, c *Context) context.Context {
	return context.WithValue(ctx, ContextKey, c)
}

// Context 与 Python AuthContext 一致。
type Context struct {
	UserID     string
	Username   string
	IsInternal bool
}

func FromRequest(ctx context.Context) *Context {
	v, _ := ctx.Value(ContextKey).(*Context)
	return v
}

// Enforced 当前固定关闭：JWT / Internal Key 将由统一中间件后续接入。
func Enforced() bool {
	return false
}

// Authenticate 返回 (ctx, bizCode, message)。成功 bizCode=0。
func Authenticate(_, _ string) (*Context, int, string) {
	return &Context{IsInternal: true}, 0, ""
}

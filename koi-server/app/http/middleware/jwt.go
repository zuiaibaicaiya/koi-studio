package middleware

import (
	"time"

	"github.com/goravel/framework/contracts/http"

	"koi-server/app/facades"
)

// JWTMiddleware JWT 认证中间件，校验请求头中的 Authorization 令牌
type JWTMiddleware struct{}

// JWT 返回 JWT 认证中间件实例
func JWT() http.Middleware {
	return &JWTMiddleware{}
}

// Signature 中间件唯一标识
func (r *JWTMiddleware) Signature() string {
	return "jwt"
}

// Handle 执行认证逻辑
func (r *JWTMiddleware) Handle(ctx http.Context) {
	token := ctx.Request().Header("Authorization")
	if token == "" {
		r.unauthorized(ctx, "未授权，请先登录")
		return
	}

	if _, err := facades.Auth(ctx).Parse(token); err != nil {
		r.unauthorized(ctx, "令牌无效或已过期")
		return
	}

	ctx.Request().Next()
}

func (r *JWTMiddleware) unauthorized(ctx http.Context, msg string) {
	ctx.Request().AbortWithStatusJson(http.StatusUnauthorized, http.Json{
		"code":      http.StatusUnauthorized,
		"msg":       msg,
		"timestamp": time.Now().Unix(),
	})
}

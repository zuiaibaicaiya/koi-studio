package users

import "github.com/goravel/framework/contracts/http"

// UserLoginRequest 登录请求
type UserLoginRequest struct {
	// Username 用户名
	Username string `form:"username" json:"username" validate:"required" example:"zhangsan"`
	// Password 密码
	Password string `form:"password" json:"password" validate:"required" example:"password123"`
}

func (r *UserLoginRequest) Authorize(ctx http.Context) error {
	return nil
}

func (r *UserLoginRequest) Filters(ctx http.Context) map[string]any {
	return map[string]any{
		"username": "trim",
		"password": "trim",
	}
}

func (r *UserLoginRequest) Rules(ctx http.Context) map[string]any {
	return map[string]any{
		"username": "required",
		"password": "required",
	}
}

func (r *UserLoginRequest) Messages(ctx http.Context) map[string]string {
	return map[string]string{
		"username.required": ":attribute不可以为空",
		"password.required": ":attribute不可以为空",
	}
}

func (r *UserLoginRequest) Attributes(ctx http.Context) map[string]string {
	return map[string]string{
		"username": "用户名",
		"password": "密码",
	}
}

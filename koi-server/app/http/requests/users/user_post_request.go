package users

import (
	"github.com/goravel/framework/contracts/http"
	"github.com/goravel/framework/contracts/validation"
)

// UserPostRequest 创建用户请求
type UserPostRequest struct {
	// Username 用户名
	Username string `form:"username" json:"username" minLength:"6" maxLength:"255" example:"zhangsan"`
	// Password 密码
	Password string `form:"password" json:"password" minLength:"6" maxLength:"255" example:"password123"`
	// Nickname 昵称
	Nickname string `form:"nickname" json:"nickname" maxLength:"50" example:"张三"`
	// Email 邮箱
	Email string `form:"email" json:"email" example:"zhangsan@example.com"`
	// Phone 手机号
	Phone string `form:"phone" json:"phone" example:"13800138000"`
	// Avatar 头像
	Avatar string `form:"avatar" json:"avatar" example:"https://example.com/avatar.jpg"`
	// Status 状态
	Status string `form:"status" json:"status" example:"active"`
}

func (r *UserPostRequest) Authorize(ctx http.Context) error {
	return nil
}

func (r *UserPostRequest) Filters(ctx http.Context) map[string]any {
	return map[string]any{
		"username": "trim",
		"password": "trim",
		"nickname": "trim",
		"email":    "trim",
		"phone":    "trim",
	}
}

func (r *UserPostRequest) Rules(ctx http.Context) map[string]any {
	return map[string]any{
		"username": "required|min_len:6|max_len:255",
		"password": "required|min_len:6|max_len:255",
		"nickname": "max_len:50",
		"email":    "email",
		"phone":    "regex:^1[3-9]\\d{9}$",
		"status":   "in:active,inactive",
	}
}

func (r *UserPostRequest) Messages(ctx http.Context) map[string]string {
	return map[string]string{
		"username.required": ":attribute不可以为空",
		"username.min_len":  ":attribute长度至少%d位",
		"username.max_len":  ":attribute长度最多%d位",
		"password.required": ":attribute不可以为空",
		"password.min_len":  ":attribute长度至少%d位",
		"password.max_len":  ":attribute长度最多%d位",
		"nickname.max_len":  ":attribute长度最多%d位",
		"email.email":       ":attribute格式不正确",
		"phone.regex":       ":attribute格式不正确",
		"status.in":         ":attribute值不正确",
	}
}

func (r *UserPostRequest) Attributes(ctx http.Context) map[string]string {
	return map[string]string{
		"username": "用户名",
		"password": "密码",
		"nickname": "昵称",
		"email":    "邮箱",
		"phone":    "手机号",
		"status":   "状态",
	}
}

func (r *UserPostRequest) PrepareForValidation(ctx http.Context, data validation.Data) error {
	if _, exist := data.Get("status"); !exist {
		data.Set("status", "active")
	}

	return nil
}

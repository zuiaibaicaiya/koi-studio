package users

import (
	"github.com/goravel/framework/contracts/http"
	"github.com/goravel/framework/contracts/validation"
)

// UserUpdateProfileRequest 当前用户更新自身资料请求（不允许修改用户名和状态）
type UserUpdateProfileRequest struct {
	// Nickname 昵称
	Nickname string `form:"nickname" json:"nickname" example:"张三"`
	// Email 邮箱
	Email string `form:"email" json:"email" example:"zhangsan@example.com"`
	// Phone 手机号
	Phone string `form:"phone" json:"phone" example:"13800138000"`
	// Avatar 头像
	Avatar string `form:"avatar" json:"avatar" example:"https://example.com/avatar.jpg"`
}

func (r *UserUpdateProfileRequest) Authorize(ctx http.Context) error {
	return nil
}

func (r *UserUpdateProfileRequest) Filters(ctx http.Context) map[string]any {
	return map[string]any{
		"nickname": "trim",
		"email":    "trim",
		"phone":    "trim",
		"avatar":   "trim",
	}
}

func (r *UserUpdateProfileRequest) Rules(ctx http.Context) map[string]any {
	return map[string]any{
		"nickname": "max_len:50",
		"email":    "email",
		"phone":    "regex:^1[3-9]\\d{9}$",
	}
}

func (r *UserUpdateProfileRequest) Messages(ctx http.Context) map[string]string {
	return map[string]string{
		"nickname.max_len": ":attribute长度最多%d位",
		"email.email":      ":attribute格式不正确",
		"phone.regex":      ":attribute格式不正确",
	}
}

func (r *UserUpdateProfileRequest) Attributes(ctx http.Context) map[string]string {
	return map[string]string{
		"nickname": "昵称",
		"email":    "邮箱",
		"phone":    "手机号",
		"avatar":   "头像",
	}
}

func (r *UserUpdateProfileRequest) PrepareForValidation(ctx http.Context, data validation.Data) error {
	return nil
}

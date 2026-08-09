package users

import (
	"github.com/goravel/framework/contracts/http"
	"github.com/goravel/framework/contracts/validation"
)

// UserChangePasswordRequest 修改密码请求
type UserChangePasswordRequest struct {
	// OldPassword 旧密码
	OldPassword string `form:"oldPassword" json:"oldPassword" example:"oldpassword123"`
	// NewPassword 新密码
	NewPassword string `form:"newPassword" json:"newPassword" example:"newpassword123"`
}

func (r *UserChangePasswordRequest) Authorize(ctx http.Context) error {
	return nil
}

func (r *UserChangePasswordRequest) Filters(ctx http.Context) map[string]any {
	return map[string]any{
		"oldPassword": "trim",
		"newPassword": "trim",
	}
}

func (r *UserChangePasswordRequest) Rules(ctx http.Context) map[string]any {
	return map[string]any{
		"oldPassword": "required|min_len:6|max_len:255",
		"newPassword": "required|min_len:6|max_len:255",
	}
}

func (r *UserChangePasswordRequest) Messages(ctx http.Context) map[string]string {
	return map[string]string{
		"oldPassword.required": ":attribute不可以为空",
		"oldPassword.min_len":  ":attribute长度至少%d位",
		"oldPassword.max_len":  ":attribute长度最多%d位",
		"newPassword.required": ":attribute不可以为空",
		"newPassword.min_len":  ":attribute长度至少%d位",
		"newPassword.max_len":  ":attribute长度最多%d位",
	}
}

func (r *UserChangePasswordRequest) Attributes(ctx http.Context) map[string]string {
	return map[string]string{
		"oldPassword": "旧密码",
		"newPassword": "新密码",
	}
}

func (r *UserChangePasswordRequest) PrepareForValidation(ctx http.Context, data validation.Data) error {
	return nil
}

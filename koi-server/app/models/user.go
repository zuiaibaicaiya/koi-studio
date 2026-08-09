package models

import "github.com/goravel/framework/database/orm"

// User 用户模型
type User struct {
	orm.Model
	orm.SoftDeletes
	// Username 用户名
	Username string `json:"username" example:"zhangsan"`
	// Password 密码（不参与 JSON 序列化）
	Password string `json:"-" example:"password123"`
	// Nickname 昵称
	Nickname string `json:"nickname" example:"张三"`
	// Email 邮箱
	Email string `json:"email" example:"zhangsan@example.com"`
	// Phone 手机号
	Phone string `json:"phone" example:"13800138000"`
	// Avatar 头像
	Avatar string `json:"avatar" example:"https://example.com/avatar.jpg"`
	// Status 状态：active-启用，inactive-禁用
	Status string `json:"status" example:"active"`
}

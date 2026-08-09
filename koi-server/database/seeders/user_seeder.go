package seeders

import (
	"koi-server/app/facades"
	"koi-server/app/models"
)

type UserSeeder struct{}

// Signature The unique signature of the seeder.
func (s *UserSeeder) Signature() string {
	return "UserSeeder"
}

// Run executes the seeder logic.
func (s *UserSeeder) Run() error {
	count, err := facades.Orm().Query().Model(&models.User{}).Count()
	if err != nil {
		return err
	}

	// 已有数据时跳过，避免重复写入
	if count > 0 {
		return nil
	}

	password, err := facades.Hash().Make("admin123")
	if err != nil {
		return err
	}

	users := []models.User{
		{
			Username: "admin",
			Password: password,
			Nickname: "系统管理员",
			Email:    "admin@example.com",
			Phone:    "13800138000",
			Status:   "active",
		},
	}

	for i := range users {
		if err := facades.Orm().Query().Create(&users[i]); err != nil {
			return err
		}
	}

	return nil
}

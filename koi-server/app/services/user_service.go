package services

import (
	"github.com/goravel/framework/contracts/database/db"

	"koi-server/app/facades"
	"koi-server/app/models"
)

// UserService 用户服务，封装用户相关的数据访问逻辑
type UserService struct {
}

func NewUserService() *UserService {
	return &UserService{}
}

// GetUserList 分页获取用户列表，支持关键词与状态筛选
func (userService *UserService) GetUserList(page int, pageSize int, keyword string, status string) (users []models.User, total int64, err error) {
	query := facades.Orm().Query()

	if keyword != "" {
		like := "%" + keyword + "%"
		query = query.Where("username LIKE ? OR nickname LIKE ? OR email LIKE ? OR phone LIKE ?", like, like, like, like)
	}

	if status != "" {
		query = query.Where("status = ?", status)
	}

	err = query.OrderByDesc("id").Paginate(page, pageSize, &users, &total)

	return users, total, err
}

// AddUser 创建用户
func (userService *UserService) AddUser(user *models.User) error {
	return facades.Orm().Query().Create(user)
}

// GetUserById 根据 ID 查询用户，不存在时返回错误
func (userService *UserService) GetUserById(id int) (user models.User, err error) {
	err = facades.Orm().Query().FindOrFail(&user, id)

	return user, err
}

// GetUserByUsername 根据用户名查询用户，不存在时返回错误
func (userService *UserService) GetUserByUsername(username string) (user models.User, err error) {
	err = facades.Orm().Query().Where("username = ?", username).FirstOrFail(&user)

	return user, err
}

// UpdateUser 更新用户
func (userService *UserService) UpdateUser(user *models.User) error {
	return facades.Orm().Query().Save(user)
}

// DeleteUserById 根据 ID 软删除用户
func (userService *UserService) DeleteUserById(id int) (*db.Result, error) {
	return facades.Orm().Query().Model(&models.User{}).Where("id = ?", id).Delete()
}

// IsUsernameExists 判断用户名是否已存在，excludeID 大于 0 时排除该用户（用于更新场景）
func (userService *UserService) IsUsernameExists(username string, excludeID uint) (bool, error) {
	query := facades.Orm().Query().Model(&models.User{}).Where("username = ?", username)

	if excludeID > 0 {
		query = query.Where("id != ?", excludeID)
	}

	count, err := query.Count()

	return count > 0, err
}

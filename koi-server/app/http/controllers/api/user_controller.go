package api

import (
	"github.com/goravel/framework/contracts/http"

	"koi-server/app/facades"
	"koi-server/app/http/requests/users"
	"koi-server/app/models"
	"koi-server/app/services"
)

// UserController 用户管理控制器
type UserController struct {
	BaseController
	userService *services.UserService
}

func NewUserController() *UserController {
	return &UserController{
		userService: services.NewUserService(),
	}
}

// ListUsers 用户列表，支持分页、关键词与状态筛选
func (ctrl *UserController) ListUsers(ctx http.Context) http.Response {
	var userListReq users.UserListRequest
	errors, err := ctx.Request().ValidateRequest(&userListReq)
	if err != nil {
		return ctrl.ApiErrorMsg(ctx, err.Error())
	}
	if errors != nil {
		return ctrl.ApiErrorMsg(ctx, ctrl.GetFirstError(errors))
	}

	page := userListReq.Page
	if page <= 0 {
		page = 1
	}

	pageSize := userListReq.PageSize
	if pageSize <= 0 {
		pageSize = 16
	}
	if pageSize > 100 {
		pageSize = 100
	}

	usersList, total, err := ctrl.userService.GetUserList(page, pageSize, userListReq.Keyword, userListReq.Status)
	if err != nil {
		facades.Log().WithContext(ctx).Error("获取用户列表失败: " + err.Error())
		return ctrl.ApiErrorMsg(ctx, "获取用户列表失败")
	}

	return ctrl.ApiPaginate(ctx, usersList, total, page, pageSize)
}

// GetUser 用户详情
func (ctrl *UserController) GetUser(ctx http.Context) http.Response {
	id := ctx.Request().RouteInt("id")
	if id <= 0 {
		return ctrl.ApiErrorMsg(ctx, "用户ID不正确")
	}

	user, err := ctrl.userService.GetUserById(id)
	if err != nil {
		return ctrl.ApiErrorMsg(ctx, "用户不存在")
	}

	return ctrl.ApiSuccess(ctx, user)
}

// CreateUser 新增用户，密码自动加密存储
func (ctrl *UserController) CreateUser(ctx http.Context) http.Response {
	var userPost users.UserPostRequest
	errors, err := ctx.Request().ValidateRequest(&userPost)
	if err != nil {
		return ctrl.ApiErrorMsg(ctx, err.Error())
	}
	if errors != nil {
		return ctrl.ApiErrorMsg(ctx, ctrl.GetFirstError(errors))
	}

	exists, err := ctrl.userService.IsUsernameExists(userPost.Username, 0)
	if err != nil {
		facades.Log().WithContext(ctx).Error("校验用户名失败: " + err.Error())
		return ctrl.ApiErrorMsg(ctx, "创建用户失败")
	}
	if exists {
		return ctrl.ApiErrorMsg(ctx, "用户已经存在")
	}

	password, err := facades.Hash().Make(userPost.Password)
	if err != nil {
		facades.Log().WithContext(ctx).Error("密码加密失败: " + err.Error())
		return ctrl.ApiErrorMsg(ctx, "创建用户失败")
	}

	status := userPost.Status
	if status == "" {
		status = "active"
	}

	user := models.User{
		Username: userPost.Username,
		Password: password,
		Nickname: userPost.Nickname,
		Email:    userPost.Email,
		Phone:    userPost.Phone,
		Avatar:   userPost.Avatar,
		Status:   status,
	}

	if err := ctrl.userService.AddUser(&user); err != nil {
		facades.Log().WithContext(ctx).Error("创建用户失败: " + err.Error())
		return ctrl.ApiErrorMsg(ctx, "创建用户失败")
	}

	facades.Log().WithContext(ctx).Info("创建用户成功: " + user.Username)

	return ctrl.ApiSuccess(ctx, user)
}

// UpdateUser 更新用户，仅更新传入的字段，密码为空时不更新
func (ctrl *UserController) UpdateUser(ctx http.Context) http.Response {
	id := ctx.Request().RouteInt("id")
	if id <= 0 {
		return ctrl.ApiErrorMsg(ctx, "用户ID不正确")
	}

	var userUpdate users.UserUpdateRequest
	errors, err := ctx.Request().ValidateRequest(&userUpdate)
	if err != nil {
		return ctrl.ApiErrorMsg(ctx, err.Error())
	}
	if errors != nil {
		return ctrl.ApiErrorMsg(ctx, ctrl.GetFirstError(errors))
	}

	user, err := ctrl.userService.GetUserById(id)
	if err != nil {
		return ctrl.ApiErrorMsg(ctx, "用户不存在")
	}

	if userUpdate.Username != "" && userUpdate.Username != user.Username {
		exists, err := ctrl.userService.IsUsernameExists(userUpdate.Username, user.ID)
		if err != nil {
			facades.Log().WithContext(ctx).Error("校验用户名失败: " + err.Error())
			return ctrl.ApiErrorMsg(ctx, "更新用户失败")
		}
		if exists {
			return ctrl.ApiErrorMsg(ctx, "用户已经存在")
		}

		user.Username = userUpdate.Username
	}

	if userUpdate.Password != "" {
		password, err := facades.Hash().Make(userUpdate.Password)
		if err != nil {
			facades.Log().WithContext(ctx).Error("密码加密失败: " + err.Error())
			return ctrl.ApiErrorMsg(ctx, "更新用户失败")
		}

		user.Password = password
	}

	if userUpdate.Nickname != "" {
		user.Nickname = userUpdate.Nickname
	}
	if userUpdate.Email != "" {
		user.Email = userUpdate.Email
	}
	if userUpdate.Phone != "" {
		user.Phone = userUpdate.Phone
	}
	if userUpdate.Avatar != "" {
		user.Avatar = userUpdate.Avatar
	}
	if userUpdate.Status != "" {
		user.Status = userUpdate.Status
	}

	if err := ctrl.userService.UpdateUser(&user); err != nil {
		facades.Log().WithContext(ctx).Error("更新用户失败: " + err.Error())
		return ctrl.ApiErrorMsg(ctx, "更新用户失败")
	}

	facades.Log().WithContext(ctx).Info("更新用户成功: " + user.Username)

	return ctrl.ApiSuccess(ctx, user)
}

// DeleteUser 删除用户（软删除）
func (ctrl *UserController) DeleteUser(ctx http.Context) http.Response {
	id := ctx.Request().RouteInt("id")
	if id <= 0 {
		return ctrl.ApiErrorMsg(ctx, "用户ID不正确")
	}

	user, err := ctrl.userService.GetUserById(id)
	if err != nil {
		return ctrl.ApiErrorMsg(ctx, "用户不存在")
	}

	// 禁止删除当前登录用户
	if currentUser, err := ctrl.GetCurrentUser(ctx); err == nil && currentUser.ID == user.ID {
		return ctrl.ApiErrorMsg(ctx, "不能删除当前登录用户")
	}

	result, err := ctrl.userService.DeleteUserById(id)
	if err != nil {
		facades.Log().WithContext(ctx).Error("删除用户失败: " + err.Error())
		return ctrl.ApiErrorMsg(ctx, "删除用户失败")
	}
	if result.RowsAffected == 0 {
		return ctrl.ApiErrorMsg(ctx, "删除失败")
	}

	facades.Log().WithContext(ctx).Info("删除用户成功: " + user.Username)

	return ctrl.ApiSuccess(ctx, map[string]string{})
}

// Login 用户登录，成功后返回 JWT 令牌
func (ctrl *UserController) Login(ctx http.Context) http.Response {
	var userLogin users.UserLoginRequest
	errors, err := ctx.Request().ValidateRequest(&userLogin)
	if err != nil {
		return ctrl.ApiErrorMsg(ctx, err.Error())
	}
	if errors != nil {
		return ctrl.ApiErrorMsg(ctx, ctrl.GetFirstError(errors))
	}

	user, err := ctrl.userService.GetUserByUsername(userLogin.Username)
	if err != nil {
		facades.Log().WithContext(ctx).Warning("登录失败，用户不存在: " + userLogin.Username)
		return ctrl.ApiErrorMsg(ctx, "用户名或密码错误")
	}

	if !facades.Hash().Check(userLogin.Password, user.Password) {
		facades.Log().WithContext(ctx).Warning("登录失败，密码错误: " + userLogin.Username)
		return ctrl.ApiErrorMsg(ctx, "用户名或密码错误")
	}

	if user.Status == "inactive" {
		return ctrl.ApiErrorMsg(ctx, "账号已被禁用")
	}

	token, err := facades.Auth(ctx).Login(&user)
	if err != nil {
		facades.Log().WithContext(ctx).Error("生成令牌失败: " + err.Error())
		return ctrl.ApiErrorMsg(ctx, "生成令牌失败")
	}

	facades.Log().WithContext(ctx).Info("登录成功: " + user.Username)

	return ctrl.ApiSuccess(ctx, map[string]any{
		"token": token,
		"user":  user,
	})
}

// Logout 退出登录，使当前令牌失效
func (ctrl *UserController) Logout(ctx http.Context) http.Response {
	if err := facades.Auth(ctx).Logout(); err != nil {
		return ctrl.ApiErrorMsg(ctx, "退出登录失败")
	}

	return ctrl.ApiSuccess(ctx, map[string]string{})
}

// GetCurrentUserInfo 获取当前登录用户信息
func (ctrl *UserController) GetCurrentUserInfo(ctx http.Context) http.Response {
	currentUser, err := ctrl.GetCurrentUser(ctx)
	if err != nil {
		return ctrl.ApiErrorMsg(ctx, "未授权，请重新登录")
	}

	return ctrl.ApiSuccess(ctx, currentUser)
}

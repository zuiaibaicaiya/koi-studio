package api

import (
	"strings"

	"github.com/goravel/framework/contracts/http"

	"koi-server/app/facades"
	"koi-server/app/http/requests/hotwords"
	"koi-server/app/models"
	"koi-server/app/services"
)

// HotWordLibraryController 热词库管理控制器
type HotWordLibraryController struct {
	BaseController
	libraryService *services.HotWordLibraryService
	excelService   *services.HotWordExcelService
}

func NewHotWordLibraryController() *HotWordLibraryController {
	return &HotWordLibraryController{
		libraryService: services.NewHotWordLibraryService(),
		excelService:   services.NewHotWordExcelService(),
	}
}

// ListLibraries 热词库列表，支持分页、关键词与状态筛选
func (ctrl *HotWordLibraryController) ListLibraries(ctx http.Context) http.Response {
	var listReq hotwords.LibraryListRequest
	errors, err := ctx.Request().ValidateRequest(&listReq)
	if err != nil {
		return ctrl.ApiErrorMsg(ctx, err.Error())
	}
	if errors != nil {
		return ctrl.ApiErrorMsg(ctx, ctrl.GetFirstError(errors))
	}

	page, pageSize := normalizePagination(listReq.Page, listReq.PageSize)

	libraries, total, err := ctrl.libraryService.GetLibraryList(page, pageSize, listReq.Keyword, listReq.Status)
	if err != nil {
		facades.Log().WithContext(ctx).Error("获取热词库列表失败: " + err.Error())
		return ctrl.ApiErrorMsg(ctx, "获取热词库列表失败")
	}

	return ctrl.ApiPaginate(ctx, libraries, total, page, pageSize)
}

// GetLibrary 热词库详情
func (ctrl *HotWordLibraryController) GetLibrary(ctx http.Context) http.Response {
	id := ctx.Request().RouteInt("id")
	if id <= 0 {
		return ctrl.ApiErrorMsg(ctx, "热词库ID不正确")
	}

	library, err := ctrl.libraryService.GetLibraryById(id)
	if err != nil {
		return ctrl.ApiErrorMsg(ctx, "热词库不存在")
	}

	return ctrl.ApiSuccess(ctx, library)
}

// CreateLibrary 新增热词库
func (ctrl *HotWordLibraryController) CreateLibrary(ctx http.Context) http.Response {
	var libraryPost hotwords.LibraryPostRequest
	errors, err := ctx.Request().ValidateRequest(&libraryPost)
	if err != nil {
		return ctrl.ApiErrorMsg(ctx, err.Error())
	}
	if errors != nil {
		return ctrl.ApiErrorMsg(ctx, ctrl.GetFirstError(errors))
	}

	exists, err := ctrl.libraryService.IsLibraryNameExists(libraryPost.Name, 0)
	if err != nil {
		facades.Log().WithContext(ctx).Error("校验热词库名称失败: " + err.Error())
		return ctrl.ApiErrorMsg(ctx, "创建热词库失败")
	}
	if exists {
		return ctrl.ApiErrorMsg(ctx, "热词库已经存在")
	}

	status := libraryPost.Status
	if status == "" {
		status = "active"
	}

	library := models.HotWordLibrary{
		Name:        libraryPost.Name,
		Description: libraryPost.Description,
		Status:      status,
	}

	if err := ctrl.libraryService.AddLibrary(&library); err != nil {
		facades.Log().WithContext(ctx).Error("创建热词库失败: " + err.Error())
		return ctrl.ApiErrorMsg(ctx, "创建热词库失败")
	}

	facades.Log().WithContext(ctx).Info("创建热词库成功: " + library.Name)

	return ctrl.ApiSuccess(ctx, library)
}

// ImportLibrary 导入 Excel 创建热词库，以文件名作为热词库名称
func (ctrl *HotWordLibraryController) ImportLibrary(ctx http.Context) http.Response {
	file, err := ctx.Request().File("file")
	if err != nil {
		return ctrl.ApiErrorMsg(ctx, "请选择需要导入的Excel文件")
	}

	extension := strings.ToLower(file.GetClientOriginalExtension())
	if extension != "xlsx" && extension != "xlsm" && extension != "xltx" && extension != "xltm" {
		return ctrl.ApiErrorMsg(ctx, "仅支持xlsx格式的Excel文件")
	}

	name := ctrl.excelService.LibraryNameFromFileName(file.GetClientOriginalName())
	if name == "" {
		return ctrl.ApiErrorMsg(ctx, "无法从文件名解析热词库名称")
	}
	if len([]rune(name)) > 100 {
		return ctrl.ApiErrorMsg(ctx, "热词库名称长度最多100位")
	}

	exists, err := ctrl.libraryService.IsLibraryNameExists(name, 0)
	if err != nil {
		facades.Log().WithContext(ctx).Error("校验热词库名称失败: " + err.Error())
		return ctrl.ApiErrorMsg(ctx, "导入热词库失败")
	}
	if exists {
		return ctrl.ApiErrorMsg(ctx, "热词库已经存在")
	}

	hotWords, err := ctrl.excelService.ParseHotWords(file.File())
	if err != nil {
		facades.Log().WithContext(ctx).Warning("解析热词Excel失败: " + err.Error())
		return ctrl.ApiErrorMsg(ctx, err.Error())
	}

	library := models.HotWordLibrary{
		Name:        name,
		Description: ctx.Request().Input("description"),
		Status:      "active",
	}

	if err := ctrl.libraryService.CreateLibraryWithWords(&library, hotWords); err != nil {
		facades.Log().WithContext(ctx).Error("导入热词库失败: " + err.Error())
		return ctrl.ApiErrorMsg(ctx, "导入热词库失败")
	}

	facades.Log().WithContext(ctx).Info("导入热词库成功: " + library.Name)

	return ctrl.ApiSuccess(ctx, library)
}

// UpdateLibrary 更新热词库，仅更新传入的字段
func (ctrl *HotWordLibraryController) UpdateLibrary(ctx http.Context) http.Response {
	id := ctx.Request().RouteInt("id")
	if id <= 0 {
		return ctrl.ApiErrorMsg(ctx, "热词库ID不正确")
	}

	var libraryUpdate hotwords.LibraryUpdateRequest
	errors, err := ctx.Request().ValidateRequest(&libraryUpdate)
	if err != nil {
		return ctrl.ApiErrorMsg(ctx, err.Error())
	}
	if errors != nil {
		return ctrl.ApiErrorMsg(ctx, ctrl.GetFirstError(errors))
	}

	library, err := ctrl.libraryService.GetLibraryById(id)
	if err != nil {
		return ctrl.ApiErrorMsg(ctx, "热词库不存在")
	}

	if libraryUpdate.Name != "" && libraryUpdate.Name != library.Name {
		exists, err := ctrl.libraryService.IsLibraryNameExists(libraryUpdate.Name, library.ID)
		if err != nil {
			facades.Log().WithContext(ctx).Error("校验热词库名称失败: " + err.Error())
			return ctrl.ApiErrorMsg(ctx, "更新热词库失败")
		}
		if exists {
			return ctrl.ApiErrorMsg(ctx, "热词库已经存在")
		}

		library.Name = libraryUpdate.Name
	}

	if libraryUpdate.Description != "" {
		library.Description = libraryUpdate.Description
	}
	if libraryUpdate.Status != "" {
		library.Status = libraryUpdate.Status
	}

	if err := ctrl.libraryService.UpdateLibrary(&library); err != nil {
		facades.Log().WithContext(ctx).Error("更新热词库失败: " + err.Error())
		return ctrl.ApiErrorMsg(ctx, "更新热词库失败")
	}

	facades.Log().WithContext(ctx).Info("更新热词库成功: " + library.Name)

	return ctrl.ApiSuccess(ctx, library)
}

// DeleteLibrary 删除热词库（软删除），同时删除其下所有热词
func (ctrl *HotWordLibraryController) DeleteLibrary(ctx http.Context) http.Response {
	id := ctx.Request().RouteInt("id")
	if id <= 0 {
		return ctrl.ApiErrorMsg(ctx, "热词库ID不正确")
	}

	library, err := ctrl.libraryService.GetLibraryById(id)
	if err != nil {
		return ctrl.ApiErrorMsg(ctx, "热词库不存在")
	}

	result, err := ctrl.libraryService.DeleteLibraryById(id)
	if err != nil {
		facades.Log().WithContext(ctx).Error("删除热词库失败: " + err.Error())
		return ctrl.ApiErrorMsg(ctx, "删除热词库失败")
	}
	if result == nil || result.RowsAffected == 0 {
		return ctrl.ApiErrorMsg(ctx, "删除失败")
	}

	facades.Log().WithContext(ctx).Info("删除热词库成功: " + library.Name)

	return ctrl.ApiSuccess(ctx, map[string]string{})
}

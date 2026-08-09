package api

import (
	"github.com/goravel/framework/contracts/http"

	"koi-server/app/facades"
	"koi-server/app/http/requests/hotwords"
	"koi-server/app/models"
	"koi-server/app/services"
)

// HotWordController 热词管理控制器，热词均归属于某个热词库
type HotWordController struct {
	BaseController
	hotWordService *services.HotWordService
	libraryService *services.HotWordLibraryService
}

func NewHotWordController() *HotWordController {
	return &HotWordController{
		hotWordService: services.NewHotWordService(),
		libraryService: services.NewHotWordLibraryService(),
	}
}

// ListHotWords 指定热词库下的热词列表，支持分页与关键词筛选
func (ctrl *HotWordController) ListHotWords(ctx http.Context) http.Response {
	library, response := ctrl.resolveLibrary(ctx)
	if response != nil {
		return response
	}

	var listReq hotwords.WordListRequest
	errors, err := ctx.Request().ValidateRequest(&listReq)
	if err != nil {
		return ctrl.ApiErrorMsg(ctx, err.Error())
	}
	if errors != nil {
		return ctrl.ApiErrorMsg(ctx, ctrl.GetFirstError(errors))
	}

	page, pageSize := normalizePagination(listReq.Page, listReq.PageSize)

	hotWordList, total, err := ctrl.hotWordService.GetHotWordList(library.ID, page, pageSize, listReq.Keyword)
	if err != nil {
		facades.Log().WithContext(ctx).Error("获取热词列表失败: " + err.Error())
		return ctrl.ApiErrorMsg(ctx, "获取热词列表失败")
	}

	return ctrl.ApiPaginate(ctx, hotWordList, total, page, pageSize)
}

// GetHotWord 热词详情
func (ctrl *HotWordController) GetHotWord(ctx http.Context) http.Response {
	library, response := ctrl.resolveLibrary(ctx)
	if response != nil {
		return response
	}

	hotWord, response := ctrl.resolveHotWord(ctx, library.ID)
	if response != nil {
		return response
	}

	return ctrl.ApiSuccess(ctx, hotWord)
}

// CreateHotWord 向热词库中新增热词
func (ctrl *HotWordController) CreateHotWord(ctx http.Context) http.Response {
	library, response := ctrl.resolveLibrary(ctx)
	if response != nil {
		return response
	}

	var wordPost hotwords.WordPostRequest
	errors, err := ctx.Request().ValidateRequest(&wordPost)
	if err != nil {
		return ctrl.ApiErrorMsg(ctx, err.Error())
	}
	if errors != nil {
		return ctrl.ApiErrorMsg(ctx, ctrl.GetFirstError(errors))
	}

	exists, err := ctrl.hotWordService.IsWordExists(library.ID, wordPost.Word, 0)
	if err != nil {
		facades.Log().WithContext(ctx).Error("校验热词失败: " + err.Error())
		return ctrl.ApiErrorMsg(ctx, "创建热词失败")
	}
	if exists {
		return ctrl.ApiErrorMsg(ctx, "该热词已经存在")
	}

	hotWord := models.HotWord{
		LibraryID: library.ID,
		Word:      wordPost.Word,
		Weight:    wordPost.Weight,
	}

	if err := ctrl.hotWordService.AddHotWord(&hotWord); err != nil {
		facades.Log().WithContext(ctx).Error("创建热词失败: " + err.Error())
		return ctrl.ApiErrorMsg(ctx, "创建热词失败")
	}

	if err := ctrl.libraryService.RefreshWordCount(library.ID); err != nil {
		facades.Log().WithContext(ctx).Warning("更新热词数量失败: " + err.Error())
	}

	facades.Log().WithContext(ctx).Info("创建热词成功: " + hotWord.Word)

	return ctrl.ApiSuccess(ctx, hotWord)
}

// UpdateHotWord 更新热词，仅更新传入的字段
func (ctrl *HotWordController) UpdateHotWord(ctx http.Context) http.Response {
	library, response := ctrl.resolveLibrary(ctx)
	if response != nil {
		return response
	}

	hotWord, response := ctrl.resolveHotWord(ctx, library.ID)
	if response != nil {
		return response
	}

	var wordUpdate hotwords.WordUpdateRequest
	errors, err := ctx.Request().ValidateRequest(&wordUpdate)
	if err != nil {
		return ctrl.ApiErrorMsg(ctx, err.Error())
	}
	if errors != nil {
		return ctrl.ApiErrorMsg(ctx, ctrl.GetFirstError(errors))
	}

	if wordUpdate.Word != "" && wordUpdate.Word != hotWord.Word {
		exists, err := ctrl.hotWordService.IsWordExists(library.ID, wordUpdate.Word, hotWord.ID)
		if err != nil {
			facades.Log().WithContext(ctx).Error("校验热词失败: " + err.Error())
			return ctrl.ApiErrorMsg(ctx, "更新热词失败")
		}
		if exists {
			return ctrl.ApiErrorMsg(ctx, "该热词已经存在")
		}

		hotWord.Word = wordUpdate.Word
	}

	if wordUpdate.Weight != nil {
		hotWord.Weight = *wordUpdate.Weight
	}

	if err := ctrl.hotWordService.UpdateHotWord(&hotWord); err != nil {
		facades.Log().WithContext(ctx).Error("更新热词失败: " + err.Error())
		return ctrl.ApiErrorMsg(ctx, "更新热词失败")
	}

	facades.Log().WithContext(ctx).Info("更新热词成功: " + hotWord.Word)

	return ctrl.ApiSuccess(ctx, hotWord)
}

// DeleteHotWord 删除热词（软删除）
func (ctrl *HotWordController) DeleteHotWord(ctx http.Context) http.Response {
	library, response := ctrl.resolveLibrary(ctx)
	if response != nil {
		return response
	}

	hotWord, response := ctrl.resolveHotWord(ctx, library.ID)
	if response != nil {
		return response
	}

	result, err := ctrl.hotWordService.DeleteHotWordById(int(hotWord.ID))
	if err != nil {
		facades.Log().WithContext(ctx).Error("删除热词失败: " + err.Error())
		return ctrl.ApiErrorMsg(ctx, "删除热词失败")
	}
	if result.RowsAffected == 0 {
		return ctrl.ApiErrorMsg(ctx, "删除失败")
	}

	if err := ctrl.libraryService.RefreshWordCount(library.ID); err != nil {
		facades.Log().WithContext(ctx).Warning("更新热词数量失败: " + err.Error())
	}

	facades.Log().WithContext(ctx).Info("删除热词成功: " + hotWord.Word)

	return ctrl.ApiSuccess(ctx, map[string]string{})
}

// resolveLibrary 解析并校验路由中的热词库
func (ctrl *HotWordController) resolveLibrary(ctx http.Context) (models.HotWordLibrary, http.Response) {
	libraryID := ctx.Request().RouteInt("id")
	if libraryID <= 0 {
		return models.HotWordLibrary{}, ctrl.ApiErrorMsg(ctx, "热词库ID不正确")
	}

	library, err := ctrl.libraryService.GetLibraryById(libraryID)
	if err != nil {
		return models.HotWordLibrary{}, ctrl.ApiErrorMsg(ctx, "热词库不存在")
	}

	return library, nil
}

// resolveHotWord 解析并校验路由中的热词，同时确保其归属于指定热词库
func (ctrl *HotWordController) resolveHotWord(ctx http.Context, libraryID uint) (models.HotWord, http.Response) {
	id := ctx.Request().RouteInt("wordId")
	if id <= 0 {
		return models.HotWord{}, ctrl.ApiErrorMsg(ctx, "热词ID不正确")
	}

	hotWord, err := ctrl.hotWordService.GetHotWordById(id)
	if err != nil {
		return models.HotWord{}, ctrl.ApiErrorMsg(ctx, "热词不存在")
	}

	if hotWord.LibraryID != libraryID {
		return models.HotWord{}, ctrl.ApiErrorMsg(ctx, "热词不属于该热词库")
	}

	return hotWord, nil
}

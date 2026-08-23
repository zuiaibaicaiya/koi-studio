package api

import (
	"github.com/goravel/framework/contracts/http"

	"koi-server/app/facades"
	"koi-server/app/services"
)

// DashboardController 仪表盘统计控制器
type DashboardController struct {
	BaseController
	dashboardService *services.DashboardService
}

func NewDashboardController() *DashboardController {
	return &DashboardController{
		dashboardService: services.NewDashboardService(),
	}
}

// Stats 返回仪表盘聚合统计数据（概览指标、近 7 日趋势、各类分布与最近会议）。
//
// @Route GET /api/dashboard/stats
func (ctrl *DashboardController) Stats(ctx http.Context) http.Response {
	stats, err := ctrl.dashboardService.Stats()
	if err != nil {
		facades.Log().WithContext(ctx).Error("获取仪表盘统计失败: " + err.Error())
		return ctrl.ApiErrorMsg(ctx, "获取仪表盘统计失败")
	}

	return ctrl.ApiSuccess(ctx, stats)
}

package api

import (
	"github.com/goravel/framework/contracts/http"

	"koi-server/app/facades"
)

type AudioController struct {
	*BaseController
}

func NewAudioController() *AudioController {
	return &AudioController{}
}

// Serve 直接返回音频文件内容，供前端通过 /audio/{file} 直接访问、播放。
//
// 音频文件由存储磁盘管理，路由与磁盘根目录解耦，确保在不同运行环境下均可正常访问。
func (c *AudioController) Serve(ctx http.Context) http.Response {
	file := ctx.Request().Route("file")
	if file == "" {
		return c.ApiErrorMsg(ctx, "音频不存在")
	}

	disk := facades.Storage().Disk(facades.Config().GetString("audio.storage.disk", "audio"))
	if !disk.Exists(file) {
		return c.ApiErrorMsg(ctx, "音频不存在")
	}

	content, err := disk.Get(file)
	if err != nil {
		facades.Log().WithContext(ctx).Error("读取音频文件失败: " + err.Error())
		return c.ApiErrorMsg(ctx, "读取音频失败")
	}

	mime, _ := disk.MimeType(file)
	if mime == "" {
		mime = "application/octet-stream"
	}

	return ctx.Response().Data(http.StatusOK, mime, []byte(content))
}

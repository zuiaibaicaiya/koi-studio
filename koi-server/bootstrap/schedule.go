package bootstrap

import (
	"fmt"
	"time"

	"github.com/goravel/framework/contracts/schedule"

	"koi-server/app/facades"
)

// Schedule 注册应用的定时任务。
func Schedule() []schedule.Event {
	return []schedule.Event{
		// 回收空闲转写会话。
		//
		// 客户端异常断线（断网、进程被杀）时不会触发 disconnect 事件，
		// 若不回收会残留识别流、临时文件与工作协程。改用框架调度器统一管理，
		// 取代此前服务内部自建的 ticker 协程，便于观测与统一治理。
		facades.Schedule().Call(func() {
			timeout := time.Duration(facades.Config().GetInt("audio.cleanup.idle_timeout", 10)) * time.Minute
			if released := facades.Audio().CleanupInactive(timeout); released > 0 {
				facades.Log().Info(fmt.Sprintf("audio: reclaimed %d idle session(s)", released))
			}
		}).
			Name("audio:cleanup-idle-sessions").
			EveryFiveMinutes().
			SkipIfStillRunning(),
	}
}

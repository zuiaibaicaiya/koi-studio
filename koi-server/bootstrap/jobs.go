package bootstrap

import (
	"github.com/goravel/framework/contracts/queue"

	"koi-server/app/jobs"
)

// Jobs 注册可入队执行的任务。
func Jobs() []queue.Job {
	return []queue.Job{
		&jobs.ArchiveRecording{},
	}
}

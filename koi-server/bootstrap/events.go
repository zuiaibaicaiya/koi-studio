package bootstrap

import (
	"github.com/goravel/framework/contracts/event"

	"koi-server/app/events"
	"koi-server/app/listeners"
)

// Events 注册事件与监听器的映射关系。
func Events() map[event.Event][]event.Listener {
	return map[event.Event][]event.Listener{
		&events.TranscriptGenerated{}: {
			&listeners.BroadcastTranscript{},
		},
	}
}

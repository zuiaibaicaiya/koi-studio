package bootstrap

import (
	"github.com/goravel/framework/ai"
	"github.com/goravel/framework/auth"
	"github.com/goravel/framework/cache"
	"github.com/goravel/framework/contracts/foundation"
	"github.com/goravel/framework/crypt"
	"github.com/goravel/framework/database"
	"github.com/goravel/framework/event"
	"github.com/goravel/framework/filesystem"
	"github.com/goravel/framework/grpc"
	"github.com/goravel/framework/hash"
	"github.com/goravel/framework/http"
	"github.com/goravel/framework/log"
	"github.com/goravel/framework/mail"
	"github.com/goravel/framework/queue"
	"github.com/goravel/framework/route"
	"github.com/goravel/framework/schedule"
	"github.com/goravel/framework/session"
	"github.com/goravel/framework/telemetry"
	"github.com/goravel/framework/testing"
	"github.com/goravel/framework/translation"
	"github.com/goravel/framework/validation"
	"github.com/goravel/framework/view"
	"github.com/goravel/gin"
	"github.com/goravel/openai"
	"github.com/goravel/postgres"
	"github.com/goravel/sqlite"

	"koi-server/app/providers"
	"koi-server/packages/socketio"
)

func Providers() []foundation.ServiceProvider {
	return []foundation.ServiceProvider{
		&log.ServiceProvider{},
		&cache.ServiceProvider{},
		&hash.ServiceProvider{},
		&http.ServiceProvider{},
		&session.ServiceProvider{},
		&filesystem.ServiceProvider{},
		&validation.ServiceProvider{},
		&view.ServiceProvider{},
		&route.ServiceProvider{},
		&gin.ServiceProvider{},
		&ai.ServiceProvider{},
		&openai.ServiceProvider{},
		&database.ServiceProvider{},
		&postgres.ServiceProvider{},
		&auth.ServiceProvider{},
		&crypt.ServiceProvider{},
		&queue.ServiceProvider{},
		&event.ServiceProvider{},
		&grpc.ServiceProvider{},
		&translation.ServiceProvider{},
		&mail.ServiceProvider{},
		&schedule.ServiceProvider{},
		&telemetry.ServiceProvider{},
		&testing.ServiceProvider{},
		&sqlite.ServiceProvider{},

		// 应用自身的服务提供者，需排在框架提供者之后，
		// 以保证其 Register/Boot 阶段可以安全使用 config、log、storage 等基础设施。
		&socketio.ServiceProvider{},
		&providers.AudioServiceProvider{},
	}
}

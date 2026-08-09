package bootstrap

import (
	contractsfoundation "github.com/goravel/framework/contracts/foundation"
	"github.com/goravel/framework/foundation"

	"koi-server/config"
	"koi-server/routes"
)

func Boot() contractsfoundation.Application {
	return foundation.Setup().
		WithSeeders(Seeders).
		WithMigrations(Migrations).
		WithRouting(func() {
			routes.Web()
			routes.Api()
			routes.Grpc()
		}).
		WithProviders(Providers).
		WithConfig(config.Boot).
		WithEvents(Events).
		WithJobs(Jobs).
		WithSchedule(Schedule).
		Create()
}

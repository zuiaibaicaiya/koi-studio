package bootstrap

import (
	"github.com/goravel/framework/contracts/database/seeder"

	"koi-server/database/seeders"
)

func Seeders() []seeder.Seeder {
	return []seeder.Seeder{
		&seeders.UserSeeder{},
	}
}

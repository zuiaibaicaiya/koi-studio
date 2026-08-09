package bootstrap

import (
	"github.com/goravel/framework/contracts/database/schema"

	"koi-server/database/migrations"
)

func Migrations() []schema.Migration {
	return []schema.Migration{
		&migrations.M20210101000001CreateJobsTable{},
		&migrations.M20260225113619CreateUsersTable{},
	}
}

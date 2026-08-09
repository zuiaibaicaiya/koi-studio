package migrations

import (
	"github.com/goravel/framework/contracts/database/schema"

	"koi-server/app/facades"
)

type M20260225113619CreateUsersTable struct{}

// Signature The unique signature for the migration.
func (r *M20260225113619CreateUsersTable) Signature() string {
	return "20260225113619_create_users_table"
}

// Up Run the migrations.
func (r *M20260225113619CreateUsersTable) Up() error {
	if facades.Schema().HasTable("users") {
		return nil
	}

	return facades.Schema().Create("users", func(table schema.Blueprint) {
		table.ID()
		table.String("username").Comment("用户名")
		table.String("password").Comment("密码")
		table.String("nickname").Nullable().Comment("昵称")
		table.String("email").Nullable().Comment("邮箱")
		table.String("phone").Nullable().Comment("手机号")
		table.String("avatar").Nullable().Comment("头像")
		table.String("status").Default("active").Comment("状态：active-启用，inactive-禁用")
		table.SoftDeletes()
		table.TimestampsTz()

		table.Unique("username")
	})
}

// Down Reverse the migrations.
func (r *M20260225113619CreateUsersTable) Down() error {
	return facades.Schema().DropIfExists("users")
}

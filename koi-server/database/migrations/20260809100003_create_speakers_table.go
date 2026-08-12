package migrations

import (
	"github.com/goravel/framework/contracts/database/schema"

	"koi-server/app/facades"
)

type M20260809100003CreateSpeakersTable struct{}

// Signature The unique signature for the migration.
func (r *M20260809100003CreateSpeakersTable) Signature() string {
	return "20260809100003_create_speakers_table"
}

// Up Run the migrations.
func (r *M20260809100003CreateSpeakersTable) Up() error {
	if facades.Schema().HasTable("speakers") {
		return nil
	}

	return facades.Schema().Create("speakers", func(table schema.Blueprint) {
		table.ID()
		table.String("name").Comment("说话人名称，同时作为声纹库检索标识")
		table.String("description").Nullable().Comment("说话人描述")
		table.Integer("embedding_dim").Default(0).Comment("声纹特征维度")
		table.Integer("audio_count").Default(0).Comment("已注册声纹音频数量")
		table.SoftDeletes()
		table.TimestampsTz()

		table.Index("name")
	})
}

// Down Reverse the migrations.
func (r *M20260809100003CreateSpeakersTable) Down() error {
	return facades.Schema().DropIfExists("speakers")
}

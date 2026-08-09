package migrations

import (
	"github.com/goravel/framework/contracts/database/schema"

	"koi-server/app/facades"
)

type M20260809100001CreateHotWordLibrariesTable struct{}

// Signature The unique signature for the migration.
func (r *M20260809100001CreateHotWordLibrariesTable) Signature() string {
	return "20260809100001_create_hot_word_libraries_table"
}

// Up Run the migrations.
func (r *M20260809100001CreateHotWordLibrariesTable) Up() error {
	if facades.Schema().HasTable("hot_word_libraries") {
		return nil
	}

	return facades.Schema().Create("hot_word_libraries", func(table schema.Blueprint) {
		table.ID()
		table.String("name").Comment("热词库名称")
		table.String("description").Nullable().Comment("热词库描述")
		table.String("status").Default("active").Comment("状态：active-启用，inactive-禁用")
		table.Integer("word_count").Default(0).Comment("热词数量")
		table.SoftDeletes()
		table.TimestampsTz()

		table.Index("name")
	})
}

// Down Reverse the migrations.
func (r *M20260809100001CreateHotWordLibrariesTable) Down() error {
	return facades.Schema().DropIfExists("hot_word_libraries")
}

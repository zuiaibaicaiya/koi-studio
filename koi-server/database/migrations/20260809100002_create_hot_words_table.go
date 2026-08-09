package migrations

import (
	"github.com/goravel/framework/contracts/database/schema"

	"koi-server/app/facades"
)

type M20260809100002CreateHotWordsTable struct{}

// Signature The unique signature for the migration.
func (r *M20260809100002CreateHotWordsTable) Signature() string {
	return "20260809100002_create_hot_words_table"
}

// Up Run the migrations.
func (r *M20260809100002CreateHotWordsTable) Up() error {
	if facades.Schema().HasTable("hot_words") {
		return nil
	}

	return facades.Schema().Create("hot_words", func(table schema.Blueprint) {
		table.ID()
		table.UnsignedBigInteger("library_id").Comment("所属热词库ID")
		table.String("word").Comment("热词内容")
		table.Integer("weight").Default(0).Comment("热词权重")
		table.SoftDeletes()
		table.TimestampsTz()

		table.Index("library_id")
		table.Index("word")
	})
}

// Down Reverse the migrations.
func (r *M20260809100002CreateHotWordsTable) Down() error {
	return facades.Schema().DropIfExists("hot_words")
}

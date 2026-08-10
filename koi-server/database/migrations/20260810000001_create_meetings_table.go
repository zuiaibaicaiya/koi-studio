package migrations

import (
	"github.com/goravel/framework/contracts/database/schema"

	"koi-server/app/facades"
)

type M20260810000001CreateMeetingsTable struct{}

func (r *M20260810000001CreateMeetingsTable) Signature() string {
	return "20260810000001_create_meetings_table"
}

func (r *M20260810000001CreateMeetingsTable) Up() error {
	if facades.Schema().HasTable("meetings") {
		return nil
	}

	return facades.Schema().Create("meetings", func(table schema.Blueprint) {
		table.ID()
		table.String("name").Comment("会议名称（纯文本）")
		table.Text("participants").Nullable().Comment("参会人员（纯文本）")
		table.String("speaker_ids").Nullable().Comment("说话人ID列表，逗号分隔，关联speakers表")
		table.String("hot_word_library_ids").Nullable().Comment("关联热词库ID列表，逗号分隔")
		table.TimestampTz("start_time").Comment("开始时间")
		table.TimestampTz("end_time").Comment("结束时间")
		table.String("status").Default("created").Comment("状态：created-已创建，ongoing-进行中，finished-已结束")
		table.UnsignedBigInteger("created_by").Default(0).Comment("创建人ID，关联users表")
		table.SoftDeletes()
		table.TimestampsTz()

		table.Index("name")
		table.Index("status")
		table.Index("start_time")
	})
}

func (r *M20260810000001CreateMeetingsTable) Down() error {
	return facades.Schema().DropIfExists("meetings")
}

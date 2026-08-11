package migrations

import (
	"github.com/goravel/framework/contracts/database/schema"

	"koi-server/app/facades"
)

type M20260811100001CreateMeetingTranscriptsTable struct{}

// Signature 迁移签名
func (r *M20260811100001CreateMeetingTranscriptsTable) Signature() string {
	return "20260811100001_create_meeting_transcripts_table"
}

// Up 执行迁移
func (r *M20260811100001CreateMeetingTranscriptsTable) Up() error {
	if facades.Schema().HasTable("meeting_transcripts") {
		return nil
	}

	return facades.Schema().Create("meeting_transcripts", func(table schema.Blueprint) {
		table.ID()
		table.UnsignedBigInteger("meeting_id").Comment("关联会议ID")
		table.UnsignedBigInteger("speaker_id").Nullable().Comment("说话人ID，未识别时为null")
		table.String("speaker_name").Default("未知说话人").Comment("说话人名称，未识别时为'未知说话人'")
		table.Text("text").Comment("转写文本内容")
		table.BigInteger("start_ms").Default(0).Comment("相对音频开头的起始毫秒偏移")
		table.BigInteger("end_ms").Default(0).Comment("相对音频开头的结束毫秒偏移")
		table.Text("word_timestamps").Nullable().Comment("词级时间戳，JSON数组")
		table.TinyInteger("is_final").Default(1).Comment("是否为最终结果：1-是，0-中间结果")
		table.SoftDeletes()
		table.TimestampsTz()

		table.Index("meeting_id")
		table.Index("speaker_id")
	})
}

// Down 回滚迁移
func (r *M20260811100001CreateMeetingTranscriptsTable) Down() error {
	return facades.Schema().DropIfExists("meeting_transcripts")
}

package migrations

import (
	"github.com/goravel/framework/contracts/database/schema"

	"koi-server/app/facades"
)

type M20260809100004CreateSpeakerAudiosTable struct{}

// Signature The unique signature for the migration.
func (r *M20260809100004CreateSpeakerAudiosTable) Signature() string {
	return "20260809100004_create_speaker_audios_table"
}

// Up Run the migrations.
func (r *M20260809100004CreateSpeakerAudiosTable) Up() error {
	if facades.Schema().HasTable("speaker_audios") {
		return nil
	}

	return facades.Schema().Create("speaker_audios", func(table schema.Blueprint) {
		table.ID()
		table.UnsignedBigInteger("speaker_id").Comment("所属说话人ID")
		table.String("file_name").Comment("上传时的原始文件名")
		table.String("file_path").Comment("音频在存储磁盘中的相对路径")
		table.BigInteger("file_size").Default(0).Comment("音频文件大小（字节）")
		table.Integer("sample_rate").Default(0).Comment("提取声纹时使用的采样率")
		table.Double("duration").Default(0).Comment("音频时长（秒）")
		table.Integer("dim").Default(0).Comment("声纹特征维度")
		table.Text("embedding").Nullable().Comment("声纹特征向量，JSON 数组文本")
		table.String("remark").Nullable().Comment("备注")
		table.SoftDeletes()
		table.TimestampsTz()

		table.Index("speaker_id")
	})
}

// Down Reverse the migrations.
func (r *M20260809100004CreateSpeakerAudiosTable) Down() error {
	return facades.Schema().DropIfExists("speaker_audios")
}

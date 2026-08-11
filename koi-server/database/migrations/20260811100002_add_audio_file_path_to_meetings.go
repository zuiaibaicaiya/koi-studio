package migrations

import (
	"koi-server/app/facades"
)

type M20260811100002AddAudioFilePathToMeetings struct{}

// Signature 迁移签名
func (r *M20260811100002AddAudioFilePathToMeetings) Signature() string {
	return "20260811100002_add_audio_file_path_to_meetings"
}

// Up 执行迁移：为 meetings 表新增 audio_file_path 字段
func (r *M20260811100002AddAudioFilePathToMeetings) Up() error {
	if !facades.Schema().HasTable("meetings") {
		return nil
	}

	// 使用原始 SQL 安全添加列（Goravel schema 可能不支持 ALTER 操作）
	return facades.Schema().Sql("ALTER TABLE meetings ADD COLUMN audio_file_path VARCHAR(500) NULL DEFAULT NULL")
}

// Down 回滚迁移
func (r *M20260811100002AddAudioFilePathToMeetings) Down() error {
	if !facades.Schema().HasTable("meetings") {
		return nil
	}

	return facades.Schema().Sql("ALTER TABLE meetings DROP COLUMN audio_file_path")
}

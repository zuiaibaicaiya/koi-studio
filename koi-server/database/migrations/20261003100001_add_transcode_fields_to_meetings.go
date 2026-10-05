package migrations

import (
	"koi-server/app/facades"
)

type M20261003100001AddTranscodeFieldsToMeetings struct{}

// Signature 迁移签名
func (r *M20261003100001AddTranscodeFieldsToMeetings) Signature() string {
	return "20261003100001_add_transcode_fields_to_meetings"
}

// Up 执行迁移：为 meetings 表新增音频转码相关字段。
//
// SQLite 的 ALTER TABLE 每条语句只允许添加一列，因此逐条执行。
func (r *M20261003100001AddTranscodeFieldsToMeetings) Up() error {
	if !facades.Schema().HasTable("meetings") {
		return nil
	}

	columns := []struct {
		name string
		ddl  string
	}{
		{"original_file_path", "ALTER TABLE meetings ADD COLUMN original_file_path VARCHAR(500) NULL DEFAULT NULL"},
		{"transcode_status", "ALTER TABLE meetings ADD COLUMN transcode_status VARCHAR(20) NULL DEFAULT NULL"},
		{"transcode_progress", "ALTER TABLE meetings ADD COLUMN transcode_progress INT NOT NULL DEFAULT 0"},
		{"transcode_error", "ALTER TABLE meetings ADD COLUMN transcode_error VARCHAR(1000) NULL DEFAULT NULL"},
		{"transcode_attempts", "ALTER TABLE meetings ADD COLUMN transcode_attempts INT NOT NULL DEFAULT 0"},
	}

	for _, col := range columns {
		if facades.Schema().HasColumn("meetings", col.name) {
			continue
		}
		if err := facades.Schema().Sql(col.ddl); err != nil {
			return err
		}
	}
	return nil
}

// Down 回滚迁移
func (r *M20261003100001AddTranscodeFieldsToMeetings) Down() error {
	if !facades.Schema().HasTable("meetings") {
		return nil
	}

	for _, name := range []string{"transcode_attempts", "transcode_error", "transcode_progress", "transcode_status", "original_file_path"} {
		if !facades.Schema().HasColumn("meetings", name) {
			continue
		}
		if err := facades.Schema().Sql("ALTER TABLE meetings DROP COLUMN " + name); err != nil {
			return err
		}
	}
	return nil
}

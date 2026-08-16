package migrations

import (
	"koi-server/app/facades"
)

type M20260816000001AddModeToMeetings struct{}

// Signature 迁移签名
func (r *M20260816000001AddModeToMeetings) Signature() string {
	return "20260816000001_add_mode_to_meetings"
}

// Up 执行迁移：为 meetings 表新增 mode 字段（live-实时会议 / audio-音频转写）
func (r *M20260816000001AddModeToMeetings) Up() error {
	if !facades.Schema().HasTable("meetings") {
		return nil
	}
	if facades.Schema().HasColumn("meetings", "mode") {
		return nil
	}

	// 默认值为 live，保证存量实时会议数据兼容
	if err := facades.Schema().Sql("ALTER TABLE meetings ADD COLUMN mode VARCHAR(20) NOT NULL DEFAULT 'live'"); err != nil {
		return err
	}

	// 模式筛选为高频查询，建立索引（跨库通用语法）
	return facades.Schema().Sql("CREATE INDEX idx_meetings_mode ON meetings (mode)")
}

// Down 回滚迁移
func (r *M20260816000001AddModeToMeetings) Down() error {
	if !facades.Schema().HasTable("meetings") {
		return nil
	}
	if !facades.Schema().HasColumn("meetings", "mode") {
		return nil
	}

	// 不同数据库 DROP INDEX 语法略有差异，忽略失败继续删除列
	_ = facades.Schema().Sql("DROP INDEX idx_meetings_mode ON meetings")

	return facades.Schema().Sql("ALTER TABLE meetings DROP COLUMN mode")
}

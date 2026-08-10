package migrations

import (
	"github.com/goravel/framework/contracts/database/schema"

	"koi-server/app/facades"
)

type M20260809100005AddValidDurationToSpeakerAudiosTable struct{}

// Signature The unique signature for the migration.
func (r *M20260809100005AddValidDurationToSpeakerAudiosTable) Signature() string {
	return "20260809100005_add_valid_duration_to_speaker_audios_table"
}

// Up Run the migrations.
func (r *M20260809100005AddValidDurationToSpeakerAudiosTable) Up() error {
	if !facades.Schema().HasTable("speaker_audios") ||
		facades.Schema().HasColumn("speaker_audios", "valid_duration") {
		return nil
	}

	return facades.Schema().Table("speaker_audios", func(table schema.Blueprint) {
		table.Double("valid_duration").Default(0).Comment("去除静音后的有效语音时长（秒）")
	})
}

// Down Reverse the migrations.
func (r *M20260809100005AddValidDurationToSpeakerAudiosTable) Down() error {
	if !facades.Schema().HasColumn("speaker_audios", "valid_duration") {
		return nil
	}

	return facades.Schema().Table("speaker_audios", func(table schema.Blueprint) {
		table.DropColumn("valid_duration")
	})
}

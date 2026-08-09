package models

import "github.com/goravel/framework/database/orm"

// HotWordLibrary 热词库模型
type HotWordLibrary struct {
	orm.Model
	orm.SoftDeletes
	// Name 热词库名称（导入 Excel 时取文件名）
	Name string `json:"name" example:"通用行业热词"`
	// Description 热词库描述
	Description string `json:"description" example:"用于语音识别的通用行业热词"`
	// Status 状态：active-启用，inactive-禁用
	Status string `json:"status" example:"active"`
	// WordCount 热词数量
	WordCount int `json:"word_count" example:"120"`
	// HotWords 关联的热词列表（一对多）
	HotWords []HotWord `json:"hotWords,omitempty" gorm:"foreignKey:LibraryID"`
}

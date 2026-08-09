package models

import "github.com/goravel/framework/database/orm"

// HotWord 热词模型，归属于某个热词库
type HotWord struct {
	orm.Model
	orm.SoftDeletes
	// LibraryID 所属热词库 ID
	LibraryID uint `json:"libraryId" example:"1"`
	// Word 热词内容
	Word string `json:"word" example:"人工智能"`
	// Weight 热词权重
	Weight int `json:"weight" example:"10"`
}

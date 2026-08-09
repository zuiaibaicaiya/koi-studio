package services

import (
	"github.com/goravel/framework/contracts/database/db"

	"koi-server/app/facades"
	"koi-server/app/models"
)

// HotWordService 热词服务，封装热词相关的数据访问逻辑
type HotWordService struct {
}

func NewHotWordService() *HotWordService {
	return &HotWordService{}
}

// GetHotWordList 分页获取指定热词库下的热词列表，支持关键词筛选
func (hotWordService *HotWordService) GetHotWordList(libraryID uint, page int, pageSize int, keyword string) (hotWords []models.HotWord, total int64, err error) {
	query := facades.Orm().Query().Where("library_id = ?", libraryID)

	if keyword != "" {
		query = query.Where("word LIKE ?", "%"+keyword+"%")
	}

	err = query.OrderByDesc("weight").OrderByDesc("id").Paginate(page, pageSize, &hotWords, &total)

	return hotWords, total, err
}

// AddHotWord 创建热词
func (hotWordService *HotWordService) AddHotWord(hotWord *models.HotWord) error {
	return facades.Orm().Query().Create(hotWord)
}

// GetHotWordById 根据 ID 查询热词，不存在时返回错误
func (hotWordService *HotWordService) GetHotWordById(id int) (hotWord models.HotWord, err error) {
	err = facades.Orm().Query().FindOrFail(&hotWord, id)

	return hotWord, err
}

// UpdateHotWord 更新热词
func (hotWordService *HotWordService) UpdateHotWord(hotWord *models.HotWord) error {
	return facades.Orm().Query().Save(hotWord)
}

// DeleteHotWordById 根据 ID 软删除热词
func (hotWordService *HotWordService) DeleteHotWordById(id int) (*db.Result, error) {
	return facades.Orm().Query().Model(&models.HotWord{}).Where("id = ?", id).Delete()
}

// IsWordExists 判断热词在指定热词库内是否已存在，excludeID 大于 0 时排除该热词（用于更新场景）
func (hotWordService *HotWordService) IsWordExists(libraryID uint, word string, excludeID uint) (bool, error) {
	query := facades.Orm().Query().Model(&models.HotWord{}).
		Where("library_id = ?", libraryID).
		Where("word = ?", word)

	if excludeID > 0 {
		query = query.Where("id != ?", excludeID)
	}

	count, err := query.Count()

	return count > 0, err
}

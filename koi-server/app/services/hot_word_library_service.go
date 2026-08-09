package services

import (
	"github.com/goravel/framework/contracts/database/db"
	"github.com/goravel/framework/contracts/database/orm"

	"koi-server/app/facades"
	"koi-server/app/models"
)

// HotWordLibraryService 热词库服务，封装热词库相关的数据访问逻辑
type HotWordLibraryService struct {
}

func NewHotWordLibraryService() *HotWordLibraryService {
	return &HotWordLibraryService{}
}

// GetLibraryList 分页获取热词库列表，支持关键词与状态筛选
func (libraryService *HotWordLibraryService) GetLibraryList(page int, pageSize int, keyword string, status string) (libraries []models.HotWordLibrary, total int64, err error) {
	query := facades.Orm().Query()

	if keyword != "" {
		like := "%" + keyword + "%"
		query = query.Where("name LIKE ? OR description LIKE ?", like, like)
	}

	if status != "" {
		query = query.Where("status = ?", status)
	}

	err = query.OrderByDesc("id").Paginate(page, pageSize, &libraries, &total)
	if err != nil {
		return libraries, total, err
	}

	// 实时统计每个热词库的热词数量，避免依赖容易失准的冗余 word_count 字段
	for i := range libraries {
		count, cerr := facades.Orm().Query().
			Model(&models.HotWord{}).
			Where("library_id = ?", libraries[i].ID).
			Count()
		if cerr != nil {
			facades.Log().Warning("统计热词数量失败: " + cerr.Error())
			continue
		}
		libraries[i].WordCount = int(count)
	}

	return libraries, total, err
}

// AddLibrary 创建热词库
func (libraryService *HotWordLibraryService) AddLibrary(library *models.HotWordLibrary) error {
	return facades.Orm().Query().Create(library)
}

// GetLibraryById 根据 ID 查询热词库，不存在时返回错误
func (libraryService *HotWordLibraryService) GetLibraryById(id int) (library models.HotWordLibrary, err error) {
	err = facades.Orm().Query().FindOrFail(&library, id)
	if err != nil {
		return library, err
	}

	// 实时统计热词数量，保证详情与列表一致
	if count, cerr := facades.Orm().Query().
		Model(&models.HotWord{}).
		Where("library_id = ?", library.ID).
		Count(); cerr == nil {
		library.WordCount = int(count)
	}

	return library, err
}

// UpdateLibrary 更新热词库
func (libraryService *HotWordLibraryService) UpdateLibrary(library *models.HotWordLibrary) error {
	return facades.Orm().Query().Save(library)
}

// DeleteLibraryById 根据 ID 软删除热词库，同时软删除其下所有热词
func (libraryService *HotWordLibraryService) DeleteLibraryById(id int) (*db.Result, error) {
	var result *db.Result

	err := facades.Orm().Transaction(func(tx orm.Query) error {
		if _, err := tx.Model(&models.HotWord{}).Where("library_id = ?", id).Delete(); err != nil {
			return err
		}

		deleted, err := tx.Model(&models.HotWordLibrary{}).Where("id = ?", id).Delete()
		if err != nil {
			return err
		}

		result = deleted

		return nil
	})

	return result, err
}

// IsLibraryNameExists 判断热词库名称是否已存在，excludeID 大于 0 时排除该热词库（用于更新场景）
func (libraryService *HotWordLibraryService) IsLibraryNameExists(name string, excludeID uint) (bool, error) {
	query := facades.Orm().Query().Model(&models.HotWordLibrary{}).Where("name = ?", name)

	if excludeID > 0 {
		query = query.Where("id != ?", excludeID)
	}

	count, err := query.Count()

	return count > 0, err
}

// CreateLibraryWithWords 在同一事务内创建热词库及其热词，用于 Excel 导入场景
func (libraryService *HotWordLibraryService) CreateLibraryWithWords(library *models.HotWordLibrary, words []models.HotWord) error {
	return facades.Orm().Transaction(func(tx orm.Query) error {
		library.WordCount = len(words)

		if err := tx.Create(library); err != nil {
			return err
		}

		if len(words) == 0 {
			return nil
		}

		for index := range words {
			words[index].LibraryID = library.ID
		}

		return tx.Create(&words)
	})
}

// RefreshWordCount 重新统计并更新热词库的热词数量
func (libraryService *HotWordLibraryService) RefreshWordCount(libraryID uint) error {
	count, err := facades.Orm().Query().Model(&models.HotWord{}).Where("library_id = ?", libraryID).Count()
	if err != nil {
		return err
	}

	_, err = facades.Orm().Query().Model(&models.HotWordLibrary{}).Where("id = ?", libraryID).Update("word_count", count)

	return err
}

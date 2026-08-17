package services

import (
	"errors"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/xuri/excelize/v2"

	"koi-server/app/models"
)

// HotWordExcelService 热词 Excel 解析服务
type HotWordExcelService struct {
}

func NewHotWordExcelService() *HotWordExcelService {
	return &HotWordExcelService{}
}

// LibraryNameFromFileName 取 Excel 文件名（去掉扩展名）作为热词库名称
func (excelService *HotWordExcelService) LibraryNameFromFileName(fileName string) string {
	name := filepath.Base(strings.TrimSpace(fileName))
	name = strings.TrimSuffix(name, filepath.Ext(name))

	return strings.TrimSpace(name)
}

// ParseHotWords 解析 Excel 文件中的热词。
//
// 约定第一个工作表的第一列为热词内容，第二列为权重（可选，缺省为 0）。
// 首行为标题栏，固定跳过；同一文件内的重复热词会被去重。
func (excelService *HotWordExcelService) ParseHotWords(filePath string) ([]models.HotWord, error) {
	file, err := excelize.OpenFile(filePath)
	if err != nil {
		return nil, errors.New("Excel文件解析失败")
	}
	defer func() {
		_ = file.Close()
	}()

	sheets := file.GetSheetList()
	if len(sheets) == 0 {
		return nil, errors.New("Excel文件没有可用的工作表")
	}

	rows, err := file.GetRows(sheets[0])
	if err != nil {
		return nil, errors.New("Excel内容读取失败")
	}

	hotWords := make([]models.HotWord, 0, len(rows))
	existed := make(map[string]struct{}, len(rows))

	for index, row := range rows {
		// 首行为标题栏，固定跳过
		if index == 0 {
			continue
		}

		if len(row) == 0 {
			continue
		}

		word := strings.TrimSpace(row[0])
		if word == "" {
			continue
		}

		if _, ok := existed[word]; ok {
			continue
		}
		existed[word] = struct{}{}

		weight := 0
		if len(row) > 1 {
			if parsed, err := strconv.Atoi(strings.TrimSpace(row[1])); err == nil {
				weight = parsed
			}
		}

		hotWords = append(hotWords, models.HotWord{
			Word:   word,
			Weight: weight,
		})
	}

	if len(hotWords) == 0 {
		return nil, errors.New("Excel文件中没有有效的热词数据")
	}

	return hotWords, nil
}

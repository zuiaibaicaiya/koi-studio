package services

import (
	"sort"

	"github.com/dromara/carbon/v2"

	"koi-server/app/facades"
	"koi-server/app/models"
)

// DashboardStats 仪表盘聚合统计，由 DashboardService 从数据库实时计算。
//
// 早期仪表盘在前端用随机/种子数据拼装，统计口径与后端不一致且无法反映真实业务。
// 这里统一在服务端聚合，前端只负责展示，保证数据口径单一、可审计。
type DashboardStats struct {
	Overview       DashboardOverview  `json:"overview"`
	Trends         DashboardTrend     `json:"trends"`
	UserStatusDist []NameValue        `json:"userStatusDist"`
	HotWordLibDist []NameValue        `json:"hotWordLibDist"`
	TopSpeakers    []NameValue        `json:"topSpeakers"`
	RecentMeetings []DashboardMeeting `json:"recentMeetings"`
}

// DashboardOverview 关键指标概览
type DashboardOverview struct {
	UserTotal           int64 `json:"userTotal"`
	UserActive          int64 `json:"userActive"`
	SpeakerTotal        int64 `json:"speakerTotal"`
	HotWordTotal        int64 `json:"hotWordTotal"`
	HotWordLibraryTotal int64 `json:"hotWordLibraryTotal"`
	MeetingTotal        int64 `json:"meetingTotal"`
	MeetingOngoing      int64 `json:"meetingOngoing"`
	MeetingFinished     int64 `json:"meetingFinished"`
	TranscriptTotal     int64 `json:"transcriptTotal"`
}

// NameValue 通用「名称-数值」结构，用于饼图/柱状图的数据项。
type NameValue struct {
	Label string `json:"label"`
	Value int64  `json:"value"`
}

// DashboardTrend 近 7 日新增会议与转写趋势
type DashboardTrend struct {
	Labels           []string `json:"labels"`
	MeetingSeries    []int64  `json:"meetingSeries"`
	TranscriptSeries []int64  `json:"transcriptSeries"`
}

// DashboardMeeting 最近会议条目
type DashboardMeeting struct {
	ID              uint   `json:"id"`
	Name            string `json:"name"`
	Status          string `json:"status"`
	Mode            string `json:"mode"`
	StartTime       string `json:"startTime"`
	TranscriptCount int64  `json:"transcriptCount"`
}

// DashboardService 仪表盘统计服务
type DashboardService struct{}

func NewDashboardService() *DashboardService {
	return &DashboardService{}
}

// Stats 计算并返回仪表盘全部统计指标。
func (s *DashboardService) Stats() (*DashboardStats, error) {
	stats := &DashboardStats{}

	if err := s.fillOverview(&stats.Overview); err != nil {
		return nil, err
	}
	s.fillTrends(&stats.Trends)
	if err := s.fillUserStatusDist(&stats.UserStatusDist); err != nil {
		return nil, err
	}
	if err := s.fillHotWordLibDist(&stats.HotWordLibDist); err != nil {
		return nil, err
	}
	if err := s.fillTopSpeakers(&stats.TopSpeakers); err != nil {
		return nil, err
	}
	if err := s.fillRecentMeetings(&stats.RecentMeetings); err != nil {
		return nil, err
	}

	return stats, nil
}

// fillOverview 统计各资源总量与会议状态分布。
func (s *DashboardService) fillOverview(o *DashboardOverview) error {
	q := facades.Orm().Query()
	var err error

	if o.UserTotal, err = q.Model(&models.User{}).Count(); err != nil {
		return err
	}
	if o.UserActive, err = q.Model(&models.User{}).Where("status = ?", "active").Count(); err != nil {
		return err
	}
	if o.SpeakerTotal, err = q.Model(&models.Speaker{}).Count(); err != nil {
		return err
	}
	if o.HotWordTotal, err = q.Model(&models.HotWord{}).Count(); err != nil {
		return err
	}
	if o.HotWordLibraryTotal, err = q.Model(&models.HotWordLibrary{}).Count(); err != nil {
		return err
	}
	if o.MeetingTotal, err = q.Model(&models.Meeting{}).Count(); err != nil {
		return err
	}
	if o.MeetingOngoing, err = q.Model(&models.Meeting{}).Where("status = ?", models.MeetingStatusOngoing).Count(); err != nil {
		return err
	}
	if o.MeetingFinished, err = q.Model(&models.Meeting{}).Where("status = ?", models.MeetingStatusFinished).Count(); err != nil {
		return err
	}
	if o.TranscriptTotal, err = q.Model(&models.MeetingTranscript{}).Count(); err != nil {
		return err
	}

	return nil
}

// fillTrends 按「月-日」粒度统计近 7 日新增会议与转写数量。
//
// 为避免 SQLite / MySQL 的日期函数方言差异，这里仅用 created_at 范围过滤，
// 在 Go 侧按日期聚合，保证跨数据库一致。
func (s *DashboardService) fillTrends(t *DashboardTrend) {
	now := carbon.Now()
	since := now.Copy().SubDays(6).StartOfDay()

	labels := make([]string, 0, 7)
	labelIndex := make(map[string]int, 7)
	for i := 0; i < 7; i++ {
		label := since.Copy().AddDays(i).Format("01-02")
		labels = append(labels, label)
		labelIndex[label] = i
	}

	meetingSeries := make([]int64, 7)
	transcriptSeries := make([]int64, 7)

	var meetings []models.Meeting
	if err := facades.Orm().Query().
		Where("created_at >= ?", since.ToDateTimeString()).
		Find(&meetings); err == nil {
		for _, m := range meetings {
			if idx, ok := labelIndex[m.CreatedAt.Format("01-02")]; ok {
				meetingSeries[idx]++
			}
		}
	}

	var transcripts []models.MeetingTranscript
	if err := facades.Orm().Query().
		Where("created_at >= ?", since.ToDateTimeString()).
		Find(&transcripts); err == nil {
		for _, tr := range transcripts {
			if idx, ok := labelIndex[tr.CreatedAt.Format("01-02")]; ok {
				transcriptSeries[idx]++
			}
		}
	}

	t.Labels = labels
	t.MeetingSeries = meetingSeries
	t.TranscriptSeries = transcriptSeries
}

// fillUserStatusDist 统计用户启用 / 禁用分布。
func (s *DashboardService) fillUserStatusDist(dist *[]NameValue) error {
	var total, active int64
	var err error

	if total, err = facades.Orm().Query().Model(&models.User{}).Count(); err != nil {
		return err
	}
	if active, err = facades.Orm().Query().Model(&models.User{}).Where("status = ?", "active").Count(); err != nil {
		return err
	}

	*dist = []NameValue{
		{Label: "启用", Value: active},
		{Label: "禁用", Value: total - active},
	}
	return nil
}

// fillHotWordLibDist 统计各热词库的热词数量，取数量最多的前 8 个。
func (s *DashboardService) fillHotWordLibDist(dist *[]NameValue) error {
	var libraries []models.HotWordLibrary
	if err := facades.Orm().Query().Find(&libraries); err != nil {
		return err
	}

	nameByID := make(map[uint]string, len(libraries))
	for _, lib := range libraries {
		nameByID[lib.ID] = lib.Name
	}

	var hotWords []models.HotWord
	if err := facades.Orm().Query().Find(&hotWords); err != nil {
		return err
	}

	countByLib := make(map[uint]int64, len(libraries))
	for _, w := range hotWords {
		countByLib[w.LibraryID]++
	}

	items := make([]NameValue, 0, len(libraries))
	for id, name := range nameByID {
		items = append(items, NameValue{Label: name, Value: countByLib[id]})
	}

	sort.Slice(items, func(i, j int) bool {
		if items[i].Value == items[j].Value {
			return items[i].Label < items[j].Label
		}
		return items[i].Value > items[j].Value
	})

	top := items
	if len(top) > 8 {
		top = top[:8]
	}
	*dist = top
	return nil
}

// fillTopSpeakers 取声纹样本数（audio_count）最多的前 6 位说话人。
func (s *DashboardService) fillTopSpeakers(dist *[]NameValue) error {
	var speakers []models.Speaker
	var total int64
	if err := facades.Orm().Query().
		OrderByDesc("audio_count").
		OrderByDesc("id").
		Paginate(1, 6, &speakers, &total); err != nil {
		return err
	}

	items := make([]NameValue, 0, len(speakers))
	for _, sp := range speakers {
		items = append(items, NameValue{Label: sp.Name, Value: int64(sp.AudioCount)})
	}
	*dist = items
	return nil
}

// fillRecentMeetings 取最近创建的 5 场会议，并附各自的转写条数。
func (s *DashboardService) fillRecentMeetings(meetings *[]DashboardMeeting) error {
	var list []models.Meeting
	var total int64
	if err := facades.Orm().Query().
		OrderByDesc("id").
		Paginate(1, 5, &list, &total); err != nil {
		return err
	}

	result := make([]DashboardMeeting, 0, len(list))
	for _, m := range list {
		count, cerr := facades.Orm().Query().
			Model(&models.MeetingTranscript{}).
			Where("meeting_id = ?", m.ID).
			Count()
		if cerr != nil {
			facades.Log().Warning("统计会议转写条数失败: " + cerr.Error())
			count = 0
		}

		startTime := ""
		if !m.StartTime.IsZero() {
			startTime = m.StartTime.ToDateTimeString()
		}

		result = append(result, DashboardMeeting{
			ID:              m.ID,
			Name:            m.Name,
			Status:          m.Status,
			Mode:            m.Mode,
			StartTime:       startTime,
			TranscriptCount: count,
		})
	}

	*meetings = result
	return nil
}

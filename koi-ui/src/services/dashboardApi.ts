import http from './http';

/** 通用「名称-数值」结构，对应饼图 / 柱状图的数据项。 */
export interface NameValue {
  label: string;
  value: number;
}

/** 关键指标概览 */
export interface DashboardOverview {
  userTotal: number;
  userActive: number;
  speakerTotal: number;
  hotWordTotal: number;
  hotWordLibraryTotal: number;
  meetingTotal: number;
  meetingOngoing: number;
  meetingFinished: number;
  transcriptTotal: number;
}

/** 近 7 日新增会议与转写趋势 */
export interface DashboardTrend {
  labels: string[];
  meetingSeries: number[];
  transcriptSeries: number[];
}

/** 最近会议条目 */
export interface DashboardMeeting {
  id: number;
  name: string;
  status: string;
  mode: string;
  startTime: string;
  transcriptCount: number;
}

/** 仪表盘聚合统计 */
export interface DashboardStats {
  overview: DashboardOverview;
  trends: DashboardTrend;
  userStatusDist: NameValue[];
  hotWordLibDist: NameValue[];
  topSpeakers: NameValue[];
  recentMeetings: DashboardMeeting[];
}

export const dashboardApi = {
  /** 获取仪表盘全部统计指标 */
  stats: () => http.get<DashboardStats>('/api/dashboard/stats'),
};

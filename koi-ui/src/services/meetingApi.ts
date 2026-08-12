import http from './http';

/** 后端 meetings 模型（见 koi-server/app/models/meeting.go） */
export interface MeetingDTO {
  id: number;
  name: string;
  participants: string;
  speaker_ids: string;
  hot_word_library_ids: string;
  start_time: string;
  end_time: string;
  status: 'created' | 'ongoing' | 'finished';
  created_by: number;
  created_at?: string;
  updated_at?: string;
  audio_url?: string;
}

/** 创建实时会议请求体（见 MeetingPostRequest）。 */
export interface CreateMeetingPayload {
  name: string;
  participants?: string;
  speaker_ids?: string;
  hot_word_library_ids?: string;
  start_time?: string;
  end_time?: string;
}

/** 更新实时会议请求体（见 MeetingUpdateRequest）。 */
export interface UpdateMeetingPayload {
  name?: string;
  participants?: string;
  speaker_ids?: string;
  hot_word_library_ids?: string;
  start_time?: string;
  end_time?: string;
  status?: string;
}

/** 会议列表查询参数（见 MeetingListRequest）。 */
export interface MeetingListParams {
  page?: number;
  pageSize?: number;
  keyword?: string;
  status?: string;
}

/** 分页响应 */
export interface PaginatedResult<T> {
  items: T[];
  total: number;
  page: number;
  pageSize: number;
}

export const meetingApi = {
  listMeetings: (params: MeetingListParams = {}) =>
    http.get<PaginatedResult<MeetingDTO>>('/api/meeting', params),
  createMeeting: (payload: CreateMeetingPayload) =>
    http.post<MeetingDTO>('/api/meeting', payload),
  getMeeting: (id: number) => http.get<MeetingDTO>(`/api/meeting/${id}`),
  updateMeeting: (id: number, payload: UpdateMeetingPayload) =>
    http.put<MeetingDTO>(`/api/meeting/${id}`, payload),
  deleteMeeting: (id: number) => http.delete<void>(`/api/meeting/${id}`),
  startMeeting: (id: number) => http.post<void>(`/api/meeting/${id}/start`),
  finishMeeting: (id: number) => http.post<void>(`/api/meeting/${id}/finish`),
};

export default meetingApi;

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
  /** 会议模式：live-实时会议，audio-音频转写 */
  mode?: 'live' | 'audio';
  created_by: number;
  createdAt?: string;
  updatedAt?: string;
  audio_url?: string;
}

/** 创建会议请求体（见 MeetingPostRequest）。 */
export interface CreateMeetingPayload {
  name: string;
  participants?: string;
  speaker_ids?: string;
  hot_word_library_ids?: string;
  start_time?: string;
  end_time?: string;
  /** 会议模式：live-实时会议（默认），audio-音频转写 */
  mode?: 'live' | 'audio';
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
  /** 会议模式：live-实时会议，audio-音频转写 */
  mode?: 'live' | 'audio';
}

/** 会议列表查询参数（见 MeetingListRequest）。 */
export interface MeetingListParams {
  page?: number;
  pageSize?: number;
  keyword?: string;
  status?: string;
  /** 模式筛选：live-实时会议，audio-音频转写 */
  mode?: 'live' | 'audio';
  start_time?: string;
  end_time?: string;
}

/** 分页响应 */
export interface PaginatedResult<T> {
  items: T[];
  total: number;
  page: number;
  pageSize: number;
}

/** 词级时间戳 DTO（见 koi-server/app/models/meeting_transcript.go 的 WordTimestamp） */
export interface WordTimestampDTO {
  word: string;
  start_ms: number;
  end_ms: number;
}

/** 会议转写记录 DTO（见 koi-server/app/models/meeting_transcript.go） */
export interface MeetingTranscriptDTO {
  id: number;
  meeting_id: number;
  speaker_id: number | null;
  speaker_name: string;
  text: string;
  start_ms: number;
  end_ms: number;
  word_timestamps: WordTimestampDTO[];
  is_final: boolean;
  created_at?: string;
}

/** 离线转写：上传音频文件响应（见 MeetingController.UploadAudio） */
export interface UploadAudioResult {
  meeting_id: number;
  audio_file_path: string;
  audio_url: string;
  file_size: number;
  original_filename: string;
  sample_rate: number;
  channels: number;
  bits_per_sample: number;
  duration: number;
  /** 格式兼容性警告（例如多声道需混音） */
  warning?: string;
  /** 后端是否已自动触发转写：started 表示已触发 */
  transcription?: 'started';
  /** 触发转写失败时的错误信息（音频已上传成功，但转写未启动） */
  transcription_error?: string;
}

/** 离线转写进度状态 */
export interface TranscriptionProgress {
  meeting_id: number;
  status: 'pending' | 'running' | 'completed' | 'failed';
  /** 0-100 */
  progress: number;
  current_step: string;
  total_seconds: number;
  error_message?: string;
  started_at?: string;
  finished_at?: string;
}

export const meetingApi = {
  listMeetings: (params: MeetingListParams = {}) =>
    http.get<PaginatedResult<MeetingDTO>>('/api/meeting', params),
  createMeeting: (payload: CreateMeetingPayload) =>
    http.post<MeetingDTO>('/api/meeting', payload),
  getMeeting: (id: number) => http.get<MeetingDTO>(`/api/meeting/${id}`),
  updateMeeting: (id: number, payload: UpdateMeetingPayload) =>
    http.put<MeetingDTO>(`/api/meeting/${id}`, payload),

  /** 获取会议转写记录（分页，按时间升序） */
  getMeetingTranscripts: (id: number, params: { page?: number; pageSize?: number } = {}) =>
    http.get<PaginatedResult<MeetingTranscriptDTO>>(`/api/meeting/${id}/transcripts`, params),
  deleteMeeting: (id: number) => http.delete<void>(`/api/meeting/${id}`),
  startMeeting: (id: number) => http.post<void>(`/api/meeting/${id}/start`),
  finishMeeting: (id: number) => http.post<void>(`/api/meeting/${id}/finish`),

  /** 离线转写：为会议上传音频文件（multipart/form-data，字段名 audio）。
   *  后端会校验 WAV 头、格式兼容性，并使用 UUIDv7 命名落盘。
   *  同一会议重复上传将覆盖旧文件并清理旧转写记录。 */
  uploadMeetingAudio: (id: number, file: File) => {
    const fd = new FormData();
    fd.append('audio', file);
    return http.upload<UploadAudioResult>(`/api/meeting/${id}/audio`, fd);
  },

  /** 离线转写：触发会议已上传音频的异步转写。
   *  转写在后台执行，通过 getTranscriptionProgress 查询进度。 */
  startTranscription: (id: number) => http.post<void>(`/api/meeting/${id}/transcribe`),

  /** 重新转写：无论实时还是离线会议，均基于已归档音频调用离线转写，
   *  并清空旧转写记录。转写在后台执行，通过 getTranscriptionProgress 查询进度。 */
  retranscribeMeeting: (id: number) => http.post<void>(`/api/meeting/${id}/retranscribe`),

  /** 离线转写：查询转写进度（status / progress 0-100 / current_step / error_message）。 */
  getTranscriptionProgress: (id: number) =>
    http.get<TranscriptionProgress>(`/api/meeting/${id}/progress`),
};

export default meetingApi;

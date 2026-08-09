import http from './http';

export type Gender = 'male' | 'female' | 'unknown';
export type SpeakerStatus = 'active' | 'inactive';

/** 声纹音频，字段与后端 models.SpeakerAudio 的 json tag 一致 */
export interface SpeakerAudioDTO {
  id: number;
  speaker_id: number;
  file_name: string;
  file_path: string;
  file_size: number;
  sample_rate: number;
  duration: number;
  dim: number;
  remark: string;
  created_at: string;
  updated_at?: string;
}

/** 说话人，字段与后端 models.Speaker 的 json tag 一致 */
export interface SpeakerDTO {
  id: number;
  name: string;
  gender: Gender;
  description: string;
  status: SpeakerStatus;
  embedding_dim: number;
  audio_count: number;
  created_at: string;
  updated_at?: string;
  audios?: SpeakerAudioDTO[];
}

export interface SpeakerListResult {
  items: SpeakerDTO[];
  total: number;
  page: number;
  pageSize: number;
  totalPage: number;
}

export interface SpeakerListParams {
  page?: number;
  pageSize?: number;
  keyword?: string;
  gender?: Gender;
  status?: SpeakerStatus;
}

/** 音频样本：录音或导入的文件，随表单一并以 multipart/form-data 提交 */
export interface AudioSamplePayload {
  file: Blob | File;
  /** 文件名必须带后缀（后端按扩展名校验，仅支持 wav） */
  fileName?: string;
  remark?: string;
}

export interface CreateSpeakerPayload {
  name: string;
  gender?: Gender;
  description?: string;
  status?: SpeakerStatus;
  /** 注册时一并提交的声纹样本 */
  audio?: AudioSamplePayload;
}

export interface UpdateSpeakerPayload {
  name?: string;
  gender?: Gender;
  description?: string;
  status?: SpeakerStatus;
}

/** 声纹模型状态 */
export interface ServiceStatus {
  loaded: boolean;
  dim: number;
  threshold: number;
  error: string;
  speakers: string[];
}

/** 声纹识别（1:N）结果 */
export interface IdentifyResult {
  matched: boolean;
  score: number;
  threshold: number;
  name: string;
  speaker: SpeakerDTO | null;
}

/** 声纹校验（1:1）结果 */
export interface VerifyResult {
  matched: boolean;
  score: number;
  threshold: number;
  speaker: SpeakerDTO;
}

/** 把音频样本写入表单：必须带文件名，否则后端拿不到扩展名会判定格式不支持 */
function appendAudio(fd: FormData, audio: AudioSamplePayload) {
  const fileName = audio.fileName ?? (audio.file instanceof File ? audio.file.name : 'sample.wav');
  fd.append('file', audio.file, fileName);
  if (audio.remark) fd.append('remark', audio.remark);
}

export const speakerApi = {
  /** 分页查询说话人，支持关键词 / 性别 / 状态筛选 */
  list(params?: SpeakerListParams) {
    return http.get<SpeakerListResult>('/api/speaker', params as Record<string, unknown>);
  },
  /** 查询单个说话人详情（含声纹音频列表） */
  get(id: number) {
    return http.get<SpeakerDTO>(`/api/speaker/${id}`);
  },
  /** 注册（创建）说话人：统一以 multipart/form-data 提交，可同时携带录音样本 */
  create(payload: CreateSpeakerPayload) {
    const fd = new FormData();
    fd.append('name', payload.name);
    if (payload.gender) fd.append('gender', payload.gender);
    if (payload.description) fd.append('description', payload.description);
    if (payload.status) fd.append('status', payload.status);
    if (payload.audio) appendAudio(fd, payload.audio);
    return http.upload<SpeakerDTO>('/api/speaker', fd);
  },
  /** 更新说话人 */
  update(id: number, payload: UpdateSpeakerPayload) {
    return http.put<SpeakerDTO>(`/api/speaker/${id}`, payload);
  },
  /** 删除说话人 */
  remove(id: number) {
    return http.delete<void>(`/api/speaker/${id}`);
  },
  /** 查询说话人名下的声纹音频 */
  listAudios(id: number) {
    return http.get<SpeakerAudioDTO[]>(`/api/speaker/${id}/audio`);
  },
  /** 上传声纹音频（multipart/form-data，字段名 file + remark） */
  uploadAudio(id: number, audio: AudioSamplePayload) {
    const fd = new FormData();
    appendAudio(fd, audio);
    return http.upload<SpeakerAudioDTO>(`/api/speaker/${id}/audio`, fd);
  },
  /** 删除一条声纹音频 */
  deleteAudio(id: number, audioId: number) {
    return http.delete<void>(`/api/speaker/${id}/audio/${audioId}`);
  },
  /** 查询声纹服务状态 */
  status() {
    return http.get<ServiceStatus>('/api/speaker/status');
  },
  /** 声纹识别：判定音频属于哪位说话人 */
  identify(audio: AudioSamplePayload, threshold?: number) {
    const fd = new FormData();
    appendAudio(fd, audio);
    if (threshold !== undefined) fd.append('threshold', String(threshold));
    return http.upload<IdentifyResult>('/api/speaker/identify', fd);
  },
  /** 声纹验证：判定音频是否为指定说话人 */
  verify(id: number, audio: AudioSamplePayload, threshold?: number) {
    const fd = new FormData();
    appendAudio(fd, audio);
    if (threshold !== undefined) fd.append('threshold', String(threshold));
    return http.upload<VerifyResult>(`/api/speaker/${id}/verify`, fd);
  },
};

export default speakerApi;

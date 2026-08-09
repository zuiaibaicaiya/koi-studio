import { defineStore } from 'pinia';
import { ref, computed } from 'vue';
import {
  speakerApi,
  type Gender,
  type SpeakerStatus,
  type SpeakerAudioDTO,
  type SpeakerDTO,
} from '../services/speakerApi';

export type UIGender = '男' | '女' | '未知';
export type UIStatus = '启用' | '禁用';

/** 声纹音频（含本地录制/导入的临时 blob，用于上传到后端） */
export interface SpeakerAudio {
  id?: number;
  name: string;
  url?: string;
  duration?: number;
  size?: number;
  source?: 'record' | 'import';
  remark?: string;
  /** 录音或导入产生的原始 Blob，用于上传到后端 */
  blob?: Blob;
  // 来自后端的字段
  speakerId?: number;
  sampleRate?: number;
  createdAt?: string;
}

export interface Speaker {
  id: number;
  name: string;
  gender: UIGender;
  language?: string;
  description: string;
  status?: UIStatus;
  embeddingDim?: number;
  sampleCount?: number;
  createdAt: string;
  audios?: SpeakerAudio[];
  audio?: SpeakerAudio | null;
}

const genderMap: Record<Gender, UIGender> = { male: '男', female: '女', unknown: '未知' };
const genderMapInv: Record<UIGender, Gender> = { '男': 'male', '女': 'female', '未知': 'unknown' };
const statusMap: Record<SpeakerStatus, UIStatus> = { active: '启用', inactive: '禁用' };
const statusMapInv: Record<UIStatus, SpeakerStatus> = { '启用': 'active', '禁用': 'inactive' };

function fromAudioDTO(a: SpeakerAudioDTO): SpeakerAudio {
  return {
    id: a.id,
    name: a.file_name || a.remark || `音频 ${a.id}`,
    remark: a.remark,
    speakerId: a.speaker_id,
    duration: a.duration,
    size: a.file_size,
    sampleRate: a.sample_rate,
    createdAt: a.created_at,
  };
}

function fromDTO(dto: SpeakerDTO): Speaker {
  return {
    id: dto.id,
    name: dto.name,
    gender: genderMap[dto.gender] ?? '未知',
    language: '中文',
    description: dto.description,
    status: statusMap[dto.status] ?? '启用',
    embeddingDim: dto.embedding_dim,
    sampleCount: dto.audio_count,
    createdAt: dto.created_at,
    audios: (dto.audios ?? []).map(fromAudioDTO),
    audio: null,
  };
}

/** 待提交的音频样本（录音或导入） */
export interface AudioInput {
  blob: Blob;
  /** 带扩展名的文件名，后端按扩展名校验（仅 wav） */
  fileName?: string;
  remark?: string;
}

export const useSpeakerStore = defineStore('speaker', () => {
  const list = ref<Speaker[]>([]);
  const loading = ref(false);
  const total = ref(0);
  const page = ref(1);
  const pageSize = ref(10);

  const genderOptions = computed(() => ['男', '女', '未知'] as UIGender[]);
  const statusOptions = computed(() => ['启用', '禁用'] as UIStatus[]);

  /** 本地按 id 查找（依赖 list 已加载） */
  function getById(id: number) {
    return list.value.find((s) => s.id === id);
  }

  /** 拉取说话人列表（分页 + 关键词 / 性别 / 状态筛选） */
  async function load(params?: {
    page?: number;
    pageSize?: number;
    keyword?: string;
    gender?: UIGender | '';
    status?: UIStatus | '';
  }) {
    loading.value = true;
    try {
      const res = await speakerApi.list({
        page: params?.page ?? page.value,
        pageSize: params?.pageSize ?? pageSize.value,
        keyword: params?.keyword || undefined,
        gender: params?.gender ? genderMapInv[params.gender] : undefined,
        status: params?.status ? statusMapInv[params.status] : undefined,
      });
      list.value = res.items.map(fromDTO);
      total.value = res.total;
      page.value = res.page;
      pageSize.value = res.pageSize;
    } finally {
      loading.value = false;
    }
  }

  /** 注册（创建）说话人：表单与录音一次性以 multipart/form-data 提交 */
  async function add(payload: {
    name: string;
    gender: UIGender;
    description?: string;
    status?: UIStatus;
    audio?: AudioInput;
  }) {
    const created = await speakerApi.create({
      name: payload.name.trim(),
      gender: genderMapInv[payload.gender],
      description: payload.description?.trim() || '',
      status: statusMapInv[payload.status ?? '启用'],
      audio: payload.audio
        ? { file: payload.audio.blob, fileName: payload.audio.fileName, remark: payload.audio.remark }
        : undefined,
    });
    await load();
    return getById(created.id);
  }

  /** 更新说话人，若提供新的音频样本则追加上传一条声纹 */
  async function update(
    id: number,
    payload: {
      name?: string;
      gender?: UIGender;
      description?: string;
      status?: UIStatus;
      audio?: AudioInput;
    },
  ) {
    await speakerApi.update(id, {
      name: payload.name?.trim() || undefined,
      gender: payload.gender ? genderMapInv[payload.gender] : undefined,
      description: payload.description?.trim() || undefined,
      status: payload.status ? statusMapInv[payload.status] : undefined,
    });
    if (payload.audio) {
      await speakerApi.uploadAudio(id, {
        file: payload.audio.blob,
        fileName: payload.audio.fileName,
        remark: payload.audio.remark,
      });
    }
    await load();
    return getById(id);
  }

  /** 删除说话人 */
  async function remove(id: number) {
    await speakerApi.remove(id);
    await load();
  }

  /** 批量导入说话人（CSV 解析后逐条创建） */
  async function importRows(rows: { name: string; gender?: UIGender; description?: string }[]) {
    const created: Speaker[] = [];
    for (const r of rows) {
      const s = await add({
        name: r.name,
        gender: r.gender ?? '未知',
        description: r.description,
      });
      if (s) created.push(s);
    }
    await load();
    return created;
  }

  return {
    list,
    loading,
    total,
    page,
    pageSize,
    genderOptions,
    statusOptions,
    getById,
    load,
    add,
    update,
    remove,
    importRows,
  };
});

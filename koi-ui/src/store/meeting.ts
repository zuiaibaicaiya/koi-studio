import { defineStore } from 'pinia';
import { ref, computed } from 'vue';
import {
  meetingApi,
  type MeetingDTO,
  type MeetingListParams,
} from '../services/meetingApi';

/** 后端状态值映射为中文展示 */
export type UIMeetingStatus = '已预约' | '进行中' | '已结束';
export const statusMap: Record<string, UIMeetingStatus> = {
  created: '已预约',
  ongoing: '进行中',
  finished: '已结束',
};
export const statusMapInv: Record<UIMeetingStatus, string> = {
  '已预约': 'created',
  '进行中': 'ongoing',
  '已结束': 'finished',
};

/** 前端直观使用的会议模型（基于后端 DTO 映射） */
export interface Meeting {
  id: number;
  name: string;
  participants: string;
  speakerIds: string;
  hotWordLibraryIds: string;
  startTime: string;
  endTime: string;
  status: UIMeetingStatus;
  rawStatus: string;
  /** 会议模式：live-实时会议，audio-音频转写 */
  mode: 'live' | 'audio';
  createdBy: number;
  createdAt: string;
  updatedAt: string;
  /** 会议音频地址 */
  audioUrl?: string;
  /** 会议链接（预留） */
  meetingUrl?: string;
  /** 实时转写内容（预留） */
  transcriptions?: Transcription[];
}

export interface Transcription {
  id: number;
  speaker: string;
  time: string;
  content: string;
}

function fromDTO(dto: MeetingDTO): Meeting {
  return {
    id: dto.id,
    name: dto.name,
    participants: dto.participants || '',
    speakerIds: dto.speaker_ids || '',
    hotWordLibraryIds: dto.hot_word_library_ids || '',
    startTime: dto.start_time || '',
    endTime: dto.end_time || '',
    status: statusMap[dto.status] ?? '已预约',
    rawStatus: dto.status,
    mode: (dto.mode as 'live' | 'audio') || 'live',
    createdBy: dto.created_by,
    createdAt: dto.createdAt || '',
    updatedAt: dto.updatedAt || '',
    audioUrl: dto.audio_url || '',
  };
}

export const useMeetingStore = defineStore('meeting', () => {
  // ---- 实时会议（对接后端） ----
  const list = ref<Meeting[]>([]);
  const loading = ref(false);
  const total = ref(0);
  const page = ref(1);
  const pageSize = ref(10);

  const statusOptions = computed<UIMeetingStatus[]>(() => ['已预约', '进行中', '已结束']);

  function getById(id: number) {
    return list.value.find((m) => m.id === id);
  }

  /** 拉取会议列表（分页 + 关键词 / 状态 / 模式 / 时间段筛选） */
  async function load(params?: {
    page?: number;
    pageSize?: number;
    keyword?: string;
    status?: UIMeetingStatus | '';
    mode?: 'live' | 'audio';
    startTime?: string;
    endTime?: string;
  }) {
    loading.value = true;
    try {
      const apiParams: MeetingListParams = {
        page: params?.page ?? page.value,
        pageSize: params?.pageSize ?? pageSize.value,
        keyword: params?.keyword || undefined,
        status: params?.status ? statusMapInv[params.status] : undefined,
        mode: params?.mode || undefined,
        start_time: params?.startTime || undefined,
        end_time: params?.endTime || undefined,
      };
      const res = await meetingApi.listMeetings(apiParams);
      list.value = res.items.map(fromDTO);
      total.value = res.total;
      page.value = res.page;
      pageSize.value = res.pageSize;
    } finally {
      loading.value = false;
    }
  }

  /** 创建会议 */
  async function add(payload: {
    name: string;
    participants?: string;
    speakerIds?: string;
    hotWordLibraryIds?: string;
    startTime: string;
    endTime: string;
    mode?: 'live' | 'audio';
  }) {
    const created = await meetingApi.createMeeting({
      name: payload.name.trim(),
      participants: payload.participants?.trim() || undefined,
      speaker_ids: payload.speakerIds?.trim() || undefined,
      hot_word_library_ids: payload.hotWordLibraryIds?.trim() || undefined,
      start_time: payload.startTime,
      end_time: payload.endTime,
      mode: payload.mode,
    });
    await load();
    return getById(created.id);
  }

  /** 更新会议 */
  async function update(
    id: number,
    payload: {
      name?: string;
      participants?: string;
      speakerIds?: string;
      hotWordLibraryIds?: string;
      startTime?: string;
      endTime?: string;
      status?: UIMeetingStatus;
    },
  ) {
    await meetingApi.updateMeeting(id, {
      name: payload.name?.trim() || undefined,
      participants: payload.participants?.trim() || undefined,
      speaker_ids: payload.speakerIds?.trim() || undefined,
      hot_word_library_ids: payload.hotWordLibraryIds?.trim() || undefined,
      start_time: payload.startTime || undefined,
      end_time: payload.endTime || undefined,
      status: payload.status ? statusMapInv[payload.status] : undefined,
    });
    await load();
    return getById(id);
  }

  /** 删除会议 */
  async function remove(id: number) {
    await meetingApi.deleteMeeting(id);
    await load();
  }

  /** 开始会议 */
  async function start(id: number) {
    await meetingApi.startMeeting(id);
    await load();
    return getById(id);
  }

  /** 结束会议 */
  async function finish(id: number) {
    await meetingApi.finishMeeting(id);
    await load();
    return getById(id);
  }

  return {
    // 实时会议 / 音频转写（均对接后端，按 mode 区分）
    list,
    loading,
    total,
    page,
    pageSize,
    statusOptions,
    getById,
    load,
    add,
    update,
    remove,
    start,
    finish,
  };
});

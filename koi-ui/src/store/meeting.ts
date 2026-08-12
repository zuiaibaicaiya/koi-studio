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
    createdBy: dto.created_by,
    createdAt: dto.createdAt,
    updatedAt: dto.updatedAt,
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

  /** 拉取会议列表（分页 + 关键词 / 状态筛选） */
  async function load(params?: {
    page?: number;
    pageSize?: number;
    keyword?: string;
    status?: UIMeetingStatus | '';
  }) {
    loading.value = true;
    try {
      const apiParams: MeetingListParams = {
        page: params?.page ?? page.value,
        pageSize: params?.pageSize ?? pageSize.value,
        keyword: params?.keyword || undefined,
        status: params?.status ? statusMapInv[params.status] : undefined,
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
  }) {
    const created = await meetingApi.createMeeting({
      name: payload.name.trim(),
      participants: payload.participants?.trim() || undefined,
      speaker_ids: payload.speakerIds?.trim() || undefined,
      hot_word_library_ids: payload.hotWordLibraryIds?.trim() || undefined,
      start_time: payload.startTime,
      end_time: payload.endTime,
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

  // ---- 实时转写（仍使用本地模拟数据） ----
  const transcriptions = ref<Transcription[]>([
    { id: 1, speaker: '张伟', time: '2025-07-01 09:05:22', content: '今天我们讨论一下项目进度和下一步计划。' },
    { id: 2, speaker: '李娜', time: '2025-07-01 09:05:45', content: '好的，我先汇报一下我负责的模块。前端部分已经完成了80%，预计下周可以交付测试。' },
    { id: 3, speaker: '王强', time: '2025-07-01 09:06:10', content: '后端API开发进展顺利，已经完成了用户认证、权限管理等核心功能。' },
    { id: 4, speaker: '张伟', time: '2025-07-01 09:06:35', content: '很好，那数据库设计方面有什么需要协调的吗？' },
    { id: 5, speaker: '赵敏', time: '2025-07-01 09:07:00', content: '数据库表结构已经确定，我这边在准备测试数据，预计明天可以完成。' },
    { id: 6, speaker: '李娜', time: '2025-07-01 09:07:25', content: '我建议今天下午开一个技术评审会，把接口规范确定下来。' },
    { id: 7, speaker: '王强', time: '2025-07-01 09:07:50', content: '同意，接口文档我已经写了一部分，下午可以一起过一下。' },
    { id: 8, speaker: '张伟', time: '2025-07-01 09:08:15', content: '好的，那下午2点我们在大会议室集合。另外提醒大家，本周五要提交周报。' },
    { id: 9, speaker: '赵敏', time: '2025-07-01 09:08:40', content: '收到，我的测试脚本还需要一点时间准备。' },
    { id: 10, speaker: '张伟', time: '2025-07-01 09:09:00', content: '没问题，大家继续加油，今天会议到此结束。' },
  ]);

  return {
    // 实时会议
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
    // 实时转写
    transcriptions,
  };
});

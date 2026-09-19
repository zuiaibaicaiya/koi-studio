import { beforeEach, describe, expect, test } from '@rstest/core';
import { createApp } from 'vue';
import { createPinia, setActivePinia } from 'pinia';
import { useMeetingStore } from '../src/store/meeting';
import meetingApi from '../src/services/meetingApi';
import type { MeetingDTO } from '../src/services/meetingApi';

function freshPinia() {
  const pinia = createPinia();
  createApp({ render: () => null }).use(pinia);
  setActivePinia(pinia);
}

const baseDto: MeetingDTO = {
  id: 1,
  name: '周会',
  participants: 'a,b',
  speaker_ids: '1,2',
  hot_word_library_ids: '',
  start_time: '2026-01-01T10:00:00',
  end_time: '2026-01-01T11:00:00',
  status: 'ongoing',
  mode: 'live',
  created_by: 1,
  createdAt: '2026-01-01',
  updatedAt: '2026-01-01',
};

const pageResult = (items: MeetingDTO[]) => ({ items, total: items.length, page: 1, pageSize: 10 });

let createMeetingArgs: MeetingDTO | null = null;

beforeEach(() => {
  freshPinia();
  createMeetingArgs = null;
  meetingApi.listMeetings = (() => Promise.resolve(pageResult([baseDto]))) as never;
  meetingApi.createMeeting = ((p: MeetingDTO) => {
    createMeetingArgs = p;
    return Promise.resolve(p);
  }) as never;
  meetingApi.startMeeting = (() => Promise.resolve(undefined)) as never;
  meetingApi.finishMeeting = (() => Promise.resolve(undefined)) as never;
  meetingApi.updateMeeting = ((p: MeetingDTO) => Promise.resolve(p)) as never;
});

describe('useMeetingStore - DTO→UI 映射与业务流程', () => {
  test('load 将后端 DTO 映射为 UI 模型（中文状态 / 字符串字段 / rawStatus）', async () => {
    const s = useMeetingStore();
    await s.load();
    expect(s.list).toHaveLength(1);
    const m = s.list[0];
    expect(m.status).toBe('进行中');
    expect(m.rawStatus).toBe('ongoing');
    expect(m.speakerIds).toBe('1,2');
    expect(m.mode).toBe('live');
    expect(s.total).toBe(1);
    expect(s.getById(1)?.name).toBe('周会');
  });

  test('未知状态码回退为「已预约」并保留 rawStatus', async () => {
    meetingApi.listMeetings = (() =>
      Promise.resolve(
        pageResult([{ ...baseDto, id: 2, status: 'weird' as MeetingDTO['status'] }]),
      )) as never;
    const s = useMeetingStore();
    await s.load();
    expect(s.list[0].status).toBe('已预约');
    expect(s.list[0].rawStatus).toBe('weird');
  });

  test('add：name 被 trim 后发送，返回映射后的会议', async () => {
    const created: MeetingDTO = { ...baseDto, id: 3, name: '新建会议', status: 'created' };
    meetingApi.createMeeting = ((p: MeetingDTO) => {
      createMeetingArgs = p;
      return Promise.resolve(created);
    }) as never;
    meetingApi.listMeetings = (() => Promise.resolve(pageResult([created]))) as never;
    const s = useMeetingStore();
    const result = await s.add({ name: ' 新建会议 ', startTime: 'x', endTime: 'y' });
    expect(createMeetingArgs?.name).toBe('新建会议');
    expect(result?.id).toBe(3);
    expect(result?.status).toBe('已预约'); // created → 已预约
  });

  test('start：调用后端并刷新为「进行中」', async () => {
    const started: MeetingDTO = { ...baseDto, id: 1, status: 'ongoing' };
    meetingApi.listMeetings = (() => Promise.resolve(pageResult([started]))) as never;
    const s = useMeetingStore();
    const result = await s.start(1);
    expect(result?.status).toBe('进行中');
  });

  test('finish：调用后端并刷新为「已结束」', async () => {
    const finished: MeetingDTO = { ...baseDto, id: 1, status: 'finished' };
    meetingApi.listMeetings = (() => Promise.resolve(pageResult([finished]))) as never;
    const s = useMeetingStore();
    const result = await s.finish(1);
    expect(result?.status).toBe('已结束');
  });

  test('update：透传字段并用映射后的状态返回', async () => {
    const updated: MeetingDTO = { ...baseDto, id: 1, name: '改名', status: 'finished' };
    meetingApi.updateMeeting = ((p: MeetingDTO) => Promise.resolve(p)) as never;
    meetingApi.listMeetings = (() => Promise.resolve(pageResult([updated]))) as never;
    const s = useMeetingStore();
    const result = await s.update(1, { name: '改名' });
    expect(result?.status).toBe('已结束');
  });
});

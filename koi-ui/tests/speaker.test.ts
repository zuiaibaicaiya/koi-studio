import { beforeEach, describe, expect, test } from '@rstest/core';
import { createApp } from 'vue';
import { createPinia, setActivePinia } from 'pinia';
import { useSpeakerStore } from '../src/store/speaker';
import speakerApi from '../src/services/speakerApi';
import type { SpeakerDTO } from '../src/services/speakerApi';

function freshPinia() {
  const pinia = createPinia();
  createApp({ render: () => null }).use(pinia);
  setActivePinia(pinia);
}

const baseDto: SpeakerDTO = {
  id: 1,
  name: '张三',
  gender: 'male',
  description: 'd',
  status: 'active',
  embedding_dim: 256,
  audio_count: 1,
  created_at: '2026-01-01',
  audios: [
    {
      id: 10,
      speaker_id: 1,
      file_name: 'a.wav',
      file_path: '/p',
      file_size: 1,
      sample_rate: 16000,
      duration: 2,
      dim: 256,
      remark: '',
      created_at: '2026-01-01',
    },
  ],
};

const pageResult = (items: SpeakerDTO[]) => ({
  items,
  total: items.length,
  page: 1,
  pageSize: 10,
  totalPage: 1,
});

let createArgs: Record<string, unknown> | null = null;

beforeEach(() => {
  freshPinia();
  createArgs = null;
  speakerApi.list = (() => Promise.resolve(pageResult([baseDto]))) as never;
  speakerApi.create = ((p: Record<string, unknown>) => {
    createArgs = p;
    return Promise.resolve(baseDto);
  }) as never;
});

describe('useSpeakerStore - DTO→UI 映射与业务流程', () => {
  test('load 映射 gender/status，并将音频名回落到 file_name', async () => {
    const s = useSpeakerStore();
    await s.load();
    const sp = s.list[0];
    expect(sp.gender).toBe('男');
    expect(sp.status).toBe('启用');
    expect(sp.sampleCount).toBe(1);
    expect(sp.language).toBe('中文');
    expect(sp.audios?.[0].name).toBe('a.wav');
  });

  test('genderOptions / statusOptions 枚举', () => {
    const s = useSpeakerStore();
    expect(s.genderOptions).toEqual(['男', '女', '未知']);
    expect(s.statusOptions).toEqual(['启用', '禁用']);
  });

  test('add：发送 trim 后的 name 与映射后的 gender（男→male）', async () => {
    const s = useSpeakerStore();
    await s.add({ name: ' 张三 ', gender: '男', description: 'd' });
    expect(createArgs?.name).toBe('张三');
    expect(createArgs?.gender).toBe('male');
  });

  test('add 返回映射后的说话人（gender 男）', async () => {
    const s = useSpeakerStore();
    const result = await s.add({ name: '张三', gender: '男', description: 'd' });
    expect(result?.gender).toBe('男');
  });

  test('getById 在列表已加载时按 id 查找', async () => {
    const s = useSpeakerStore();
    await s.load();
    expect(s.getById(1)?.name).toBe('张三');
    expect(s.getById(999)).toBeUndefined();
  });
});

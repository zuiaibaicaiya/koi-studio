import { beforeEach, describe, expect, test } from '@rstest/core';
import { createApp } from 'vue';
import { createPinia, setActivePinia } from 'pinia';
import { useHotWordLibraryStore, type HotWordLibrary } from '../src/store/hotWordLibrary';
import { useSystemUserStore } from '../src/store/systemUser';
import { statusMap, statusMapInv } from '../src/store/meeting';

const freshPinia = () => {
  const pinia = createPinia();
  createApp({ render: () => null }).use(pinia);
  setActivePinia(pinia);
};

beforeEach(() => {
  freshPinia();
});

function makeLibrary(id: number, wordCount: number): HotWordLibrary {
  return {
    id,
    name: `L${id}`,
    description: '',
    status: 'active',
    createdAt: '',
    wordCount,
    words: [],
  };
}

describe('useHotWordLibraryStore', () => {
  test('初始为空', () => {
    const s = useHotWordLibraryStore();
    expect(s.libraryCount).toBe(0);
    expect(s.totalWordCount).toBe(0);
  });

  test('replaceAll 写入后统计与 getLibrary 生效', () => {
    const s = useHotWordLibraryStore();
    s.replaceAll([makeLibrary(1, 5), makeLibrary(2, 3)]);
    expect(s.libraryCount).toBe(2);
    expect(s.totalWordCount).toBe(8);
    expect(s.getLibrary(2)?.name).toBe('L2');
    expect(s.getLibrary(99)).toBeUndefined();
  });

  test('setWords 仅更新指定热词库的 words', () => {
    const s = useHotWordLibraryStore();
    s.replaceAll([makeLibrary(1, 0), makeLibrary(2, 0)]);
    s.setWords(1, [{ id: 10, word: '锦鲤', weight: 80 }]);
    expect(s.getLibrary(1)?.words).toHaveLength(1);
    expect(s.getLibrary(2)?.words).toHaveLength(0);
  });
});

describe('useSystemUserStore', () => {
  test('内置 seed 36 条', () => {
    const s = useSystemUserStore();
    expect(s.list).toHaveLength(36);
  });

  test('add 分配自增 id 并置顶', () => {
    const s = useSystemUserStore();
    const created = s.add({
      username: 'new',
      name: '新人',
      email: 'n@koi.studio',
      role: '普通用户',
      status: '启用',
    });
    expect(created.id).toBeGreaterThanOrEqual(1000);
    expect(s.list[0].id).toBe(created.id);
    expect(s.getById(created.id)?.username).toBe('new');
  });

  test('update 局部合并，未传字段保持不变', () => {
    const s = useSystemUserStore();
    const created = s.add({
      username: 'u',
      name: 'a',
      email: 'e',
      role: '普通用户',
      status: '启用',
    });
    s.update(created.id, { status: '禁用' });
    expect(s.getById(created.id)?.status).toBe('禁用');
    expect(s.getById(created.id)?.name).toBe('a');
  });

  test('remove 删除指定用户', () => {
    const s = useSystemUserStore();
    const created = s.add({
      username: 'u2',
      name: 'b',
      email: 'e',
      role: '普通用户',
      status: '启用',
    });
    s.remove(created.id);
    expect(s.getById(created.id)).toBeUndefined();
  });
});

describe('meeting statusMap', () => {
  test('状态中文映射完整', () => {
    expect(statusMap).toEqual({ created: '已预约', ongoing: '进行中', finished: '已结束' });
  });

  test('statusMap 与 statusMapInv 互为逆映射', () => {
    for (const raw of Object.keys(statusMap)) {
      expect(statusMapInv[statusMap[raw as keyof typeof statusMap]]).toBe(raw);
    }
    for (const ui of Object.keys(statusMapInv)) {
      expect(statusMap[statusMapInv[ui as keyof typeof statusMapInv]]).toBe(ui);
    }
  });
});

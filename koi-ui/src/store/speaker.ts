import { defineStore } from 'pinia';
import { ref } from 'vue';

export type SpeakerGender = '男' | '女' | '未知';

export interface Speaker {
  id: number;
  name: string;
  gender: SpeakerGender;
  language: string;
  sampleCount: number;
  description: string;
  createdAt: string;
}

const genders: SpeakerGender[] = ['男', '女', '未知'];
const languages = ['中文', '英文', '粤语', '四川话', '日语'];

function seed(): Speaker[] {
  const list: Speaker[] = [];
  for (let i = 1; i <= 30; i++) {
    list.push({
      id: i,
      name: `说话人${i}`,
      gender: genders[i % 3],
      language: languages[i % languages.length],
      sampleCount: Math.floor(Math.random() * 200) + 5,
      description: `注册说话人样本库记录 ${i}`,
      createdAt: new Date(Date.now() - i * 172800000).toISOString().slice(0, 10),
    });
  }
  return list;
}

export const useSpeakerStore = defineStore('speaker', () => {
  const list = ref<Speaker[]>(seed());
  let nextId = 1000;

  function getById(id: number) {
    return list.value.find((s) => s.id === id);
  }
  function add(data: Omit<Speaker, 'id' | 'createdAt'>) {
    const item: Speaker = {
      ...data,
      id: nextId++,
      sampleCount: Number(data.sampleCount) || 0,
      createdAt: new Date().toISOString().slice(0, 10),
    };
    list.value.unshift(item);
    return item;
  }
  function update(id: number, data: Partial<Omit<Speaker, 'id'>>) {
    const idx = list.value.findIndex((s) => s.id === id);
    if (idx !== -1) {
      const merged = { ...list.value[idx], ...data };
      merged.sampleCount = Number(merged.sampleCount) || 0;
      list.value[idx] = merged;
    }
  }
  function remove(id: number) {
    list.value = list.value.filter((s) => s.id !== id);
  }
  function importRows(rows: Speaker[]) {
    rows.forEach((r) => add(r));
  }
  return { list, getById, add, update, remove, importRows };
});

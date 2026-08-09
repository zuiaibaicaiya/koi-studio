import { defineStore } from 'pinia';
import { ref } from 'vue';

export interface HotWord {
  id: number;
  word: string;
  category: string;
  weight: number;
  description: string;
  createdAt: string;
}

const categories = ['通用', '金融', '医疗', '法律', '科技', '教育'];

function seed(): HotWord[] {
  const list: HotWord[] = [];
  for (let i = 1; i <= 42; i++) {
    list.push({
      id: i,
      word: `热词${i}`,
      category: categories[i % categories.length],
      weight: Math.floor(Math.random() * 90) + 10,
      description: `用于提升「${categories[i % categories.length]}」领域识别准确率的热词${i}`,
      createdAt: new Date(Date.now() - i * 43200000).toISOString().slice(0, 10),
    });
  }
  return list;
}

export const useHotWordStore = defineStore('hotWord', () => {
  const list = ref<HotWord[]>(seed());
  let nextId = 1000;

  function getById(id: number) {
    return list.value.find((w) => w.id === id);
  }
  function add(data: Omit<HotWord, 'id' | 'createdAt'>) {
    const item: HotWord = {
      ...data,
      id: nextId++,
      weight: Number(data.weight) || 0,
      createdAt: new Date().toISOString().slice(0, 10),
    };
    list.value.unshift(item);
    return item;
  }
  function update(id: number, data: Partial<Omit<HotWord, 'id'>>) {
    const idx = list.value.findIndex((w) => w.id === id);
    if (idx !== -1) {
      const merged = { ...list.value[idx], ...data };
      merged.weight = Number(merged.weight) || 0;
      list.value[idx] = merged;
    }
  }
  function remove(id: number) {
    list.value = list.value.filter((w) => w.id !== id);
  }
  function importRows(rows: HotWord[]) {
    rows.forEach((r) => add(r));
  }
  return { list, getById, add, update, remove, importRows };
});

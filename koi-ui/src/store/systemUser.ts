import { defineStore } from 'pinia';
import { ref } from 'vue';

export type UserRole = '管理员' | '编辑' | '普通用户';
export type UserStatus = '启用' | '禁用';

export interface SystemUser {
  id: number;
  username: string;
  name: string;
  email: string;
  role: UserRole;
  status: UserStatus;
  createdAt: string;
}

const roles: UserRole[] = ['管理员', '编辑', '普通用户'];
const statuses: UserStatus[] = ['启用', '禁用'];

function seed(): SystemUser[] {
  const list: SystemUser[] = [];
  for (let i = 1; i <= 36; i++) {
    list.push({
      id: i,
      username: `user${String(i).padStart(3, '0')}`,
      name: `用户${i}`,
      email: `user${i}@koi.studio`,
      role: roles[i % 3],
      status: statuses[i % 5 === 0 ? 1 : 0],
      createdAt: new Date(Date.now() - i * 86400000).toISOString().slice(0, 10),
    });
  }
  return list;
}

export const useSystemUserStore = defineStore('systemUser', () => {
  const list = ref<SystemUser[]>(seed());
  let nextId = 1000;

  function getById(id: number) {
    return list.value.find((u) => u.id === id);
  }
  function add(data: Omit<SystemUser, 'id' | 'createdAt'>) {
    const item: SystemUser = {
      ...data,
      id: nextId++,
      createdAt: new Date().toISOString().slice(0, 10),
    };
    list.value.unshift(item);
    return item;
  }
  function update(id: number, data: Partial<Omit<SystemUser, 'id'>>) {
    const idx = list.value.findIndex((u) => u.id === id);
    if (idx !== -1) list.value[idx] = { ...list.value[idx], ...data };
  }
  function remove(id: number) {
    list.value = list.value.filter((u) => u.id !== id);
  }
  function importRows(rows: SystemUser[]) {
    rows.forEach((r) => add(r));
  }
  return { list, getById, add, update, remove, importRows };
});

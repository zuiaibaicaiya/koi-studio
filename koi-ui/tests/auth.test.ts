import { beforeEach, describe, expect, test } from '@rstest/core';
import { createApp } from 'vue';
import { createPinia, setActivePinia } from 'pinia';
import { useAuthStore } from '../src/store/auth';
import userApi from '../src/services/userApi';

function freshPinia() {
  const pinia = createPinia();
  createApp({ render: () => null }).use(pinia);
  setActivePinia(pinia);
}

function makeUser(status: string) {
  return {
    id: 1,
    username: 'u',
    nickname: '用户',
    email: 'u@koi.studio',
    phone: '13800000000',
    avatar: '',
    status,
    created_at: '2026-01-01',
    updated_at: '2026-01-01',
  };
}

// 直接替换 userApi 单例上的方法（与 store 引用的是同一对象），无需 mock 框架
let loginCalls: unknown[] = [];
let logoutCalls = 0;

beforeEach(() => {
  freshPinia();
  localStorage.clear();
  loginCalls = [];
  logoutCalls = 0;
  userApi.login = ((p: { username: string; password: string }) => {
    loginCalls.push(p);
    return Promise.resolve({ token: 'TOKEN', user: makeUser('active') });
  }) as never;
  userApi.logout = (() => {
    logoutCalls += 1;
    return Promise.resolve(undefined);
  }) as never;
  userApi.refreshToken = (() => Promise.resolve({ token: 'NEW' })) as never;
  userApi.getCurrentUser = (() => Promise.resolve(makeUser('active'))) as never;
});

describe('useAuthStore - 登录与会话业务', () => {
  test('login 成功：写入 token/user 并返回 true', async () => {
    const s = useAuthStore();
    const ok = await s.login({ username: 'u', password: 'p' });
    expect(ok).toBe(true);
    expect(s.token).toBe('TOKEN');
    expect(s.user?.username).toBe('u');
    expect(s.isAuthenticated).toBe(true);
  });

  test('login 失败：token 不被写入且异常向上抛出', async () => {
    userApi.login = (() => Promise.reject(new Error('invalid'))) as never;
    const s = useAuthStore();
    await expect(s.login({ username: 'u', password: 'p' })).rejects.toThrow('invalid');
    expect(s.token).toBe('');
    expect(s.isAuthenticated).toBe(false);
  });

  test('isAuthenticated 由 token 推导', () => {
    const s = useAuthStore();
    expect(s.isAuthenticated).toBe(false);
    s.token = 'X';
    expect(s.isAuthenticated).toBe(true);
  });

  test('userRole 由 user.status 推导', () => {
    const s = useAuthStore();
    expect(s.userRole).toBe('');
    s.user = makeUser('active');
    expect(s.userRole).toBe('正常用户');
    s.user = makeUser('inactive');
    expect(s.userRole).toBe('已禁用');
  });

  test('clearSession 清空会话并清理本地存储（保留非认证数据）', () => {
    localStorage.setItem('token', 'x');
    localStorage.setItem('theme', 'indigo');
    const s = useAuthStore();
    s.token = 'X';
    s.user = makeUser('active');
    s.clearSession();
    expect(s.token).toBe('');
    expect(s.user).toBeNull();
    expect(localStorage.getItem('token')).toBeNull();
    expect(localStorage.getItem('theme')).toBe('indigo');
  });

  test('logout 先调后端登出再清理本地会话', async () => {
    localStorage.setItem('token', 'x');
    const s = useAuthStore();
    s.token = 'X';
    await s.logout();
    expect(logoutCalls).toBe(1);
    expect(localStorage.getItem('token')).toBeNull();
    expect(s.token).toBe('');
  });

  test('refreshToken 更新 token', async () => {
    const s = useAuthStore();
    await s.refreshToken();
    expect(s.token).toBe('NEW');
  });

  test('fetchCurrentUser 写入当前用户', async () => {
    const s = useAuthStore();
    const r = await s.fetchCurrentUser();
    expect(r.username).toBe('u');
    expect(s.user?.username).toBe('u');
  });

  test('init 无 token 时直接放行，不探活', async () => {
    let called = 0;
    userApi.getCurrentUser = (() => {
      called += 1;
      return Promise.resolve(makeUser('active'));
    }) as never;
    const s = useAuthStore(); // token 默认 ''
    await s.init();
    expect(called).toBe(0);
  });

  test('init 令牌有效：探活成功不清理会话', async () => {
    const s = useAuthStore();
    s.token = 'VALID';
    await s.init();
    expect(s.token).toBe('VALID');
    expect(localStorage.getItem('token')).toBeNull(); // 未被清理
  });

  test('init 探活 401：清理本地会话', async () => {
    userApi.getCurrentUser = (() =>
      Promise.reject(Object.assign(new Error('unauthorized'), { code: 401 }))) as never;
    localStorage.setItem('token', 'x');
    const s = useAuthStore();
    s.token = 'STALE';
    await s.init();
    expect(localStorage.getItem('token')).toBeNull();
    expect(s.token).toBe('');
  });
});

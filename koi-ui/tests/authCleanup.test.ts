import { beforeEach, describe, expect, test } from '@rstest/core';
import { clearAuthStorage, normalizeRedirect } from '../src/utils/authCleanup';

describe('normalizeRedirect', () => {
  test('空 / 根路径 / 登录页自身不产生 redirect', () => {
    expect(normalizeRedirect(undefined)).toBe('');
    expect(normalizeRedirect('/')).toBe('');
    expect(normalizeRedirect('/login')).toBe('');
    expect(normalizeRedirect('/login?next=/a')).toBe('');
  });

  test('业务页路径作为 redirect 原样返回', () => {
    expect(normalizeRedirect('/meeting/1')).toBe('/meeting/1');
    expect(normalizeRedirect('/meeting/1?tab=2')).toBe('/meeting/1?tab=2');
  });
});

describe('clearAuthStorage', () => {
  beforeEach(() => {
    localStorage.clear();
    sessionStorage.clear();
    // 尽力清理 happy-dom 中可能残留的 cookie
    document.cookie.split(';').forEach((p) => {
      const n = p.split('=')[0]?.trim();
      if (n) document.cookie = `${n}=; expires=Thu, 01 Jan 1970 00:00:00 GMT; path=/`;
    });
  });

  test('移除 localStorage 中认证相关 key，保留非认证数据', () => {
    localStorage.setItem('auth', 'x');
    localStorage.setItem('token', 'x');
    localStorage.setItem('session', 'x');
    localStorage.setItem('credential', 'x');
    localStorage.setItem('loginFlag', 'x');
    localStorage.setItem('theme', 'indigo'); // 非认证，必须保留

    clearAuthStorage();

    expect(localStorage.getItem('auth')).toBeNull();
    expect(localStorage.getItem('token')).toBeNull();
    expect(localStorage.getItem('session')).toBeNull();
    expect(localStorage.getItem('loginFlag')).toBeNull();
    expect(localStorage.getItem('theme')).toBe('indigo');
  });

  test('sessionStorage 中的认证 key 同样被清理', () => {
    sessionStorage.setItem('auth', 'x');
    sessionStorage.setItem('loginInfo', 'x');
    sessionStorage.setItem('uiLayout', 'x'); // 非认证保留

    clearAuthStorage();

    expect(sessionStorage.getItem('auth')).toBeNull();
    expect(sessionStorage.getItem('uiLayout')).toBe('x');
  });

  test('重复调用幂等且不抛错', () => {
    localStorage.setItem('token', 'x');
    sessionStorage.setItem('token', 'x');
    expect(() => {
      clearAuthStorage();
      clearAuthStorage();
    }).not.toThrow();
    expect(localStorage.getItem('token')).toBeNull();
    expect(sessionStorage.getItem('token')).toBeNull();
  });

  test('无认证数据时调用也不抛错', () => {
    expect(() => clearAuthStorage()).not.toThrow();
  });
});

import { defineStore } from 'pinia';
import { ref, computed } from 'vue';

export interface UserInfo {
  username: string;
  role: string;
  avatar?: string;
  email?: string;
}

export interface LoginPayload {
  username: string;
  password: string;
}

// Mock backend. Replace with a real API call.
function mockLogin(payload: LoginPayload): Promise<{ token: string; user: UserInfo }> {
  return new Promise((resolve, reject) => {
    setTimeout(() => {
      if (payload.username === 'admin' && payload.password === '123456') {
        resolve({
          token: `mock-token-${Date.now()}`,
          user: {
            username: payload.username,
            role: 'Administrator',
            email: 'admin@koi.studio',
          },
        });
      } else {
        reject(new Error('用户名或密码错误'));
      }
    }, 800);
  });
}

export const useAuthStore = defineStore(
  'auth',
  () => {
    const token = ref<string>('');
    const user = ref<UserInfo | null>(null);
    const loading = ref(false);

    const isAuthenticated = computed(() => !!token.value);

    async function login(payload: LoginPayload) {
      loading.value = true;
      try {
        const { token: t, user: u } = await mockLogin(payload);
        token.value = t;
        user.value = u;
        return true;
      } finally {
        loading.value = false;
      }
    }

    function logout() {
      token.value = '';
      user.value = null;
    }

    return { token, user, loading, isAuthenticated, login, logout };
  },
  {
    persist: true,
  },
);

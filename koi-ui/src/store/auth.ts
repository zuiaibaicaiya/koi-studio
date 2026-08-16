import { defineStore } from 'pinia';
import { ref, computed } from 'vue';
import userApi, {
  type UserDTO,
  type LoginPayload,
  type UpdateProfilePayload,
  type ChangePasswordPayload,
  type AuthResult,
} from '../services/userApi';

/** 用户信息（与后端 User 模型对齐） */
export { type UserDTO as UserInfo };

export { type LoginPayload, type UpdateProfilePayload, type ChangePasswordPayload };

export const useAuthStore = defineStore(
  'auth',
  () => {
    const token = ref<string>('');
    const user = ref<UserDTO | null>(null);
    const loading = ref(false);

    const isAuthenticated = computed(() => !!token.value);

    /** 角色展示：根据 status 或其他字段计算 */
    const userRole = computed(() => {
      if (!user.value) return '';
      return user.value.status === 'active' ? '正常用户' : '已禁用';
    });

    /** 登录 */
    async function login(payload: LoginPayload) {
      loading.value = true;
      try {
        const result: AuthResult = await userApi.login(payload);
        token.value = result.token;
        user.value = result.user;
        return true;
      } finally {
        loading.value = false;
      }
    }

    /** 刷新令牌 */
    async function refreshToken() {
      const result = await userApi.refreshToken();
      token.value = result.token;
    }

    /** 获取当前用户信息 */
    async function fetchCurrentUser() {
      const current = await userApi.getCurrentUser();
      user.value = current;
      return current;
    }

    /** 更新当前用户资料 */
    async function updateProfile(payload: UpdateProfilePayload) {
      const updated = await userApi.updateProfile(payload);
      user.value = updated;
      return updated;
    }

    /** 修改密码 */
    async function changePassword(payload: ChangePasswordPayload) {
      return await userApi.changePassword(payload);
    }

    /** 清除本地会话（token + 用户信息），并同步持久化存储。 */
    function clearSession() {
      token.value = '';
      user.value = null;
    }

    /**
     * 启动时校验持久化令牌：localStorage 中可能残留「非空但已失效」的令牌，
     * 若直接信任会让路由守卫放行进入受保护页面，却又因 401 而接口全部失败。
     * 此处向后端探活一次，仅在确认令牌失效（401）时清除本地会话；
     * 清除后路由守卫会自动将其重定向至登录页。
     */
    async function init() {
      if (!token.value) return;
      // 探活：仅在确认令牌失效（401）时清除会话；后端不可达等异常不清除，
      // 由页面接口后续的 401 兜底登出处理。.catch 兜底避免悬空 Promise 抛未捕获异常。
      const probe = fetchCurrentUser().catch((err: unknown) => {
        if ((err as { code?: number })?.code === 401) clearSession();
      });
      // 后端不可达时最多等待 3s，超时则直接放行启动（避免阻塞首屏）。
      await Promise.race([probe, new Promise((resolve) => setTimeout(resolve, 3000))]);
    }

    /** 登出 */
    async function logout() {
      try {
        await userApi.logout();
      } catch {
        // 即使后端登出失败也清除本地状态
      } finally {
        clearSession();
      }
    }

    return {
      token,
      user,
      loading,
      isAuthenticated,
      userRole,
      login,
      clearSession,
      refreshToken,
      fetchCurrentUser,
      init,
      updateProfile,
      changePassword,
      logout,
    };
  },
  {
    persist: true,
  },
);

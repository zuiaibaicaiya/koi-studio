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
      updateProfile,
      changePassword,
      logout,
    };
  },
  {
    persist: true,
  },
);

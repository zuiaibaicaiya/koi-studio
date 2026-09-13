<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted } from 'vue';
import { useRouter } from 'vue-router';
import { App } from 'antdv-next';
const { message, modal } = App.useApp();
import { LogoutOutlined } from '@antdv-next/icons';
import { useAuthStore } from '../store/auth';

const router = useRouter();
const auth = useAuthStore();

const currentUser = computed(() => auth.user);
const displayName = computed(
  () => currentUser.value?.nickname || currentUser.value?.username || '未登录',
);
/** 无头像时展示名称首字母 */
const avatarText = computed(() => displayName.value.charAt(0).toUpperCase() || 'U');

let confirming = false;

/** 退出登录：弹确认框，确认后清除会话并跳转登录页 */
function showLogoutConfirm() {
  if (confirming) return;
  confirming = true;
  modal.confirm({
    title: '退出登录',
    content: '确认要退出当前账号吗？退出后需要重新登录才能访问系统。',
    okText: '确认退出',
    cancelText: '取消',
    okType: 'danger',
    onOk: async () => {
      await auth.logout();
      message.success('已退出登录');
      router.replace('/login');
    },
    onClose: () => {
      confirming = false;
    },
  });
}

// 退出登录快捷键（Ctrl/Cmd + Shift + Q）：组件仅在已登录时挂载，无需再判断登录态
function onKeydown(e: KeyboardEvent) {
  if ((e.ctrlKey || e.metaKey) && e.shiftKey && (e.key === 'Q' || e.key === 'q')) {
    e.preventDefault();
    showLogoutConfirm();
  }
}

onMounted(() => window.addEventListener('keydown', onKeydown));
onBeforeUnmount(() => window.removeEventListener('keydown', onKeydown));
</script>

<template>
  <a-dropdown placement="bottomRight" :trigger="['click']">
    <button class="user-trigger" type="button" :title="displayName" aria-label="账号信息">
      <a-avatar :src="currentUser?.avatar" :size="26" class="user-avatar">
        <template v-if="!currentUser?.avatar">{{ avatarText }}</template>
      </a-avatar>
    </button>

    <template #popupRender>
      <a-menu class="user-menu">
        <a-menu-item-group>
          <template #title>
            <div class="user-card">
              <a-avatar :src="currentUser?.avatar" :size="40">
                <template v-if="!currentUser?.avatar">{{ avatarText }}</template>
              </a-avatar>
              <div class="user-card-meta">
                <div class="user-card-name">{{ displayName }}</div>
                <div class="user-card-role">{{ auth.userRole }}</div>
                <div class="user-card-email">{{ currentUser?.email || '—' }}</div>
              </div>
            </div>
          </template>
        </a-menu-item-group>
        <a-menu-divider />
        <a-menu-item key="logout" @click="showLogoutConfirm">
          <LogoutOutlined />
          退出登录
          <span class="shortcut">Ctrl/⌘ + Shift + Q</span>
        </a-menu-item>
      </a-menu>
    </template>
  </a-dropdown>
</template>

<style scoped>
/* 标题栏里只保留一枚头像图标，空间紧凑，悬停时用柔环提示可点击 */
.user-trigger {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 30px;
  height: 30px;
  padding: 0;
  border: none;
  border-radius: 50%;
  background: transparent;
  cursor: pointer;
  transition: box-shadow 0.18s ease;
}
.user-trigger:hover {
  box-shadow: 0 0 0 2px var(--color-brand-soft);
}
.user-trigger:focus-visible {
  outline: 2px solid var(--color-brand);
  outline-offset: 1px;
}
.user-avatar {
  background: linear-gradient(135deg, var(--color-brand), var(--color-accent));
  color: #fff;
  font-size: 12px;
  flex: none;
}
.user-menu {
  min-width: 240px;
}
.user-card {
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 4px 0;
}
.user-card-meta {
  display: flex;
  flex-direction: column;
  gap: 2px;
  min-width: 0;
}
.user-card-name {
  font-size: 14px;
  font-weight: 600;
  color: var(--color-text);
}
.user-card-role {
  font-size: 12px;
  color: var(--color-brand);
}
.user-card-email {
  font-size: 12px;
  color: var(--color-text-muted);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.shortcut {
  margin-left: auto;
  font-size: 11px;
  color: var(--color-text-muted);
}
</style>

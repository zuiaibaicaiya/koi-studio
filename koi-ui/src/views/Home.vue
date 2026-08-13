<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted } from 'vue';
import { useRouter } from 'vue-router';
import { useAuthStore } from '../store/auth';
import { App } from 'antdv-next';
const { message, modal } = App.useApp();
import {
  VideoCameraOutlined,
  AudioOutlined,
  SettingOutlined,
  LogoutOutlined,
  DownOutlined,
} from '@antdv-next/icons';
import ThemeToggle from '../components/ThemeToggle.vue';

const router = useRouter();
const auth = useAuthStore();

const currentUser = computed(() => auth.user);

// 退出登录：弹确认框，确认后清除会话并跳转登录页
let confirming = false;
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

// 键盘快捷键退出（Ctrl/Cmd + Shift + Q）
function onKeydown(e: KeyboardEvent) {
  if ((e.ctrlKey || e.metaKey) && e.shiftKey && (e.key === 'Q' || e.key === 'q')) {
    e.preventDefault();
    showLogoutConfirm();
  }
}

onMounted(() => {
  if (auth.isAuthenticated) {
    window.addEventListener('keydown', onKeydown);
  }
});
onBeforeUnmount(() => {
  window.removeEventListener('keydown', onKeydown);
});

const modules = [
  {
    key: 'meeting',
    title: '实时会议',
    desc: '发起或加入实时音视频会议，支持同声转写与字幕。',
    icon: VideoCameraOutlined,
    color: '#1677ff',
    to: 'liveCreate',
  },
  {
    key: 'transcribe',
    title: '音频转写',
    desc: '上传音频文件，自动识别说话人并生成文字稿与热词标记。',
    icon: AudioOutlined,
    color: '#52c41a',
  },
  {
    key: 'system',
    title: '系统管理',
    desc: '管理用户、角色权限、热词库、说话人与会议配置。',
    icon: SettingOutlined,
    color: '#faad14',
    to: 'dashboard',
  },
];

function open(to?: string) {
  if (to) router.push({ name: to });
}

function openModule(m: { to?: string }) {
  open(m.to);
}
</script>

<template>
  <div class="home">
    <header class="home-header">
      <div class="header-right">
        <ThemeToggle class="theme-switch" />
        <a-dropdown placement="bottomRight" :trigger="['click']">
          <span class="user">
            <a-avatar :src="currentUser?.avatar" class="user-avatar">
              <template v-if="!currentUser?.avatar">{{ currentUser?.nickname?.charAt(0) || currentUser?.username?.charAt(0)?.toUpperCase() || 'U' }}</template>
            </a-avatar>
            <span class="user-meta">
              <span class="username">{{ currentUser?.nickname || currentUser?.username || '未登录' }}</span>
              <span class="user-role">{{ auth.userRole }}</span>
            </span>
            <DownOutlined class="user-caret" />
          </span>
          <template #popupRender>
            <a-menu class="user-menu">
              <a-menu-item-group>
                <template #title>
                  <div class="user-card">
                    <a-avatar :src="currentUser?.avatar" :size="40">
                      <template v-if="!currentUser?.avatar">{{ currentUser?.nickname?.charAt(0) || currentUser?.username?.charAt(0)?.toUpperCase() || 'U' }}</template>
                    </a-avatar>
                    <div class="user-card-meta">
                      <div class="user-card-name">{{ currentUser?.nickname || currentUser?.username || '未登录' }}</div>
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
      </div>
    </header>

    <a-row :gutter="[16, 16]">
      <a-col v-for="m in modules" :key="m.key" :xs="24" :sm="12" :lg="8">
        <a-card class="module-card" hoverable @click="openModule(m)">
          <div class="module-icon" :style="{ background: m.color }">
            <component :is="m.icon" />
          </div>
          <div class="module-title">{{ m.title }}</div>
          <div class="module-desc">{{ m.desc }}</div>
        </a-card>
      </a-col>
    </a-row>
  </div>
</template>

<style scoped>
.home {
  min-height: 100vh;
  padding: 0 24px 24px;
  background:
    radial-gradient(
      900px 520px at 88% -8%,
      var(--color-brand-soft),
      transparent 62%
    ),
    radial-gradient(
      760px 480px at 4% 108%,
      color-mix(in srgb, var(--color-accent) 16%, transparent),
      transparent 60%
    ),
    radial-gradient(
      120% 80% at 50% 0%,
      color-mix(in srgb, var(--color-brand-soft) 60%, transparent),
      transparent 70%
    ),
    var(--color-bg);
  color: var(--color-text);
}
.home-header {
  position: sticky;
  top: 0;
  display: flex;
  justify-content: flex-end;
  height: 56px;
  margin: 0 -24px 0;
  padding: 0 24px;
  align-items: center;
  background: color-mix(in srgb, var(--color-bg) 80%, transparent);
  backdrop-filter: blur(8px);
  -webkit-backdrop-filter: blur(8px);
  z-index: 10;
}
.header-right {
  display: flex;
  align-items: center;
  gap: 12px;
}
.theme-switch {
  margin-right: 0;
  display: inline-flex;
  align-items: center;
}
.user {
  display: flex;
  align-items: center;
  gap: 8px;
  cursor: pointer;
  color: var(--color-text);
  padding: 4px 8px;
  border-radius: var(--radius-md);
  transition: background 0.2s;
}
.user:hover {
  background: var(--color-surface-2);
}
.user-avatar {
  background: linear-gradient(135deg, var(--color-brand), var(--color-accent));
  flex: none;
}
.user-meta {
  display: flex;
  flex-direction: column;
  line-height: 1.2;
}
.username {
  font-size: 14px;
  font-weight: 500;
}
.user-role {
  font-size: 12px;
  color: var(--color-text-muted);
}
.user-caret {
  font-size: 11px;
  color: var(--color-text-muted);
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
.module-card {
  margin-top: 80px;
  background: var(--color-surface);
  border: 1px solid var(--color-border);
  color: var(--color-text);
  height: 100%;
  border-radius: var(--radius-lg);
  box-shadow: var(--shadow-sm);
  transition: transform 0.2s ease, box-shadow 0.2s ease, border-color 0.2s ease;
}
.module-card:hover {
  transform: translateY(-4px);
  box-shadow: var(--shadow-md);
  border-color: var(--color-brand);
}
.module-icon {
  width: 52px;
  height: 52px;
  border-radius: var(--radius-lg);
  display: flex;
  align-items: center;
  justify-content: center;
  font-size: 26px;
  color: #fff;
  margin-bottom: 16px;
}
.module-title {
  font-size: 18px;
  font-weight: 600;
  color: var(--color-text);
  margin-bottom: 8px;
}
.module-desc {
  color: var(--color-text-secondary);
  font-size: 13px;
  line-height: 1.6;
  min-height: 42px;
}
</style>

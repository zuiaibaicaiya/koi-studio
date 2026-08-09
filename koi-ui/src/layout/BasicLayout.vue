<script setup lang="ts">
import { computed, h, onBeforeUnmount, onMounted, ref } from 'vue';
import { useRoute, useRouter } from 'vue-router';
import { useAuthStore } from '../store/auth';
import { useThemeStore } from '../store/theme';
import { message, Modal } from 'antdv-next';
import {
  HomeOutlined,
  DashboardOutlined,
  TeamOutlined,
  TagsOutlined,
  SoundOutlined,
  ScheduleOutlined,
  LogoutOutlined,
  MenuFoldOutlined,
  MenuUnfoldOutlined,
  DownOutlined,
} from '@antdv-next/icons';
import ThemeToggle from '../components/ThemeToggle.vue';

const themeStore = useThemeStore();
const isDark = computed(() => themeStore.isDark);

const auth = useAuthStore();
const router = useRouter();
const route = useRoute();
const collapsed = ref(false);

const selectedKeys = computed(() => [route.name as string]);

const currentTitle = computed(() => (route.meta.title as string) || '系统管理');

const currentUser = computed(() => auth.user);

const menuItems = [
  { key: 'home', icon: () => h(HomeOutlined), label: '首页' },
  { key: 'dashboard', icon: () => h(DashboardOutlined), label: '仪表盘' },
  { key: 'users', icon: () => h(TeamOutlined), label: '用户管理' },
  { key: 'hotWords', icon: () => h(TagsOutlined), label: '热词库管理' },
  { key: 'speakers', icon: () => h(SoundOutlined), label: '说话人管理' },
  { key: 'meetings', icon: () => h(ScheduleOutlined), label: '会议管理' },
];

function handleMenuClick({ key }: { key: string }) {
  router.push({ name: key });
}

// 退出登录：弹确认框，确认后清除会话并跳转登录页
let confirming = false;
function showLogoutConfirm() {
  if (confirming) return;
  confirming = true;
  Modal.confirm({
    title: '退出登录',
    content: '确认要退出当前账号吗？退出后需要重新登录才能访问系统。',
    okText: '确认退出',
    cancelText: '取消',
    okType: 'danger',
    onOk: () => {
      auth.logout();
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
</script>

<template>
  <a-layout class="basic-layout">
    <a-layout-sider
      v-model:collapsed="collapsed"
      :width="220"
      :collapsed-width="64"
      collapsible
      :trigger="null"
      :class="{ 'is-dark': isDark }"
      class="sider"
    >
      <div class="logo">
        <span class="logo-mark">K</span>
        <span v-show="!collapsed" class="logo-text">Koi Studio</span>
      </div>
      <a-menu
        :theme="isDark ? 'dark' : 'light'"
        mode="inline"
        :selected-keys="selectedKeys"
        :items="menuItems"
        @click="handleMenuClick"
      />
    </a-layout-sider>

    <a-layout>
      <a-layout-header class="header">
        <div class="header-left">
          <component
            :is="collapsed ? MenuUnfoldOutlined : MenuFoldOutlined"
            class="trigger"
            @click="collapsed = !collapsed"
          />
          <a-breadcrumb class="breadcrumb">
            <a-breadcrumb-item>{{ currentTitle }}</a-breadcrumb-item>
          </a-breadcrumb>
        </div>
        <div class="header-right">
          <ThemeToggle class="theme-switch" />
          <a-dropdown placement="bottomRight" :trigger="['click']">
            <span class="user">
              <a-avatar :src="currentUser?.avatar" class="user-avatar">
                <template v-if="!currentUser?.avatar">{{ currentUser?.username?.charAt(0)?.toUpperCase() || 'U' }}</template>
              </a-avatar>
              <span class="user-meta">
                <span class="username">{{ currentUser?.username || '未登录' }}</span>
                <span class="user-role">{{ currentUser?.role || '—' }}</span>
              </span>
              <DownOutlined class="user-caret" />
            </span>
            <template #popupRender>
              <a-menu class="user-menu">
                <a-menu-item-group>
                  <template #title>
                    <div class="user-card">
                      <a-avatar :src="currentUser?.avatar" :size="40">
                        <template v-if="!currentUser?.avatar">{{ currentUser?.username?.charAt(0)?.toUpperCase() || 'U' }}</template>
                      </a-avatar>
                      <div class="user-card-meta">
                        <div class="user-card-name">{{ currentUser?.username || '未登录' }}</div>
                        <div class="user-card-role">{{ currentUser?.role || '—' }}</div>
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
      </a-layout-header>

      <a-layout-content class="content">
        <router-view v-slot="{ Component }">
          <component :is="Component" />
        </router-view>
      </a-layout-content>
    </a-layout>
  </a-layout>
</template>

<style scoped>
.basic-layout {
  min-height: 100vh;
  background: var(--color-bg);
}
.sider {
  overflow: auto;
  background: var(--color-surface) !important;
}
/* 深色主题：侧边栏切换为深色底 */
.sider.is-dark {
  background: var(--color-sider-bg) !important;
}
.sider :deep(.ant-menu) {
  background: transparent !important;
  border-inline-end: none !important;
}
.sider :deep(.ant-menu-item) {
  color: var(--color-text) !important;
}
.sider :deep(.ant-menu-item:hover) {
  background: var(--color-surface-2) !important;
}
.sider.is-dark :deep(.ant-menu-item) {
  color: var(--color-sider-text) !important;
}
.sider.is-dark :deep(.ant-menu-item:hover) {
  background: rgba(255, 255, 255, 0.08) !important;
}
.sider :deep(.ant-menu-item-selected) {
  background: var(--color-brand-soft);
  color: var(--color-brand) !important;
}
.sider.is-dark :deep(.ant-menu-item-selected) {
  background: var(--color-brand-soft);
  color: var(--color-brand-hover) !important;
}
.sider :deep(.ant-menu-item-selected::after) {
  border-inline-end-color: var(--color-brand) !important;
}
.logo {
  height: 56px;
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 0 16px;
  color: var(--color-text-inverse);
  font-weight: 600;
  font-size: 16px;
  letter-spacing: 0.5px;
}
.logo-mark {
  width: 28px;
  height: 28px;
  border-radius: var(--radius-md);
  background: linear-gradient(135deg, var(--color-brand), var(--color-accent));
  display: flex;
  align-items: center;
  justify-content: center;
  color: #fff;
  font-weight: 700;
}
.header {
  background: var(--color-surface);
  border-bottom: 1px solid var(--color-border);
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 0 16px;
  height: 56px;
  line-height: normal;
  position: sticky;
  top: 0;
  z-index: 10;
}
.header-left {
  display: flex;
  align-items: center;
  gap: 16px;
}
.trigger {
  font-size: 18px;
  color: var(--color-text-secondary);
  cursor: pointer;
  transition: color 0.2s;
}
.trigger:hover {
  color: var(--color-brand);
}
.breadcrumb :deep(.ant-breadcrumb-link),
.breadcrumb :deep(.ant-breadcrumb-separator) {
  color: var(--color-text-secondary);
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
.content {
  margin: 16px;
}
</style>

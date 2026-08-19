<script setup lang="ts">
import { computed, h, ref } from 'vue';
import { useRoute, useRouter } from 'vue-router';
import { useThemeStore } from '../store/theme';
import {
  DashboardOutlined,
  TeamOutlined,
  TagsOutlined,
  SoundOutlined,
  ScheduleOutlined,
  MenuFoldOutlined,
  MenuUnfoldOutlined,
  ArrowLeftOutlined,
} from '@antdv-next/icons';

const themeStore = useThemeStore();
const isDark = computed(() => themeStore.isDark);

const router = useRouter();
const route = useRoute();
const collapsed = ref(false);

const selectedKeys = computed(() => [route.name as string]);

const currentTitle = computed(() => (route.meta.title as string) || '系统管理');

const menuItems = [
  { key: 'dashboard', icon: () => h(DashboardOutlined), label: '仪表盘' },
  { key: 'users', icon: () => h(TeamOutlined), label: '用户管理' },
  { key: 'hotWords', icon: () => h(TagsOutlined), label: '热词库管理' },
  { key: 'speakers', icon: () => h(SoundOutlined), label: '说话人管理' },
  { key: 'meetings', icon: () => h(ScheduleOutlined), label: '会议管理' },
];

function handleMenuClick({ key }: { key: string }) {
  router.push({ name: key });
}

function goHome() {
  router.push({ name: 'home' });
}
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
      <a-button type="text" class="logo" @click="goHome">
        <template #icon><ArrowLeftOutlined class="logo-icon" /></template>
        <span v-show="!collapsed" class="logo-text">返回首页</span>
      </a-button>
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
  margin: 0;
  position: relative;
  color: var(--color-text-secondary) !important;
  font-weight: 500;
  font-size: 16px;
  letter-spacing: 0.5px;
  width: 100%;
  justify-content: flex-start;
  border-radius: 0;
  transition: color 0.2s ease, background 0.2s ease;
  overflow: hidden;
}
.logo-icon {
  font-size: 17px;
  line-height: 1;
  transition: transform 0.2s ease;
}
.logo-text {
  font-size: 15px;
  font-weight: 500;
  letter-spacing: 0.4px;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
  transition: opacity 0.2s ease;
}
.logo:hover .logo-icon {
  transform: translateX(-2px);
}
/* 左侧品牌色竖条点缀，hover/聚焦时展开 */
.logo::before {
  content: '';
  position: absolute;
  left: 0;
  top: 50%;
  width: 3px;
  height: 0;
  border-radius: 0 3px 3px 0;
  background: var(--color-brand);
  transform: translateY(-50%);
  transition: height 0.2s ease;
}
/* 深色主题：侧边栏为深色底，按钮文字切换为更柔和的浅色 */
.sider.is-dark .logo {
  color: var(--color-sider-text-muted) !important;
}
.logo:hover,
.logo:focus-visible,
.sider.is-dark .logo:hover,
.sider.is-dark .logo:focus-visible {
  color: var(--color-brand) !important;
  background: var(--color-surface-2);
}
.logo:hover::before,
.logo:focus-visible::before,
.sider.is-dark .logo:hover::before,
.sider.is-dark .logo:focus-visible::before {
  height: 22px;
}
.sider.is-dark .logo:hover,
.sider.is-dark .logo:focus-visible {
  background: rgba(255, 255, 255, 0.08);
}
.logo:focus-visible {
  outline: 2px solid var(--color-brand);
  outline-offset: -2px;
}
.header {
  background: var(--color-surface);
  border-bottom: 1px solid var(--color-border);
  display: flex;
  align-items: center;
  padding: 0 var(--layout-gutter);
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
.content {
  width: 100%;
  box-sizing: border-box;
  padding: 0 var(--layout-gutter);
  margin: 20px 0;
}
</style>

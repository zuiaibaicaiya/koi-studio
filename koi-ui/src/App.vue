<script setup lang="ts">
import { computed } from 'vue';
import type { ThemeConfig } from 'antdv-next';
import { theme, App as AntdApp } from 'antdv-next';
import { useThemeStore } from './store/theme';

const themeStore = useThemeStore();
themeStore.init();

// 明/暗两套 antd 主题：通过官方算法（defaultAlgorithm / darkAlgorithm）派生所有组件样式，
// 确保 button、input 等控件在暗黑下正确渲染。品牌主色保持一致，仅叠加 token 覆盖。
const antdTheme = computed<ThemeConfig>(() => {
  const dark = themeStore.isDark;
  const fontFamily =
    "'PingFang SC', 'Microsoft YaHei', -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, 'Helvetica Neue', Arial, 'Noto Sans', sans-serif";

  return {
    algorithm: dark ? theme.darkAlgorithm : theme.defaultAlgorithm,
    token: {
      colorPrimary: '#1677ff',
      colorInfo: '#1677ff',
      fontFamily,
    },
  };
});
</script>

<template>
  <a-config-provider :theme="antdTheme">
    <AntdApp>
      <router-view />
    </AntdApp>
  </a-config-provider>
</template>

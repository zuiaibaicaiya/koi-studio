<script setup lang="ts">
import { computed } from 'vue';
import type { ThemeConfig } from 'antdv-next';
import { theme, App as AntdApp } from 'antdv-next';
import zhCN from 'antdv-next/locale/zh_CN';
import { useThemeStore } from './store/theme';

const themeStore = useThemeStore();
themeStore.init();

// 明/暗两套 antd 主题：通过官方算法（defaultAlgorithm / darkAlgorithm）派生所有组件样式，
// 确保各控件在暗黑下正确渲染。主色采用 geekblue（沉稳企业蓝），叠加组件级 token 精修。
const antdTheme = computed<ThemeConfig>(() => {
  const dark = themeStore.isDark;
  const fontFamily =
    "'PingFang SC', 'Microsoft YaHei', -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, 'Helvetica Neue', Arial, 'Noto Sans', sans-serif";
  const brand = dark ? '#597ef7' : '#2f54eb';

  return {
    algorithm: dark ? theme.darkAlgorithm : theme.defaultAlgorithm,
    token: {
      colorPrimary: brand,
      colorInfo: brand,
      colorLink: brand,
      fontFamily,
      fontSize: 14,
      borderRadius: 6,
      wireframe: false,
    },
    components: {
      Button: {
        fontWeight: 500,
        borderRadius: 6,
        primaryShadow: 'none',
        defaultShadow: 'none',
        dangerShadow: 'none',
      },
      Card: {
        borderRadiusLG: 8,
        paddingLG: 24,
      },
      Table: {
        headerBg: dark ? '#1f1f1f' : '#fafbfc',
        headerColor: dark ? 'rgba(255,255,255,0.65)' : 'rgba(0,0,0,0.65)',
        rowHoverBg: dark ? '#1f1f1f' : '#fafbfc',
        borderColor: dark ? '#303030' : '#f0f1f3',
        headerSplitColor: dark ? '#303030' : '#f0f1f3',
        cellPaddingBlock: 14,
      },
      Input: {
        borderRadius: 6,
        activeShadow: '0 0 0 2px rgba(47, 84, 235, 0.12)',
      },
      InputNumber: {
        borderRadius: 6,
      },
      Select: {
        borderRadius: 6,
      },
      DatePicker: {
        borderRadius: 6,
      },
      Menu: {
        itemSelectedBg: dark ? '#111d2c' : '#f0f4ff',
        itemSelectedColor: dark ? '#85a5ff' : '#2f54eb',
        itemHoverBg: dark ? 'rgba(255,255,255,0.08)' : '#fafbfc',
        itemBorderRadius: 6,
      },
      Modal: {
        borderRadiusLG: 10,
      },
      Drawer: {
        borderRadiusLG: 10,
      },
      Segmented: {
        borderRadius: 6,
        itemSelectedBg: dark ? '#1f1f1f' : '#ffffff',
      },
      Tag: {
        borderRadiusSM: 4,
      },
      Tabs: {
        itemActiveColor: brand,
        inkBarColor: brand,
      },
      Pagination: {
        itemActiveBg: dark ? '#111d2c' : '#f0f4ff',
      },
    },
  };
});
</script>

<template>
  <a-config-provider :theme="antdTheme" :locale="zhCN">
    <AntdApp>
      <router-view />
    </AntdApp>
  </a-config-provider>
</template>

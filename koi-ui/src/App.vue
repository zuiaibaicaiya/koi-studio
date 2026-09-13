<script setup lang="ts">
import { computed } from 'vue';
import type { ThemeConfig } from 'antdv-next';
import { theme, App as AntdApp } from 'antdv-next';
import zhCN from 'antdv-next/locale/zh_CN';
import TitleBar from './components/TitleBar.vue';
import { useThemeStore } from './store/theme';
import { presetMeta } from './theme/presets';

const themeStore = useThemeStore();
themeStore.init();

/** 把 #rrggbb 转成带透明度的 rgba()，用于 antd 的聚焦光圈等需要淡色的令牌 */
function withAlpha(hex: string, alpha: number) {
  const n = parseInt(hex.slice(1), 16);
  return `rgba(${(n >> 16) & 255}, ${(n >> 8) & 255}, ${n & 255}, ${alpha})`;
}

// antd 主题完全由「色系 × 明暗」推导：算法负责派生组件样式细节，主色/浅底/圆角来自色系，
// 保证自定义布局与 antd 组件在 12 套外观下始终同源。
const antdTheme = computed<ThemeConfig>(() => {
  const dark = themeStore.isDark;
  const preset = presetMeta(themeStore.preset);
  const r = preset.radius;
  const fontFamily =
    "'PingFang SC', 'Microsoft YaHei', -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, 'Helvetica Neue', Arial, 'Noto Sans', sans-serif";
  const brand = dark ? preset.primaryDark : preset.primaryLight;
  const brandSoft = dark ? preset.softDark : preset.softLight;

  return {
    algorithm: dark ? theme.darkAlgorithm : theme.defaultAlgorithm,
    token: {
      colorPrimary: brand,
      colorInfo: brand,
      colorLink: brand,
      fontFamily,
      fontSize: 14,
      borderRadius: r.md,
      wireframe: false,
    },
    components: {
      Button: {
        fontWeight: 500,
        borderRadius: r.md,
        primaryShadow: 'none',
        defaultShadow: 'none',
        dangerShadow: 'none',
      },
      Card: {
        borderRadiusLG: r.lg,
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
        borderRadius: r.md,
        activeShadow: `0 0 0 2px ${withAlpha(brand, 0.12)}`,
      },
      InputNumber: {
        borderRadius: r.md,
      },
      Select: {
        borderRadius: r.md,
      },
      DatePicker: {
        borderRadius: r.md,
      },
      Menu: {
        itemSelectedBg: brandSoft,
        itemSelectedColor: brand,
        itemHoverBg: dark ? 'rgba(255,255,255,0.08)' : '#fafbfc',
        itemBorderRadius: r.md,
      },
      Modal: {
        borderRadiusLG: r.lg,
      },
      Drawer: {
        borderRadiusLG: r.lg,
      },
      Segmented: {
        borderRadius: r.md,
        itemSelectedBg: dark ? '#1f1f1f' : '#ffffff',
      },
      Tag: {
        borderRadiusSM: r.sm,
      },
      Tabs: {
        itemActiveColor: brand,
        inkBarColor: brand,
      },
      Pagination: {
        itemActiveBg: brandSoft,
      },
    },
  };
});
</script>

<template>
  <a-config-provider :theme="antdTheme" :locale="zhCN">
    <AntdApp>
      <div class="app-shell">
        <!-- 自绘标题栏：占据窗口顶部，作为全局窗口拖拽区 -->
        <TitleBar />
        <div class="app-body">
          <router-view />
        </div>
      </div>
    </AntdApp>
  </a-config-provider>
</template>

<style scoped>
/* 整个应用固定为视口高度：标题栏 + 内容区，内容区自行滚动，
   避免页面级 100vh 把窗口撑高到标题栏之外 */
.app-shell {
  display: flex;
  flex-direction: column;
  height: 100vh;
  overflow: hidden;
}

.app-body {
  flex: 1 1 auto;
  min-height: 0;
  overflow: auto;
}
</style>

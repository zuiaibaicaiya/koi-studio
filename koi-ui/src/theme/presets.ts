/**
 * 主题色系（preset）元数据。
 *
 * 这里只放「UI 需要知道、但 CSS 表达不了」的那部分：名称、色调说明、预览色带，
 * 以及 antd 组件令牌必须用到的少量色值（主色 / 浅底 / 圆角）。
 * 完整的品牌令牌（hover / active / 深色侧栏底等）在 `src/index.css` 的
 * `[data-preset='*']` 段落里定义，两处 key 必须一一对应。
 *
 * 六个色系来自锦鲤池与东方矿物色：靛青是默认的沉稳蓝，其余为青、绿、紫、金、灰，
 * 让「同一套界面」在不同团队/场景下换一种气质，而不是换一层皮。
 */
export type ThemePreset = 'indigo' | 'teal' | 'celadon' | 'plum' | 'amber' | 'graphite';

/** 形态层：圆角尺度，随色系变化，由 store 写入 CSS 变量 */
export interface PresetRadius {
  sm: number;
  md: number;
  lg: number;
  xl: number;
}

export interface ThemePresetMeta {
  key: ThemePreset;
  /** 色系名，用于选择器 */
  label: string;
  /** 一句话色调说明 */
  hint: string;
  /** 浅色模式主色 */
  primaryLight: string;
  /** 深色模式主色 */
  primaryDark: string;
  /** 浅色模式的浅底/选中底 */
  softLight: string;
  /** 深色模式的浅底/选中底 */
  softDark: string;
  /** 预览色带：[起点, 终点]，用于渐变示色 */
  swatch: [string, string];
  /** 圆角尺度 */
  radius: PresetRadius;
}

/** 默认色系：靛青，与 index.css 的 :root 兜底取值一致 */
export const DEFAULT_PRESET: ThemePreset = 'indigo';

const DEFAULT_RADIUS: PresetRadius = { sm: 4, md: 6, lg: 8, xl: 12 };

export const THEME_PRESETS: ThemePresetMeta[] = [
  {
    key: 'indigo',
    label: '靛青',
    hint: '蓝 · 沉稳理性',
    primaryLight: '#2f54eb',
    primaryDark: '#597ef7',
    softLight: '#f0f4ff',
    softDark: '#111d2c',
    swatch: ['#2f54eb', '#597ef7'],
    radius: DEFAULT_RADIUS,
  },
  {
    key: 'teal',
    label: '鲛青',
    hint: '青 · 清透冷静',
    primaryLight: '#0e7490',
    primaryDark: '#38a3bd',
    softLight: '#e6f5f9',
    softDark: '#082a33',
    swatch: ['#0e7490', '#38a3bd'],
    radius: DEFAULT_RADIUS,
  },
  {
    key: 'celadon',
    label: '青瓷',
    hint: '绿 · 自然柔和',
    primaryLight: '#2e7d63',
    primaryDark: '#46a183',
    softLight: '#e9f4ef',
    softDark: '#0e2a21',
    swatch: ['#2e7d63', '#46a183'],
    radius: DEFAULT_RADIUS,
  },
  {
    key: 'plum',
    label: '绛紫',
    hint: '紫 · 典雅个性',
    primaryLight: '#722ed1',
    primaryDark: '#9254de',
    softLight: '#f6f0ff',
    softDark: '#1e1430',
    swatch: ['#722ed1', '#9254de'],
    radius: DEFAULT_RADIUS,
  },
  {
    key: 'amber',
    label: '琥珀',
    hint: '金 · 温暖活力',
    primaryLight: '#b3760f',
    primaryDark: '#c48f28',
    softLight: '#fdf4e3',
    softDark: '#2c2110',
    swatch: ['#b3760f', '#d99a25'],
    // 暖色配更柔的转角，和它的气质一致
    radius: { sm: 5, md: 8, lg: 10, xl: 14 },
  },
  {
    key: 'graphite',
    label: '石墨',
    hint: '灰 · 极简专注',
    primaryLight: '#3c4351',
    primaryDark: '#64707f',
    softLight: '#f1f2f4',
    softDark: '#1c2026',
    swatch: ['#3c4351', '#6b7688'],
    // 无彩配色配更利的转角，更接近专业工具的观感
    radius: { sm: 3, md: 4, lg: 6, xl: 10 },
  },
];

/** 按 key 取元数据，未知 key 回退到默认色系 */
export function presetMeta(key: ThemePreset): ThemePresetMeta {
  return THEME_PRESETS.find((p) => p.key === key) ?? THEME_PRESETS[0];
}

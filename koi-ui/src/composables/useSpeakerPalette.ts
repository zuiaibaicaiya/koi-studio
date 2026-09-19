import { computed } from 'vue';
import { useThemeStore } from '../store/theme';
import { presetMeta } from '../theme/presets';
import { hexToHsl, hslToHex } from '../utils/color';

// ---- 说话人配色：直接用主题色（当前色系的品牌色），而不是固定的一串颜色 ----

/** 说话人头像分档数：同一主题色下用明度深浅区分不同说话人 */
const SPEAKER_TONE_COUNT = 8;
/** 头像可读区间：低于 0.3 近黑、高于 0.58 白字发灰，都不取 */
const SPEAKER_L_MIN = 0.3;
const SPEAKER_L_MAX = 0.58;
/** 分档半幅：最深/最浅档相对主题色明度的偏移量 */
const SPEAKER_TONE_HALF_SPREAD = 0.06;

/** 兜底配色：仅在品牌色无法解析时使用 */
const SPEAKER_FALLBACK = ['#2dd4bf', '#06b6d4', '#8b5cf6', '#f59e0b', '#ef4444', '#10b981', '#ec4899', '#3b82f6'];

/**
 * 说话人头像色板：色相与饱和度完全取自当前主题色（品牌色），只用明度深浅分档，
 * 因此 6 套色系下头像就是该色系自己的颜色（靛青是蓝、青瓷是绿、石墨是灰），
 * 同时不同说话人仍可通过深浅区分；色系或明暗切换时自动重算。
 */
export function useSpeakerPalette() {
  const themeStore = useThemeStore();
  return computed<string[]>(() => {
    const meta = presetMeta(themeStore.preset);
    const base = hexToHsl(themeStore.isDark ? meta.primaryDark : meta.primaryLight);
    if (!base) return SPEAKER_FALLBACK;
    // 以主题色自身明度为中心，在可读区间内上下分档；半幅按剩余空间收敛，
    // 避免深浅档被裁到边界后出现重复色（两个说话人撞色）。
    const center = Math.min(
      Math.max(base.l, SPEAKER_L_MIN + SPEAKER_TONE_HALF_SPREAD),
      SPEAKER_L_MAX - SPEAKER_TONE_HALF_SPREAD,
    );
    const span = Math.min(SPEAKER_TONE_HALF_SPREAD, center - SPEAKER_L_MIN, SPEAKER_L_MAX - center);
    const half = (SPEAKER_TONE_COUNT - 1) / 2;
    return Array.from({ length: SPEAKER_TONE_COUNT }, (_, i) =>
      hslToHex(base.h, base.s, center + ((i - half) / half) * span),
    );
  });
}

/** 由说话人姓名稳定映射到色板下标：同名同色，换页/重转写后依然一致 */
export function speakerColorIndex(name: string): number {
  let h = 0;
  for (let i = 0; i < name.length; i++) h = (h * 31 + name.charCodeAt(i)) >>> 0;
  return h % SPEAKER_TONE_COUNT;
}

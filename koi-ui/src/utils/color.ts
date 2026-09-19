/**
 * 颜色工具：HEX/HSL 转换、透明度混合与设计令牌读取。
 * 供主题联动场景使用（canvas 取色、说话人色板、波形配色等）。
 */

/** #rrggbb → HSL（h 为 0-360 度，s/l 为 0-1）；解析失败返回 null */
export function hexToHsl(hex: string): { h: number; s: number; l: number } | null {
  const m = /^#?([0-9a-f]{3}|[0-9a-f]{6})$/i.exec(hex.trim());
  if (!m) return null;
  const raw = m[1].length === 3 ? m[1].split('').map((c) => c + c).join('') : m[1];
  const n = parseInt(raw, 16);
  const r = ((n >> 16) & 255) / 255;
  const g = ((n >> 8) & 255) / 255;
  const b = (n & 255) / 255;
  const max = Math.max(r, g, b);
  const min = Math.min(r, g, b);
  const l = (max + min) / 2;
  const d = max - min;
  if (d === 0) return { h: 0, s: 0, l };
  const s = l > 0.5 ? d / (2 - max - min) : d / (max + min);
  const h =
    max === r ? ((g - b) / d + (g < b ? 6 : 0)) * 60 : max === g ? ((b - r) / d + 2) * 60 : ((r - g) / d + 4) * 60;
  return { h, s, l };
}

/** HSL → #rrggbb */
export function hslToHex(h: number, s: number, l: number): string {
  const hue = (((h % 360) + 360) % 360) / 30;
  const a = s * Math.min(l, 1 - l);
  const k = (n: number) => (n + hue) % 12;
  const f = (n: number) => l - a * Math.max(-1, Math.min(k(n) - 3, 9 - k(n), 1));
  const to = (v: number) => Math.round(Math.max(0, Math.min(1, v)) * 255).toString(16).padStart(2, '0');
  return `#${to(f(0))}${to(f(8))}${to(f(4))}`;
}

/** 读取全局设计令牌的当前取值，供 canvas 波形取色（CSS 变量在 canvas 中不可用） */
export function cssVar(name: string, fallback: string): string {
  const v = getComputedStyle(document.documentElement).getPropertyValue(name).trim();
  return v || fallback;
}

/** 把十六进制颜色转成带透明度的 rgba；非 hex（rgb()/命名色）原样返回，避免生成非法颜色 */
export function withAlpha(color: string, alpha: number): string {
  const hex = color.trim().replace('#', '');
  const full = hex.length === 3 ? hex.split('').map((c) => c + c).join('') : hex;
  if (!/^[0-9a-f]{6}$/i.test(full)) return color;
  const n = parseInt(full, 16);
  return `rgba(${(n >> 16) & 255}, ${(n >> 8) & 255}, ${n & 255}, ${alpha})`;
}

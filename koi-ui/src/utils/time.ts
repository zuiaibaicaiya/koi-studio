/**
 * 时间格式化工具。
 *
 * 统一口径：转写内容的时间轴一律使用「相对音频开头的毫秒偏移」，
 * 与音频播放器时间轴、段落跳转、逐字高亮、导出文件使用同一坐标系，
 * 不依赖会议的 start_time（对离线转写/重新转写，start_time 与音频录制时间无关）。
 */

/** 毫秒偏移 → 说话时间（MM:SS，超 1 小时显示 HH:MM:SS） */
export function formatOffset(ms: number): string {
  if (!Number.isFinite(ms) || ms < 0) ms = 0;
  const totalSec = Math.floor(ms / 1000);
  const h = Math.floor(totalSec / 3600);
  const m = Math.floor((totalSec % 3600) / 60);
  const s = totalSec % 60;
  const pad = (n: number) => String(n).padStart(2, '0');
  return h > 0 ? `${pad(h)}:${pad(m)}:${pad(s)}` : `${pad(m)}:${pad(s)}`;
}

/** 毫秒时间戳 → 相对音频开头的偏移（支持天/小时），用于实时转写段落时间 */
export function formatTimestamp(ms: number): string {
  const totalSeconds = Math.floor(ms / 1000);
  const days = Math.floor(totalSeconds / 86400);
  const remain = totalSeconds % 86400;
  const hours = Math.floor(remain / 3600);
  const minutes = Math.floor((remain % 3600) / 60);
  const seconds = remain % 60;
  const hms = [
    String(hours).padStart(2, '0'),
    String(minutes).padStart(2, '0'),
    String(seconds).padStart(2, '0'),
  ].join(':');
  return days > 0 ? `${days}天 ${hms}` : hms;
}

/** 秒 → 播放器时间（MM:SS），用于音频播放条当前/总时长 */
export function formatClock(sec: number): string {
  if (!Number.isFinite(sec) || sec < 0) sec = 0;
  const m = Math.floor(sec / 60);
  const s = Math.floor(sec % 60);
  return `${String(m).padStart(2, '0')}:${String(s).padStart(2, '0')}`;
}

/** 毫秒级时间格式化（MM:SS.mmm），用于逐字高亮 tooltip */
export function formatPreciseMs(ms: number): string {
  if (!Number.isFinite(ms) || ms < 0) ms = 0;
  const totalSec = ms / 1000;
  const m = Math.floor(totalSec / 60);
  const s = Math.floor(totalSec % 60);
  const msPart = Math.round(ms % 1000);
  return `${String(m).padStart(2, '0')}:${String(s).padStart(2, '0')}.${String(msPart).padStart(3, '0')}`;
}

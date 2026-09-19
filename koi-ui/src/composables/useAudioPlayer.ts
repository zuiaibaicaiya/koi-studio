import { computed, ref, shallowRef, toValue, watch, type MaybeRefOrGetter } from 'vue';
import WaveSurfer from 'wavesurfer.js';
import { useThemeStore } from '../store/theme';
import { cssVar, withAlpha } from '../utils/color';

const VOLUME_STORAGE_KEY = 'koi:meeting-audio-volume';
const SPEEDS = [0.75, 1, 1.25, 1.5, 2];

/** 读取本地持久化的音量（0~1），隐私模式等异常场景回退到 1 */
function readStoredVolume(): number {
  try {
    const raw = localStorage.getItem(VOLUME_STORAGE_KEY);
    if (raw === null) return 1;
    const n = Number(raw);
    return Number.isFinite(n) && n >= 0 && n <= 1 ? n : 1;
  } catch {
    return 1;
  }
}

/** 波形配色跟随当前色系与明暗（不硬编码品牌色），切换主题时由 setOptions 实时刷新 */
function wavePalette() {
  const brand = cssVar('--color-brand', '#2f54eb');
  return {
    waveColor: withAlpha(brand, 0.3),
    progressColor: brand,
    cursorColor: cssVar('--color-brand-active', brand),
  };
}

export interface UseAudioPlayerOptions {
  /** 音频加载/播放失败的提示回调 */
  onError?: (message: string) => void;
}

/**
 * 音频播放器（wavesurfer.js）封装：
 * 负责波形实例生命周期、播放状态、倍速/音量（本地持久化）、主题联动与快捷定位，
 * 视图层只保留与转写内容相关的编排（跳转到某句/某个字、逐字高亮等）。
 */
export function useAudioPlayer(src: MaybeRefOrGetter<string>, options: UseAudioPlayerOptions = {}) {
  const themeStore = useThemeStore();

  const waveRef = ref<HTMLElement | null>(null);
  const wave = shallowRef<WaveSurfer | null>(null);
  const playing = ref(false);
  const ready = ref(false);
  const currentTime = ref(0);
  const duration = ref(0);
  /** 当前播放会话的起点（毫秒）：点击文字/段落跳转时设定，高亮"已播放"从这一点开始 */
  const playAnchorMs = ref(0);

  const speedIndex = ref(1);
  const speed = computed(() => SPEEDS[speedIndex.value]);

  // ---- 音量：本地持久化，重进详情页沿用上次设置 ----
  const volume = ref(readStoredVolume());
  const muted = ref(false);

  /** 当前播放位置（毫秒，相对音频开头），由 wavesurfer 的 timeupdate 驱动 */
  const currentMs = computed(() => currentTime.value * 1000);

  function applyVolume() {
    const ws = wave.value;
    if (!ws) return;
    ws.setVolume(volume.value);
    ws.setMuted(muted.value);
  }

  function destroy() {
    if (wave.value) {
      wave.value.destroy();
      wave.value = null;
    }
    playing.value = false;
    ready.value = false;
    currentTime.value = 0;
    duration.value = 0;
    playAnchorMs.value = 0;
  }

  function initWave() {
    destroy();
    const el = waveRef.value;
    const url = toValue(src);
    if (!el || !url) return;
    const ws = WaveSurfer.create({
      container: el,
      height: 44,
      ...wavePalette(),
      cursorWidth: 2,
      barWidth: 2,
      barGap: 2,
      barRadius: 2,
      url,
    });
    // 此处 wave.value 尚未赋值（在函数末尾才绑定），音量直接作用在实例上
    ws.setVolume(volume.value);
    ws.setMuted(muted.value);
    ws.on('ready', () => {
      ready.value = true;
      duration.value = ws.getDuration();
      ws.setPlaybackRate(speed.value, false);
      // 音频元素就绪后再兜底一次（换源/重建后音量回到默认值的场景）
      applyVolume();
    });
    ws.on('timeupdate', (t: number) => {
      currentTime.value = t;
    });
    // 用户点击/拖动波形 seek：以新位置作为本轮“已播放”高亮的起点，
    // 避免拖动后整段旧位置的字仍停留在 played 高亮状态。
    ws.on('interaction', () => {
      playAnchorMs.value = ws.getCurrentTime() * 1000;
    });
    ws.on('play', () => (playing.value = true));
    ws.on('pause', () => (playing.value = false));
    ws.on('finish', () => {
      playing.value = false;
      currentTime.value = 0;
      playAnchorMs.value = 0;
    });
    ws.on('error', () => options.onError?.('音频加载失败，无法播放'));
    wave.value = ws;
  }

  function togglePlay() {
    wave.value?.playPause();
  }

  /** 跳转到指定毫秒；默认自动开始播放（与段落/逐字点击的交互一致） */
  function seekToMs(ms: number, opts: { play?: boolean } = {}) {
    const ws = wave.value;
    if (!ws) return;
    playAnchorMs.value = ms;
    ws.setTime(ms / 1000);
    if ((opts.play ?? true) && !ws.isPlaying()) {
      ws.play().catch(() => options.onError?.('音频加载失败，无法播放'));
    }
  }

  /**
   * 点击波形只做定位（wavesurfer 内置 seek），不再切换播放/暂停：
   * 原先的交互会让「想拖到某个位置继续听」的操作顺手把播放打断。
   * 播放/暂停交给播放键与空格键，定位与播放状态互不干扰。
   */
  function skip(seconds: number) {
    const ws = wave.value;
    if (!ws || !duration.value) return;
    const t = Math.min(Math.max(ws.getCurrentTime() + seconds, 0), duration.value);
    playAnchorMs.value = t * 1000;
    ws.setTime(t);
  }

  function toggleMute() {
    if (!wave.value) return;
    muted.value = !muted.value;
    applyVolume();
  }

  function onVolumeChange(v: number) {
    volume.value = v;
    // 拖到 0 视为静音；从 0 往上拖自动解除静音
    muted.value = v === 0;
    try {
      localStorage.setItem(VOLUME_STORAGE_KEY, String(v));
    } catch {
      // 隐私模式等场景写入失败可忽略，不影响本次播放
    }
    applyVolume();
  }

  function onSpeedChange(v: number) {
    const i = SPEEDS.indexOf(v);
    if (i >= 0) {
      speedIndex.value = i;
      wave.value?.setPlaybackRate(speed.value, false);
    }
  }

  // 明暗/色系切换后重新取色，避免波形停留在旧主题的颜色上
  watch(
    () => [themeStore.mode, themeStore.preset],
    () => wave.value?.setOptions(wavePalette()),
    { flush: 'post' },
  );

  // 音频源变化（URL 或版本号变化）时重建波形（flush:'post' 确保在 DOM 渲染、waveRef 绑定后再初始化）
  watch(
    () => toValue(src),
    (url) => {
      if (url) initWave();
      else destroy();
    },
    { flush: 'post' },
  );

  return {
    /** 波形容器 ref，模板绑定到播放条 DOM */
    waveRef,
    /** wavesurfer 实例（shallowRef），存在与否决定控制键是否可用 */
    wave,
    playing,
    ready,
    currentTime,
    duration,
    currentMs,
    playAnchorMs,
    speed,
    speeds: SPEEDS,
    volume,
    muted,
    togglePlay,
    seekToMs,
    skip,
    toggleMute,
    onVolumeChange,
    onSpeedChange,
    destroy,
  };
}

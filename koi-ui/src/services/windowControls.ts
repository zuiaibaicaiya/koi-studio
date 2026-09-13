import { ipcRenderer } from 'electron';

/** 自绘标题栏高度（px），必须与 electron/main.ts 的 TITLE_BAR_HEIGHT、index.css 的 --titlebar-height 一致 */
export const TITLE_BAR_HEIGHT = 36;

/** 窗口状态，与主进程 Electron WindowState 保持一致 */
export interface WindowState {
  maximized: boolean;
  fullScreen: boolean;
  focused: boolean;
}

/** Window Controls Overlay 样式，与主进程 setTitleBarOverlay 参数一致 */
export interface TitleBarOverlay {
  color?: string;
  symbolColor?: string;
  height?: number;
}

/**
 * Chromium 的 Window Controls Overlay（WCO）桥接对象。
 * 仅在主进程启用 titleBarOverlay（Windows / Linux）时可见，
 * 对应 WICG 规范 https://github.com/WICG/window-controls-overlay
 */
interface WindowControlsOverlay extends EventTarget {
  readonly visible: boolean;
  getTitlebarAreaRect(): DOMRect;
}

/** 主进程窗口相关能力封装 */
export const windowApi = {
  /** 当前运行平台（darwin / win32 / linux） */
  getPlatform: (): Promise<NodeJS.Platform> => ipcRenderer.invoke('window:get-platform'),
  /** 读取当前窗口状态 */
  getState: (): Promise<WindowState> => ipcRenderer.invoke('window:get-state'),
  /** 同步原生窗口控件配色（仅 Windows / Linux 生效） */
  setTitleBarOverlay: (overlay: TitleBarOverlay): Promise<void> =>
    ipcRenderer.invoke('window:set-title-bar-overlay', overlay),
  minimize: (): Promise<void> => ipcRenderer.invoke('window:minimize'),
  toggleMaximize: (): Promise<void> => ipcRenderer.invoke('window:toggle-maximize'),
  close: (): Promise<void> => ipcRenderer.invoke('window:close'),
  /** 订阅窗口状态变化，返回取消订阅函数 */
  onStateChange: (listener: (state: WindowState) => void): (() => void) => {
    const handler = (_event: Electron.IpcRendererEvent, state: WindowState) => listener(state);
    ipcRenderer.on('window:state-changed', handler);
    return () => {
      ipcRenderer.off('window:state-changed', handler);
    };
  },
};

const getOverlayApi = (): WindowControlsOverlay | undefined =>
  (navigator as Navigator & { windowControlsOverlay?: WindowControlsOverlay })
    .windowControlsOverlay;

/**
 * 把原生窗口控件占据的区域换算成 CSS 变量写入根节点，
 * 供自绘标题栏用 padding 预留安全区（避免内容被最小化/最大化/关闭按钮遮挡）。
 *
 * 相比直接使用 env(titlebar-area-*) 环境变量，这里读取真实像素值，
 * 可以正确处理高 DPI 缩放、RTL 以及窗口最大化时的边距变化。
 */
export const syncWindowControlsInsets = (): void => {
  const root = document.documentElement;
  const overlay = getOverlayApi();

  if (!overlay || !overlay.visible) {
    root.style.setProperty('--wco-left', '0px');
    root.style.setProperty('--wco-right', '0px');
    return;
  }

  const rect = overlay.getTitlebarAreaRect();
  root.style.setProperty('--wco-left', `${Math.max(0, rect.x)}px`);
  root.style.setProperty('--wco-right', `${Math.max(0, window.innerWidth - rect.right)}px`);
};

/** 监听原生窗口控件几何变化，返回取消监听函数 */
export const watchWindowControlsInsets = (): (() => void) => {
  const overlay = getOverlayApi();
  syncWindowControlsInsets();
  if (!overlay) return () => {};

  const handler = () => syncWindowControlsInsets();
  overlay.addEventListener('geometrychange', handler);
  window.addEventListener('resize', handler);
  return () => {
    overlay.removeEventListener('geometrychange', handler);
    window.removeEventListener('resize', handler);
  };
};

export default windowApi;

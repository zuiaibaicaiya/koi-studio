import { ipcRenderer } from 'electron';

/**
 * 第二屏投屏（实时转写展示窗口）窗口管理服务。
 *
 * 转写数据不经过主进程 IPC 转发：第二屏窗口与实时转写页一样自行建立
 * Socket.IO 连接，并以 viewer 角色订阅会议转写结果（见 LiveMeetingPresent.vue）。
 * 这里只负责窗口的打开 / 关闭 / 全屏与开关状态同步。
 */

/** 第二屏开关状态 */
export interface PresentState {
  open: boolean;
}

export const presenterApi = {
  /** 打开（或复用）第二屏窗口，hash 为目标路由（含会议参数 query） */
  open: (hash: string): Promise<PresentState> => ipcRenderer.invoke('present:open', hash),
  /** 关闭第二屏窗口 */
  close: (): Promise<void> => ipcRenderer.invoke('present:close'),
  /** 查询第二屏是否已打开 */
  isOpen: (): Promise<PresentState> => ipcRenderer.invoke('present:is-open'),
  /** 切换第二屏全屏态，返回切换后的状态 */
  toggleFullScreen: (): Promise<{ fullScreen: boolean }> =>
    ipcRenderer.invoke('present:toggle-fullscreen'),
  /** 读取第二屏全屏态（首次渲染时对齐按钮图标） */
  getFullScreen: (): Promise<boolean> => ipcRenderer.invoke('present:get-fullscreen'),
  /** 订阅第二屏开关状态变化（含在第二屏内自行关闭的情况） */
  onStateChange: (listener: (state: PresentState) => void): (() => void) => {
    const handler = (_event: Electron.IpcRendererEvent, state: PresentState) => listener(state);
    ipcRenderer.on('present:state-changed', handler);
    return () => {
      ipcRenderer.off('present:state-changed', handler);
    };
  },
  /** 订阅第二屏全屏态变化（含系统快捷键退出全屏） */
  onFullScreenChange: (listener: (fullScreen: boolean) => void): (() => void) => {
    const handler = (_event: Electron.IpcRendererEvent, fullScreen: boolean) => listener(fullScreen);
    ipcRenderer.on('present:fullscreen-changed', handler);
    return () => {
      ipcRenderer.off('present:fullscreen-changed', handler);
    };
  },
};

export default presenterApi;

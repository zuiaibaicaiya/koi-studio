import { ipcRenderer } from 'electron';

/** 媒体权限状态，与主进程 systemPreferences.getMediaAccessStatus 保持一致 */
export type MediaAccessStatus = 'not-determined' | 'granted' | 'denied' | 'restricted' | 'unknown';

export type MediaType = 'microphone' | 'camera' | 'screen';

/** 音频采集模式，详见主进程 electron/main.ts */
export type AudioCaptureMode = 'none' | 'microphone' | 'system' | 'system-silent';

export interface PermissionSnapshot {
  platform: string;
  microphone: MediaAccessStatus;
  camera: MediaAccessStatus;
  screen: MediaAccessStatus;
}

export interface PermissionResult {
  granted: boolean;
  status: MediaAccessStatus;
  message: string;
}

export interface SystemAudioSupport {
  supported: boolean;
  systemVersion: string;
  platform: string;
  reason: string;
}

export interface CaptureSource {
  id: string;
  name: string;
  type: 'screen' | 'window';
  displayId: string;
  thumbnail: string;
  appIcon: string | null;
}

export interface CaptureSelection {
  sourceId: string | null;
  audio: AudioCaptureMode;
  useSystemPicker: boolean;
}

export interface CaptureSourceQuery {
  types?: Array<'screen' | 'window'>;
  thumbnailWidth?: number;
  thumbnailHeight?: number;
  fetchWindowIcons?: boolean;
}

/** 主进程能力封装 */
export const captureApi = {
  /** 读取麦克风 / 摄像头 / 屏幕录制权限状态 */
  getPermissionStatus: (): Promise<PermissionSnapshot> => ipcRenderer.invoke('capture:get-permissions'),
  /** 查询当前系统是否支持内录系统音频 */
  getSystemAudioSupport: (): Promise<SystemAudioSupport> =>
    ipcRenderer.invoke('capture:get-system-audio-support'),
  /** 检查并申请屏幕录制权限 */
  requestScreenPermission: (prompt = true): Promise<PermissionResult> =>
    ipcRenderer.invoke('capture:ensure-screen', prompt),
  /** 打开 macOS 隐私设置面板 */
  openPrivacySettings: (pane: MediaType = 'screen'): Promise<void> =>
    ipcRenderer.invoke('capture:open-privacy-settings', pane),
  /** 获取可采集的屏幕 / 窗口列表 */
  getSources: (query: CaptureSourceQuery = {}): Promise<CaptureSource[]> =>
    ipcRenderer.invoke('capture:get-sources', query),
  /** 设置下一次 getDisplayMedia 生效的采集源与音频模式 */
  setSelection: (selection: Partial<CaptureSelection>): Promise<CaptureSelection> =>
    ipcRenderer.invoke('capture:set-selection', selection),
  getSelection: (): Promise<CaptureSelection> => ipcRenderer.invoke('capture:get-selection'),
};

export interface SystemAudioStreamOptions {
  /** 静默内录：采集系统音频且本机不外放 */
  silent?: boolean;
}

export interface SystemAudioCapture {
  /** 仅包含系统音频轨的流，可直接接入 AudioContext */
  stream: MediaStream;
  /** 结束采集，同时释放内部占位视频轨 */
  stop: () => void;
}

/**
 * 创建系统音频内录流（纯音频，不产出画面）。
 *
 * Chromium 不允许纯音频的 getDisplayMedia，必须同时申请视频轨，
 * 因此这里申请一路最小规格的占位视频轨：拿到流后立即禁用并从返回的流中剔除，
 * 采集结束时再一并 stop（提前 stop 会连带结束整个采集会话，导致音频中断）。
 *
 * 实际音频来自主进程 setDisplayMediaRequestHandler 提供的 loopback 轨，
 * 需要主进程支持（见 electron/main.ts）。
 */
export const createSystemAudioStream = async (
  options: SystemAudioStreamOptions = {},
): Promise<SystemAudioCapture> => {
  const support = await captureApi.getSystemAudioSupport();
  if (!support.supported) {
    throw new Error(support.reason);
  }

  const permission = await captureApi.requestScreenPermission();
  if (!permission.granted) {
    throw new Error(permission.message);
  }

  await captureApi.setSelection({
    sourceId: null,
    audio: options.silent ? 'system-silent' : 'system',
  });

  const raw = await navigator.mediaDevices.getDisplayMedia({
    audio: true,
    video: {
      frameRate: { ideal: 1, max: 1 },
      width: { max: 2 },
      height: { max: 2 },
    },
  });

  const audioTracks = raw.getAudioTracks();
  const videoTracks = raw.getVideoTracks();
  const stopAll = () => raw.getTracks().forEach((track) => track.stop());

  if (audioTracks.length === 0) {
    stopAll();
    throw new Error(
      `未获取到系统音频轨。${support.reason} 请确认应用已声明 NSAudioCaptureUsageDescription 并已授予屏幕录制权限。`,
    );
  }

  // 占位视频轨不参与任何录制与渲染
  videoTracks.forEach((track) => {
    track.enabled = false;
    raw.removeTrack(track);
  });

  return { stream: new MediaStream(audioTracks), stop: stopAll };
};

export default captureApi;

import {
  app,
  BrowserWindow,
  type BrowserWindowConstructorOptions,
  type DesktopCapturerSource,
  desktopCapturer,
  dialog,
  ipcMain,
  session,
  shell,
  systemPreferences,
} from 'electron';

app.commandLine.appendSwitch('remote-allow-origins', '*');
process.env.ELECTRON_DISABLE_SECURITY_WARNINGS = 'true';

const IS_MAC = process.platform === 'darwin';
const IS_WIN = process.platform === 'win32';

/* ============================================================
 * 一、采集源与 displayMedia 请求处理（系统音频内录）
 * ========================================================== */

/** 可检查权限的媒体类型 */
type MediaType = 'microphone' | 'camera' | 'screen';

/** 音频采集模式 */
type AudioCaptureMode = 'none' | 'microphone' | 'system' | 'system-silent';

/** 传递给 setDisplayMediaRequestHandler 的内录音频取值 */
type LoopbackAudio = 'loopback' | 'loopbackWithMute';

interface SystemAudioSupport {
  supported: boolean;
  systemVersion: string;
  platform: NodeJS.Platform;
  reason: string;
}

interface CaptureSelection {
  sourceId: string | null;
  audio: AudioCaptureMode;
  useSystemPicker: boolean;
}

/** 渲染进程可调用的 IPC 通道 */
const CHANNEL = {
  permissions: 'capture:get-permissions',
  systemAudioSupport: 'capture:get-system-audio-support',
  ensureMicrophone: 'capture:ensure-microphone',
  ensureScreen: 'capture:ensure-screen',
  openPrivacySettings: 'capture:open-privacy-settings',
  getSources: 'capture:get-sources',
  setSelection: 'capture:set-selection',
  getSelection: 'capture:get-selection',
} as const;

let mainWindow: BrowserWindow | null = null;
/** 权限引导弹窗去重，避免同一权限反复弹出 */
const permissionPromptPending = new Set<MediaType>();

/** 当前待生效的采集配置，渲染进程调用 getDisplayMedia 前先通过 IPC 写入 */
let captureSelection: CaptureSelection = {
  sourceId: null,
  audio: IS_MAC || IS_WIN ? 'system' : 'none',
  useSystemPicker: false,
};

/** 已注册的 displayMedia handler 的 useSystemPicker 取值，用于避免重复注册 */
let registeredSystemPicker: boolean | null = null;

/* ---------- 权限检查与申请（macOS） ---------- */

const getMediaAccessStatus = (type: MediaType): string => {
  if (!IS_MAC && !IS_WIN) return 'granted';
  try {
    return systemPreferences.getMediaAccessStatus(type);
  } catch {
    return 'unknown';
  }
};

/** 解析 macOS 版本号，形如 "14.6.1" -> [14, 6, 1] */
const parseSystemVersion = (): number[] =>
  process
    .getSystemVersion()
    .split('.')
    .map((part) => Number.parseInt(part, 10) || 0);

/** 判断当前系统能否内录系统音频，并给出中文说明 */
const getSystemAudioSupport = (): SystemAudioSupport => {
  const systemVersion = process.getSystemVersion();
  const base = { systemVersion, platform: process.platform };

  if (IS_WIN) {
    return { ...base, supported: true, reason: 'Windows 通过 WASAPI loopback 采集系统音频。' };
  }
  if (!IS_MAC) {
    return {
      ...base,
      supported: false,
      reason: '当前平台不支持系统音频内录，请改用麦克风或虚拟声卡设备。',
    };
  }

  const [major = 0, minor = 0] = parseSystemVersion();
  if (major < 13) {
    return {
      ...base,
      supported: false,
      reason: `macOS ${systemVersion} 受系统限制无法直接内录系统音频，请安装 BlackHole 等虚拟声卡后选择麦克风输入。`,
    };
  }
  if (major === 13 || (major === 14 && minor < 2)) {
    return {
      ...base,
      supported: true,
      reason: `macOS ${systemVersion} 需回退到旧版录屏音频通道，请以 KOI_LEGACY_LOOPBACK=1 启动应用；若采集不到声音说明系统不支持 CoreAudio Tap。`,
    };
  }
  return {
    ...base,
    supported: true,
    reason: 'macOS 14.2+ 通过 CoreAudio Tap 内录系统音频，需 Info.plist 声明 NSAudioCaptureUsageDescription。',
  };
};

/** 打开 macOS 隐私设置对应面板 */
const openPrivacySettings = async (pane: MediaType = 'screen'): Promise<void> => {
  if (!IS_MAC) return;
  const anchors: Record<MediaType, string> = {
    screen: 'Privacy_ScreenCapture',
    microphone: 'Privacy_Microphone',
    camera: 'Privacy_Camera',
  };
  await shell.openExternal(`x-apple.systempreferences:com.apple.preference.security?${anchors[pane]}`);
};

/** 弹出权限引导对话框，用户确认后直接跳转系统设置 */
const promptPrivacySettings = async (type: MediaType, detail: string): Promise<void> => {
  if (!IS_MAC || permissionPromptPending.has(type)) return;
  permissionPromptPending.add(type);
  const titles: Record<MediaType, string> = {
    screen: '需要「屏幕录制」权限',
    microphone: '需要「麦克风」权限',
    camera: '需要「摄像头」权限',
  };
  try {
    const options = {
      type: 'warning' as const,
      title: titles[type],
      message: titles[type],
      detail,
      buttons: ['打开系统设置', '稍后再说'],
      defaultId: 0,
      cancelId: 1,
      noLink: true,
    };
    const { response } = mainWindow && !mainWindow.isDestroyed()
      ? await dialog.showMessageBox(mainWindow, options)
      : await dialog.showMessageBox(options);
    if (response === 0) {
      await openPrivacySettings(type);
    }
  } catch (error) {
    console.error('[capture] 展示权限引导弹窗失败:', error);
  } finally {
    permissionPromptPending.delete(type);
  }
};

/** 检查并申请屏幕录制权限 */
const ensureScreenPermission = async (prompt = true): Promise<{ granted: boolean; status: string; message: string }> => {
  let status = getMediaAccessStatus('screen');
  if (IS_MAC && status !== 'granted') {
    try {
      await desktopCapturer.getSources({ types: ['screen'], thumbnailSize: { width: 1, height: 1 } });
    } catch (error) {
      console.error('[capture] 探测屏幕录制权限失败:', error);
    }
    status = getMediaAccessStatus('screen');
  }
  if (status === 'granted') {
    return { granted: true, status, message: '屏幕录制权限已授权。' };
  }
  const message =
    '屏幕录制权限未开启，无法采集屏幕画面与系统音频。请在「系统设置 → 隐私与安全性 → 屏幕录制」中勾选本应用，然后重启应用。';
  if (prompt) {
    await promptPrivacySettings('screen', message);
  }
  return { granted: false, status, message };
};

/* ---------- 采集源定位与 displayMedia handler ---------- */

const toSourceType = (id: string): 'screen' | 'window' => (id.startsWith('screen:') ? 'screen' : 'window');

const resolveVideoSource = async (sourceId: string | null): Promise<DesktopCapturerSource | null> => {
  const sources = await desktopCapturer.getSources({
    types: sourceId ? ['screen', 'window'] : ['screen'],
    thumbnailSize: { width: 0, height: 0 },
  });
  if (sources.length === 0) return null;
  if (sourceId) {
    const matched = sources.find((item) => item.id === sourceId);
    if (matched) return matched;
    console.log('[capture] 采集源已失效，回落到默认屏幕');
  }
  return sources.find((item) => item.id.startsWith('screen:')) ?? sources[0];
};

/** 把音频模式映射为 Electron 的 loopback 取值；麦克风 / 不采集时返回 undefined */
const resolveLoopbackAudio = (mode: AudioCaptureMode): LoopbackAudio | undefined => {
  if (mode === 'none' || mode === 'microphone') return undefined;
  if (!getSystemAudioSupport().supported) {
    console.log('[capture] 当前系统不支持内录系统音频，本次仅采集视频');
    return undefined;
  }
  return mode === 'system-silent' ? 'loopbackWithMute' : 'loopback';
};

/**
 * 注册 displayMedia 请求处理器。
 * 主进程拦截渲染进程的 getDisplayMedia，提供系统音频 loopback 轨 + 占位视频轨。
 * useSystemPicker 启用系统选择器后会绕过本 handler，因此内录时必须关闭。
 */
const registerDisplayMediaHandler = (useSystemPicker: boolean): void => {
  if (registeredSystemPicker === useSystemPicker) return;
  registeredSystemPicker = useSystemPicker;

  session.defaultSession.setDisplayMediaRequestHandler(
    (_request, callback) => {
      void (async () => {
        try {
          if (IS_MAC) {
            const screenStatus = getMediaAccessStatus('screen');
            if (screenStatus !== 'granted') {
              console.error(`[capture] 屏幕录制权限状态为 ${screenStatus}，拒绝本次采集请求`);
              void ensureScreenPermission(true);
              callback({});
              return;
            }
          }

          const source = await resolveVideoSource(captureSelection.sourceId);
          if (!source) {
            console.error('[capture] 未找到任何可用的屏幕 / 窗口采集源');
            callback({});
            return;
          }

          const audio = resolveLoopbackAudio(captureSelection.audio);
          console.log(`[capture] 授予采集权限: 占位视频源=${source.name}(${source.id}) audio=${audio ?? 'none'}`);
          callback(audio ? { video: source, audio } : { video: source });
        } catch (error) {
          console.error('[capture] 处理 displayMedia 请求失败:', error);
          callback({});
        }
      })();
    },
    { useSystemPicker },
  );
};

/** 更新采集配置；内录系统音频或指定采集源时强制关闭系统选择器 */
const updateCaptureSelection = (partial: Partial<CaptureSelection>): CaptureSelection => {
  captureSelection = { ...captureSelection, ...partial };
  const needsCustomHandler =
    Boolean(captureSelection.sourceId) || captureSelection.audio === 'system' || captureSelection.audio === 'system-silent';
  if (needsCustomHandler) {
    captureSelection.useSystemPicker = false;
  }
  registerDisplayMediaHandler(captureSelection.useSystemPicker);
  return captureSelection;
};

/* ---------- 会话权限与 IPC 注册 ---------- */

const registerSessionHandlers = (): void => {
  registerDisplayMediaHandler(captureSelection.useSystemPicker);

  session.defaultSession.setPermissionRequestHandler((_webContents, permission, callback) => {
    if (permission === 'media') {
      callback(getMediaAccessStatus('microphone') !== 'denied');
      return;
    }
    callback(true);
  });

  session.defaultSession.setPermissionCheckHandler((_webContents, permission) => {
    if (permission === 'media') {
      return getMediaAccessStatus('microphone') !== 'denied';
    }
    return true;
  });
};

const registerCaptureIpc = (): void => {
  ipcMain.handle(CHANNEL.permissions, (): unknown => ({
    platform: process.platform,
    microphone: getMediaAccessStatus('microphone'),
    camera: getMediaAccessStatus('camera'),
    screen: getMediaAccessStatus('screen'),
  }));

  ipcMain.handle(CHANNEL.systemAudioSupport, (): SystemAudioSupport => getSystemAudioSupport());

  ipcMain.handle(CHANNEL.ensureScreen, (_event, prompt = true): Promise<unknown> => ensureScreenPermission(prompt !== false));

  ipcMain.handle(CHANNEL.openPrivacySettings, (_event, pane: MediaType = 'screen') => openPrivacySettings(pane));

  ipcMain.handle(CHANNEL.getSources, (_event, options) =>
    desktopCapturer
      .getSources({
        types: (options?.types ?? ['screen', 'window']) as Array<'screen' | 'window'>,
        thumbnailSize: {
          width: options?.thumbnailWidth ?? 320,
          height: options?.thumbnailHeight ?? 180,
        },
        fetchWindowIcons: options?.fetchWindowIcons ?? false,
      })
      .then((sources) =>
        sources.map((source) => ({
          id: source.id,
          name: source.name,
          type: toSourceType(source.id),
          displayId: source.display_id,
          thumbnail: source.thumbnail && !source.thumbnail.isEmpty() ? source.thumbnail.toDataURL() : '',
          appIcon: source.appIcon && !source.appIcon.isEmpty() ? source.appIcon.toDataURL() : null,
        })),
      ),
  );

  ipcMain.handle(CHANNEL.setSelection, (_event, selection: Partial<CaptureSelection> = {}) =>
    updateCaptureSelection(selection),
  );

  ipcMain.handle(CHANNEL.getSelection, (): CaptureSelection => captureSelection);
};

/* ============================================================
 * 二、窗口与应用生命周期
 * ========================================================== */

const createWindow = async () => {
  const config: BrowserWindowConstructorOptions = {
    webPreferences: {
      nodeIntegration: true,
      webSecurity: false,
      contextIsolation: false,
    },
  };
  mainWindow = new BrowserWindow(config);
  mainWindow.on('closed', () => {
    mainWindow = null;
  });
  if (process.env['ELECTRON_RENDERER_URL']) {
    await mainWindow.loadURL(process.env['ELECTRON_RENDERER_URL']).then(() => {
      mainWindow?.webContents.openDevTools({ mode: 'bottom' });
    });
  } else {
    await mainWindow.loadFile(app.getAppPath() + '/dist/index.html');
  }
};

const gotTheLock = app.requestSingleInstanceLock();

if (!gotTheLock) {
  app.quit();
} else {
  app.on('second-instance', () => {
    if (mainWindow) {
      if (mainWindow.isMinimized()) {
        mainWindow.restore();
      }
      if (!mainWindow.isVisible()) {
        mainWindow.show();
      }
      mainWindow.focus();
    }
  });
  app.whenReady().then(async () => {
    // macOS 提前申请麦克风权限，避免首次录音出现静默失败
    if (IS_MAC) {
      const snapshot = {
        platform: process.platform,
        microphone: getMediaAccessStatus('microphone'),
        camera: getMediaAccessStatus('camera'),
        screen: getMediaAccessStatus('screen'),
      };
      console.log('[capture] 启动权限快照:', snapshot);
      console.log('[capture] 系统音频内录支持:', getSystemAudioSupport());
      if (snapshot.microphone === 'not-determined') {
        try {
          await systemPreferences.askForMediaAccess('microphone');
        } catch (error) {
          console.error('[capture] 申请麦克风权限失败:', error);
        }
      }
    }

    registerSessionHandlers();
    registerCaptureIpc();

    await createWindow();
  });
  app.on('window-all-closed', () => {
    if (process.platform !== 'darwin') {
      app.quit();
    } else {
      app.exit();
    }
  });
  app.on('activate', () => {
    if (mainWindow === null) {
      createWindow();
    }
  });
}

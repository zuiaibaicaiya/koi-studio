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

/* ---------- 自绘标题栏（VS Code 风格 window chrome） ---------- */

/** 自绘标题栏高度（px），必须与渲染进程 --titlebar-height / windowControls.ts 保持一致 */
const TITLE_BAR_HEIGHT = 36;

/** macOS 红绿灯窗口按钮的尺寸（用于垂直居中计算） */
const TRAFFIC_LIGHT_SIZE = 12;

/**
 * 无边框自绘标题栏的窗口外观配置（主窗口与第二屏窗口共用）。
 * macOS 保留原生红绿灯，Windows / Linux 使用 Window Controls Overlay。
 */
const WINDOW_CHROME: BrowserWindowConstructorOptions = {
  titleBarStyle: 'hidden',
  ...(IS_MAC
    ? {
        trafficLightPosition: {
          x: 12,
          y: Math.round((TITLE_BAR_HEIGHT - TRAFFIC_LIGHT_SIZE) / 2),
        },
      }
    : {
        titleBarOverlay: {
          color: '#ffffff',
          symbolColor: 'rgba(0, 0, 0, 0.88)',
          height: TITLE_BAR_HEIGHT,
        },
      }),
};

/** 窗口控制相关 IPC 通道 */
const WINDOW_CHANNEL = {
  platform: 'window:get-platform',
  state: 'window:get-state',
  stateChanged: 'window:state-changed',
  setOverlay: 'window:set-title-bar-overlay',
  minimize: 'window:minimize',
  toggleMaximize: 'window:toggle-maximize',
  close: 'window:close',
} as const;

interface WindowState {
  maximized: boolean;
  fullScreen: boolean;
  focused: boolean;
}

interface TitleBarOverlay {
  color?: string;
  symbolColor?: string;
  height?: number;
}

/** 第二屏相关 IPC 通道（实时转写投屏窗口，仅窗口管理，不承载转写数据） */
const PRESENT_CHANNEL = {
  /** 打开（或复用）第二屏，参数为 hash 路由 */
  open: 'present:open',
  /** 关闭第二屏 */
  close: 'present:close',
  /** 查询第二屏是否已打开 */
  isOpen: 'present:is-open',
  /** 切换第二屏全屏态 */
  toggleFullScreen: 'present:toggle-fullscreen',
  /** 主进程 -> 主窗口：第二屏开关状态变化 */
  stateChanged: 'present:state-changed',
} as const;

let mainWindow: BrowserWindow | null = null;
/** 第二屏展示窗口（实时转写投屏） */
let presentWindow: BrowserWindow | null = null;
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

/* ---------- 窗口状态与窗口控制 IPC ---------- */

const getWindowState = (): WindowState => ({
  maximized: mainWindow?.isMaximized() ?? false,
  fullScreen: mainWindow?.isFullScreen() ?? false,
  focused: mainWindow?.isFocused() ?? true,
});

/** 把窗口状态推送给渲染进程，供自绘标题栏同步最大化/全屏/聚焦态 */
const emitWindowState = (): void => {
  if (!mainWindow || mainWindow.isDestroyed()) return;
  mainWindow.webContents.send(WINDOW_CHANNEL.stateChanged, getWindowState());
};

const registerWindowIpc = (): void => {
  ipcMain.handle(WINDOW_CHANNEL.platform, (): NodeJS.Platform => process.platform);

  ipcMain.handle(WINDOW_CHANNEL.state, (): WindowState => getWindowState());

  ipcMain.handle(WINDOW_CHANNEL.minimize, () => mainWindow?.minimize());

  ipcMain.handle(WINDOW_CHANNEL.toggleMaximize, () => {
    if (!mainWindow || mainWindow.isDestroyed()) return;
    if (mainWindow.isMaximized()) mainWindow.unmaximize();
    else mainWindow.maximize();
  });

  ipcMain.handle(WINDOW_CHANNEL.close, () => mainWindow?.close());

  // 主题切换时同步 Window Controls Overlay 配色；macOS 走原生红绿灯，无需覆盖层
  ipcMain.handle(WINDOW_CHANNEL.setOverlay, (_event, overlay: TitleBarOverlay = {}) => {
    if (IS_MAC || !mainWindow || mainWindow.isDestroyed()) return;
    mainWindow.setTitleBarOverlay({
      color: overlay.color,
      symbolColor: overlay.symbolColor,
      // height 必须为整数，否则 Electron 会忽略该次设置
      height: Math.round(overlay.height ?? TITLE_BAR_HEIGHT),
    });
  });
};

/* ============================================================
 * 二、第二屏展示窗口（实时转写投屏）
 *
 * 主进程只负责开窗与窗口状态同步：转写数据不经 IPC 转发，
 * 第二屏窗口像实时转写页一样自行建立 Socket.IO 连接，
 * 以 viewer 角色订阅会议转写结果（见 services/presenter.ts 与 LiveMeetingPresent.vue）。
 * ========================================================== */

/** 第二屏窗口默认尺寸 */
const PRESENT_WINDOW_SIZE = { width: 1280, height: 800 };

/** 第二屏默认路由 */
const PRESENT_DEFAULT_HASH = '/live/present';

/** 判断窗口是否仍然可用（未关闭 / 未销毁） */
const isAlive = (win: BrowserWindow | null): win is BrowserWindow => !!win && !win.isDestroyed();

/**
 * 加载第二屏页面，取址方式与主窗口保持一致：
 * - dev：Rsbuild devServer 地址 + hash 路由
 * - 打包后：dist/index.html + hash 路由（loadFile 的 hash 选项会自动补 '#'）
 */
const loadPresentPage = async (win: BrowserWindow, hash: string): Promise<void> => {
  const devServerUrl = process.env['ELECTRON_RENDERER_URL'];
  if (devServerUrl) {
    await win.loadURL(`${devServerUrl}#${hash}`);
    return;
  }
  await win.loadFile(app.getAppPath() + '/dist/index.html', { hash });
};

/** 把第二屏开关状态同步给主窗口（按钮态展示用） */
const emitPresentState = (): void => {
  if (!isAlive(mainWindow)) return;
  mainWindow.webContents.send(PRESENT_CHANNEL.stateChanged, { open: isAlive(presentWindow) });
};

/** 关闭第二屏（结束会议 / 主窗口关闭时调用） */
const closePresentWindow = (): void => {
  if (isAlive(presentWindow)) presentWindow.close();
};

/**
 * 打开第二屏展示窗口；已存在时复用同一窗口并重载目标路由，
 * 保证会议参数与主窗口一致（重载后第二屏会重新拉取历史转写并重新订阅会议频道）。
 */
const openPresentWindow = async (hash = PRESENT_DEFAULT_HASH): Promise<{ open: boolean }> => {
  const existing = isAlive(presentWindow) ? presentWindow : null;
  const win =
    existing ??
    new BrowserWindow({
      ...WINDOW_CHROME,
      ...PRESENT_WINDOW_SIZE,
      minWidth: 640,
      minHeight: 420,
      title: 'koi-studio 实时转写',
      webPreferences: {
        nodeIntegration: true,
        webSecurity: false,
        contextIsolation: false,
        // 投屏窗口长期处于非焦点状态，关闭后台节流以保证画面实时刷新
        backgroundThrottling: false,
      },
    });

  if (!existing) {
    presentWindow = win;
    win.on('closed', () => {
      presentWindow = null;
      emitPresentState();
    });
  }

  await loadPresentPage(win, hash);
  if (win.isMinimized()) win.restore();
  win.show();
  win.focus();
  emitPresentState();
  return { open: true };
};

const registerPresentIpc = (): void => {
  ipcMain.handle(PRESENT_CHANNEL.open, (_event, hash?: string) =>
    openPresentWindow(hash || PRESENT_DEFAULT_HASH),
  );

  ipcMain.handle(PRESENT_CHANNEL.close, () => {
    closePresentWindow();
    emitPresentState();
  });

  ipcMain.handle(PRESENT_CHANNEL.isOpen, (): { open: boolean } => ({ open: isAlive(presentWindow) }));

  ipcMain.handle(PRESENT_CHANNEL.toggleFullScreen, (): { fullScreen: boolean } => {
    if (!isAlive(presentWindow)) return { fullScreen: false };
    const next = !presentWindow.isFullScreen();
    presentWindow.setFullScreen(next);
    return { fullScreen: next };
  });
};

/* ============================================================
 * 三、窗口与应用生命周期
 * ========================================================== */

const createWindow = async () => {
  // 无边框自绘标题栏（VS Code 风格）：macOS 保留原生红绿灯，
  // Windows / Linux 通过 Window Controls Overlay 提供原生窗口控件。
  const config: BrowserWindowConstructorOptions = {
    ...WINDOW_CHROME,
    webPreferences: {
      nodeIntegration: true,
      webSecurity: false,
      contextIsolation: false,
    },
  };
  mainWindow = new BrowserWindow(config);
  mainWindow.on('closed', () => {
    mainWindow = null;
    // 主窗口退出时一并关闭第二屏，避免残留无数据的投屏窗口
    closePresentWindow();
  });
  // 最大化 / 全屏 / 聚焦态变化时通知渲染进程，标题栏据此调整布局与样式
  (
    [
      'maximize',
      'unmaximize',
      'enter-full-screen',
      'leave-full-screen',
      'focus',
      'blur',
    ] as const
  ).forEach((event) => {
    mainWindow?.on(event, emitWindowState);
  });
  mainWindow.webContents.on('did-finish-load', emitWindowState);
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
    registerWindowIpc();
    registerPresentIpc();

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

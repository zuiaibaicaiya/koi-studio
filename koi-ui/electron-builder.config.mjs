import { join } from 'path';
import { existsSync, readFileSync, writeFileSync } from 'fs';

const productName = 'koi-ui';
const serverPort = '5168';

export default {
  productName,
  artifactName: '${productName}-${version}-${arch}.${ext}',
  directories: {
    output: join(import.meta.dirname, 'build-electron'),
  },
  files: ['dist'],
  afterPack: async () => {
    const filePath = join(
      import.meta.dirname,
      'build-electron',
      'mac',
      `${productName}.app`,
      'Contents',
      'Resources',
      '.env',
    );
    // 仅在打包了后端服务（含 .env）时重写 APP_URL / APP_PORT，避免前端项目构建报错
    if (!existsSync(filePath)) return Promise.resolve();
    const content = readFileSync(filePath, 'utf8');
    const updatedContent = content
      .replace(
        /^(\s*APP_PORT\s*=\s*)\d+(\s*)$/gm,
        `$1${serverPort}$2`,
      )
      .replace(
        /^(\s*APP_URL\s*=\s*)\S+(\s*)$/gm,
        `$1http://localhost:${serverPort}$2`,
      );

    writeFileSync(filePath, updatedContent, 'utf8');
    return Promise.resolve();
  },
  extraResources: [
    {
      from: join(import.meta.dirname, '..', 'koi-server', 'koi-server'),
      to: 'koi-server',
    },
    {
      from: join(import.meta.dirname, '..', 'koi-server', '.env'),
      to: '.env',
    },
    {
      from: join(import.meta.dirname, '..', 'koi-server', 'resources'),
      to: 'resources',
    },
    {
      from: join(import.meta.dirname, '..', 'koi-server', 'models'),
      to: 'models',
    },
    {
      from: join(import.meta.dirname, '..', 'koi-server', 'lib'),
      to: 'lib',
    },
  ].filter((item) => existsSync(item.from)),
  win: {},
  mac: {
    target: ['pkg'],
    category: 'public.app-category.developer-tools',
    extendInfo: {
      NSMicrophoneUsageDescription: '此应用需要访问您的麦克风以进行音频录制。',
      NSCameraUsageDescription: '此应用需要访问您的摄像头以进行视频录制。',
      // macOS 14.2+ 通过 CoreAudio Tap 捕获系统音频(loopback)时必须声明此键，
      // 否则 desktopCapturer 启动音频流会静默失败（无任何报错）。
      NSAudioCaptureUsageDescription: '此应用需要访问系统音频以进行录屏/系统声音录制。',
      NSAppleEventsUsageDescription: '此应用需要访问系统功能以请求麦克风权限。',
    },
    entitlements: 'entitlements.plist',
    entitlementsInherit: 'entitlements.plist',
  },
  pkg: {
    scripts: join(import.meta.dirname, 'scripts', 'mac'),
    allowAnywhere: true,
    allowCurrentUserHome: false,
    allowRootDirectory: false,
    isVersionChecked: false,
    isRelocatable: false,
    hasStrictIdentifier: true,
    overwriteAction: 'upgrade',
  },
  linux: {
    target: ['deb'],
    category: 'Office',
    desktop: {
      entry: {
        Name: 'koi-ui',
        Comment: 'koi-ui',
        Type: 'Application',
        StartupWMClass: 'koi-ui',
      },
    },
  },
  deb: {
    fpm: [
      `--before-install=${join(import.meta.dirname, 'scripts', 'deb', 'beforeInstall.sh')}`,
      `--after-install=${join(import.meta.dirname, 'scripts', 'deb', 'afterInstall.sh')}`,
    ],
  },
};

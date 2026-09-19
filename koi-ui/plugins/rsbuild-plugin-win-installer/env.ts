import type { LanguageSpec, ResolvedWinInstallerOptions, StagingRoot } from './types';
import { toWinPath } from './utils';

/**
 * 传给 Inno Setup 的环境变量集合。
 *
 * 约定：变量名以 `KOI_` 开头，`.iss` 里通过 `GetEnv('KOI_XXX')` 读取，
 * 为空时回退到脚本内的默认值，因此缺变量不会导致编译失败。
 */
export type InnoEnv = Record<string, string>;

/**
 * 双引号与换行会破坏 `.iss` 的字段分隔，统一剔除；
 * 应用名/发布者等展示字段本身也不应包含它们。
 */
const sanitizeForIspp = (value: string): string => value.replace(/["\r\n]/g, ' ').trim();

/**
 * 把 `%USERPROFILE%` 之类的环境变量转成 Inno 的 `{%NAME}` 形式，
 * 这样 .iss 中无论是 [Icons] 的 Path 还是 [Code] 的 ExpandConstant 都能直接使用。
 */
export const toInnoEnvRef = (value: string): string => {
  const replaced = value.replace(/%([A-Za-z_][A-Za-z0-9_]*)%/g, (_whole, name: string) => `{%${name}}`);
  // 未包含环境变量时，保持原样（绝对路径）
  return replaced;
};

/** 生成 x.y.z.w 形式的文件版本号，非法时返回空串（.iss 会跳过该指令） */
export const toFileVersion = (version: string): string => {
  const numeric = /^(\d+)\.(\d+)(?:\.(\d+))?(?:\.(\d+))?/.exec(version);
  if (!numeric) return '';
  return [numeric[1], numeric[2], numeric[3] ?? '0', numeric[4] ?? '0'].join('.');
};

const languageEnv = (languages: LanguageSpec[]): InnoEnv => {
  const env: InnoEnv = {};
  languages.slice(0, 3).forEach((language, position) => {
    env[`KOI_LANG${position + 1}_NAME`] = language.name;
    env[`KOI_LANG${position + 1}_FILE`] = language.messagesFile;
  });
  return env;
};

/**
 * 免压缩通配符：附带的两个变量供 .iss 生成独立的 `nocompression` Source 行，
 * 已匹配到的通配符同时作为 Excludes，避免同一文件被安装两次。
 */
const noCompressEnv = (root: StagingRoot | undefined, key: string): InnoEnv => {
  const globs = (root?.noCompression ?? []).slice(0, 2);
  return {
    [`KOI_NC_${key}_EXCLUDES`]: globs.join(','),
    [`KOI_NC_${key}_1`]: globs[0] ?? '',
    [`KOI_NC_${key}_2`]: globs[1] ?? '',
  };
};

export interface BuildInnoEnvContext {
  options: ResolvedWinInstallerOptions;
  /** 桌面客户端主程序文件名 */
  appExecutable: string;
  roots: StagingRoot[];
}

export const buildInnoEnv = ({ options, appExecutable, roots }: BuildInnoEnvContext): InnoEnv => {
  const appRoot = roots.find((root) => root.name === 'app');
  const serverRoot = roots.find((root) => root.name === 'server');

  return {
    /* 应用标识 */
    KOI_APP_NAME: sanitizeForIspp(options.appName),
    KOI_APP_ID: options.appId,
    KOI_APP_VERSION: options.version,
    KOI_APP_PUBLISHER: sanitizeForIspp(options.publisher),
    KOI_APP_DESCRIPTION: sanitizeForIspp(options.description),
    KOI_APP_EXE: appExecutable,

    /* 安装位置与权限 */
    KOI_INSTALL_DIR: options.installDir,
    KOI_PRIVILEGES: options.requireAdmin ? 'admin' : 'lowest',
    KOI_ALLOW_PRIV_OVERRIDE: options.allowPrivilegeOverride ? '1' : '0',

    /* 架构与压缩 */
    KOI_ARCH: options.architecture,
    KOI_COMPRESSION: options.compression,
    KOI_FILE_VERSION: toFileVersion(options.version),

    /* 输出与暂存 */
    KOI_OUTPUT_DIR: toWinPath(options.outputDir),
    KOI_OUTPUT_BASENAME: options.installerFileName,
    KOI_STAGING_DIR: toWinPath(options.stagingDir),

    /* 安装内容布局 */
    KOI_SERVER_DIR: options.backend.dirName,
    KOI_SCRIPT_DIR: options.backend.scriptDirName,
    KOI_SERVER_EXE: options.backend.executable,
    KOI_DATA_DIR: toInnoEnvRef(options.backend.dataDir),

    /* 可选文件 */
    KOI_SETUP_ICON: options.icon ? toWinPath(options.icon) : '',
    KOI_LICENSE_FILE: options.licenseFile ? toWinPath(options.licenseFile) : '',

    /* 自启动与交互开关 */
    KOI_AUTOSTART_SERVER: options.backend.autoStart.enabled ? '1' : '0',
    KOI_AUTOSTART_APP: options.appAutoStart.enabled ? '1' : '0',
    KOI_DESKTOP_SHORTCUT: options.desktopShortcut ? '1' : '0',
    KOI_LAUNCH_AFTER: options.launchAfterInstall ? '1' : '0',
    KOI_PROMPT_DELETE_DATA: options.promptDeleteUserData ? '1' : '0',

    /* 注册表自启动位置 */
    KOI_SERVER_SCOPE: options.backend.autoStart.scope === 'machine' ? 'HKLM' : 'HKCU',
    KOI_APP_SCOPE: options.appAutoStart.scope === 'machine' ? 'HKLM' : 'HKCU',

    /* 语言 */
    ...languageEnv(options.languages),

    /* 免压缩文件 */
    ...noCompressEnv(appRoot, 'APP'),
    ...noCompressEnv(serverRoot, 'SERVER'),
  };
};

/** 过滤掉空值，便于日志与调试文件保持简洁 */
export const compactInnoEnv = (env: InnoEnv): InnoEnv =>
  Object.fromEntries(Object.entries(env).filter(([, value]) => value !== ''));

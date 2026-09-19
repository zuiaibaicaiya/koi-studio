import { createHash } from 'node:crypto';
import path from 'node:path';
import type {
  BackendOptions,
  LanguageSpec,
  ResolvedAutoStart,
  ResolvedBackendOptions,
  ResolvedRestart,
  ResolvedWinInstallerOptions,
  WinInstallerOptions,
} from './types';
import { captureCommand, isFile, pathExists, readText, toWinPath } from './utils';

/** Inno Setup 尽可能常见的安装位置（按优先级） */
const ISCC_CANDIDATES = [
  'C:\\Program Files (x86)\\Inno Setup 6\\ISCC.exe',
  'C:\\Program Files\\Inno Setup 6\\ISCC.exe',
  'C:\\Program Files (x86)\\Inno Setup 5\\ISCC.exe',
];

const DEFAULT_APP_NAME = 'koi-studio';

/** 后端安装包内目录名 */
const SERVER_DIR_NAME = 'server';
/** 运行时脚本目录名 */
const SCRIPT_DIR_NAME = 'scripts';

/** 后端随包分发的默认文件/目录（相对 serverRoot） */
const DEFAULT_SERVER_INCLUDE = [
  'koi-server.exe',
  'onnxruntime.dll',
  'sherpa-onnx-c-api.dll',
  'sherpa-onnx-cxx-api.dll',
  '.env',
  'resources',
  'LICENSE',
];

const DEFAULT_SERVER_MODELS = ['models'];

const DEFAULT_RESTART: ResolvedRestart = {
  enabled: true,
  delayMs: 3000,
  maxRestarts: 0,
  windowMs: 60000,
  stopOnExitCodes: [0],
  logMaxBytes: 5 * 1024 * 1024,
};

const DEFAULT_AUTO_START: ResolvedAutoStart = {
  enabled: true,
  scope: 'user',
  delaySeconds: 0,
};

/** 按应用名生成稳定的 GUID，保证升级安装时 AppId 不变 */
const buildAppId = (appName: string): string => {
  const hash = createHash('sha1').update(`rsbuild-plugin-win-installer:${appName}`).digest('hex');
  return [
    hash.slice(0, 8),
    hash.slice(8, 12),
    `5${hash.slice(13, 16)}`,
    `a${hash.slice(17, 20)}`,
    hash.slice(20, 32),
  ].join('-');
};

/** 读取项目 package.json 的版本号 */
const readPackageVersion = async (projectRoot: string): Promise<string> => {
  try {
    const raw = await readText(path.join(projectRoot, 'package.json'));
    const parsed: unknown = JSON.parse(raw);
    if (parsed && typeof parsed === 'object' && typeof (parsed as { version?: unknown }).version === 'string') {
      return (parsed as { version: string }).version;
    }
  } catch {
    /* 交给默认值 */
  }
  return '1.0.0';
};

/**
 * 从 `.env.production` 之类的文件中读取前端 API 地址，
 * 用于让后端监听端口与前端编译期写入的地址保持一致。
 */
export const resolveFrontendApiBase = async (
  projectRoot: string,
): Promise<{ url: string; port: string } | null> => {
  const candidates = ['.env.production', '.env'];
  for (const file of candidates) {
    const full = path.join(projectRoot, file);
    if (!(await isFile(full))) continue;
    const content = await readText(full);
    const matched = /^\s*PUBLIC_API_BASE\s*=\s*(.+?)\s*$/m.exec(content);
    if (!matched) continue;
    const url = matched[1].replace(/^["']|["']$/g, '');
    const portMatched = /:(\d{2,5})\s*\/?$/.exec(url);
    if (portMatched) return { url: url.replace(/\/$/, ''), port: portMatched[1] };
  }
  return null;
};

/** 插件目录下的默认脚本模板相对路径（相对 projectRoot） */
const DEFAULT_TEMPLATE_RELATIVE = path.join(
  'plugins',
  'rsbuild-plugin-win-installer',
  'installer',
  'koi-installer.iss',
);

/** 定位 Inno Setup 脚本模板：显式配置优先，其次取插件自带模板 */
const resolveTemplateFile = async (
  configured: string | undefined,
  projectRoot: string,
): Promise<string> => {
  const candidates: string[] = [];
  if (configured) candidates.push(path.resolve(projectRoot, configured));
  candidates.push(path.join(projectRoot, DEFAULT_TEMPLATE_RELATIVE));

  for (const candidate of candidates) {
    if (await isFile(candidate)) return candidate;
  }
  throw new Error(
    `未找到 Inno Setup 脚本模板，请通过 installer.templateFile 指定。已尝试：\n${candidates.join('\n')}`,
  );
};

/** 探测 Inno Setup 安装目录与编译器路径 */
export const resolveInnoSetup = async (
  configured: string | undefined,
  logger: { warn: (message: string) => void },
): Promise<{ isccPath: string; innoDir: string | null }> => {
  const candidates: string[] = [];
  if (configured) candidates.push(configured);
  if (process.env['ISCC']) candidates.push(process.env['ISCC']);
  const home = process.env['INNO_SETUP_HOME'];
  if (home) candidates.push(path.join(home, 'ISCC.exe'));
  const localAppData = process.env['LOCALAPPDATA'];
  if (localAppData) {
    candidates.push(path.join(localAppData, 'Programs', 'Inno Setup 6', 'ISCC.exe'));
  }
  candidates.push(...ISCC_CANDIDATES);

  for (const candidate of candidates) {
    if (await isFile(candidate)) {
      return { isccPath: path.resolve(candidate), innoDir: path.dirname(path.resolve(candidate)) };
    }
  }

  const fromPath = await captureCommand('where ISCC.exe', process.cwd());
  if (fromPath) {
    const first = fromPath.split(/\r?\n/).map((line) => line.trim()).filter(Boolean)[0];
    if (first && (await isFile(first))) {
      return { isccPath: first, innoDir: path.dirname(first) };
    }
  }

  logger.warn('未能自动定位 ISCC.exe，请通过 `commands.iscc` 显式指定 Inno Setup 编译器路径。');
  return { isccPath: 'ISCC.exe', innoDir: null };
};

/** Inno Setup 常见语言名 -> MessagesFile 映射（非英文语言包为可选组件，需本机存在） */
const KNOWN_LANGUAGES: Record<string, { messagesFile: string; isl: string | null }> = {
  english: { messagesFile: 'compiler:Default.isl', isl: null },
  chinesesimplified: { messagesFile: 'compiler:Languages\\ChineseSimplified.isl', isl: 'ChineseSimplified.isl' },
  chinesetraditional: { messagesFile: 'compiler:Languages\\ChineseTraditional.isl', isl: 'ChineseTraditional.isl' },
};

/** 依据 Inno Setup 自带语言包探测可用的语言列表 */
export const resolveLanguages = async (
  configured: string[] | undefined,
  innoDir: string | null,
): Promise<LanguageSpec[]> => {
  const hasChinese = Boolean(innoDir) && (await isFile(path.join(innoDir ?? '', 'Languages', 'ChineseSimplified.isl')));
  const names = configured?.length ? configured : hasChinese ? ['chinesesimplified', 'english'] : ['english'];

  const result: LanguageSpec[] = [];
  for (const name of names) {
    const key = name.toLowerCase().replace(/[\s_-]/g, '');
    if (!KNOWN_LANGUAGES[key]) {
      // 允许直接传入 `语言名:MessagesFile` 形式，例如 `italian:compiler:Languages\Italian.isl`
      const separator = name.indexOf(':');
      if (separator > 0) {
        result.push({ name: name.slice(0, separator), messagesFile: name.slice(separator + 1) });
      }
      continue;
    }
    const spec = KNOWN_LANGUAGES[key];
    if (spec.isl && !(await isFile(path.join(innoDir ?? '', 'Languages', spec.isl)))) continue;
    result.push({ name: key, messagesFile: spec.messagesFile });
  }

  return result.length > 0 ? result : [{ name: 'english', messagesFile: 'compiler:Default.isl' }];
};

const resolveBackend = async (
  raw: BackendOptions | undefined,
  context: {
    serverRoot: string;
    appName: string;
    defaultPort: string;
    defaultGrpcPort: string;
    logger: { warn: (message: string) => void };
  },
): Promise<ResolvedBackendOptions> => {
  const backend = raw ?? {};
  const { serverRoot, appName, defaultPort, defaultGrpcPort, logger } = context;

  let executable = backend.executable;
  if (!executable) {
    // 依次尝试 <appName>-server.exe / koi-server.exe，都不存在时按前者命名（go build 会生成）
    if (await isFile(path.join(serverRoot, `${appName}-server.exe`))) executable = `${appName}-server.exe`;
    else if (await isFile(path.join(serverRoot, 'koi-server.exe'))) executable = 'koi-server.exe';
    else executable = `${appName}-server.exe`;
  }

  const include = (backend.include ?? [...DEFAULT_SERVER_INCLUDE, executable]).filter(
    (item, index, list) => list.indexOf(item) === index,
  );

  const envFromFrontend: Record<string, string> = {
    APP_HOST: '127.0.0.1',
    APP_PORT: defaultPort,
    APP_URL: `http://127.0.0.1:${defaultPort}`,
    GRPC_HOST: '127.0.0.1',
    GRPC_PORT: defaultGrpcPort,
  };

  const env: Record<string, string> = { ...envFromFrontend };
  for (const [key, value] of Object.entries(backend.env ?? {})) {
    env[key] = String(value);
  }

  const models = backend.models ?? DEFAULT_SERVER_MODELS;
  for (const model of models) {
    if (!(await pathExists(path.join(serverRoot, model)))) {
      logger.warn(`后端模型目录不存在，已跳过：${path.join(serverRoot, model)}`);
    }
  }

  return {
    enabled: backend.enabled ?? true,
    dirName: SERVER_DIR_NAME,
    scriptDirName: SCRIPT_DIR_NAME,
    executable,
    include,
    models,
    env,
    dataDir: backend.dataDir ?? `%USERPROFILE%\\.${appName}`,
    dataStrategy: backend.dataStrategy ?? 'copy',
    overwriteEnvOnUpgrade: backend.overwriteEnvOnUpgrade ?? false,
    restart: { ...DEFAULT_RESTART, ...(backend.restart ?? {}) },
    autoStart: { ...DEFAULT_AUTO_START, ...(backend.autoStart ?? {}) },
  };
};

/**
 * 把用户配置归一化为完整配置。
 * 所有相对路径都基于 `projectRoot`（默认 `process.cwd()`）解析。
 */
export const resolveOptions = async (
  raw: WinInstallerOptions,
  context: {
    /** 由 rsbuild 触发时为 true */
    builtByRsbuild: boolean;
    logger: { info: (message: string) => void; detail: (message: string) => void; warn: (message: string) => void };
  },
): Promise<{ options: ResolvedWinInstallerOptions; isccPath: string; innoSetupDir: string | null }> => {
  const projectRoot = path.resolve(raw.projectRoot ?? process.cwd());
  const appName = raw.appName ?? DEFAULT_APP_NAME;
  const serverRoot = path.resolve(raw.serverRoot ?? path.join(projectRoot, '..', 'koi-server'));
  const outputDir = path.resolve(raw.outputDir ?? path.join(projectRoot, 'build-installer'));
  const version = raw.version ?? (await readPackageVersion(projectRoot));

  const apiBase = await resolveFrontendApiBase(projectRoot);
  const defaultPort = apiBase?.port ?? '5168';
  context.logger.detail(`前端接口地址：${apiBase?.url ?? `http://127.0.0.1:${defaultPort}（未找到 PUBLIC_API_BASE，使用默认端口）`}`);

  const inno = await resolveInnoSetup(raw.commands?.iscc, context.logger);
  const appearance = raw.installer ?? {};
  const languages = await resolveLanguages(appearance.languages, inno.innoDir);

  const backend = await resolveBackend(raw.backend, {
    serverRoot,
    appName,
    defaultPort,
    defaultGrpcPort: '50052',
    logger: context.logger,
  });

  const iconCandidate = appearance.icon ? path.resolve(appearance.icon) : null;
  const licenseCandidate = appearance.licenseFile ? path.resolve(appearance.licenseFile) : null;
  if (iconCandidate && !(await isFile(iconCandidate))) {
    context.logger.warn(`安装程序图标不存在，已忽略：${iconCandidate}`);
  }
  if (licenseCandidate && !(await isFile(licenseCandidate))) {
    context.logger.warn(`许可协议文件不存在，已忽略：${licenseCandidate}`);
  }

  const electronConfigCandidate = raw.electronConfigFile
    ? path.resolve(raw.electronConfigFile)
    : path.join(projectRoot, 'electron-builder.config.mjs');

  const options: ResolvedWinInstallerOptions = {
    enabled: raw.enabled ?? process.env['KOI_PACKAGE'] === '1',
    appName,
    appId: raw.appId ?? buildAppId(appName),
    version,
    publisher: raw.publisher ?? `${appName} Team`,
    description: raw.description ?? `${appName} 桌面客户端与本地后端服务`,
    projectRoot,
    serverRoot,
    outputDir,
    stagingDir: path.join(outputDir, 'staging'),
    issFile: path.join(outputDir, 'installer.iss'),
    installerTemplate: await resolveTemplateFile(appearance.templateFile, projectRoot),
    innoEnvFile: path.join(outputDir, 'installer.env.json'),
    installerFileName: raw.installerFileName ?? `${appName}-setup-${version}`,
    installDir: appearance.installDir ?? `{autopf}\\${appName}`,
    requireAdmin: appearance.requireAdmin ?? false,
    allowPrivilegeOverride: appearance.allowPrivilegeOverride ?? true,
    architecture: appearance.architecture ?? 'x64compatible',
    compression: appearance.compression ?? 'lzma2/max',
    languages,
    icon: iconCandidate && (await isFile(iconCandidate)) ? iconCandidate : null,
    licenseFile: licenseCandidate && (await isFile(licenseCandidate)) ? licenseCandidate : null,
    launchAfterInstall: appearance.launchAfterInstall ?? true,
    desktopShortcut: appearance.desktopShortcut ?? true,
    promptDeleteUserData: appearance.promptDeleteUserData ?? true,
    noCompressionGlobs: appearance.noCompressionGlobs ?? ['*.onnx'],
    frontendDist: path.resolve(projectRoot, raw.frontendDist ?? 'dist'),
    electronOutputDir: path.resolve(raw.electronOutputDir ?? path.join(outputDir, 'electron')),
    electronConfigFile: (await isFile(electronConfigCandidate)) ? electronConfigCandidate : null,
    electronProductName: raw.electronProductName ?? null,
    keepStaging: raw.keepStaging ?? true,
    verbose: raw.verbose ?? false,
    steps: {
      frontend: raw.steps?.frontend ?? true,
      backend: raw.steps?.backend ?? true,
      electron: raw.steps?.electron ?? true,
      installer: raw.steps?.installer ?? true,
    },
    commands: {
      frontend: raw.commands?.frontend ?? 'npx rsbuild build',
      backend: raw.commands?.backend ?? `go build -ldflags "-s -w" -o "${backend.executable}" .`,
      electron: raw.commands?.electron ?? '',
      iscc: inno.isccPath,
    },
    backend,
    appAutoStart: { ...DEFAULT_AUTO_START, ...(raw.appAutoStart ?? {}) },
  };

  return { options, isccPath: inno.isccPath, innoSetupDir: inno.innoDir };
};

/** 便捷导出：把路径转成 `.iss` 可用的 Windows 反斜杠形式 */
export { toWinPath };

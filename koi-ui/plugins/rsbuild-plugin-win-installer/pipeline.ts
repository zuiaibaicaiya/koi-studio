import { readdir, rm } from 'node:fs/promises';
import path from 'node:path';
import { pathToFileURL } from 'node:url';
import { buildInnoEnv, compactInnoEnv } from './env';
import {
  buildRuntimeConfig,
  renderBootstrapScript,
  renderHiddenLauncher,
  renderReadme,
  renderRuntimeConfigFile,
  renderStopHiddenLauncher,
  renderStopScript,
  renderWatchdogScript,
} from './templates/runtime';
import type {
  PackLogger,
  PipelineContext,
  ResolvedWinInstallerOptions,
  StagingRoot,
} from './types';
import {
  buildRuntimeKey,
  copyTree,
  dirSize,
  ensureDir,
  fileSize,
  formatBytes,
  isDirectory,
  isFile,
  matchGlob,
  pathExists,
  readText,
  resetDir,
  runCommand,
  toErrorMessage,
  walkFiles,
  writeText,
  writeTextWithBom,
} from './utils';

const STAGING_APP = 'app';
const STAGING_SERVER = 'server';
const STAGING_SCRIPTS = 'scripts';

/** 流水线步骤总数（含可选跳过项） */
const TOTAL_STEPS = 6;

/* ------------------------------------------------------------------ */
/* 公共小工具                                                          */
/* ------------------------------------------------------------------ */

/** 定位项目内 node_modules/.bin 下的可执行文件，避免 npx 触发联网下载 */
const resolveLocalBin = async (
  projectRoot: string,
  name: string,
  fallback: string,
): Promise<string> => {
  for (const candidate of [`${name}.cmd`, name, `${name}.exe`]) {
    const full = path.join(projectRoot, 'node_modules', '.bin', candidate);
    if (await isFile(full)) return `"${full}"`;
  }
  return fallback;
};

/** 把键值对写进 .env 文本（保留注释与原有键顺序，缺失的键追加到末尾） */
export const applyEnvOverrides = (content: string, overrides: Record<string, string>): string => {
  const keys = Object.keys(overrides);
  if (keys.length === 0) return content;

  const applied = new Set<string>();
  const lines = content.split(/\r?\n/).map((line) => {
    const trimmed = line.trimStart();
    if (!trimmed || trimmed.startsWith('#')) return line;
    const matched = /^\s*([A-Za-z_][A-Za-z0-9_]*)\s*=/.exec(line);
    if (!matched) return line;
    const key = matched[1];
    if (!(key in overrides)) return line;
    applied.add(key);
    return `${key}=${overrides[key]}`;
  });

  for (const key of keys) {
    if (!applied.has(key)) lines.push(`${key}=${overrides[key]}`);
  }
  return lines.join('\r\n');
};

/* ------------------------------------------------------------------ */
/* 步骤 1：前端构建（独立运行打包时使用）                               */
/* ------------------------------------------------------------------ */

const buildFrontend = async (context: PipelineContext, index: number): Promise<void> => {
  const { options, logger } = context;
  const step = logger.step(index, TOTAL_STEPS, '构建前端资源');

  if (context.builtByRsbuild) {
    step.skip('由 rsbuild 本次构建，直接复用 dist');
    return;
  }
  if (!options.steps.frontend) {
    step.skip('steps.frontend = false');
    return;
  }

  step.info(`构建命令：${options.commands.frontend}`);
  await runCommand(options.commands.frontend, {
    cwd: options.projectRoot,
    logger,
    label: 'frontend',
  });

  if (!(await isFile(path.join(options.frontendDist, 'index.html')))) {
    step.fail(`前端产物缺失：${path.join(options.frontendDist, 'index.html')}`);
    throw new Error('前端构建未生成预期产物');
  }
  step.done(`前端产物 ${options.frontendDist}`);
};

/* ------------------------------------------------------------------ */
/* 步骤 2：后端构建                                                    */
/* ------------------------------------------------------------------ */

const buildBackend = async (context: PipelineContext, index: number): Promise<void> => {
  const { options, logger } = context;
  const step = logger.step(index, TOTAL_STEPS, '构建 Go 后端服务');

  if (!options.backend.enabled) {
    step.skip('backend.enabled = false');
    return;
  }
  if (!options.steps.backend) {
    step.skip('steps.backend = false');
    return;
  }
  if (!(await isDirectory(options.serverRoot))) {
    step.fail(`后端目录不存在：${options.serverRoot}`);
    throw new Error(`后端目录不存在：${options.serverRoot}`);
  }

  step.info(`工程目录：${options.serverRoot}`);
  step.info(`构建命令：${options.commands.backend}`);

  try {
    await runCommand(options.commands.backend, {
      cwd: options.serverRoot,
      logger,
      label: 'go',
      hint: '请确认已安装 Go 工具链；若提示文件被占用，请先停止正在运行的后端服务。',
    });
  } catch (error) {
    step.fail(toErrorMessage(error));
    throw error;
  }

  const executable = path.join(options.serverRoot, options.backend.executable);
  if (!(await isFile(executable))) {
    step.fail(`未生成可执行文件：${executable}`);
    throw new Error(`未生成可执行文件：${executable}`);
  }
  step.done(`${options.backend.executable} · ${formatBytes(await fileSize(executable))}`);
};

/* ------------------------------------------------------------------ */
/* 步骤 3：桌面端（Electron）打包                                      */
/* ------------------------------------------------------------------ */

const renderElectronConfig = (
  options: ResolvedWinInstallerOptions,
  baseConfigPath: string | null,
): string => {
  const target = baseConfigPath ? pathToFileURL(baseConfigPath).href : null;
  const lines: string[] = [
    '// 由 rsbuild-plugin-win-installer 自动生成：仅用于 Windows 免安装目录（win-unpacked）打包',
    "import { createRequire } from 'node:module';",
    "import { pathToFileURL } from 'node:url';",
    '',
    'const require = createRequire(import.meta.url);',
  ];

  if (target) {
    lines.push(
      `const baseTarget = ${JSON.stringify(target)};`,
      "const loaded = baseTarget.endsWith('.cjs')",
      '  ? { default: require(new URL(baseTarget).pathname) }',
      '  : await import(baseTarget);',
      'const base = loaded.default ?? loaded;',
    );
  } else {
    lines.push('const base = {};');
  }

  lines.push(
    'export default {',
    '  ...base,',
    '  directories: {',
    '    ...(base.directories ?? {}),',
    `    output: ${JSON.stringify(options.electronOutputDir.replace(/\\/g, '/'))},`,
    '  },',
    '  // 后端与模型由安装程序单独分发，避免在客户端 resources 里重复打包一份',
    '  extraResources: [],',
    "  files: base.files ?? ['dist'],",
    '  // afterPack 面向 macOS 的 .env 改写，Windows 侧由安装程序负责',
    '  afterPack: undefined,',
    '  pkg: undefined,',
    '  deb: undefined,',
    '  mac: undefined,',
    '  linux: undefined,',
    '  win: {',
    '    ...(base.win ?? {}),',
    "    target: ['dir'],",
    options.icon ? `    icon: ${JSON.stringify(options.icon.replace(/\\/g, '/'))},` : '',
    '  },',
    '};',
  );

  return lines.filter((line) => line !== '').join('\n') + '\n';
};

/** 在 win-unpacked 根目录中识别桌面客户端主程序 */
const detectAppExecutable = async (unpackedDir: string, hint: string | null): Promise<string> => {
  const entries = await readdir(unpackedDir, { withFileTypes: true });
  const candidates: Array<{ name: string; size: number }> = [];
  for (const entry of entries) {
    if (!entry.isFile()) continue;
    const lower = entry.name.toLowerCase();
    if (!lower.endsWith('.exe')) continue;
    if (lower.startsWith('uninstall') || lower.includes('elevate') || lower === 'updater.exe') continue;
    candidates.push({ name: entry.name, size: await fileSize(path.join(unpackedDir, entry.name)) });
  }
  if (candidates.length === 0) {
    throw new Error(`未在 ${unpackedDir} 中找到桌面客户端主程序（*.exe）`);
  }

  if (hint) {
    const matched = candidates.find((item) => item.name.toLowerCase() === `${hint.toLowerCase()}.exe`);
    if (matched) return matched.name;
  }
  // 主程序体积远大于辅助进程，按体积降序取第一个最稳妥
  candidates.sort((a, b) => b.size - a.size);
  return candidates[0].name;
};

const buildElectron = async (context: PipelineContext, index: number): Promise<void> => {
  const { options, logger } = context;
  const step = logger.step(index, TOTAL_STEPS, '打包桌面客户端（electron-builder --win --dir）');

  if (!options.steps.electron) {
    step.skip('steps.electron = false');
  } else {
    if (!(await isFile(path.join(options.frontendDist, 'index.html')))) {
      step.fail(`前端产物不完整，缺少 ${path.join(options.frontendDist, 'index.html')}`);
      throw new Error('前端构建产物缺失，请先执行前端构建');
    }
    if (!(await isFile(path.join(options.frontendDist, 'electron', 'main.cjs')))) {
      logger.warn(`未找到 ${path.join(options.frontendDist, 'electron', 'main.cjs')}，请确认 electron-rs 插件已启用。`);
    }

    const configPath = path.join(options.outputDir, 'electron-builder.win.mjs');
    await writeText(configPath, renderElectronConfig(options, options.electronConfigFile));
    step.info(`electron-builder 配置：${configPath}`);

    const bin = await resolveLocalBin(options.projectRoot, 'electron-builder', 'npx --yes electron-builder');
    const command = options.commands.electron || `${bin} --win --dir --config "${configPath}"`;
    step.info(`打包命令：${command}`);

    await resetDir(options.electronOutputDir);
    try {
      await runCommand(command, {
        cwd: options.projectRoot,
        logger,
        label: 'electron',
        hint: 'electron-builder 首次运行需要联网下载 winCodeSign / nsis 等辅助工具，失败后可直接重试。',
      });
    } catch (error) {
      step.fail(toErrorMessage(error));
      throw error;
    }
  }

  const unpackedDir = path.join(options.electronOutputDir, 'win-unpacked');
  if (!(await isDirectory(unpackedDir))) {
    step.fail(`未找到 win-unpacked 目录：${unpackedDir}`);
    throw new Error(`未找到 win-unpacked 目录：${unpackedDir}`);
  }

  context.appExecutable = await detectAppExecutable(unpackedDir, options.electronProductName);
  step.done(`客户端主程序 ${context.appExecutable}`);
};

/* ------------------------------------------------------------------ */
/* 步骤 4：产物收集                                                    */
/* ------------------------------------------------------------------ */

type StepLike = { info: (message: string) => void };

const stageBackend = async (context: PipelineContext, step: StepLike): Promise<string[]> => {
  const { options, logger } = context;
  const serverDest = await ensureDir(path.join(options.stagingDir, STAGING_SERVER));
  const syncedDirs: string[] = [];

  const entries = [...options.backend.include, ...options.backend.models].filter(
    (item, position, list) => list.indexOf(item) === position,
  );

  for (const entry of entries) {
    const source = path.join(options.serverRoot, entry);
    if (entry === '.env') {
      // .env 需按插件配置重写端口，单独处理
      if (await isFile(source)) {
        const content = applyEnvOverrides(await readText(source), options.backend.env);
        await writeText(path.join(serverDest, '.env'), content);
        step.info(`写入 .env（APP_PORT=${options.backend.env['APP_PORT'] ?? '未指定'}）`);
      } else {
        logger.warn(`缺少环境文件：${source}`);
      }
      continue;
    }
    if (await isFile(source)) {
      await copyTree(source, path.join(serverDest, entry));
      step.info(`包含文件 ${entry} · ${formatBytes(await fileSize(source))}`);
      continue;
    }
    if (await isDirectory(source)) {
      await copyTree(source, path.join(serverDest, entry));
      const top = entry.split(/[\\/]/)[0];
      if (top !== '.') syncedDirs.push(top);
      step.info(`包含目录 ${entry}\\ · ${formatBytes(await dirSize(source))}`);
      continue;
    }
    logger.warn(`跳过不存在的后端资源：${source}`);
  }

  if (!(await isFile(path.join(serverDest, options.backend.executable)))) {
    throw new Error(`后端可执行文件未进入安装包：${options.backend.executable}`);
  }

  return [...new Set(syncedDirs)];
};

const stageRuntimeScripts = async (
  context: PipelineContext,
  syncedDirs: string[],
  step: StepLike,
): Promise<void> => {
  const { options } = context;
  const scriptsDir = await ensureDir(path.join(options.stagingDir, STAGING_SCRIPTS));

  await writeText(
    path.join(scriptsDir, 'runtime.json'),
    renderRuntimeConfigFile(buildRuntimeConfig(options, syncedDirs)),
  );
  await writeTextWithBom(path.join(scriptsDir, 'watchdog.ps1'), renderWatchdogScript());
  await writeTextWithBom(path.join(scriptsDir, 'bootstrap-runtime.ps1'), renderBootstrapScript());
  // 纯 ASCII 的运行期标识：.vbs 由 WScript 按 ANSI 读取，不能直接放应用名
  const runtimeKey = buildRuntimeKey(options.appName);
  await writeTextWithBom(path.join(scriptsDir, 'stop-server.ps1'), renderStopScript(runtimeKey));
  await writeText(path.join(scriptsDir, 'launch-hidden.vbs'), renderHiddenLauncher(runtimeKey));
  await writeText(path.join(scriptsDir, 'stop-hidden.vbs'), renderStopHiddenLauncher());

  step.info(`守护脚本：${path.join(options.stagingDir, STAGING_SCRIPTS)}\\watchdog.ps1`);
  step.info(`数据目录：${options.backend.dataDir}`);
  step.info(
    `崩溃重启：${options.backend.restart.enabled ? `开启（延迟 ${options.backend.restart.delayMs}ms` : '关闭（'}${
      options.backend.restart.maxRestarts > 0
        ? `，熔断 ${options.backend.restart.maxRestarts} 次 / ${options.backend.restart.windowMs}ms）`
        : '，不熔断）'
    }`,
  );
  step.info(`资源落地方式：${options.backend.dataStrategy === 'junction' ? '目录联接（零拷贝）' : '复制到数据目录'}`);
};

/** 计算每个 staging 根目录下命中「不压缩」通配符的文件 */
const buildStagingRoots = async (options: ResolvedWinInstallerOptions): Promise<StagingRoot[]> => {
  const definitions = [
    { name: STAGING_APP, destSubDir: '' },
    { name: STAGING_SERVER, destSubDir: options.backend.dirName },
    { name: STAGING_SCRIPTS, destSubDir: options.backend.scriptDirName },
  ];

  const roots: StagingRoot[] = [];
  for (const definition of definitions) {
    const files = await walkFiles(path.join(options.stagingDir, definition.name));
    const noCompression = options.noCompressionGlobs.filter((glob) =>
      files.some((file) => matchGlob(glob, path.basename(file))),
    );
    roots.push({ ...definition, noCompression });
  }
  return roots;
};

const stageArtifacts = async (context: PipelineContext, index: number): Promise<StagingRoot[]> => {
  const { options, logger } = context;
  const step = logger.step(index, TOTAL_STEPS, '收集安装产物');

  await ensureDir(options.outputDir);
  await resetDir(options.stagingDir);

  const unpackedDir = path.join(options.electronOutputDir, 'win-unpacked');
  if (!(await isDirectory(unpackedDir))) {
    throw new Error(`未找到桌面端打包产物：${unpackedDir}`);
  }
  const appDest = path.join(options.stagingDir, STAGING_APP);
  await copyTree(unpackedDir, appDest);
  step.info(`客户端文件 · ${formatBytes(await dirSize(appDest))}`);

  const syncedDirs = options.backend.enabled ? await stageBackend(context, step) : [];
  await stageRuntimeScripts(context, syncedDirs, step);

  await writeTextWithBom(path.join(appDest, 'README.txt'), renderReadme(options));
  step.info('已生成安装目录使用说明 README.txt');

  const roots = await buildStagingRoots(options);
  step.done(`安装内容共 ${formatBytes(await dirSize(options.stagingDir))}`);
  return roots;
};

/* ------------------------------------------------------------------ */
/* 步骤 5：装配 Inno Setup 脚本（复制真实 .iss + 计算注入变量）         */
/* ------------------------------------------------------------------ */

const prepareInstallerScript = async (
  context: PipelineContext,
  roots: StagingRoot[],
  index: number,
): Promise<Record<string, string>> => {
  const { options, logger } = context;
  const step = logger.step(index, TOTAL_STEPS, '装配 Inno Setup 脚本');

  // .iss 是可读、可直接编辑的文件：这里只做转写，内容完全由环境变量驱动。
  // 必须写成带 BOM 的 UTF-8，否则 Inno Setup 会按 ANSI 解析，脚本中的中文将变成乱码。
  await writeTextWithBom(options.issFile, await readText(options.installerTemplate));
  const env = buildInnoEnv({ options, appExecutable: context.appExecutable, roots });
  const compact = compactInnoEnv(env);

  // 落一份变量快照，便于手工在 Inno Setup IDE / ISCC 中复现本次编译
  await writeText(
    options.innoEnvFile,
    `${JSON.stringify({ generatedAt: new Date().toISOString(), variables: compact }, null, 2)}\n`,
  );

  step.info(`模板：${path.relative(options.projectRoot, options.installerTemplate)}`);
  step.info(`脚本：${path.relative(options.projectRoot, options.issFile)}`);
  step.info(`变量快照：${path.relative(options.projectRoot, options.innoEnvFile)}（共 ${Object.keys(compact).length} 项）`);
  step.info(`语言：${options.languages.map((item) => item.name).join(', ')}`);
  step.info(`压缩：${options.compression}（跳过压缩：${options.noCompressionGlobs.join(', ') || '无'}）`);
  step.done(options.installerFileName + '.exe');
  return env;
};

/* ------------------------------------------------------------------ */
/* 步骤 6：编译安装包                                                  */
/* ------------------------------------------------------------------ */

const compileInstaller = async (
  context: PipelineContext,
  innoEnv: Record<string, string>,
  index: number,
): Promise<string> => {
  const { options, logger } = context;
  const step = logger.step(index, TOTAL_STEPS, '编译 Windows 安装包（Inno Setup）');

  if (!options.steps.installer) {
    step.skip('steps.installer = false');
    return '';
  }

  const target = path.join(options.outputDir, `${options.installerFileName}.exe`);
  step.info(`编译器：${context.isccPath}`);
  if (context.innoSetupDir) step.info(`Inno Setup 目录：${context.innoSetupDir}`);
  step.info(`注入环境变量：${Object.keys(innoEnv).length} 项（KOI_* 前缀）`);

  const started = Date.now();
  const result = await runCommand(`"${context.isccPath}" "${options.issFile}"`, {
    cwd: options.outputDir,
    logger,
    label: 'iscc',
    fatal: false,
    env: innoEnv,
    hint: '若提示找不到 ISCC.exe，请安装 Inno Setup 6，或通过 commands.iscc 指定编译器路径。',
  });

  // ISCC 输出较冗长，这里只回显关键行（含 .iss 中 #pragma message 打出的 koi-installer 摘要）
  for (const line of result.output.split(/\r?\n/)) {
    const trimmed = line.trim();
    if (!trimmed) continue;
    if (
      /^(Error|Warning|Compil|Successful|Preprocess|Reading)/i.test(trimmed) ||
      trimmed.includes('koi-installer') ||
      trimmed.includes('.iss')
    ) {
      logger.detail(trimmed);
    }
  }

  if (result.code !== 0 || !(await isFile(target))) {
    step.fail(`ISCC 退出码 ${result.code}`);
    throw new Error(`Inno Setup 编译失败（退出码 ${result.code}），请查看上方 ISCC 输出。`);
  }

  step.done(`耗时 ${((Date.now() - started) / 1000).toFixed(1)}s`);
  logger.success(`安装包已生成：${target} · ${formatBytes(await fileSize(target))}`);
  return target;
};

/* ------------------------------------------------------------------ */
/* 编排                                                                */
/* ------------------------------------------------------------------ */

export interface PipelineRuntime {
  builtByRsbuild: boolean;
  isccPath: string;
  innoSetupDir: string | null;
}

export interface PipelineResult {
  installerPath: string;
  stagingDir: string;
  issFile: string;
}

export const runPipeline = async (
  options: ResolvedWinInstallerOptions,
  logger: PackLogger,
  runtime: PipelineRuntime,
): Promise<PipelineResult> => {
  const context: PipelineContext = {
    options,
    logger,
    builtByRsbuild: runtime.builtByRsbuild,
    appExecutable: '',
    isccPath: runtime.isccPath,
    innoSetupDir: runtime.innoSetupDir,
    startedAt: new Date(),
  };

  logger.title(`Windows 安装包打包 · ${options.appName} ${options.version}`);
  logger.info(`前端目录：${options.projectRoot}`);
  logger.info(`后端目录：${options.serverRoot}`);
  logger.info(`输出目录：${options.outputDir}`);
  logger.info(
    `前端产物：${options.frontendDist}（${runtime.builtByRsbuild ? '由本次 rsbuild 构建' : '复用已有产物'}）`,
  );
  logger.info(
    `安装位置：${options.installDir} · ${options.requireAdmin ? '需要管理员权限' : '当前用户免管理员'}`,
  );
  logger.info(
    `自启动：后端 ${options.backend.autoStart.enabled ? '开' : '关'} / 客户端 ${options.appAutoStart.enabled ? '开' : '关'}；` +
      `崩溃重启：${options.backend.restart.enabled ? `开（延迟 ${options.backend.restart.delayMs}ms）` : '关'}`,
  );
  logger.info('失败时会打印子进程输出的最后 40 行；配置 verbose=true 可实时查看全部日志。');

  await ensureDir(options.outputDir);

  try {
    await buildFrontend(context, 1);

    if (!(await pathExists(options.frontendDist))) {
      throw new Error(`前端产物目录不存在：${options.frontendDist}，请先执行前端构建。`);
    }

    await buildBackend(context, 2);
    await buildElectron(context, 3);
    const roots = await stageArtifacts(context, 4);
    const innoEnv = await prepareInstallerScript(context, roots, 5);
    const installerPath = await compileInstaller(context, innoEnv, 6);

    if (!options.keepStaging) {
      logger.detail('清理中间产物（keepStaging=false）');
      await rm(options.stagingDir, { recursive: true, force: true });
    }

    const cost = ((Date.now() - context.startedAt.getTime()) / 1000).toFixed(1);
    logger.title(`打包完成 · 总耗时 ${cost}s`);
    if (installerPath) {
      logger.success(installerPath);
      logger.info('建议在测试机实际安装一次，验证：后端随开机自启、杀掉进程后能自动重启。');
    } else {
      logger.warn(`已跳过安装包编译，脚本位于：${options.issFile}`);
    }

    return { installerPath, stagingDir: options.stagingDir, issFile: options.issFile };
  } catch (error) {
    logger.title('打包失败');
    logger.error(toErrorMessage(error));
    logger.info(`中间产物保留在 ${options.outputDir}，可用于定位问题。`);
    throw error;
  }
};

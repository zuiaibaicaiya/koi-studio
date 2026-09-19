import type { RsbuildPlugin } from '@rsbuild/core';
import { createLogger } from './logger';
import { resolveOptions } from './options';
import { runPipeline, type PipelineResult } from './pipeline';
import type { WinInstallerOptions } from './types';

export const PLUGIN_NAME = 'rsbuild-plugin-win-installer';

export type {
  AutoStartOptions,
  BackendOptions,
  BackendRestartOptions,
  InstallerAppearanceOptions,
  PackLogger,
  PipelineCommandsOptions,
  PipelineStepsOptions,
  ResolvedWinInstallerOptions,
  WinInstallerOptions,
} from './types';
export type { PipelineResult } from './pipeline';
export { applyEnvOverrides } from './pipeline';
export { createLogger, formatDuration } from './logger';

/** 打包流程的触发开关，`KOI_PACKAGE=1` 时自动启用 */
export const PACKAGE_ENV = 'KOI_PACKAGE';

export interface PackageInvocation {
  /** 是否由 rsbuild 的构建流程触发（为 true 时跳过前端构建，复用当前 dist） */
  builtByRsbuild?: boolean;
}

/**
 * 程序化执行打包流程（不经过 rsbuild 构建）。
 * 适合在自定义 Node 脚本或 CI 中直接调用。
 */
export const packageWindowsInstaller = async (
  options: WinInstallerOptions = {},
  invocation: PackageInvocation = {},
): Promise<PipelineResult | null> => {
  const logger = createLogger(options.verbose ?? false);
  const resolved = await resolveOptions(options, {
    builtByRsbuild: invocation.builtByRsbuild ?? false,
    logger,
  });

  if (!resolved.options.enabled) {
    logger.info(`打包流程未启用（设置 ${PACKAGE_ENV}=1 或在插件配置中传入 enabled: true）。`);
    return null;
  }

  return runPipeline(resolved.options, logger, {
    builtByRsbuild: invocation.builtByRsbuild ?? false,
    isccPath: resolved.isccPath,
    innoSetupDir: resolved.innoSetupDir,
  });
};

/**
 * 一键打包前后端并生成 Windows 安装程序的 Rsbuild 插件。
 *
 * 触发方式（满足其一即可）：
 * - 环境变量 `KOI_PACKAGE=1`（推荐：`npm run package:win`）
 * - 配置中显式传入 `enabled: true`
 *
 * 触发后会在 rsbuild 构建结束的钩子里依次执行：
 * 后端 go build → electron-builder 打包桌面端 → 收集产物 → 生成 .iss → 调用 ISCC 编译安装包。
 */
export const pluginWinInstaller = (options: WinInstallerOptions = {}): RsbuildPlugin => ({
  name: PLUGIN_NAME,
  setup(api) {
    // 未启用的常规构建完全不介入，避免影响日常开发
    const enabled = options.enabled ?? process.env[PACKAGE_ENV] === '1';

    api.onAfterBuild(async () => {
      if (!enabled) return;

      const logger = createLogger(options.verbose ?? false);
      const resolved = await resolveOptions(options, { builtByRsbuild: true, logger });

      await runPipeline(resolved.options, logger, {
        builtByRsbuild: true,
        isccPath: resolved.isccPath,
        innoSetupDir: resolved.innoSetupDir,
      });
    });
  },
});

export default pluginWinInstaller;

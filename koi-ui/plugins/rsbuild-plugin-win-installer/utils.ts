import { spawn } from 'node:child_process';
import { createHash } from 'node:crypto';
import { constants } from 'node:fs';
import { access, cp, mkdir, readdir, readFile, rm, stat, writeFile } from 'node:fs/promises';
import path from 'node:path';
import type { PackLogger } from './types';

/* ------------------------------------------------------------------ */
/* 文件系统工具                                                        */
/* ------------------------------------------------------------------ */

export const pathExists = async (target: string): Promise<boolean> => {
  try {
    await access(target, constants.F_OK);
    return true;
  } catch {
    return false;
  }
};

/** 判断是否为文件（不存在返回 false） */
export const isFile = async (target: string): Promise<boolean> => {
  try {
    return (await stat(target)).isFile();
  } catch {
    return false;
  }
};

/** 判断是否为目录（不存在返回 false） */
export const isDirectory = async (target: string): Promise<boolean> => {
  try {
    return (await stat(target)).isDirectory();
  } catch {
    return false;
  }
};

export const ensureDir = async (target: string): Promise<string> => {
  await mkdir(target, { recursive: true });
  return target;
};

/** 清空并重建目录 */
export const resetDir = async (target: string): Promise<string> => {
  await rm(target, { recursive: true, force: true });
  await mkdir(target, { recursive: true });
  return target;
};

/** 递归复制（保留目录结构） */
export const copyTree = async (from: string, to: string): Promise<void> => {
  await mkdir(path.dirname(to), { recursive: true });
  await cp(from, to, { recursive: true, force: true });
};

/** 递归列出目录下所有文件（相对路径使用 posix 分隔符） */
export const walkFiles = async (root: string, prefix = ''): Promise<string[]> => {
  const result: string[] = [];
  let entries;
  try {
    entries = await readdir(root, { withFileTypes: true });
  } catch {
    return result;
  }
  for (const entry of entries) {
    const relative = prefix ? `${prefix}/${entry.name}` : entry.name;
    if (entry.isDirectory()) {
      result.push(...(await walkFiles(path.join(root, entry.name), relative)));
    } else if (entry.isFile()) {
      result.push(relative);
    }
  }
  return result;
};

/** 统计目录占用字节数 */
export const dirSize = async (root: string): Promise<number> => {
  let total = 0;
  let entries;
  try {
    entries = await readdir(root, { withFileTypes: true });
  } catch {
    return 0;
  }
  for (const entry of entries) {
    const full = path.join(root, entry.name);
    if (entry.isDirectory()) total += await dirSize(full);
    else if (entry.isFile()) {
      try {
        total += (await stat(full)).size;
      } catch {
        /* 忽略竞态删除 */
      }
    }
  }
  return total;
};

/** 单文件大小（不存在返回 0） */
export const fileSize = async (target: string): Promise<number> => {
  try {
    return (await stat(target)).size;
  } catch {
    return 0;
  }
};

export const formatBytes = (bytes: number): string => {
  if (bytes < 1024) return `${bytes} B`;
  const units = ['KB', 'MB', 'GB', 'TB'];
  let value = bytes / 1024;
  let index = 0;
  while (value >= 1024 && index < units.length - 1) {
    value /= 1024;
    index += 1;
  }
  return `${value.toFixed(value >= 100 ? 0 : 1)} ${units[index]}`;
};

/** 读取 UTF-8 文本（自动剥离 BOM） */
export const readText = async (target: string): Promise<string> => {
  const content = await readFile(target, 'utf8');
  return content.charCodeAt(0) === 0xfeff ? content.slice(1) : content;
};

/** 写入带 BOM 的 UTF-8 文件：Windows PowerShell 5.1 与 Inno Setup 均依赖 BOM 判定编码 */
export const writeTextWithBom = async (target: string, content: string): Promise<void> => {
  await mkdir(path.dirname(target), { recursive: true });
  await writeFile(target, `\uFEFF${content}`, 'utf8');
};

/** 写入不带 BOM 的 UTF-8 文件 */
export const writeText = async (target: string, content: string): Promise<void> => {
  await mkdir(path.dirname(target), { recursive: true });
  await writeFile(target, content, 'utf8');
};

/** 把 Windows 路径统一成反斜杠，便于写进 .iss 脚本 */
export const toWinPath = (target: string): string => path.resolve(target).replace(/\//g, '\\');

/* ------------------------------------------------------------------ */
/* 进程执行工具                                                        */
/* ------------------------------------------------------------------ */

export interface RunCommandOptions {
  cwd: string;
  logger: PackLogger;
  /** 日志前缀，默认取命令首个 token */
  label?: string;
  env?: NodeJS.ProcessEnv;
  /** 非 0 退出码是否视为失败，默认 true */
  fatal?: boolean;
  /** 实时输出子进程日志（默认跟随 logger.verbose） */
  stream?: boolean;
  /** 失败时额外打印的上下文说明 */
  hint?: string;
}

export interface RunCommandResult {
  code: number;
  durationMs: number;
  output: string;
}

const MAX_BUFFERED_LINES = 4000;

/**
 * 通过 shell 执行命令（Windows 下为 cmd.exe）。
 * - 默认不实时刷屏，仅在失败时打印尾部输出，避免 electron-builder 之类命令淹没日志；
 * - `stream` 为 true 或 `logger.verbose` 时逐行实时输出。
 */
export const runCommand = async (
  command: string,
  options: RunCommandOptions,
): Promise<RunCommandResult> => {
  const { cwd, logger, fatal = true, hint } = options;
  const label = options.label ?? command.trim().split(/\s+/)[0] ?? 'cmd';
  const stream = options.stream ?? logger.verbose;
  const startedAt = Date.now();

  logger.detail(`$ ${command}`);

  return new Promise<RunCommandResult>((resolve, reject) => {
    const child = spawn(command, {
      cwd,
      shell: true,
      windowsHide: true,
      env: { ...process.env, ...options.env },
    });

    const buffered: string[] = [];
    let rest = '';

    const consume = (chunk: Buffer): void => {
      const text = rest + chunk.toString('utf8');
      const lines = text.split(/\r?\n/);
      rest = lines.pop() ?? '';
      for (const line of lines) {
        if (!line.trim()) continue;
        buffered.push(line);
        if (buffered.length > MAX_BUFFERED_LINES) buffered.shift();
        if (stream) logger.processLine(label, line);
      }
    };

    child.stdout?.on('data', consume);
    child.stderr?.on('data', consume);

    child.on('error', (error) => {
      reject(new Error(`执行命令失败：${command}\n${error.message}`));
    });

    child.on('close', (code) => {
      if (rest.trim()) {
        buffered.push(rest);
        if (stream) logger.processLine(label, rest);
      }
      const durationMs = Date.now() - startedAt;
      const exitCode = code ?? -1;

      if (exitCode !== 0 && fatal) {
        logger.error(`命令退出码 ${exitCode}：${command}`);
        if (hint) logger.error(hint);
        const tail = buffered.slice(-40);
        if (tail.length > 0 && !stream) {
          logger.error('----- 最后 40 行输出 -----');
          for (const line of tail) logger.processLine(label, line);
          logger.error('----- 输出结束 -----');
        }
        reject(new Error(`命令执行失败（退出码 ${exitCode}）：${command}`));
        return;
      }

      resolve({ code: exitCode, durationMs, output: buffered.join('\n') });
    });
  });
};

/** 执行命令并返回标准输出（失败不抛错，返回 null） */
export const captureCommand = async (command: string, cwd: string): Promise<string | null> => {
  return new Promise((resolve) => {
    const child = spawn(command, { cwd, shell: true, windowsHide: true });
    let output = '';
    child.stdout?.on('data', (chunk: Buffer) => {
      output += chunk.toString('utf8');
    });
    child.stderr?.on('data', (chunk: Buffer) => {
      output += chunk.toString('utf8');
    });
    child.on('error', () => resolve(null));
    child.on('close', (code) => resolve(code === 0 ? output.trim() : null));
  });
};

/* ------------------------------------------------------------------ */
/* 其它工具                                                            */
/* ------------------------------------------------------------------ */

/** 简单的通配符匹配（支持 `*` 与 `?`） */
export const matchGlob = (pattern: string, value: string): boolean => {
  const escaped = pattern.replace(/[.+^${}()|[\]\\]/g, '\\$&').replace(/\*/g, '.*').replace(/\?/g, '.');
  return new RegExp(`^${escaped}$`, 'i').test(value);
};

/** 全局唯一标识，用于注册表 / Inno Setup 的 AppId 命名空间 */
export const slugify = (value: string): string =>
  value
    .trim()
    .toLowerCase()
    .replace(/[^a-z0-9._-]+/g, '-')
    .replace(/^-+|-+$/g, '') || 'app';

/**
 * 生成纯 ASCII 的运行期标识，用于让守护进程与停止脚本互相识别。
 *
 * 之所以不用应用名本身：`.vbs` 由 WScript 按 ANSI 解析（不支持 UTF-8 BOM），
 * 中文应用名会在启动器里变成乱码，导致停止脚本匹配不到守护进程。
 */
export const buildRuntimeKey = (appName: string): string => {
  const hash = createHash('sha1').update(appName).digest('hex').slice(0, 10);
  return `koi-${hash}`;
};

export const nowStamp = (): string => {
  const now = new Date();
  const pad = (value: number): string => String(value).padStart(2, '0');
  return `${now.getFullYear()}-${pad(now.getMonth() + 1)}-${pad(now.getDate())} ${pad(now.getHours())}:${pad(now.getMinutes())}:${pad(now.getSeconds())}`;
};

export const fileStamp = (): string => {
  const now = new Date();
  const pad = (value: number): string => String(value).padStart(2, '0');
  return `${now.getFullYear()}${pad(now.getMonth() + 1)}${pad(now.getDate())}-${pad(now.getHours())}${pad(now.getMinutes())}${pad(now.getSeconds())}`;
};

/** 判断错误是否为「用户取消/中断」类错误 */
export const toErrorMessage = (error: unknown): string =>
  error instanceof Error ? error.message : String(error);

import type { PackLogger, StepReporter } from './types';

const PREFIX = '[win-installer]';

const ANSI = {
  reset: '\u001b[0m',
  bold: '\u001b[1m',
  dim: '\u001b[2m',
  red: '\u001b[31m',
  green: '\u001b[32m',
  yellow: '\u001b[33m',
  blue: '\u001b[34m',
  magenta: '\u001b[35m',
  cyan: '\u001b[36m',
  gray: '\u001b[90m',
} as const;

type ColorName = keyof typeof ANSI;

const useColor = (): boolean => {
  if (process.env['NO_COLOR']) return false;
  if (process.env['FORCE_COLOR']) return true;
  return Boolean(process.stdout.isTTY);
};

const paint = (text: string, color: ColorName): string =>
  useColor() ? `${ANSI[color]}${text}${ANSI.reset}` : text;

/** 毫秒 -> 人类可读耗时 */
export const formatDuration = (ms: number): string => {
  if (ms < 1000) return `${ms}ms`;
  const totalSeconds = ms / 1000;
  if (totalSeconds < 60) return `${totalSeconds.toFixed(1)}s`;
  const minutes = Math.floor(totalSeconds / 60);
  const seconds = Math.round(totalSeconds - minutes * 60);
  return `${minutes}m${seconds}s`;
};

/** 换行 / 制表等控制字符会破坏日志版式，统一替换为可见占位 */
const sanitize = (line: string): string => line.replace(/\r/g, '').replace(/\t/g, '    ');

export const createLogger = (verbose = false): PackLogger => {
  const write = (line: string): void => {
    process.stdout.write(`${line}\n`);
  };

  const stamp = (): string => paint(PREFIX, 'gray');

  const logger: PackLogger = {
    verbose,

    title(text) {
      const bar = '='.repeat(Math.max(text.length + 8, 56));
      write('');
      write(paint(bar, 'blue'));
      write(paint(`    ${text}`, 'bold'));
      write(paint(bar, 'blue'));
    },

    info(message) {
      write(`${stamp()} ${message}`);
    },

    detail(message) {
      write(`${stamp()}   ${paint(message, 'gray')}`);
    },

    warn(message) {
      write(`${stamp()} ${paint('WARN ', 'yellow')} ${message}`);
    },

    error(message) {
      write(`${stamp()} ${paint('ERROR', 'red')} ${message}`);
    },

    success(message) {
      write(`${stamp()} ${paint('OK   ', 'green')} ${message}`);
    },

    step(index, total, title) {
      const startedAt = Date.now();
      const label = `[${String(index).padStart(2, ' ')}/${String(total).padStart(2, ' ')}]`;
      write('');
      write(`${stamp()} ${paint(label, 'cyan')} ${paint(title, 'bold')}`);

      let finished = false;
      const finish = (mark: string, color: ColorName, extra?: string): void => {
        if (finished) return;
        finished = true;
        const cost = paint(formatDuration(Date.now() - startedAt), 'gray');
        const suffix = extra ? ` ${paint(extra, 'gray')}` : '';
        write(`${stamp()} ${paint(mark, color)} ${title} ${cost}${suffix}`);
      };

      const reporter: StepReporter = {
        info(message) {
          write(`${stamp()}      ${message}`);
        },
        detail(message) {
          write(`${stamp()}      ${paint(message, 'gray')}`);
        },
        done(extra) {
          finish('OK  ', 'green', extra);
        },
        skip(reason) {
          finish('SKIP', 'yellow', reason);
        },
        fail(message) {
          finish('FAIL', 'red', message);
        },
      };
      return reporter;
    },

    processLine(prefix, line) {
      write(`${stamp()} ${paint(`${prefix} |`, 'gray')} ${paint(sanitize(line), 'gray')}`);
    },

    raw(line) {
      write(line);
    },
  };

  return logger;
};

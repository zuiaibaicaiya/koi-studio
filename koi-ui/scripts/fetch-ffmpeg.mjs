// 构建前把当前平台/架构的 ffmpeg 静态二进制复制到 koi-server/bin/，
// electron-builder 会将其作为 extraResources 打进客户端安装包
// （macOS: Contents/Resources/koi-server-bin/ffmpeg）。
//
// ffmpeg 不进版本控制：本脚本按需动态下载（ffmpeg-static 发布的平台静态构建），
// koi-server/bin/ 已加入 .gitignore。失败时仅告警并退出 0：
// koi-server/bin 目录不存在时 electron-builder 的 extraResources 会自动跳过，
// 客户端将以无转码模式运行（服务端可改用系统 PATH 中的 ffmpeg）。
import { chmodSync, copyFileSync, existsSync, mkdirSync } from 'fs';
import { createRequire } from 'module';
import { execSync } from 'child_process';
import { join } from 'path';

const require = createRequire(import.meta.url);

function resolveFFmpegStatic() {
  // ffmpeg-static 默认导出当前平台/架构的静态 ffmpeg 二进制绝对路径
  return require('ffmpeg-static');
}

let ffmpegPath;
try {
  ffmpegPath = resolveFFmpegStatic();
} catch {
  // 未安装时动态下载（--no-save 不污染 package.json 与 lockfile）
  console.log('[fetch-ffmpeg] 正在下载 ffmpeg-static（约 70-80MB，仅首次）...');
  try {
    execSync('npm install --no-save --no-audit --no-fund ffmpeg-static@^5', {
      cwd: join(import.meta.dirname, '..'),
      stdio: 'inherit',
    });
    ffmpegPath = resolveFFmpegStatic();
  } catch (err) {
    console.warn('[fetch-ffmpeg] ffmpeg 下载失败，跳过捆绑:', err.message);
    process.exit(0);
  }
}

if (!ffmpegPath || !existsSync(ffmpegPath)) {
  console.warn('[fetch-ffmpeg] ffmpeg 二进制不存在，跳过捆绑（客户端将以无转码模式运行）');
  process.exit(0);
}

const destDir = join(import.meta.dirname, '..', '..', 'koi-server', 'bin');
mkdirSync(destDir, { recursive: true });

const destName = process.platform === 'win32' ? 'ffmpeg.exe' : 'ffmpeg';
const dest = join(destDir, destName);

copyFileSync(ffmpegPath, dest);
if (process.platform !== 'win32') {
  chmodSync(dest, 0o755);
}

console.log(`[fetch-ffmpeg] ffmpeg 已复制到 ${dest}`);

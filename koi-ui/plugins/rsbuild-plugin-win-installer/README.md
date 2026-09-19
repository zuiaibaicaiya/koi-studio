# rsbuild-plugin-win-installer

一键把 **前端（Rsbuild + Electron 桌面端）** 与 **Go 后端** 打包成 **Windows 安装程序（Inno Setup）** 的 Rsbuild 插件。

生成的安装包具备：

- 后端 **崩溃自动重启**（守护进程 watchdog，延迟 / 熔断 / 停止退出码可配）
- 后端守护进程与桌面客户端 **开机自启动**（注册表 Run 项，user / machine 可选）
- 安装、覆盖安装、卸载时 **自动结束后端进程**，避免文件占用导致安装失败
- 后端只读资源（`models` / `resources` / `.env`）落到 **用户可写数据目录**，安装到 Program Files 也能正常运行

## 快速开始

```bash
# 前置：Go 工具链、Inno Setup 6（含 Languages\ChineseSimplified.isl 可选）
npm run package:win          # 等价于 set KOI_PACKAGE=1 && rsbuild build
```

产物位于 `build-installer/`：

| 文件 | 说明 |
| --- | --- |
| `koi-studio-setup-<version>.exe` | 最终安装包 |
| `installer.iss` | 本次实际编译的 Inno Setup 脚本（由模板复制，可直接改后重编译） |
| `installer.env.json` | 本次注入的全部 `KOI_*` 环境变量快照，便于复现 |
| `electron-builder.win.mjs` | 由插件生成的 electron-builder 配置（继承项目配置并覆盖输出目录） |
| `electron/win-unpacked/` | 桌面客户端打包中间产物 |
| `staging/` | 安装内容暂存：`app/`、`server/`、`scripts/` |

也可在脚本中直接调用（不经过 `rsbuild build`）：

```ts
import { packageWindowsInstaller } from './plugins/rsbuild-plugin-win-installer';

await packageWindowsInstaller({ appName: 'koi-studio' });
```

## 安装后的目录结构

```
<安装目录>                       默认 %LOCALAPPDATA%\Programs\koi-studio
├── koi-ui.exe                   桌面客户端
├── server\                      后端（koi-server.exe + 3 个 DLL + models + resources + .env）
├── scripts\
│   ├── runtime.json             运行期配置（重启策略、数据目录、同步目录）
│   ├── watchdog.ps1             守护进程：启动后端 + 崩溃自动重启 + 日志归档
│   ├── bootstrap-runtime.ps1    把只读资源同步到数据目录（安装与每次启动都会执行）
│   ├── stop-server.ps1          停止守护进程与后端（安装/卸载/开始菜单调用）
│   ├── launch-hidden.vbs        隐藏窗口启动守护进程（注册表自启动项）
│   └── stop-hidden.vbs          隐藏窗口停止后端（开始菜单快捷方式）
└── README.txt                   使用说明（含日志与自启动位置）

%USERPROFILE%\.koi-studio        可写数据目录
├── .env                         端口已与前端 PUBLIC_API_BASE 对齐
├── models\ / resources\         从安装目录同步而来（copy 或 junction）
├── database.sqlite / storage\   运行时生成
└── logs\
    ├── watchdog.log             守护进程日志（每次启动/重启都有记录）
    ├── run-<时间>-out.log       每次后端运行的 stdout 归档（保留最近 20 份）
    └── run-<时间>-err.log
```

## 配置项

```ts
pluginWinInstaller({
  enabled: process.env.KOI_PACKAGE === '1', // 默认即此判断，可省略
  appName: 'koi-studio',
  version: '1.0.0',            // 默认读 package.json
  publisher: 'koi-studio',
  description: '...',
  projectRoot, serverRoot,     // 默认 process.cwd() 与 <projectRoot>/../koi-server
  outputDir: 'build-installer',
  installerFileName: 'koi-studio-setup-1.0.0',
  verbose: false,               // true 时实时打印子进程输出
  steps: { frontend: true, backend: true, electron: true, installer: true },
  commands: { frontend, backend, electron, iscc },

  appAutoStart: { enabled: true, scope: 'user', delaySeconds: 0 },

  backend: {
    enabled: true,
    executable: 'koi-server.exe',
    include: ['koi-server.exe', 'onnxruntime.dll', 'sherpa-onnx-c-api.dll',
              'sherpa-onnx-cxx-api.dll', '.env', 'resources', 'LICENSE'],
    models: ['models'],
    env: { APP_ENV: 'production' },     // 会写入安装包与数据目录的 .env
    dataDir: '%USERPROFILE%\\.koi-studio',
    dataStrategy: 'copy',               // copy | junction
    overwriteEnvOnUpgrade: false,
    restart: {
      enabled: true,
      delayMs: 3000,
      maxRestarts: 0,                   // 0 = 不熔断
      windowMs: 60000,
      stopOnExitCodes: [0],             // 0 视为正常退出，不再重启
      logMaxBytes: 5 * 1024 * 1024,
    },
    autoStart: { enabled: true, scope: 'user', delaySeconds: 0 },
  },

  installer: {
    templateFile: '...',                // 自定义 .iss 模板
    installDir: '{autopf}\\koi-studio',
    requireAdmin: false,                // true = Program Files + UAC
    allowPrivilegeOverride: true,
    architecture: 'x64compatible',       // 需 Inno Setup 6.3+
    compression: 'lzma2/max',
    languages: ['chinesesimplified', 'english'],
    icon: 'build/icon.ico',
    licenseFile: 'LICENSE',
    launchAfterInstall: true,
    desktopShortcut: true,
    promptDeleteUserData: true,
    noCompressionGlobs: ['*.onnx'],     // 大体积二进制跳过压缩，显著缩短编译时间
  },
});
```

端口对齐：插件会读取 `.env.production` 的 `PUBLIC_API_BASE` 推导 `APP_HOST/APP_PORT/APP_URL`，
再补上 `GRPC_HOST/GRPC_PORT` 写入安装包内的 `.env`，无需手工同步前后端端口。

## Inno Setup 脚本与环境变量

`installer/koi-installer.iss` 是**普通的、可直接编译的 Inno Setup 脚本**，
所有可变内容通过环境变量传入，未设置时回退脚本内默认值 —— 因此可以直接用
Inno Setup IDE 打开、改几行、按 F9 调试。

```powershell
# 手工调试：以 build-installer 为工作目录
$env:KOI_STAGING_DIR = "$PWD\staging"
$env:KOI_APP_NAME    = 'koi-studio'
$env:KOI_APP_EXE     = 'koi-ui.exe'
& "${env:ProgramFiles(x86)}\Inno Setup 6\ISCC.exe" installer.iss
```

复现插件最近一次编译（读取变量快照）：

```powershell
Get-Content installer.env.json -Raw | ConvertFrom-Json |
  ForEach-Object { $_.variables.PSObject.Properties } |
  ForEach-Object { Set-Item "env:$($_.Name)" $_.Value }
& "${env:ProgramFiles(x86)}\Inno Setup 6\ISCC.exe" installer.iss
```

常用变量（完整清单见 `installer.env.json`）：

| 变量 | 默认值 | 说明 |
| --- | --- | --- |
| `KOI_APP_NAME` / `KOI_APP_ID` / `KOI_APP_VERSION` | `koi-studio` / 按名称生成 / `1.0.0` | 应用标识 |
| `KOI_APP_EXE` | `koi-ui.exe` | 桌面客户端主程序（插件自动探测） |
| `KOI_INSTALL_DIR` | `{autopf}\koi-studio` | 安装目录，`{autopf}` 随安装模式映射 |
| `KOI_PRIVILEGES` | `lowest` | `lowest`（免 UAC）/ `admin` |
| `KOI_STAGING_DIR` | `staging` | 安装内容暂存目录，相对 `.iss` 所在目录 |
| `KOI_OUTPUT_DIR` / `KOI_OUTPUT_BASENAME` | `.` / `koi-studio-setup-<版本>` | 输出位置与文件名 |
| `KOI_SERVER_DIR` / `KOI_SCRIPT_DIR` / `KOI_SERVER_EXE` | `server` / `scripts` / `koi-server.exe` | 安装内布局 |
| `KOI_DATA_DIR` | `{%USERPROFILE}\.koi-studio` | 可写数据目录，支持 `{%ENVVAR}` |
| `KOI_AUTOSTART_SERVER` / `KOI_AUTOSTART_APP` | `1` | `0` 表示该自启动项默认不勾选 |
| `KOI_SERVER_SCOPE` / `KOI_APP_SCOPE` | `HKCU` | 自启动注册表根，可选 `HKLM` |
| `KOI_DESKTOP_SHORTCUT` / `KOI_LAUNCH_AFTER` / `KOI_PROMPT_DELETE_DATA` | `1` | 交互开关 |
| `KOI_LANG1_NAME` / `KOI_LANG1_FILE` … `3` | `english` / `compiler:Default.isl` | 最多 3 种语言 |
| `KOI_NC_APP_1/2`、`KOI_NC_APP_EXCLUDES` | 空 | 客户端免压缩通配符与对应 Excludes |
| `KOI_NC_SERVER_1/2`、`KOI_NC_SERVER_EXCLUDES` | 空 | 后端免压缩通配符（默认 `*.onnx`） |

> 免压缩条目必须与实际匹配到的文件成对出现（Inno 对「无匹配文件」会直接报错），
> 插件已按 staging 实际内容计算，手工调试时请一并设置。

## 常见问题

- **报错 `Unknown constant`**：环境变量里写了未转义的 `{`，路径请写成 `{%USERPROFILE}` 形式。
- **报错 `No files found matching`**：设置了 `KOI_NC_*` 但 staging 中没有匹配文件，清空该变量即可。
- **ISCC 找不到**：安装 Inno Setup 6，或用 `commands.iscc` 指定 `ISCC.exe` 路径。
- **electron-builder 首次失败**：首次运行需联网下载 winCodeSign 等辅助工具，重试即可。
- **安装后打开应用连不上后端**：查看 `%USERPROFILE%\.koi-studio\logs\watchdog.log`，
  确认 `.env` 里的 `APP_PORT` 与前端编译时的 `PUBLIC_API_BASE` 一致；改完 `.env` 后重跑一次
  `scripts\launch-hidden.vbs` 或重启守护进程。
- **想改重启策略**：编辑 `<安装目录>\scripts\runtime.json` 的 `restart` 段，然后重新运行 `launch-hidden.vbs`。

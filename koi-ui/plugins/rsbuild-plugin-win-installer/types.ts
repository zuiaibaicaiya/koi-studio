/**
 * rsbuild-plugin-win-installer 配置类型
 *
 * 所有字段均为可选，未提供时使用 `options.ts` 中的默认值。
 */

/** 后端崩溃重启策略 */
export interface BackendRestartOptions {
  /** 是否启用崩溃自动重启，默认 `true` */
  enabled?: boolean;
  /** 进程退出后等待多久再拉起（毫秒），默认 `3000` */
  delayMs?: number;
  /** 熔断阈值：连续重启次数达到该值时停止守护；`0` 表示永不熔断，默认 `0` */
  maxRestarts?: number;
  /**
   * 熔断统计窗口（毫秒）。进程存活时长超过该值即视为「稳定运行」，
   * 重启计数清零，默认 `60000`。
   */
  windowMs?: number;
  /** 命中这些退出码时不再重启（`0` 通常代表用户主动退出），默认 `[0]` */
  stopOnExitCodes?: number[];
  /** 单个日志文件最大字节数，超出后轮转为 `.1`，默认 `5MB` */
  logMaxBytes?: number;
}

/** 开机自启动配置 */
export interface AutoStartOptions {
  /** 是否写入开机自启动项，默认 `true` */
  enabled?: boolean;
  /** 注册表位置：`user` 写 HKCU（当前用户登录时启动），`machine` 写 HKLM（所有用户），默认 `user` */
  scope?: 'user' | 'machine';
  /** 延迟启动秒数，避免开机瞬间抢占资源，默认 `0` */
  delaySeconds?: number;
}

/** 后端（Go 服务）相关配置 */
export interface BackendOptions {
  /** 是否随包分发并注册后端，默认 `true` */
  enabled?: boolean;
  /** 后端可执行文件名，默认 `<serverRoot>/koi-server.exe` 的实际文件名 */
  executable?: string;
  /** 除可执行文件外随包分发的文件/目录（相对 serverRoot），默认见 options.ts */
  include?: string[];
  /** 模型目录（相对 serverRoot），默认 `['models']` */
  models?: string[];
  /**
   * 安装时写入 .env 的键值对。未显式提供的键会按前端 `PUBLIC_API_BASE`
   * 自动推导端口，保证前后端端口一致。
   */
  env?: Record<string, string | number>;
  /**
   * 运行时数据目录（存放 .env、models、resources、database.sqlite、storage、logs），
   * 支持 `%USERPROFILE%` 等环境变量，默认 `%USERPROFILE%\.<appName>`。
   */
  dataDir?: string;
  /**
   * 资源落地方式：
   * - `copy`（默认）：复制到数据目录，占用一份额外磁盘，升级时增量同步
   * - `junction`：用目录联接指向安装目录，零拷贝（卸载时自动清理联接）
   */
  dataStrategy?: 'copy' | 'junction';
  /** 升级安装时是否用安装包内的 .env 覆盖数据目录里已被用户修改的 .env，默认 `false` */
  overwriteEnvOnUpgrade?: boolean;
  /** 崩溃重启策略 */
  restart?: BackendRestartOptions;
  /** 后端守护进程开机自启动 */
  autoStart?: AutoStartOptions;
}

/** 安装包 / 安装程序外观类配置 */
export interface InstallerAppearanceOptions {
  /**
   * Inno Setup 脚本模板路径，默认取插件自带的
   * `plugins/rsbuild-plugin-win-installer/installer/koi-installer.iss`。
   * 复制一份改路径即可完全自定义安装行为（无需改插件代码）。
   */
  templateFile?: string;
  /** 安装目录，默认 `{autopf}\<appName>`（按用户/管理员模式自动映射到 Program Files 或 %LOCALAPPDATA%\Programs） */
  installDir?: string;
  /** 是否以管理员权限安装，默认 `false`（按用户安装到 %LOCALAPPDATA%\Programs，无需 UAC） */
  requireAdmin?: boolean;
  /** 是否允许用户在安装时切换安装模式，默认 `true` */
  allowPrivilegeOverride?: boolean;
  /** 目标架构指令，默认 `x64compatible`（Inno Setup 6.3+） */
  architecture?: string;
  /** 压缩方式，默认 `lzma2/max` */
  compression?: string;
  /** 安装向导语言列表（按顺序），默认自动探测：有 ChineseSimplified.isl 时为 `['chinesesimplified', 'english']` */
  languages?: string[];
  /** 安装程序图标（`.ico` 文件路径）；未提供时沿用 Inno 默认图标 */
  icon?: string;
  /** 许可协议文件路径（`.txt` / `.rtf`） */
  licenseFile?: string;
  /** 安装完成后是否勾选「立即启动」复选框，默认 `true` */
  launchAfterInstall?: boolean;
  /** 是否在开始菜单与桌面创建快捷方式（桌面项默认不勾选） */
  desktopShortcut?: boolean;
  /** 卸载时是否询问删除用户数据目录，默认 `true` */
  promptDeleteUserData?: boolean;
  /**
   * 不参与压缩的文件通配符（大型二进制重复压缩收益极低，跳过可显著缩短编译时间），
   * 默认 `['*.onnx']`。
   */
  noCompressionGlobs?: string[];
}

/** 构建步骤开关与命令覆盖 */
export interface PipelineStepsOptions {
  /** 是否执行前端构建命令（仅在插件之外独立运行打包时需要；由 `rsbuild build` 触发时自动复用当前产物） */
  frontend?: boolean;
  /** 是否执行后端构建命令 */
  backend?: boolean;
  /** 是否执行桌面端（electron-builder）打包 */
  electron?: boolean;
  /** 是否调用 Inno Setup 编译器生成安装包 */
  installer?: boolean;
}

export interface PipelineCommandsOptions {
  /** 前端构建命令，默认 `npx rsbuild build` */
  frontend?: string;
  /** 后端构建命令，默认 `go build -ldflags "-s -w" -o <exe> .` */
  backend?: string;
  /** 桌面端打包命令，默认 `npx electron-builder --win --dir --config <生成的配置>` */
  electron?: string;
  /** Inno Setup 编译器路径，默认自动探测 */
  iscc?: string;
}

/** 插件配置项 */
export interface WinInstallerOptions {
  /** 是否启用打包流程，默认取环境变量 `KOI_PACKAGE=1` */
  enabled?: boolean;
  /** 应用名称，用于安装向导、快捷方式、注册表项，默认 `koi-studio` */
  appName?: string;
  /** Inno Setup 的 AppId，默认按 appName 生成稳定 GUID */
  appId?: string;
  /** 版本号，默认读取项目 `package.json#version` */
  version?: string;
  /** 发布者，显示在「程序和功能」中，默认 `<appName> Team` */
  publisher?: string;
  /** 应用描述 */
  description?: string;
  /** 前端项目根目录（存在 rsbuild.config.ts 的目录），默认 `process.cwd()` */
  projectRoot?: string;
  /** 后端项目根目录，默认 `<projectRoot>/../koi-server` */
  serverRoot?: string;
  /** 打包输出目录，默认 `<projectRoot>/build-installer` */
  outputDir?: string;
  /** 安装包文件名（不含扩展名），默认 `<appName>-setup-<version>` */
  installerFileName?: string;
  /** 前端产物目录（相对 projectRoot），默认 `dist` */
  frontendDist?: string;
  /** electron-builder 输出目录（相对 projectRoot），默认 `build-installer/electron` */
  electronOutputDir?: string;
  /** electron-builder 配置文件路径，默认 `<projectRoot>/electron-builder.config.mjs` */
  electronConfigFile?: string;
  /** 覆盖 electron-builder 的 productName（决定桌面端 exe 名称），默认沿用项目配置 */
  electronProductName?: string;
  /** 是否保留中间产物（staging），默认 `true` */
  keepStaging?: boolean;
  /** 输出详细日志（包含子进程实时输出），默认 `false` */
  verbose?: boolean;
  /** 应用（桌面客户端）开机自启动 */
  appAutoStart?: AutoStartOptions;
  /** 安装程序外观配置 */
  installer?: InstallerAppearanceOptions;
  /** 后端配置 */
  backend?: BackendOptions;
  /** 步骤开关 */
  steps?: PipelineStepsOptions;
  /** 命令覆盖 */
  commands?: PipelineCommandsOptions;
}

/** 语言定义 */
export interface LanguageSpec {
  name: string;
  /** Inno Setup 的 MessagesFile 值 */
  messagesFile: string;
}

/** staging 中一个子目录的落地方案 */
export interface StagingRoot {
  /** 相对 staging 的目录名，如 `app` */
  name: string;
  /** 安装到 `{app}` 下的子目录，空字符串表示安装根目录 */
  destSubDir: string;
  /** 该目录内已确认命中的「不压缩」通配符 */
  noCompression: string[];
}

/** 归一化后的自启动配置 */
export type ResolvedAutoStart = Required<AutoStartOptions>;

/** 归一化后的重启配置 */
export type ResolvedRestart = Required<BackendRestartOptions>;

/** 归一化后的后端配置 */
export interface ResolvedBackendOptions {
  enabled: boolean;
  /** 安装包内后端目录名（相对安装根目录） */
  dirName: string;
  /** 脚本目录名（相对安装根目录） */
  scriptDirName: string;
  /** 可执行文件名 */
  executable: string;
  /** 随包分发的文件/目录（相对 serverRoot） */
  include: string[];
  /** 模型目录（相对 serverRoot） */
  models: string[];
  /** 写入 .env 的键值对 */
  env: Record<string, string>;
  /** 数据目录（可能含环境变量，运行时展开） */
  dataDir: string;
  dataStrategy: 'copy' | 'junction';
  overwriteEnvOnUpgrade: boolean;
  restart: ResolvedRestart;
  autoStart: ResolvedAutoStart;
}

/** 归一化后的完整配置 */
export interface ResolvedWinInstallerOptions {
  enabled: boolean;
  appName: string;
  appId: string;
  version: string;
  publisher: string;
  description: string;
  projectRoot: string;
  serverRoot: string;
  outputDir: string;
  stagingDir: string;
  issFile: string;
  /** Inno Setup 脚本模板路径（会被复制为 issFile） */
  installerTemplate: string;
  /** 注入 Inno 变量的调试快照路径 */
  innoEnvFile: string;
  installerFileName: string;
  installDir: string;
  requireAdmin: boolean;
  allowPrivilegeOverride: boolean;
  architecture: string;
  compression: string;
  languages: LanguageSpec[];
  icon: string | null;
  licenseFile: string | null;
  launchAfterInstall: boolean;
  desktopShortcut: boolean;
  promptDeleteUserData: boolean;
  noCompressionGlobs: string[];
  frontendDist: string;
  electronOutputDir: string;
  electronConfigFile: string | null;
  electronProductName: string | null;
  keepStaging: boolean;
  verbose: boolean;
  steps: Required<PipelineStepsOptions>;
  commands: Required<PipelineCommandsOptions>;
  backend: ResolvedBackendOptions;
  appAutoStart: ResolvedAutoStart;
}

/** 打包流水线运行上下文 */
export interface PipelineContext {
  options: ResolvedWinInstallerOptions;
  logger: PackLogger;
  /** 由 rsbuild 触发时为 true，此时跳过前端构建命令，直接复用 dist */
  builtByRsbuild: boolean;
  /** 探测到的桌面端主程序文件名（如 koi-ui.exe），在 electron 步骤后填充 */
  appExecutable: string;
  /** Inno Setup 安装根目录 */
  innoSetupDir: string;
  /** Inno Setup 编译器路径 */
  isccPath: string;
  /** 本次运行的时间戳，用于产物命名 */
  startedAt: Date;
}

/** 打包日志器 */
export interface PackLogger {
  readonly verbose: boolean;
  /** 输出标题横幅 */
  title(text: string): void;
  /** 输出一条普通信息 */
  info(message: string): void;
  /** 输出缩进细节信息 */
  detail(message: string): void;
  /** 输出警告 */
  warn(message: string): void;
  /** 输出错误 */
  error(message: string): void;
  /** 输出成功信息 */
  success(message: string): void;
  /** 开始一个步骤，返回结束回调（自动打印耗时与结果） */
  step(index: number, total: number, title: string): StepReporter;
  /** 子进程输出行 */
  processLine(prefix: string, line: string): void;
  /** 直接输出一行（不加前缀修饰） */
  raw(line: string): void;
}

/** 步骤上报器 */
export interface StepReporter {
  /** 输出该步骤内的信息 */
  info(message: string): void;
  /** 输出该步骤内的细节 */
  detail(message: string): void;
  /** 该步骤执行成功 */
  done(extra?: string): void;
  /** 该步骤被跳过 */
  skip(reason: string): void;
  /** 该步骤失败（打印错误后由调用方抛出） */
  fail(message: string): void;
}

import type { ResolvedWinInstallerOptions } from '../types';

/** VBScript 字符串字面量转义 */
const vbsString = (value: string): string => value.replace(/"/g, '""');

/** PowerShell 单引号字符串转义 */
const psSingleQuote = (value: string): string => value.replace(/'/g, "''");

/**
 * 落地到 `<安装目录>\scripts\runtime.json` 的运行时配置。
 * 守护进程 / 资源同步 / 停止脚本都从这里读取参数，便于安装后直接调整策略。
 */
export interface RuntimeConfig {
  appName: string;
  version: string;
  serverDir: string;
  serverExe: string;
  dataDir: string;
  dataStrategy: 'copy' | 'junction';
  overwriteEnvOnUpgrade: boolean;
  envFile: string;
  envOverrides: Record<string, string>;
  syncDirs: string[];
  restart: {
    enabled: boolean;
    delayMs: number;
    maxRestarts: number;
    windowMs: number;
    stopOnExitCodes: number[];
    logMaxBytes: number;
  };
  autoStart: {
    delaySeconds: number;
    launcher: string;
  };
}

export const buildRuntimeConfig = (
  options: ResolvedWinInstallerOptions,
  syncDirs: string[],
): RuntimeConfig => ({
  appName: options.appName,
  version: options.version,
  serverDir: options.backend.dirName,
  serverExe: options.backend.executable,
  dataDir: options.backend.dataDir,
  dataStrategy: options.backend.dataStrategy,
  overwriteEnvOnUpgrade: options.backend.overwriteEnvOnUpgrade,
  envFile: '.env',
  envOverrides: options.backend.env,
  syncDirs,
  restart: {
    enabled: options.backend.restart.enabled,
    delayMs: options.backend.restart.delayMs,
    maxRestarts: options.backend.restart.maxRestarts,
    windowMs: options.backend.restart.windowMs,
    stopOnExitCodes: [...options.backend.restart.stopOnExitCodes],
    logMaxBytes: options.backend.restart.logMaxBytes,
  },
  autoStart: {
    delaySeconds: options.backend.autoStart.delaySeconds,
    launcher: 'launch-hidden.vbs',
  },
});

/** 运行时配置文件内容 */
export const renderRuntimeConfigFile = (config: RuntimeConfig): string =>
  `${JSON.stringify(config, null, 2)}\n`;

/**
 * 资源同步脚本：把只读的 .env / models / resources 落到用户可写的数据目录。
 * 安装程序与守护进程都会调用它，必须保证幂等。
 */
export const renderBootstrapScript = (): string => `#Requires -Version 5.1
<#
  bootstrap-runtime.ps1
  由 rsbuild-plugin-win-installer 自动生成，请勿手工修改。

  作用：把安装目录（可能位于 Program Files，普通用户不可写）中的
        .env / models / resources 同步到用户可写的数据目录，
        使后端能够在数据目录中写入 database.sqlite、storage 与日志。
  幂等：可被安装程序与守护进程反复调用。
#>
[CmdletBinding()]
param(
    [string]$ConfigPath = '',
    [switch]$Force,
    [switch]$Quiet
)

$ErrorActionPreference = 'Stop'

if ([string]::IsNullOrWhiteSpace($ConfigPath)) {
    $ConfigPath = Join-Path (Split-Path -Parent $MyInvocation.MyCommand.Path) 'runtime.json'
}

function Write-Info([string]$Message) {
    if (-not $Quiet) {
        Write-Host ('[runtime] ' + $Message)
    }
}

function Write-EnvFile([string]$Path, [hashtable]$Overrides) {
    $lines = @(Get-Content -LiteralPath $Path -Encoding UTF8)
    $applied = @{}
    $result = New-Object System.Collections.Generic.List[string]
    foreach ($line in $lines) {
        $trimmed = $line.TrimStart()
        if ($trimmed.StartsWith('#') -or $trimmed.Length -eq 0) {
            $result.Add($line)
            continue
        }
        $matched = [regex]::Match($line, '^\\s*([A-Za-z_][A-Za-z0-9_]*)\\s*=')
        if ($matched.Success -and $Overrides.ContainsKey($matched.Groups[1].Value)) {
            $key = $matched.Groups[1].Value
            $result.Add(($key + '=' + $Overrides[$key]))
            $applied[$key] = $true
        } else {
            $result.Add($line)
        }
    }
    foreach ($key in $Overrides.Keys) {
        if (-not $applied.ContainsKey($key)) {
            $result.Add(($key + '=' + $Overrides[$key]))
        }
    }
    # 必须写入无 BOM 的 UTF-8：带 BOM 会让首行键名解析失败
    [System.IO.File]::WriteAllLines($Path, $result, (New-Object System.Text.UTF8Encoding($false)))
}

function Invoke-Robocopy([string]$Source, [string]$Target) {
    if (-not (Test-Path -LiteralPath $Source)) { return }
    if (Test-Path -LiteralPath $Target) {
        $item = Get-Item -LiteralPath $Target -Force
        if ($item.LinkType -eq 'Junction' -or $item.LinkType -eq 'SymbolicLink') {
            Write-Info ('  ' + $Target + ' 已是目录联接，跳过复制')
            return
        }
    }
    Write-Info ('  复制 ' + $Source + ' -> ' + $Target)
    & robocopy $Source $Target /E /XO /R:1 /W:1 /NFL /NDL /NJH /NJS /NP | Out-Null
    # robocopy 的 0~7 均为成功状态码
    if ($LASTEXITCODE -ge 8) {
        throw ('robocopy 复制失败：' + $Source + ' -> ' + $Target + '（退出码 ' + $LASTEXITCODE + '）')
    }
}

function Invoke-Junction([string]$Source, [string]$Target) {
    $parent = Split-Path -Parent $Target
    if (-not (Test-Path -LiteralPath $parent)) {
        New-Item -ItemType Directory -Force -Path $parent | Out-Null
    }
    & cmd.exe /c mklink /J $Target $Source | Out-Null
    return ($LASTEXITCODE -eq 0)
}

if (-not (Test-Path -LiteralPath $ConfigPath)) {
    throw ('找不到运行时配置：' + $ConfigPath)
}

$cfg = Get-Content -LiteralPath $ConfigPath -Raw -Encoding UTF8 | ConvertFrom-Json
$scriptDir = Split-Path -Parent $ConfigPath
$appDir = Split-Path -Parent $scriptDir
$serverDir = Join-Path $appDir $cfg.serverDir
$dataDir = [Environment]::ExpandEnvironmentVariables($cfg.dataDir)
$markerPath = Join-Path $dataDir '.runtime-version'

$overrides = @{}
$cfg.envOverrides.PSObject.Properties | ForEach-Object { $overrides[$_.Name] = [string]$_.Value }

Write-Info ('安装目录：' + $appDir)
Write-Info ('数据目录：' + $dataDir)

New-Item -ItemType Directory -Force -Path $dataDir | Out-Null
foreach ($sub in @('logs', 'storage')) {
    New-Item -ItemType Directory -Force -Path (Join-Path $dataDir $sub) | Out-Null
}

# ---------------- 1. .env ----------------
$envSource = Join-Path $serverDir $cfg.envFile
$envTarget = Join-Path $dataDir $cfg.envFile
$installedVersion = ''
if (Test-Path -LiteralPath $markerPath) {
    $installedVersion = (Get-Content -LiteralPath $markerPath -Raw -Encoding UTF8).Trim()
}
$isUpgrade = ($installedVersion.Length -gt 0) -and ($installedVersion -ne $cfg.version)

if ((Test-Path -LiteralPath $envSource) -and -not (Test-Path -LiteralPath $envTarget)) {
    Copy-Item -LiteralPath $envSource -Destination $envTarget -Force
    Write-EnvFile $envTarget $overrides
    Write-Info ('  已生成 ' + $envTarget)
} elseif ($isUpgrade -and $cfg.overwriteEnvOnUpgrade -and (Test-Path -LiteralPath $envSource)) {
    $backup = $envTarget + '.bak'
    if (Test-Path -LiteralPath $envTarget) { Copy-Item -LiteralPath $envTarget -Destination $backup -Force }
    Copy-Item -LiteralPath $envSource -Destination $envTarget -Force
    Write-EnvFile $envTarget $overrides
    Write-Info ('  升级覆盖 ' + $envTarget + '（原文件备份为 ' + $backup + '）')
} elseif (Test-Path -LiteralPath $envTarget) {
    Write-Info ('  保留已有 ' + $envTarget + '（overwriteEnvOnUpgrade=false）')
} else {
    Write-Info ('  安装目录缺少 ' + $envSource + '，跳过')
}

# ---------------- 2. models / resources ----------------
foreach ($name in $cfg.syncDirs) {
    $source = Join-Path $serverDir $name
    $target = Join-Path $dataDir $name
    if (-not (Test-Path -LiteralPath $source)) {
        Write-Info ('  安装目录缺少 ' + $source + '，跳过')
        continue
    }
    if ($cfg.dataStrategy -eq 'junction' -and -not (Test-Path -LiteralPath $target)) {
        if (Invoke-Junction $source $target) {
            Write-Info ('  已创建目录联接 ' + $target)
            continue
        }
        Write-Info ('  目录联接创建失败，回退为复制')
    }
    Invoke-Robocopy $source $target
}

# ---------------- 3. 版本标记 ----------------
Set-Content -LiteralPath $markerPath -Value $cfg.version -Encoding ASCII
Write-Info ('  运行环境就绪（版本 ' + $cfg.version + '）')
`;

/** 后端守护进程：崩溃自动重启 + 开机自启动 + 日志轮转 */
export const renderWatchdogScript = (): string => `#Requires -Version 5.1
<#
  watchdog.ps1
  由 rsbuild-plugin-win-installer 自动生成，重启策略请修改同目录 runtime.json。

  职责：
    1. 首次运行时同步 .env / models / resources 到用户数据目录；
    2. 拉起后端进程并监控其退出；
    3. 按 runtime.json 中的策略自动重启（可配置延迟、熔断、停止退出码）；
    4. 每次运行结束把 stdout / stderr 归档为 run-<时间戳>-*.log，避免被下一次启动覆盖。
#>
[CmdletBinding()]
param(
    [string]$ConfigPath = '',
    # 应用标识：仅用于让 stop-server.ps1 精确识别「本应用的守护进程」
    [string]$RuntimeKey = '',
    [switch]$Foreground,
    [switch]$Once
)

$ErrorActionPreference = 'Continue'

$scriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path
if ([string]::IsNullOrWhiteSpace($ConfigPath)) {
    $ConfigPath = Join-Path $scriptDir 'runtime.json'
}
if (-not (Test-Path -LiteralPath $ConfigPath)) {
    Write-Error ('找不到运行时配置：' + $ConfigPath)
    exit 1
}

$cfg = Get-Content -LiteralPath $ConfigPath -Raw -Encoding UTF8 | ConvertFrom-Json
$appDir = Split-Path -Parent $scriptDir
$serverDir = Join-Path $appDir $cfg.serverDir
$exePath = Join-Path $serverDir $cfg.serverExe
$dataDir = [Environment]::ExpandEnvironmentVariables($cfg.dataDir)
$logDir = Join-Path $dataDir 'logs'
New-Item -ItemType Directory -Force -Path $logDir | Out-Null

$watchLog = Join-Path $logDir 'watchdog.log'
$outLog = Join-Path $logDir 'server.out.log'
$errLog = Join-Path $logDir 'server.err.log'

function Write-WatchLog([string]$Level, [string]$Message) {
    $line = '{0} [{1}] {2}' -f (Get-Date -Format 'yyyy-MM-dd HH:mm:ss'), $Level, $Message
    if ($Foreground) { Write-Host $line }
    try {
        Add-Content -LiteralPath $watchLog -Value $line -Encoding UTF8 -ErrorAction SilentlyContinue
    } catch { }
}

function Invoke-LogRotate {
    $max = [long]$cfg.restart.logMaxBytes
    if ($max -le 0) { return }
    try {
        if ((Test-Path -LiteralPath $watchLog) -and (Get-Item -LiteralPath $watchLog -Force).Length -ge $max) {
            $archive = $watchLog + '.1'
            if (Test-Path -LiteralPath $archive) { Remove-Item -LiteralPath $archive -Force -ErrorAction SilentlyContinue }
            Move-Item -LiteralPath $watchLog -Destination $archive -Force
        }
    } catch { }
}

function Invoke-ArchiveRunLogs {
    $stamp = Get-Date -Format 'yyyyMMdd-HHmmss'
    try {
        foreach ($item in @(@{ Path = $outLog; Tag = 'out' }, @{ Path = $errLog; Tag = 'err' })) {
            if (-not (Test-Path -LiteralPath $item.Path)) { continue }
            if ((Get-Item -LiteralPath $item.Path -Force).Length -le 0) { continue }
            $dest = Join-Path $logDir ('run-' + $stamp + '-' + $item.Tag + '.log')
            Move-Item -LiteralPath $item.Path -Destination $dest -Force -ErrorAction SilentlyContinue
        }
        Get-ChildItem -LiteralPath $logDir -Filter 'run-*.log' -ErrorAction SilentlyContinue |
            Sort-Object LastWriteTime -Descending |
            Select-Object -Skip 20 |
            Remove-Item -Force -ErrorAction SilentlyContinue
    } catch { }
}

# cmd 包装启动后端，做到「实时写日志」+「拿到可靠退出码」：
#   1. PowerShell 5.1 的 Start-Process -PassThru 取不到 ExitCode（实测为空），
#      因此这里直接用 .NET Process，并把输出交给 cmd 的 >> / 2>> 重定向；
#   2. 使用追加模式，避免下一次重启把上一次的崩溃输出覆盖掉。
function Start-BackendProcess {
    $comspec = $env:ComSpec
    if ([string]::IsNullOrEmpty($comspec)) { $comspec = 'cmd.exe' }

    $info = New-Object System.Diagnostics.ProcessStartInfo
    $info.FileName = $comspec
    $info.Arguments = '/s /c ""' + $exePath + '" >> "' + $outLog + '" 2>> "' + $errLog + '""'
    $info.WorkingDirectory = $dataDir
    $info.UseShellExecute = $false
    $info.CreateNoWindow = $true

    $process = New-Object System.Diagnostics.Process
    $process.StartInfo = $info
    $null = $process.Start()
    return $process
}

# ---------------- 单实例互斥：避免开机自启动与手动启动叠加出两个守护进程 ----------------
$mutexKey = 'Local\\' + ($cfg.appName -replace '[^A-Za-z0-9._-]', '_') + '.watchdog'
$mutex = New-Object System.Threading.Mutex($false, $mutexKey)
$owned = $false
try {
    $owned = $mutex.WaitOne(0)
} catch [System.Threading.AbandonedMutexException] {
    $owned = $true
}
if (-not $owned) {
    Write-WatchLog 'WARN' '检测到已有守护进程在运行，本次启动退出'
    exit 0
}

$child = $null
try {
    if (-not (Test-Path -LiteralPath $exePath)) {
        Write-WatchLog 'ERROR' ('找不到后端可执行文件：' + $exePath)
        exit 1
    }

    # 延迟启动：开机瞬间先让出磁盘与 CPU
    $delaySeconds = [int]$cfg.autoStart.delaySeconds
    if ($delaySeconds -gt 0 -and -not $Foreground) {
        Write-WatchLog 'INFO' ('延迟 ' + $delaySeconds + ' 秒后启动后端')
        Start-Sleep -Seconds $delaySeconds
    }

    # 资源同步（.env / models / resources -> 数据目录）
    $bootstrap = Join-Path $scriptDir 'bootstrap-runtime.ps1'
    if (Test-Path -LiteralPath $bootstrap) {
        try {
            & $bootstrap -ConfigPath $ConfigPath -Quiet
        } catch {
            Write-WatchLog 'WARN' ('运行环境同步失败：' + $_.Exception.Message)
        }
    }

    $delayMs = [int]$cfg.restart.delayMs
    $maxRestarts = [int]$cfg.restart.maxRestarts
    $windowMs = [int]$cfg.restart.windowMs
    $restartEnabled = [bool]$cfg.restart.enabled
    $stopOnExit = @()
    foreach ($code in @($cfg.restart.stopOnExitCodes)) { $stopOnExit += [int]$code }

    Write-WatchLog 'INFO' ('守护进程已启动，后端可执行文件：' + $exePath)
    Write-WatchLog 'INFO' ('工作目录：' + $dataDir)

    $restarts = 0

    while ($true) {
        Invoke-LogRotate
        Write-WatchLog 'INFO' '正在启动后端进程...'

        $child = $null
        $exitCode = -1
        $startedAt = Get-Date
        try {
            $child = Start-BackendProcess
            Write-WatchLog 'INFO' ('后端进程已启动，PID=' + $child.Id)
            $child.WaitForExit()
            $exitCode = $child.ExitCode
        } catch {
            Write-WatchLog 'ERROR' ('启动后端进程失败：' + $_.Exception.Message)
        }

        $uptime = (Get-Date) - $startedAt
        Write-WatchLog 'WARN' ('后端进程已退出，退出码 ' + $exitCode + '，本次运行 ' + [math]::Round($uptime.TotalSeconds, 1) + ' 秒')
        Invoke-ArchiveRunLogs

        if ($Once) {
            Write-WatchLog 'INFO' '-Once 模式，守护进程退出'
            break
        }
        if (-not $restartEnabled) {
            Write-WatchLog 'INFO' '自动重启已关闭，守护进程退出'
            break
        }
        if ($stopOnExit -contains $exitCode) {
            Write-WatchLog 'INFO' ('退出码 ' + $exitCode + ' 命中停止列表，不再重启')
            break
        }

        if ($maxRestarts -gt 0) {
            if ($uptime.TotalMilliseconds -ge $windowMs) { $restarts = 0 }
            if ($restarts -ge $maxRestarts) {
                Write-WatchLog 'ERROR' ('连续重启已达 ' + $maxRestarts + ' 次（' + $windowMs + ' 毫秒内），触发熔断并停止守护')
                break
            }
        }
        $restarts += 1

        Write-WatchLog 'INFO' ('等待 ' + $delayMs + ' 毫秒后重启（累计第 ' + $restarts + ' 次）')
        Start-Sleep -Milliseconds $delayMs
    }
} finally {
    if ($null -ne $child) {
        try {
            if (-not $child.HasExited) {
                # 结束 cmd 包装层的同时带上子树，避免残留后端进程
                & taskkill.exe /PID $child.Id /T /F | Out-Null
            }
        } catch { }
    }
    try { $mutex.ReleaseMutex() } catch { }
    try { $mutex.Dispose() } catch { }
}
`;

/**
 * 停止脚本：安装升级前与卸载时调用。
 * 先结束守护进程（避免它把后端重新拉起），再结束后端本体。
 */
export const renderStopScript = (runtimeKey: string): string => `#Requires -Version 5.1
<#
  stop-server.ps1
  由 rsbuild-plugin-win-installer 自动生成。

  安装 / 升级前终止正在运行的应用与后端，卸载时额外可选清理用户数据目录。
#>
[CmdletBinding()]
param(
    [string]$ConfigPath = '',
    [switch]$DeleteUserData,
    [switch]$Quiet
)

$ErrorActionPreference = 'SilentlyContinue'

$scriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path
if ([string]::IsNullOrWhiteSpace($ConfigPath)) {
    $ConfigPath = Join-Path $scriptDir 'runtime.json'
}

function Write-Info([string]$Message) {
    if (-not $Quiet) { Write-Host ('[stop] ' + $Message) }
}

$cfg = $null
if (Test-Path -LiteralPath $ConfigPath) {
    try {
        $cfg = Get-Content -LiteralPath $ConfigPath -Raw -Encoding UTF8 | ConvertFrom-Json
    } catch { }
}

# 只删除联接 / 符号链接本身（Directory.Delete 不会递归），防止误删链接目标
function Remove-Link([string]$Path) {
    try {
        $item = Get-Item -LiteralPath $Path -Force -ErrorAction Stop
        if ($item.LinkType -eq 'Junction' -or $item.LinkType -eq 'SymbolicLink') {
            [System.IO.Directory]::Delete($Path, $false)
            return $true
        }
    } catch { }
    return $false
}

function Remove-DataDir([string]$Path) {
    if (-not (Test-Path -LiteralPath $Path)) { return }
    Get-ChildItem -LiteralPath $Path -Directory -Force -ErrorAction SilentlyContinue | ForEach-Object {
        if (-not (Remove-Link $_.FullName)) { Remove-DataDir $_.FullName }
    }
    Remove-Item -LiteralPath $Path -Recurse -Force -ErrorAction SilentlyContinue
}

# ---------------- 1. 结束守护进程 ----------------
# 按「脚本名 + 应用标识」匹配，既能跨安装目录覆盖旧版本，又不会误杀其它应用的守护进程
$runtimeKey = '${psSingleQuote(runtimeKey)}'
$killed = 0
Get-CimInstance Win32_Process -Filter "Name = 'powershell.exe' OR Name = 'pwsh.exe'" -ErrorAction SilentlyContinue |
    Where-Object {
        $_.CommandLine -and
        $_.CommandLine.Contains('watchdog.ps1') -and
        $_.CommandLine.Contains($runtimeKey) -and
        $_.ProcessId -ne $PID
    } |
    ForEach-Object {
        Write-Info ('结束守护进程 PID=' + $_.ProcessId)
        Stop-Process -Id $_.ProcessId -Force -ErrorAction SilentlyContinue
        $killed += 1
    }
if ($killed -eq 0) { Write-Info '未发现运行中的守护进程' }

# ---------------- 2. 结束后端本体 ----------------
$serverExe = 'koi-server.exe'
if ($null -ne $cfg) { $serverExe = $cfg.serverExe }
$serverDir = $null
if ($null -ne $cfg) {
    $appDir = Split-Path -Parent $scriptDir
    $serverDir = Join-Path $appDir $cfg.serverDir
}

$targets = @()
Get-CimInstance Win32_Process -Filter "Name = '$serverExe'" -ErrorAction SilentlyContinue | ForEach-Object {
    if ($null -eq $serverDir -or $_.ExecutablePath -like ($serverDir + '*')) { $targets += $_ }
}
foreach ($process in $targets) {
    Write-Info ('结束后端进程 PID=' + $process.ProcessId)
    Stop-Process -Id $process.ProcessId -Force -ErrorAction SilentlyContinue
}
if ($targets.Count -eq 0) { Write-Info '未发现运行中的后端进程' }

Start-Sleep -Milliseconds 500

# ---------------- 3. 可选：清理用户数据目录 ----------------
if ($DeleteUserData -and $null -ne $cfg) {
    $dataDir = [Environment]::ExpandEnvironmentVariables($cfg.dataDir)
    if (Test-Path -LiteralPath $dataDir) {
        Write-Info ('删除用户数据目录 ' + $dataDir)
        Remove-DataDir $dataDir
    }
}

Write-Info '完成'
exit 0
`;

/** 隐藏窗口启动器：由注册表自启动项调用，避免弹出控制台窗口 */
export const renderHiddenLauncher = (runtimeKey: string): string => `' launch-hidden.vbs
' Generated by rsbuild-plugin-win-installer. Do not edit manually.
' Starts the backend watchdog (watchdog.ps1) with a hidden window.
' NOTE: keep this file ASCII-only - WScript reads .vbs as ANSI, not UTF-8.
Option Explicit

Dim shell, fso, scriptDir, scriptPath, runtimeKey, command

runtimeKey = "${vbsString(runtimeKey)}"

Set shell = CreateObject("WScript.Shell")
Set fso = CreateObject("Scripting.FileSystemObject")

scriptDir = fso.GetParentFolderName(WScript.ScriptFullName)
scriptPath = fso.BuildPath(scriptDir, "watchdog.ps1")

If Not fso.FileExists(scriptPath) Then
    WScript.Quit 1
End If

shell.CurrentDirectory = scriptDir
command = "powershell.exe -NoProfile -NonInteractive -ExecutionPolicy Bypass -WindowStyle Hidden -File """ & scriptPath & """ -RuntimeKey """ & runtimeKey & """"
shell.Run command, 0, False
`;

/** 隐藏窗口停止器：供开始菜单「停止后端服务」快捷方式调用 */
export const renderStopHiddenLauncher = (): string => `' stop-hidden.vbs
' Generated by rsbuild-plugin-win-installer. Do not edit manually.
' Stops the backend watchdog and the backend process itself, with a hidden window.
' NOTE: keep this file ASCII-only - WScript reads .vbs as ANSI, not UTF-8.
Option Explicit

Dim shell, fso, scriptDir, scriptPath, command

Set shell = CreateObject("WScript.Shell")
Set fso = CreateObject("Scripting.FileSystemObject")

scriptDir = fso.GetParentFolderName(WScript.ScriptFullName)
scriptPath = fso.BuildPath(scriptDir, "stop-server.ps1")

If Not fso.FileExists(scriptPath) Then
    WScript.Quit 1
End If

shell.CurrentDirectory = scriptDir
command = "powershell.exe -NoProfile -NonInteractive -ExecutionPolicy Bypass -WindowStyle Hidden -File """ & scriptPath & """ -Quiet"
shell.Run command, 0, False
`;

/** 安装目录内的使用说明 */
export const renderReadme = (options: ResolvedWinInstallerOptions): string => {
  const { backend, appName, version } = options;
  const dataDir = backend.dataDir;
  const syncList = backend.models.join('`, `');
  return `${appName} ${version}
生成时间：见安装包属性
============================================================

目录结构
------------------------------------------------------------
<安装目录>\\                桌面客户端（Electron）主程序
<安装目录>\\${backend.dirName}\\          后端服务程序（${backend.executable}）与只读资源
<安装目录>\\${backend.scriptDirName}\\    守护 / 同步 / 停止脚本与 runtime.json
<安装目录>\\README.txt      本文件

运行期数据目录
------------------------------------------------------------
${dataDir}
  ├── .env                后端环境变量（端口已与前端保持一致）
  ├── models\\             语音模型（安装时由安装目录同步而来）
  ├── resources\\          模板等静态资源
  ├── database.sqlite     数据库（首次运行时自动创建）
  ├── storage\\            会话音频等运行时文件
  └── logs\\               日志
      ├── watchdog.log        守护进程日志（启动 / 重启记录）
      ├── run-<时间>-out.log  每次后端运行的 stdout 归档
      └── run-<时间>-err.log  每次后端运行的 stderr 归档

后端崩溃自动重启
------------------------------------------------------------
由 ${backend.scriptDirName}\\watchdog.ps1 负责：
  · 后端进程退出后 ${backend.restart.delayMs} 毫秒自动拉起；
  · 退出码 ${backend.restart.stopOnExitCodes.join(' / ') || '(空)'} 视为正常退出，不再重启；
  · 熔断阈值 ${backend.restart.maxRestarts === 0 ? '未启用（永不停止重试）' : `${backend.restart.maxRestarts} 次 / ${backend.restart.windowMs} 毫秒窗口`}；
  · 修改策略：编辑 ${backend.scriptDirName}\\runtime.json 中的 restart 段，然后重新启动守护进程。

开机自启动
------------------------------------------------------------
  · 桌面客户端：${
    options.appAutoStart.enabled
      ? `注册表 ${options.appAutoStart.scope === 'machine' ? 'HKLM' : 'HKCU'}\\Software\\Microsoft\\Windows\\CurrentVersion\\Run`
      : '未启用（可在安装时勾选）'
  }
  · 后端服务：${
    backend.autoStart.enabled
      ? `注册表 ${backend.autoStart.scope === 'machine' ? 'HKLM' : 'HKCU'}\\Software\\Microsoft\\Windows\\CurrentVersion\\Run`
      : '未启用（可在安装时勾选）'
  }
关闭方式：任务管理器 → 启动 选项卡 → 禁用对应项；或直接删除上述注册表值。

常用操作
------------------------------------------------------------
  · 手动启动后端：双击 ${backend.scriptDirName}\\launch-hidden.vbs
  · 前台调试运行：powershell -NoProfile -ExecutionPolicy Bypass -File "${backend.scriptDirName}\\watchdog.ps1" -Foreground
  · 停止后端：powershell -NoProfile -ExecutionPolicy Bypass -File "${backend.scriptDirName}\\stop-server.ps1"
  · 同步模型/资源：powershell -NoProfile -ExecutionPolicy Bypass -File "${backend.scriptDirName}\\bootstrap-runtime.ps1" -Force

随包分发的模型目录
------------------------------------------------------------
\`${syncList}\`

注意：如果安装目录位于 Program Files，请勿手工修改其中的文件；
所有可变数据都在上面的运行期数据目录中。
`;
};

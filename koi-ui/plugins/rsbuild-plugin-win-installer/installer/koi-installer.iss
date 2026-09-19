; ============================================================================
;  koi-studio Windows 安装脚本（Inno Setup 6.3+）
;
;  设计约定
;  ---------------------------------------------------------------------------
;  1. 本文件是普通的、可直接编辑与编译的 Inno Setup 脚本，不依赖任何代码生成；
;     所有可变内容一律通过「环境变量」注入，见下方【变量区】。
;  2. 取值优先级：环境变量 > 文件内默认值。因此不设置任何环境变量也能编译，
;     非常适合用 Inno Setup IDE 打开、改几行、直接按 F9 调试。
;  3. 由 rsbuild-plugin-win-installer 调用时：插件会先把本文件复制到
;     build-installer\installer.iss，再注入环境变量并调用 ISCC.exe。
;     因此 relative 路径（OutputDir / Source）都以 build-installer 为基准。
;
;  手动调试示例（PowerShell，工作目录 = build-installer）
;  ---------------------------------------------------------------------------
;     $env:KOI_APP_NAME   = 'koi-studio'
;     $env:KOI_APP_EXE    = 'koi-ui.exe'
;     $env:KOI_STAGING_DIR = "$PWD\staging"
;     $env:KOI_NC_SERVER_1 = '*.onnx'
;     & "${env:ProgramFiles(x86)}\Inno Setup 6\ISCC.exe" installer.iss
;
;  变量一览见 plugins/rsbuild-plugin-win-installer/README.md
; ============================================================================

; ============================================================================
;  【变量区】GetEnv 读环境变量，为空时回退默认值
; ============================================================================

; ---- 应用标识 ----
#define AppName GetEnv('KOI_APP_NAME')
#if AppName == ""
  #define AppName "koi-studio"
#endif

#define AppIdRaw GetEnv('KOI_APP_ID')
#if AppIdRaw == ""
  ; 与插件按应用名生成的稳定 GUID 保持一致（见 options.ts 的 buildAppId）
  #define AppIdRaw "0656d535-18ce-5722-ab6f-aba4f8178b8a"
#endif
; Inno 里 "{{" 表示字面量 "{"，最终 AppId 形如 {GUID}
#define AppIdValue "{{" + AppIdRaw + "}"

#define AppVersion GetEnv('KOI_APP_VERSION')
#if AppVersion == ""
  #define AppVersion "1.0.0"
#endif

#define AppPublisher GetEnv('KOI_APP_PUBLISHER')
#if AppPublisher == ""
  #define AppPublisher "koi-studio"
#endif

#define AppDescription GetEnv('KOI_APP_DESCRIPTION')
#if AppDescription == ""
  #define AppDescription "koi-studio 桌面客户端与本地后端服务"
#endif

; ---- 安装位置与权限 ----
#define InstallDir GetEnv('KOI_INSTALL_DIR')
#if InstallDir == ""
  ; {autopf} 会随安装模式自动映射为 Program Files 或 %LOCALAPPDATA%\Programs
  #define InstallDir "{autopf}\koi-studio"
#endif

#define Privileges GetEnv('KOI_PRIVILEGES')
#if Privileges == ""
  #define Privileges "lowest"
#endif

#define AllowPrivOverride GetEnv('KOI_ALLOW_PRIV_OVERRIDE')
#if AllowPrivOverride == ""
  #define AllowPrivOverride "1"
#endif

; ---- 架构与压缩 ----
#define Arch GetEnv('KOI_ARCH')
#if Arch == ""
  #define Arch "x64compatible"
#endif

#define Compression GetEnv('KOI_COMPRESSION')
#if Compression == ""
  #define Compression "lzma2/max"
#endif

#define FileVersion GetEnv('KOI_FILE_VERSION')

; ---- 输出与暂存目录（相对路径以本 .iss 所在目录为基准） ----
#define OutputDir GetEnv('KOI_OUTPUT_DIR')
#if OutputDir == ""
  #define OutputDir "."
#endif

#define OutputBaseName GetEnv('KOI_OUTPUT_BASENAME')
#if OutputBaseName == ""
  #define OutputBaseName "koi-studio-setup-" + AppVersion
#endif

#define StagingDir GetEnv('KOI_STAGING_DIR')
#if StagingDir == ""
  #define StagingDir "staging"
#endif

; ---- 安装内容布局 ----
#define AppExe GetEnv('KOI_APP_EXE')
#if AppExe == ""
  #define AppExe "koi-ui.exe"
#endif

#define ServerDir GetEnv('KOI_SERVER_DIR')
#if ServerDir == ""
  #define ServerDir "server"
#endif

#define ScriptDir GetEnv('KOI_SCRIPT_DIR')
#if ScriptDir == ""
  #define ScriptDir "scripts"
#endif

#define ServerExe GetEnv('KOI_SERVER_EXE')
#if ServerExe == ""
  #define ServerExe "koi-server.exe"
#endif

; 运行期数据目录（可写）。支持 {%ENVVAR} 形式，例如 {%USERPROFILE}\.koi-studio
#define DataDir GetEnv('KOI_DATA_DIR')
#if DataDir == ""
  #define DataDir "{%USERPROFILE}\.koi-studio"
#endif

; ---- 可选文件 ----
#define SetupIcon GetEnv('KOI_SETUP_ICON')
#if SetupIcon != ""
  #define IconLine "SetupIconFile=" + SetupIcon
#else
  #define IconLine "; SetupIconFile 未指定，使用 Inno Setup 默认图标"
#endif

#define LicenseFile GetEnv('KOI_LICENSE_FILE')
#if LicenseFile != ""
  #define LicenseLine "LicenseFile=" + LicenseFile
#else
  #define LicenseLine "; LicenseFile 未指定"
#endif

; ---- 自启动与交互开关（"0" 表示关闭） ----
#define AutoStartServer GetEnv('KOI_AUTOSTART_SERVER')
#define AutoStartApp GetEnv('KOI_AUTOSTART_APP')
#define DesktopShortcut GetEnv('KOI_DESKTOP_SHORTCUT')
#define LaunchAfter GetEnv('KOI_LAUNCH_AFTER')
#define PromptDeleteData GetEnv('KOI_PROMPT_DELETE_DATA')

#if AutoStartServer == "0"
  #define TaskServerFlags "unchecked"
#else
  #define TaskServerFlags "checkedonce"
#endif

#if AutoStartApp == "0"
  #define TaskAppFlags "unchecked"
#else
  #define TaskAppFlags "checkedonce"
#endif

#if LaunchAfter == "0"
  #define LaunchLine "; LaunchAfterInstall 已关闭"
#else
  #define LaunchLine "Filename: ""{app}\" + AppExe + """; Description: ""{cm:LaunchProgram," + AppName + "}""; Flags: nowait postinstall skipifsilent"
#endif

; ---- 注册表自启动位置 ----
#define ServerScope GetEnv('KOI_SERVER_SCOPE')
#if ServerScope == ""
  #define ServerScope "HKCU"
#endif

#define AppScope GetEnv('KOI_APP_SCOPE')
#if AppScope == ""
  #define AppScope "HKCU"
#endif

; ---- 多语言（最多 3 种，第一种为默认语言） ----
#define Lang1Name GetEnv('KOI_LANG1_NAME')
#if Lang1Name == ""
  #define Lang1Name "english"
#endif
#define Lang1File GetEnv('KOI_LANG1_FILE')
#if Lang1File == ""
  #define Lang1File "compiler:Default.isl"
#endif

#define Lang2Name GetEnv('KOI_LANG2_NAME')
#define Lang2File GetEnv('KOI_LANG2_FILE')
#define Lang3Name GetEnv('KOI_LANG3_NAME')
#define Lang3File GetEnv('KOI_LANG3_FILE')

; ---- 免压缩文件（大体积二进制二次压缩收益极低，跳过可显著缩短编译时间） ----
#define NcAppExcludes GetEnv('KOI_NC_APP_EXCLUDES')
#define NcApp1 GetEnv('KOI_NC_APP_1')
#define NcApp2 GetEnv('KOI_NC_APP_2')

#if NcAppExcludes == ""
  #define NcAppParam ""
#else
  #define NcAppParam "Excludes: """ + NcAppExcludes + """;"
#endif

#define NcServerExcludes GetEnv('KOI_NC_SERVER_EXCLUDES')
#define NcServer1 GetEnv('KOI_NC_SERVER_1')
#define NcServer2 GetEnv('KOI_NC_SERVER_2')

#if NcServerExcludes == ""
  #define NcServerParam ""
#else
  #define NcServerParam "Excludes: """ + NcServerExcludes + """;"
#endif

; 编译期回显关键变量，便于排查参数问题
#pragma message "koi-installer | app=" + AppName + " " + AppVersion + " | staging=" + StagingDir + " | exe=" + AppExe
#pragma message "koi-installer | install=" + InstallDir + " | privileges=" + Privileges + " | compression=" + Compression

; ============================================================================
;  【Setup】
; ============================================================================
[Setup]
AppId={#AppIdValue}
AppName={#AppName}
AppVersion={#AppVersion}
AppVerName={#AppName} {#AppVersion}
AppPublisher={#AppPublisher}
AppComments={#AppDescription}
DefaultDirName={#InstallDir}
DefaultGroupName={#AppName}
UninstallDisplayName={#AppName}
UninstallDisplayIcon={app}\{#AppExe}
OutputDir={#OutputDir}
OutputBaseFilename={#OutputBaseName}
Compression={#Compression}
SolidCompression=yes
WizardStyle=modern
DisableProgramGroupPage=yes
AllowNoIcons=yes
AllowUNCPath=no
UsePreviousAppDir=yes
; 升装/覆盖安装前由 [Code] 主动结束进程，因此关闭 Inno 的自动检测
CloseApplications=no
RestartApplications=no
SetupLogging=yes
MinVersion=10.0
ArchitecturesAllowed={#Arch}
ArchitecturesInstallIn64BitMode={#Arch}
PrivilegesRequired={#Privileges}
#if AllowPrivOverride != "0"
PrivilegesRequiredOverridesAllowed=dialog
#endif
LZMAUseSeparateProcess=yes
#if FileVersion != ""
VersionInfoVersion={#FileVersion}
#endif
{#IconLine}
{#LicenseLine}

; ============================================================================
;  【Languages】
; ============================================================================
[Languages]
Name: "{#Lang1Name}"; MessagesFile: "{#Lang1File}"
#if Lang2Name != ""
Name: "{#Lang2Name}"; MessagesFile: "{#Lang2File}"
#endif
#if Lang3Name != ""
Name: "{#Lang3Name}"; MessagesFile: "{#Lang3File}"
#endif

; ============================================================================
;  【Tasks】安装时可勾选的启动选项
; ============================================================================
[Tasks]
Name: "autostartserver"; Description: "开机自动启动 {#AppName} 后端服务（后台常驻，崩溃自动重启）"; GroupDescription: "启动选项："; Flags: {#TaskServerFlags}
Name: "autostartapp"; Description: "开机自动启动 {#AppName} 桌面客户端"; GroupDescription: "启动选项："; Flags: {#TaskAppFlags}
#if DesktopShortcut != "0"
Name: "desktopicon"; Description: "{cm:CreateDesktopIcon}"; GroupDescription: "{cm:AdditionalIcons}"; Flags: unchecked
#endif

; ============================================================================
;  【Files】安装内容
;   - {#StagingDir}\app      -> {app}               桌面客户端
;   - {#StagingDir}\server   -> {app}\server        后端服务、DLL、模型、.env
;   - {#StagingDir}\scripts  -> {app}\scripts       守护/同步/停止脚本与 runtime.json
; ============================================================================
[Files]
; ---- 桌面客户端 ----
Source: "{#StagingDir}\app\*"; {#NcAppParam} DestDir: "{app}"; Flags: ignoreversion recursesubdirs createallsubdirs
#if NcApp1 != ""
Source: "{#StagingDir}\app\{#NcApp1}"; DestDir: "{app}"; Flags: ignoreversion recursesubdirs createallsubdirs nocompression
#endif
#if NcApp2 != ""
Source: "{#StagingDir}\app\{#NcApp2}"; DestDir: "{app}"; Flags: ignoreversion recursesubdirs createallsubdirs nocompression
#endif

; ---- 后端服务 ----
Source: "{#StagingDir}\server\*"; {#NcServerParam} DestDir: "{app}\{#ServerDir}"; Flags: ignoreversion recursesubdirs createallsubdirs
#if NcServer1 != ""
Source: "{#StagingDir}\server\{#NcServer1}"; DestDir: "{app}\{#ServerDir}"; Flags: ignoreversion recursesubdirs createallsubdirs nocompression
#endif
#if NcServer2 != ""
Source: "{#StagingDir}\server\{#NcServer2}"; DestDir: "{app}\{#ServerDir}"; Flags: ignoreversion recursesubdirs createallsubdirs nocompression
#endif

; ---- 运行时脚本 ----
Source: "{#StagingDir}\scripts\*"; DestDir: "{app}\{#ScriptDir}"; Flags: ignoreversion recursesubdirs createallsubdirs

; ============================================================================
;  【Icons】
; ============================================================================
[Icons]
Name: "{group}\{#AppName}"; Filename: "{app}\{#AppExe}"
Name: "{group}\停止 {#AppName} 后端服务"; Filename: "{sys}\wscript.exe"; Parameters: """{app}\{#ScriptDir}\stop-hidden.vbs"""; WorkingDir: "{app}\{#ScriptDir}"
Name: "{group}\打开数据与日志目录"; Filename: "{sys}\explorer.exe"; Parameters: "{#DataDir}"
Name: "{group}\使用说明"; Filename: "{app}\README.txt"
Name: "{group}\{cm:UninstallProgram,{#AppName}}"; Filename: "{uninstallexe}"
#if DesktopShortcut != "0"
Name: "{autodesktop}\{#AppName}"; Filename: "{app}\{#AppExe}"; Tasks: desktopicon
#endif

; ============================================================================
;  【Registry】开机自启动（卸载时自动清理）
; ============================================================================
[Registry]
Root: {#ServerScope}; Subkey: "Software\Microsoft\Windows\CurrentVersion\Run"; ValueType: string; ValueName: "{#AppName} 后端服务"; ValueData: """{sys}\wscript.exe"" ""{app}\{#ScriptDir}\launch-hidden.vbs"""; Flags: uninsdeletevalue; Tasks: autostartserver
Root: {#AppScope}; Subkey: "Software\Microsoft\Windows\CurrentVersion\Run"; ValueType: string; ValueName: "{#AppName}"; ValueData: """{app}\{#AppExe}"""; Flags: uninsdeletevalue; Tasks: autostartapp

; ============================================================================
;  【Run】安装完成后：初始化运行环境 -> 启动后端守护 -> 可选启动客户端
; ============================================================================
[Run]
Filename: "{sys}\WindowsPowerShell\v1.0\powershell.exe"; Parameters: "-NoProfile -NonInteractive -ExecutionPolicy Bypass -File ""{app}\{#ScriptDir}\bootstrap-runtime.ps1"""; WorkingDir: "{app}\{#ScriptDir}"; StatusMsg: "正在初始化后端运行环境（首次安装需同步模型文件，请稍候）..."; Flags: runhidden waituntilterminated
Filename: "{sys}\wscript.exe"; Parameters: """{app}\{#ScriptDir}\launch-hidden.vbs"""; WorkingDir: "{app}\{#ScriptDir}"; Description: "立即启动后端服务"; StatusMsg: "正在启动后端服务..."; Flags: runhidden nowait
{#LaunchLine}

; ============================================================================
;  【UninstallRun】卸载前兜底结束后端进程
; ============================================================================
[UninstallRun]
Filename: "{sys}\taskkill.exe"; Parameters: "/F /IM ""{#AppExe}"""; Flags: runhidden; RunOnceId: "KillApp"

; ============================================================================
;  【Code】
; ============================================================================
[Code]
const
  STOP_ARGS = '-NoProfile -NonInteractive -ExecutionPolicy Bypass -File ';

function StopScriptPath(): String;
begin
  Result := ExpandConstant('{app}\{#ScriptDir}\stop-server.ps1');
end;

function DataDirPath(): String;
begin
  Result := ExpandConstant('{#DataDir}');
end;

// 结束桌面客户端（后端由 stop-server.ps1 处理）
procedure KillApp();
var
  ResultCode: Integer;
begin
  Exec(ExpandConstant('{sys}\taskkill.exe'), '/F /IM "{#AppExe}"', '', SW_HIDE, ewWaitUntilTerminated, ResultCode);
end;

// 调用随包脚本结束后端守护进程与后端本体，避免文件被占用导致安装失败
procedure StopBackend(ExtraArgs: String);
var
  ResultCode: Integer;
  Script: String;
  Command: String;
begin
  Script := StopScriptPath();
  if not FileExists(Script) then
    Exit;
  Command := STOP_ARGS + '"' + Script + '" -Quiet' + ExtraArgs;
  Exec(ExpandConstant('{sys}\WindowsPowerShell\v1.0\powershell.exe'), Command, '', SW_HIDE, ewWaitUntilTerminated, ResultCode);
end;

procedure CurStepChanged(CurStep: TSetupStep);
begin
  if CurStep = ssInstall then
  begin
    KillApp();
    StopBackend('');
  end;
end;

// 询问是否顺带删除用户数据目录（KOI_PROMPT_DELETE_DATA=0 时整段编译期移除）
procedure AskDeleteUserData(var ExtraArgs: String);
var
  DataDir: String;
begin
  ExtraArgs := '';
#if PromptDeleteData != "0"
  DataDir := DataDirPath();
  if not DirExists(DataDir) then
    Exit;
  // 静默卸载时 SuppressibleMsgBox 直接返回默认值（保留数据）
  if SuppressibleMsgBox('是否同时删除用户数据目录？' + #13#10 + #13#10 + DataDir + #13#10 + '（包含模型缓存、数据库与日志）' + #13#10 + #13#10 + '选择「否」将保留该目录，便于下次安装继续使用。', mbConfirmation, MB_YESNO, IDNO) = IDYES then
    ExtraArgs := ' -DeleteUserData';
#endif
end;

procedure CurUninstallStepChanged(CurUninstallStep: TUninstallStep);
var
  Extra: String;
begin
  if CurUninstallStep <> usUninstall then
    Exit;

  KillApp();
  AskDeleteUserData(Extra);
  StopBackend(Extra);
end;

function InitializeSetup(): Boolean;
begin
  Result := True;
end;

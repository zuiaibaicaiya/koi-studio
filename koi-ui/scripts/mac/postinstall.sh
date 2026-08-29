#!/bin/sh

# 注意：pkg 脚本一旦以非 0 退出，安装器就会报
# “安装器遇到了一个错误，导致安装失败”，因此本脚本必须保证 exit 0。
# 注册后台服务属于“尽力而为”的操作，失败不应阻断 app 安装。
set -u

log() {
    echo "[$(date '+%Y-%m-%d %H:%M:%S')] $1"
}

log "开始执行 postinstall 脚本"
log "当前用户: $(whoami)"

APP_NAME="koi-ui.app"
BUNDLE_ID="com.koi-ui.app"
LABEL="${BUNDLE_ID}.service"
Resources="/Applications/${APP_NAME}/Contents/Resources"
SERVICE_EXEC="${Resources}/koi-server"

LOGGED_IN_USER=$(stat -f "%Su" /dev/console 2>/dev/null || echo "")
if [ -z "${LOGGED_IN_USER}" ]; then
    log "警告：未检测到登录用户，跳过 launchd 服务注册"
    log "postinstall 脚本执行完成"
    exit 0
fi

LOGGED_IN_UID=$(id -u "${LOGGED_IN_USER}" 2>/dev/null || echo "")
LOGGED_IN_USER_HOME=$(eval echo ~"${LOGGED_IN_USER}")
WorkingDirectory="${LOGGED_IN_USER_HOME}/.koi-ui"
USER_AGENTS_DIR="${LOGGED_IN_USER_HOME}/Library/LaunchAgents"
PLIST_FILE="${USER_AGENTS_DIR}/${LABEL}.plist"

if [ ! -f "${SERVICE_EXEC}" ]; then
    log "未打包后端服务（${SERVICE_EXEC} 不存在），跳过 launchd 服务注册"
    log "postinstall 脚本执行完成"
    exit 0
fi

if [ ! -d "${Resources}" ]; then
    log "警告：Resources 目录 ${Resources} 不存在，跳过服务注册"
    log "postinstall 脚本执行完成"
    exit 0
fi

# 以 root 身份安装时，直接操作 gui/<uid> 域会报
# “Bootstrap failed: 5: Input/output error”，必须经 launchctl asuser 进入用户会话。
run_launchctl() {
    if [ "$(id -u)" -eq 0 ] && [ -n "${LOGGED_IN_UID}" ]; then
        launchctl asuser "${LOGGED_IN_UID}" launchctl "$@" 2>&1 | sed 's/^/    launchctl: /'
    else
        launchctl "$@" 2>&1 | sed 's/^/    launchctl: /'
    fi
    return 0
}

# 复制目录内容到已存在的目标目录，使用 "<src>/." 避免目标已存在时产生 models/models 嵌套
copy_dir() {
    src="$1"
    dst="$2"
    if [ ! -d "${src}" ]; then
        log "警告：${src} 不存在，跳过复制"
        return 0
    fi
    mkdir -p "${dst}"
    cp -R "${src}/." "${dst}/" || log "警告：复制 ${src} -> ${dst} 失败"
    chown -R "${LOGGED_IN_USER}:staff" "${dst}" 2>/dev/null || true
    chmod -R u+rwX,go+rX "${dst}" 2>/dev/null || true
    return 0
}

# 创建工作目录
log "创建工作目录 ${WorkingDirectory}"
mkdir -p "${WorkingDirectory}"
chown "${LOGGED_IN_USER}:staff" "${WorkingDirectory}" 2>/dev/null || true
chmod 755 "${WorkingDirectory}" 2>/dev/null || true

# 复制环境文件与模型/资源
if [ -f "${Resources}/.env" ]; then
    log "复制环境文件到 ${WorkingDirectory}"
    cp "${Resources}/.env" "${WorkingDirectory}/.env" || log "警告：复制 .env 失败"
    chown "${LOGGED_IN_USER}:staff" "${WorkingDirectory}/.env" 2>/dev/null || true
    chmod 644 "${WorkingDirectory}/.env" 2>/dev/null || true
else
    log "警告：环境文件 ${Resources}/.env 不存在，跳过复制"
fi

log "复制 models 到 ${WorkingDirectory}/models"
copy_dir "${Resources}/models" "${WorkingDirectory}/models"

log "复制 resources 到 ${WorkingDirectory}/resources"
copy_dir "${Resources}/resources" "${WorkingDirectory}/resources"

# 确保 LaunchAgents 目录存在
log "确保 LaunchAgents 目录存在"
mkdir -p "${USER_AGENTS_DIR}"

# 写入 plist
log "创建 plist 文件 ${PLIST_FILE}"
cat > "${PLIST_FILE}" << EOF
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
  <dict>
    <key>Label</key>
    <string>${LABEL}</string>
    <key>Program</key>
    <string>${SERVICE_EXEC}</string>
    <key>RunAtLoad</key>
    <true/>
    <key>KeepAlive</key>
    <true/>
    <key>WorkingDirectory</key>
    <string>${WorkingDirectory}</string>
    <key>StandardOutPath</key>
    <string>/tmp/${LABEL}.out.log</string>
    <key>StandardErrorPath</key>
    <string>/tmp/${LABEL}.err.log</string>
  </dict>
</plist>
EOF

chown "${LOGGED_IN_USER}:staff" "${PLIST_FILE}" 2>/dev/null || true
chmod 644 "${PLIST_FILE}" 2>/dev/null || true

chmod +x "${SERVICE_EXEC}" 2>/dev/null || true

# 注册并启动服务（失败仅记录）
log "卸载旧服务（若存在）"
run_launchctl bootout "gui/${LOGGED_IN_UID}/${LABEL}"

log "注册服务 ${LABEL}"
run_launchctl bootstrap "gui/${LOGGED_IN_UID}" "${PLIST_FILE}"

log "启动服务 ${LABEL}"
run_launchctl kickstart -p "gui/${LOGGED_IN_UID}/${LABEL}"

log "验证服务状态"
sleep 2
if launchctl print "gui/${LOGGED_IN_UID}/${LABEL}" >/dev/null 2>&1; then
    launchctl print "gui/${LOGGED_IN_UID}/${LABEL}" 2>/dev/null | sed -n '1,6p' | sed 's/^/    /'
    log "服务已注册并运行"
else
    log "警告：服务未在运行（不影响应用安装，可在启动 App 后重试）"
fi

log "实际登录用户: ${LOGGED_IN_USER} (UID: ${LOGGED_IN_UID})"
log "服务路径: ${SERVICE_EXEC}"
log "工作目录: ${WorkingDirectory}"
log "plist 文件: ${PLIST_FILE}"
log "日志: /tmp/${LABEL}.out.log、/tmp/${LABEL}.err.log"

log "postinstall 脚本执行完成"
log "===== PKG 脚本结束执行（$(date)）====="

exit 0

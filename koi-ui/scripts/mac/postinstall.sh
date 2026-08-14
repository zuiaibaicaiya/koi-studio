#!/bin/sh

# 错误时退出
set -e

# 日志函数
log() {
    echo "[$(date '+%Y-%m-%d %H:%M:%S')] $1"
}

# 脚本开始
log "开始执行 postinstall 脚本"
log "当前用户: $(whoami)"

# 定义变量（适配当前项目 koi-ui）
APP_NAME="koi-ui.app"
BUNDLE_ID="com.koi-ui.app"
# 使用系统 Applications 目录
Resources="/Applications/${APP_NAME}/Contents/Resources"
SERVICE_EXEC="${Resources}/koi-server"
# 获取当前登录用户的主目录
LOGGED_IN_USER=$(stat -f "%Su" /dev/console)
LOGGED_IN_USER_HOME=$(eval echo ~"${LOGGED_IN_USER}")
WorkingDirectory="${LOGGED_IN_USER_HOME}/.koi-ui"
USER_AGENTS_DIR="${LOGGED_IN_USER_HOME}/Library/LaunchAgents"
PLIST_FILE="${USER_AGENTS_DIR}/${BUNDLE_ID}.service.plist"

# 仅当打包了后端服务时才注册 launchd 服务（前端项目无后端则跳过）
if [ ! -f "${SERVICE_EXEC}" ]; then
    log "未打包后端服务（${SERVICE_EXEC} 不存在），跳过 launchd 服务注册"
    log "postinstall 脚本执行完成"
    exit 0
fi

# 检查必要文件
if [ ! -d "${Resources}" ]; then
    log "错误：Resources 目录 ${Resources} 不存在"
    exit 1
fi

# 创建工作目录
log "创建工作目录 ${WorkingDirectory}"
mkdir -p "${WorkingDirectory}"
# 设置工作目录权限，确保服务可以读写
log "设置工作目录权限"
chown "${LOGGED_IN_USER}:staff" "${WorkingDirectory}"
chmod 755 "${WorkingDirectory}"

# 复制环境文件（如果存在）
if [ -f "${Resources}/.env" ]; then
    log "复制环境文件到 ${WorkingDirectory}"
    cp "${Resources}/.env" "${WorkingDirectory}/.env"
    cp -r "${Resources}/resources"  "${WorkingDirectory}/resources"
    cp -r "${Resources}/models"  "${WorkingDirectory}/models"
    # 设置环境文件权限
    chown "${LOGGED_IN_USER}:staff" "${WorkingDirectory}/.env"
    chmod 644 "${WorkingDirectory}/.env"
    # 设置 resources/models 权限：目录需 x 才能遍历，文件只需 r
    chown -R "${LOGGED_IN_USER}:staff" "${WorkingDirectory}/resources" "${WorkingDirectory}/models"
    chmod -R u+rwX,go+rX "${WorkingDirectory}/resources" "${WorkingDirectory}/models"
else
    log "警告：环境文件 ${Resources}/.env 不存在，跳过复制"
fi

# 确保 LaunchAgents 目录存在
log "确保 LaunchAgents 目录存在"
mkdir -p "${USER_AGENTS_DIR}"

# 写入 plist
log "创建 plist 文件 ${PLIST_FILE}"
cat > "${PLIST_FILE}" << EOF
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList1.0.dtd">
<plist version="1.0">
  <dict>
    <key>Label</key>
    <string>${BUNDLE_ID}.service</string>
    <key>Program</key>
    <string>${SERVICE_EXEC}</string>
    <key>RunAtLoad</key>
    <true/>
    <key>KeepAlive</key>
    <true/>
    <key>WorkingDirectory</key>
    <string>${WorkingDirectory}</string>
    <key>StandardOutPath</key>
    <string>/tmp/${BUNDLE_ID}.service.out.log</string>
    <key>StandardErrorPath</key>
    <string>/tmp/${BUNDLE_ID}.service.err.log</string>
  </dict>
</plist>
EOF

# 设置权限
log "设置 plist 文件权限"
# 使用实际登录用户确保所有者正确
chown "${LOGGED_IN_USER}:staff" "${PLIST_FILE}"
chmod 600 "${PLIST_FILE}"

# 获取实际登录用户的 UID
LOGGED_IN_UID=$(id -u "${LOGGED_IN_USER}")

# 卸载旧服务（忽略错误）
log "卸载旧服务"
launchctl bootout gui/"${LOGGED_IN_UID}"/"${BUNDLE_ID}".service 2>/dev/null || true

# 加载新服务
log "加载新服务"
launchctl bootstrap gui/"${LOGGED_IN_UID}" "${PLIST_FILE}"

# 设置可执行权限
log "设置可执行权限"
chmod +x "${SERVICE_EXEC}"

# 启动服务
log "启动服务"
launchctl kickstart -p gui/"${LOGGED_IN_UID}"/"${BUNDLE_ID}".service

# 验证服务状态
log "验证服务状态"
sleep 2
launchctl list | grep "${BUNDLE_ID}" || log "警告：服务可能未成功启动，请检查日志"
log "实际登录用户: ${LOGGED_IN_USER} (UID: ${LOGGED_IN_UID})"
log "服务路径: ${SERVICE_EXEC}"
log "plist 文件: ${PLIST_FILE}"

log "postinstall 脚本执行完成"
log "===== PKG 脚本结束执行（$(date)）====="

exit 0

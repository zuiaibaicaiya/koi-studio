#!/bin/sh

# 注意：pkg 脚本一旦以非 0 退出，安装器就会报
# “安装器遇到了一个错误，导致安装失败”，因此本脚本必须保证 exit 0。
# 启动/停止后台服务属于“尽力而为”的操作，失败不应阻断 app 安装。
set -u

log() {
    echo "[$(date '+%Y-%m-%d %H:%M:%S')] $1"
}

APP_NAME="koi-ui.app"
BUNDLE_ID="com.koi-ui.app"
LABEL="${BUNDLE_ID}.service"
Resources="/Applications/${APP_NAME}/Contents/Resources"
SERVICE_EXEC="${Resources}/koi-server"

LOGGED_IN_USER=$(stat -f "%Su" /dev/console 2>/dev/null || echo "")
LOGGED_IN_UID=""
if [ -n "${LOGGED_IN_USER}" ]; then
    LOGGED_IN_UID=$(id -u "${LOGGED_IN_USER}" 2>/dev/null || echo "")
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

log "开始执行 preinstall 脚本"

if [ ! -f "${SERVICE_EXEC}" ]; then
    log "未打包后端服务（${SERVICE_EXEC} 不存在），跳过服务停止"
    log "preinstall 脚本执行完成"
    exit 0
fi

if [ -n "${LOGGED_IN_UID}" ]; then
    log "卸载旧服务 ${LABEL}"
    run_launchctl bootout "gui/${LOGGED_IN_UID}/${LABEL}"
else
    log "未检测到登录用户，尝试按标签停止服务 ${LABEL}"
    run_launchctl stop "${LABEL}"
fi

# 旧进程仍在退出时删除可能失败，失败仅记录，不影响安装
log "删除旧的可执行文件 ${SERVICE_EXEC}"
rm -f "${SERVICE_EXEC}" || log "警告：删除 ${SERVICE_EXEC} 失败，将由安装器覆盖"

log "preinstall 脚本执行完成"
exit 0

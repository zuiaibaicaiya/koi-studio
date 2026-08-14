#!/bin/sh

# 错误时退出
set -e

# 日志函数
log() {
    echo "[$(date '+%Y-%m-%d %H:%M:%S')] $1"
}

# 脚本开始
log "开始执行 preinstall 脚本"

# 定义变量（适配当前项目 koi-ui）
APP_NAME="koi-ui.app"
BUNDLE_ID="com.koi-ui.app"
# 使用系统 Applications 目录
Resources="/Applications/${APP_NAME}/Contents/Resources"
SERVICE_EXEC="${Resources}/koi-server"

# 仅当打包了后端服务时才停止旧服务（前端项目无后端则跳过）
if [ -f "${SERVICE_EXEC}" ]; then
    log "停止服务 ${BUNDLE_ID}.service"
    launchctl stop "${BUNDLE_ID}".service 2>/dev/null || true

    # 删除旧的可执行文件
    log "删除旧的可执行文件 ${SERVICE_EXEC}"
    rm -f "${SERVICE_EXEC}"
else
    log "未打包后端服务（${SERVICE_EXEC} 不存在），跳过服务停止"
fi

log "preinstall 脚本执行完成"
exit 0

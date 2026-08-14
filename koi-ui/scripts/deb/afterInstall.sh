#!/bin/sh

# koi-ui Debian 包安装后脚本
set -e

echo "[koi-ui] after-install: 刷新桌面数据库"

if command -v update-desktop-database >/dev/null 2>&1; then
    update-desktop-database /usr/share/applications || true
fi

exit 0

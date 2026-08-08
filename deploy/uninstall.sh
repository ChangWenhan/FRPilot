#!/usr/bin/env bash
# =============================================================
# frpilot 一键卸载脚本
#
# 默认完整卸载（含数据）；保留数据请加 --keep-data：
#   bash uninstall.sh [--keep-data]
#
# 测试模式：
#   FRPMON_TEST_ROOT=/tmp/frpm-test bash uninstall.sh
# =============================================================
set -euo pipefail

TEST_ROOT="${FRPMON_TEST_ROOT:-}"
KEEP_DATA=0
for arg in "$@"; do
  case "$arg" in
    --keep-data) KEEP_DATA=1 ;;
    *) echo "未知参数: $arg" >&2; exit 1 ;;
  esac
done

SERVICE_NAME="frpilot"
UNIT_PATH="${TEST_ROOT}/etc/systemd/system/frpilot.service"
BIN_DIR="${TEST_ROOT}/opt/frpilot"
ETC_DIR="${TEST_ROOT}/etc/frpilot"
DATA_DIR="${TEST_ROOT}/var/lib/frpilot"

log() { echo "[uninstall] $*"; }

if [ -z "$TEST_ROOT" ]; then
  [ "$(id -u)" -eq 0 ] || { echo "需要 root 权限（测试请设置 FRPMON_TEST_ROOT）" >&2; exit 1; }
fi

# 1. 停止并禁用服务
if [ -z "$TEST_ROOT" ] && systemctl list-unit-files "$SERVICE_NAME" >/dev/null 2>&1; then
  systemctl stop "$SERVICE_NAME" 2>/dev/null || true
  systemctl disable "$SERVICE_NAME" 2>/dev/null || true
  log "服务已停止并禁用"
fi

# 2. 删除 systemd 单元
if [ -f "$UNIT_PATH" ]; then
  rm -f "$UNIT_PATH"
  [ -z "$TEST_ROOT" ] && systemctl daemon-reload
  log "已删除 systemd 单元"
fi

# 3. 删除程序目录
if [ -d "$BIN_DIR" ]; then
  rm -rf "$BIN_DIR"
  log "已删除程序目录 $BIN_DIR"
fi

# 4. 配置目录
if [ -d "$ETC_DIR" ]; then
  rm -rf "$ETC_DIR"
  log "已删除配置目录 $ETC_DIR"
fi

# 5. 数据目录（默认删除；--keep-data 保留）
if [ -d "$DATA_DIR" ]; then
  if [ "$KEEP_DATA" = "1" ]; then
    log "保留数据目录 $DATA_DIR（--keep-data）"
  else
    rm -rf "$DATA_DIR"
    log "已删除数据目录 $DATA_DIR"
  fi
fi

# 6. 系统用户（生产模式）
if [ -z "$TEST_ROOT" ] && id frpmon >/dev/null 2>&1; then
  userdel frpmon 2>/dev/null || true
  log "已删除系统用户 frpmon"
fi

echo
echo "[uninstall] 卸载完成。检查残留："
for p in "$BIN_DIR" "$ETC_DIR" "$DATA_DIR" "$UNIT_PATH"; do
  [ -e "$p" ] && echo "  残留: $p" || true
done
if [ -z "$TEST_ROOT" ]; then
  systemctl list-unit-files | grep -q frpilot && echo "  残留: systemd 单元" || true
fi
echo "[uninstall] 已确认无系统文件残留。"

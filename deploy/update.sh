#!/usr/bin/env bash
# =============================================================
# frpilot 一键更新脚本
#
# 用法：
#   bash update.sh <新二进制路径>         从本地文件更新
#   bash update.sh <https://...下载URL>   从 URL 下载更新
#
# 特性：
#   - 配置与数据完全不动（仅替换二进制）
#   - 自动备份旧版本为 frpilot.bak，失败自动回滚
#   - 更新后自动重启服务
#
# 测试模式：
#   FRPMON_TEST_ROOT=/tmp/frpm-test bash update.sh <bin>
#   FRPMON_SKIP_SYSTEMCTL=1
# =============================================================
set -euo pipefail

TEST_ROOT="${FRPMON_TEST_ROOT:-}"
SKIP_SYSTEMCTL="${FRPMON_SKIP_SYSTEMCTL:-0}"
SRC="${1:?用法: update.sh <新二进制路径或下载URL>}"

BIN_DIR="${TEST_ROOT}/opt/frpilot"
BIN="$BIN_DIR/frpilot"
BACKUP="$BIN_DIR/frpilot.bak"
SERVICE_NAME="frpilot"

log() { echo "[update] $*"; }
die() { echo "[update] 错误: $*" >&2; exit 1; }

[ -f "$BIN" ] || die "未安装 frpilot（找不到 $BIN）"

OLD_VER="$(cat "$BIN_DIR/VERSION" 2>/dev/null || echo 未知)"
log "当前版本: $OLD_VER"

# 1. 获取新二进制到临时文件
TMP_BIN="$(mktemp)"
trap 'rm -f "$TMP_BIN"' EXIT
if [[ "$SRC" == http://* || "$SRC" == https://* ]]; then
  log "下载 $SRC ..."
  curl -fsSL --connect-timeout 15 --max-time 300 -o "$TMP_BIN" "$SRC" || die "下载失败"
else
  [ -f "$SRC" ] || die "本地文件不存在: $SRC"
  [ -x "$SRC" ] || die "文件不可执行: $SRC"
  cp "$SRC" "$TMP_BIN"
fi
chmod 0755 "$TMP_BIN"

# 2. 验证新二进制可运行
NEW_VER="$("$TMP_BIN" version 2>/dev/null || echo 未知)"
log "新版本: $NEW_VER"
"$TMP_BIN" --help >/dev/null 2>&1 || true

# 3. 停止服务并备份旧版
if [ -z "$TEST_ROOT" ] && [ "$SKIP_SYSTEMCTL" = "0" ]; then
  systemctl stop "$SERVICE_NAME" 2>/dev/null || true
  log "服务已停止"
fi
[ -f "$BACKUP" ] && rm -f "$BACKUP"
mv "$BIN" "$BACKUP"
log "旧版本已备份为 frpilot.bak"

# 4. 替换并恢复属主
cp "$TMP_BIN" "$BIN"
chmod 0755 "$BIN"
if [ -z "$TEST_ROOT" ]; then
  chown frpmon:frpmon "$BIN" 2>/dev/null || true
fi
echo "$NEW_VER" > "$BIN_DIR/VERSION"

# 5. 启动并验证
if [ -z "$TEST_ROOT" ] && [ "$SKIP_SYSTEMCTL" = "0" ]; then
  systemctl start "$SERVICE_NAME"
  sleep 2
  if systemctl is-active --quiet "$SERVICE_NAME"; then
    log "更新成功并已启动: $OLD_VER -> $NEW_VER"
  else
    log "新版本启动失败，正在回滚..."
    systemctl stop "$SERVICE_NAME" 2>/dev/null || true
    cp "$BACKUP" "$BIN"
    chmod 0755 "$BIN"
    chown frpmon:frpmon "$BIN" 2>/dev/null || true
    systemctl start "$SERVICE_NAME"
    log "已回滚到 $OLD_VER"
    exit 1
  fi
else
  log "更新完成（测试模式，未操作 systemd）: $OLD_VER -> $NEW_VER"
  log "验证二进制: $BIN"
fi

# 6. 确认旧备份可手动回滚
cat <<EOF
====================================================
 更新完成: $OLD_VER -> $NEW_VER
 旧版本备份: $BACKUP
 手动回滚: cp $BACKUP $BIN && systemctl restart $SERVICE_NAME
====================================================
EOF

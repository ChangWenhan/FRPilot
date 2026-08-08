#!/usr/bin/env bash
# =============================================================
# frpilot 一键安装脚本
#
# 安装足迹（全部文件清单，不影响 frps 的任何配置/数据）：
#   /opt/frpilot/frpilot        主程序二进制
#   /opt/frpilot/VERSION            版本号
#   /etc/frpilot/config.json        配置文件（0600）
#   /var/lib/frpilot/               数据目录（SQLite 数据库 + 加密密钥）
#   /etc/systemd/system/frpilot.service  systemd 服务
#   系统用户 frpmon（运行专用，无登录 shell）
#
# 本脚本【不会】修改 frps 的 frps.ini/frps.toml 等任何文件，
# 软件对 frps 配置只做只读校验。
#
# 用法：
#   bash install.sh [二进制路径]
#     二进制路径缺省时使用同目录 frpilot（构建产物）
#
# 测试模式（不写真实系统路径）：
#   FRPMON_TEST_ROOT=/tmp/frpm-test bash install.sh /path/to/frpilot
#   FRPMON_SKIP_SYSTEMCTL=1             跳过 systemd 操作
# =============================================================
set -euo pipefail

TEST_ROOT="${FRPMON_TEST_ROOT:-}"
SKIP_SYSTEMCTL="${FRPMON_SKIP_SYSTEMCTL:-0}"
SRC_BIN="${1:-$(dirname "$(readlink -f "$0")")/frpilot}"

BIN_DIR="${TEST_ROOT}/opt/frpilot"
ETC_DIR="${TEST_ROOT}/etc/frpilot"
DATA_DIR="${TEST_ROOT}/var/lib/frpilot"
UNIT_PATH="${TEST_ROOT}/etc/systemd/system/frpilot.service"
SERVICE_NAME="frpilot"
RUN_USER="frpmon"
LISTEN_ADDR="0.0.0.0:8443"

VERSION="${FRPMON_VERSION:-0.1.0}"

log() { echo "[install] $*"; }
die() { echo "[install] 错误: $*" >&2; exit 1; }

[ -f "$SRC_BIN" ] || die "找不到二进制: $SRC_BIN"
[ -x "$SRC_BIN" ] || die "二进制不可执行: $SRC_BIN"

# 确认版本
BIN_VER="$("$SRC_BIN" version 2>/dev/null || echo "$VERSION")"
log "安装 frpilot $BIN_VER"

if [ -z "$TEST_ROOT" ]; then
  [ "$(id -u)" -eq 0 ] || die "生产安装需要 root 权限（测试请设置 FRPMON_TEST_ROOT）"
fi

# 1. 创建专用运行用户（无登录 shell、无 HOME）
if [ -z "$TEST_ROOT" ]; then
  if ! id "$RUN_USER" >/dev/null 2>&1; then
    useradd --system --no-create-home --shell /usr/sbin/nologin "$RUN_USER"
    log "已创建系统用户 $RUN_USER"
  fi
fi

# 2. 目录与文件
SCRIPT_DIR="$(dirname "$(readlink -f "$0")")"
mkdir -p "$BIN_DIR" "$ETC_DIR" "$DATA_DIR"
install -m 0755 "$SRC_BIN" "$BIN_DIR/frpilot"
echo "$BIN_VER" > "$BIN_DIR/VERSION"
# 卸载/更新脚本随程序一起安装，保持"程序目录自包含"
for s in uninstall.sh update.sh; do
  if [ -f "$SCRIPT_DIR/$s" ]; then
    install -m 0755 "$SCRIPT_DIR/$s" "$BIN_DIR/$s"
  fi
done

# 3. 默认配置（仅在不存在时生成）
if [ ! -f "$ETC_DIR/config.json" ]; then
  TLS_JSON=""
  # 默认纯 HTTP；INSTALL_TLS=1 时生成自签名证书启用 HTTPS
  if [ -n "${INSTALL_TLS:-}" ] && [ -z "$TEST_ROOT" ] && command -v openssl >/dev/null 2>&1; then
    CERT_DIR="$ETC_DIR"
    CERT="$CERT_DIR/server.crt"
    KEY="$CERT_DIR/server.key"
    # SAN 包含常见访问地址：localhost、本机各 IP
    SAN_IPS="$(hostname -I 2>/dev/null | awk '{for(i=1;i<=NF;i++) printf "IP:%s,", $i}')"
    SAN_IPS="${SAN_IPS}IP:127.0.0.1,DNS:localhost"
    openssl req -x509 -newkey rsa:2048 -nodes -keyout "$KEY" -out "$CERT" \
      -days 3650 -subj "/CN=frpilot" \
      -addext "subjectAltName=${SAN_IPS}" >/dev/null 2>&1 || true
    if [ -f "$CERT" ] && [ -f "$KEY" ]; then
      chmod 0600 "$KEY" "$CERT"
      TLS_JSON=$(cat <<EOF2
  "tls": { "enabled": true, "cert": "$CERT", "key": "$KEY" },
EOF2
)
      log "已生成自签名证书并启用 HTTPS（有效期 10 年）"
    fi
  fi
  cat > "$ETC_DIR/config.json" <<EOF
{
  "listenAddr": "0.0.0.0:8443",
  "registration": "open",
  "sessionTTLDays": 7,
  "loginMaxFails": 5,
  "loginLockMinutes": 10,
$TLS_JSON  "frps": {
    "dashboardUrl": "",
    "dashboardUser": "",
    "dashboardPass": "",
    "sshHost": "",
    "sshPort": 22,
    "sshUser": "",
    "sshPass": "",
    "configPath": "",
    "token": ""
  },
  "health": {
    "cpuWarn": 70, "cpuFail": 85,
    "memWarn": 80, "memFail": 90,
    "diskWarn": 75, "diskFail": 85,
    "gpuTempWarn": 75, "gpuTempFail": 85,
    "gpuMemWarn": 85, "gpuMemFail": 95,
    "clamDbMaxDays": 7, "snapshotMaxAgeMin": 10
  },
  "cleanupCustom": [],
  "ai": { "enabled": false, "providerUrl": "", "model": "", "timeoutSec": 60 }
}
EOF
  chmod 0600 "$ETC_DIR/config.json"
  log "已生成默认配置 $ETC_DIR/config.json（请通过 Web 界面配置 frps 信息）"
fi

# 4. 属主
if [ -z "$TEST_ROOT" ]; then
  chown -R "$RUN_USER:$RUN_USER" "$BIN_DIR" "$ETC_DIR" "$DATA_DIR"
  chmod 0700 "$DATA_DIR"
fi

# 5. systemd 单元
if [ -z "$TEST_ROOT" ]; then
  cat > "$UNIT_PATH" <<'EOF'
[Unit]
Description=frpilot monitoring platform
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=frpmon
Group=frpmon
ExecStart=/opt/frpilot/frpilot server --data-dir /var/lib/frpilot --config /etc/frpilot/config.json --listen 0.0.0.0:8443
Restart=on-failure
RestartSec=5
LimitNOFILE=65535
NoNewPrivileges=true
ProtectSystem=strict
ProtectHome=true
PrivateTmp=true
PrivateDevices=true
ProtectKernelTunables=true
ProtectControlGroups=true
RestrictSUIDSGID=true
ReadWritePaths=/var/lib/frpilot /etc/frpilot

[Install]
WantedBy=multi-user.target
EOF
  chmod 0644 "$UNIT_PATH"
  systemctl daemon-reload
  if [ "$SKIP_SYSTEMCTL" = "0" ]; then
    systemctl enable --now "$SERVICE_NAME"
    log "服务已启动"
    systemctl --no-pager status "$SERVICE_NAME" | head -5 || true
  else
    log "已跳过 systemctl 启停（FRPMON_SKIP_SYSTEMCTL=1）"
  fi
fi

# 6. 防火墙放行 Web 端口（ufw active 时自动放行；阿里云安全组需另行放行）
if [ -z "$TEST_ROOT" ]; then
  LISTEN_PORT="$(echo "$LISTEN_ADDR" | sed 's/.*://')"
  if command -v ufw >/dev/null 2>&1 && ufw status 2>/dev/null | grep -q "Status: active"; then
    ufw allow "${LISTEN_PORT}/tcp" >/dev/null 2>&1 || true
    log "ufw 已放行 ${LISTEN_PORT}/tcp"
  fi
fi

# 7. 卸载/更新说明
cat <<EOF

====================================================
 frpilot $BIN_VER 安装完成
----------------------------------------------------
 程序目录 : $BIN_DIR
 配置目录 : $ETC_DIR/config.json
 数据目录 : $DATA_DIR
 服务     : systemctl status $SERVICE_NAME
 访问地址 : ${INSTALL_TLS:-http}://<本机IP>:8443   （首次注册的用户自动成为管理员）
----------------------------------------------------
 卸载     : bash $([ -n "$TEST_ROOT" ] && echo "$(dirname "$(readlink -f "$0")")" || echo /opt/frpilot)/uninstall.sh
 更新     : bash /opt/frpilot/update.sh <新二进制或下载URL>
 端口     : 仅需在云平台安全组放行 8443
====================================================
EOF

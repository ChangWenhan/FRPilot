# FRPilot

> FRP 全栈监控平台 —— 面向 frps/frpc 穿透网络的一体化运维控制台

[English](README.md) | [简体中文](README.zh-CN.md)

`FRPilot`（原名 `frp-monitor`）是部署在 **frps 服务器**上的轻量级监控与运维工具。它自动发现所有接入的 frpc 机器，通过 SSH 隧道采集系统/GPU/安全/定时任务/端口等指标，并提供流量带宽流向分析、一键体检、一键清理与 AI 诊断能力。单二进制、零外部依赖、嵌入式响应式 Web UI，同时提供 `frpm` 命令行工具。

## 特性

- **自动发现**：通过 frps dashboard API 自动列出所有 frpc 机器（含隧道端口），无需手工添加
- **token 基线保护**：自动从 frps 配置文件读取 auth token 并设为该部署的只读基线，全链路漂移检测（防止改错 token 导致 frpc 集体掉线）
- **机器生命周期管理**：自动发现 → 待配置 → 填写 SSH 凭据（AES 加密存储）→ 启用监控；未配置凭据的机器只展示、绝不探测
- **容错采集**：CPU（差分计算）/ 内存 / 磁盘 / GPU（nvidia-smi）/ 负载 / 网卡速率，30 秒快采 + 5 分钟慢采（安全软件、定时任务、端口）；单模块失败不影响其他模块，安全软件未安装显示提示而非报错
- **安全状态**：ClamAV（含病毒库新鲜度）/ CrowdSec（拦截决策数）/ fail2ban（封禁数）/ rkhunter / UFW 逐项体检
- **定时任务**：所有用户 crontab + /etc/crontab + cron.d + cron.daily 等 + systemd timers 统一展示
- **流量与带宽流向**：每台 frpc 的累计流量/实时速率/占比排行，自动标记"带宽主要流向"与速率突增异常
- **一键清理**：内置 5 项安全命令集（内存缓存/APT 缓存/日志压缩/用户缓存/临时文件），风险分级、dry-run 预览、支持自定义命令追加，执行前人工确认
- **一键体检**：基于实时快照的阈值体检（CPU/内存/磁盘/GPU 温度与显存/安全软件/隧道连通），评分制（fail 扣 10 分、warn 扣 3 分；≥90 健康、≥70 需关注、其余异常）+ 历史报告存档
- **AI 诊断**（OpenAI 兼容协议）：对体检报告逐项输出原因分析与修复思路；**只诊断、不执行**——系统提示词强制禁止命令输出，后端对疑似命令内容自动标注"仅供参考"
- **用户系统**：强制登录，首个注册用户自动成为管理员；注册模式可配（开放/审批/关闭）；两级权限（管理员/普通用户）；bcrypt 哈希、防爆破锁定、完整审计日志
- **响应式 Web UI**：电脑/手机自适应，可"添加到主屏幕"当应用使用
- **CLI**：`frpm` 命令行与 Web 共用同一套 API、权限与审计

## 架构

```
浏览器 / frpm CLI
      │  HTTP(S)
      ▼
frpilot（单二进制，部署在 frps 服务器）
 ├─ 嵌入式 Web UI（Vue3 + ECharts）
 ├─ frps dashboard API 客户端 ──► 自动发现机器 / 流量统计
 ├─ SSH 采集器（x/crypto/ssh）──► frps 本机 + 经 frp 隧道跳转各 frpc
 ├─ 操作执行器（一键清理 / 一键体检）
 ├─ AI 诊断客户端（OpenAI 兼容 chat/completions）
 └─ SQLite（WAL 模式，30 天数据自动保留清理）
```

数据流：

1. **发现与流量**：frps dashboard API（默认 `127.0.0.1:8000`，自动检测）
2. **机器指标**：SSH 直连 frps 本机 + 经隧道端口（`frps-IP:remotePort`）跳转每台 frpc
3. **动作执行**：清理/体检复用同一 SSH 通道，全部操作写入审计日志

## 快速开始

### 前置条件

- Linux（frps 所在服务器）
- 安装时需要 root 权限
- 仅需开放 **一个端口**（默认 8443）

### 安装

```bash
# 1. 将二进制与脚本上传到 frps 服务器
scp frpilot install.sh uninstall.sh update.sh root@<frps-IP>:/tmp/frpm/

# 2. 安装（自动创建专用系统用户、systemd 服务、防火墙放行）
ssh root@<frps-IP> 'bash /tmp/frpm/install.sh /tmp/frpm/frpilot'

# 3. 在云平台安全组放行 TCP 8443（阿里云等）
```

安装完成后访问 `http://<frps-IP>:8443`，**第一个注册的账号自动成为管理员**。

### 首次配置（管理员）

1. **设置** → 填写 frps 的 SSH 连接信息（主机/端口/用户/密码）
2. 点击 **「一键自动检测 frps 配置」** → 自动读取 dashboard 地址、认证信息与 token 基线
3. **机器** → 点击「重新扫描 frps」→ 自动发现全部 frpc 机器
4. 为每台机器填写 SSH 凭据（相同凭据可逐台复用）→ 点击 **「启用监控」**

启用后 30 秒开始采集系统指标，5 分钟内补齐安全软件/定时任务/端口数据。

## 使用

### Web 界面

| 页面 | 功能 |
|------|------|
| 总览 | 机器统计、frps 状态、token 基线、最近在线 |
| 机器 | 机器列表、SSH 凭据配置、监控开关、24h 趋势图、磁盘/安全/定时任务/端口详情 |
| 流量 | 带宽流向 Top 图、实时速率、24h 趋势、异常突增标记 |
| 操作 | 一键清理（预览→确认→执行）、一键体检（报告+历史）、AI 诊断、执行记录 |
| 设置 | frps 连接、token 基线、注册模式、体检阈值、自定义清理命令、AI Provider |

### CLI（frpm）

```bash
frpm login --user <用户名>            # 首次登录，token 保存在 ~/.config/frpmon/
frpm status                           # 总览：机器统计 / frps 状态 / token 基线
frpm machines list                    # 机器列表与状态
frpm machines enable <id|name>        # 启用监控（disable 停用）
frpm show <id|name>                   # 机器快照：CPU/内存/磁盘/GPU
frpm security <id|name>               # 安全软件状态
frpm crontab <id|name>                # 定时任务列表
frpm ports <id|name>                  # 端口开放情况
frpm traffic                          # 流量统计与带宽流向
frpm health <id|name>                 # 一键体检
frpm cleanup <id|name> [--items a,b]  # 一键清理（默认预览，--execute 执行）
frpm diagnose <id|name>               # AI 诊断
frpm tasks                            # 清理任务执行结果
frpm settings detect-frps             # 自动检测 frps 配置
frpm settings verify-token            # 校验 token 基线一致性
frpm audit                            # 审计日志（管理员）
frpm version
```

所有命令支持 `--json` 输出，便于脚本化。

## 配置

配置文件：`/etc/frpilot/config.json`（安装时生成，设置页热修改，无需重启）

| 配置项 | 说明 | 默认 |
|--------|------|------|
| `listenAddr` | 监听地址 | `0.0.0.0:8443` |
| `registration` | 注册模式：`open` / `approval` / `closed` | `open` |
| `frps.token` | **token 基线（只读）**：自动检测或手动设置，漂移即告警 | 自动检测 |
| `health.*` | 体检阈值（CPU/内存/磁盘/GPU 温度显存/病毒库天数） | 见下方 |
| `cleanupCustom` | 自定义清理命令（名称/描述/命令/风险等级） | 空 |
| `ai.*` | AI Provider（地址/模型/超时），API Key 加密存储 | 关闭 |

默认体检阈值：CPU 警告/异常 70/85%、内存 80/90%、磁盘 75/85%、GPU 温度 75/85°C、GPU 显存 85/95%、ClamAV 病毒库 7 天、快照过期 10 分钟。

内置清理项：`page_cache`（内存页缓存，低危）、`apt_cache`（APT 缓存，低危）、`journal`（journald 压缩至 100M，中危）、`user_cache`（~/.cache，中危）、`tmp_files`（/tmp 超过 1 天的临时文件，高危）。

## AI 诊断配置

1. 设置 → AI 诊断 → 开启
2. 填写 OpenAI 兼容 Provider 地址（如 `https://api.deepseek.com`）、模型名与 API Key
3. 在「操作」页对体检报告点击 **🤖 AI 诊断分析**

> 安全边界：AI 仅输出诊断文字。系统提示词禁止其输出命令，后端对疑似命令内容自动标注；**系统中不存在任何由 AI 触发的执行路径**。

## 安全模型

- 强制登录；密码 bcrypt 哈希；同一用户名连续失败 5 次锁定 10 分钟
- 会话 token 同时支持 Cookie 与 `Authorization: Bearer`（兼容浏览器对 HTTP 站点 Cookie 的限制）
- 机器 SSH 凭据 AES-GCM 加密存储，密钥文件与数据库分离
- 全部敏感操作（登录/注册/清理/体检/设置修改/诊断）写入审计日志，可追溯操作者
- systemd 安全加固：专用无登录用户、`ProtectSystem=strict`、`NoNewPrivileges`、私有临时目录等
- token 基线只读：frps/frpc 配置中的 token 被修改时自动告警

## 运维

```bash
# 状态
systemctl status frpilot

# 更新（支持本地文件或 URL，旧版自动备份，失败自动回滚）
bash /opt/frpilot/update.sh /path/to/new/frpilot
bash /opt/frpilot/update.sh https://example.com/frpilot

# 卸载（完整清理；--keep-data 保留数据）
bash /opt/frpilot/uninstall.sh
bash /opt/frpilot/uninstall.sh --keep-data
```

## 安装足迹

```
/opt/frpilot/          程序目录（二进制 + 运维脚本）
/etc/frpilot/          配置（含可选自签名证书）
/var/lib/frpilot/      数据（SQLite + 加密密钥）
/etc/systemd/system/       frpilot.service
```

**不修改 frps 的任何配置文件**（frps.ini/frps.toml 仅只读校验），卸载后无系统残留。

## 性能

- SQLite WAL 模式；指标/流量/审计/体检/AI 诊断历史统一 30 天保留自动清理
- 全负载实测（采集循环 + 流量轮询 + 并发 API）：**内存约 15MB、CPU 占用 < 1%**
- 采集频率：快速指标 30s、慢速模块（安全/定时任务/端口）5min、流量轮询 30s

## 开发

```bash
# 后端
go build -o frpilot .          # 需要 Go 1.22+
go test ./...

# 前端（构建产物自动嵌入二进制）
cd webui
npm install
npm run dev                        # 开发模式（/api 代理到 127.0.0.1:8443）
npm run build                      # 构建后重新编译后端即嵌入

# 端到端浏览器回归（可选）
npm install playwright             # 安装 Playwright 后编写脚本：
                                   # 注册 → 登录 → 逐一访问全部页面检查 JS 错误
```

### 项目结构

```
internal/
├── config/      配置加载 / 热修改 / token 基线
├── store/       SQLite 存储（用户/会话/机器/指标/流量/审计/体检/AI 诊断）
├── auth/        用户系统（注册/审批/权限/防爆破）
├── frpsapi/     frps dashboard API 客户端
├── sshx/        SSH 密码认证与命令执行
├── collector/   采集器（解析器 + 循环编排，模块级容错）
├── traffic/     流量轮询 / 带宽流向 / 异常检测
├── actions/     一键清理（任务模型）/ 一键体检（阈值评分）
├── ai/          AI 诊断（OpenAI 兼容客户端 + 命令内容检测）
├── registry/    机器注册表（发现/凭据/监控开关）
├── web/         HTTP API + 嵌入式前端
└── cli/         frpm 命令行（瘦客户端，复用服务端权限与审计）
deploy/          install.sh / uninstall.sh / update.sh / systemd 单元
webui/           Vue3 + Vite + ECharts 前端
```

## 常见问题

**Q：浏览器提示"连接不安全"/页面加载不出来？**
默认使用 HTTP。若浏览器开启了 HTTPS-First 模式（Chrome「始终使用安全连接」），请在 `chrome://settings/security` 关闭；或使用 `INSTALL_TLS=1` 安装启用 HTTPS（自签名证书，需在浏览器中放行/导入信任）。

**Q：8443 端口通了但页面渲染异常？**
先 `Cmd/Ctrl+Shift+R` 硬刷新清除旧缓存——前端资源带内容 hash 指纹，浏览器缓存的旧 index.html 可能引用了已删除的文件。

**Q：token 被修改后所有 frpc 掉线？**
`frpm settings verify-token` 会检测与基线的漂移。基线为部署时设定，如确实更换了 token 请重新执行「自动检测」。

**Q：机器显示"未配置凭据"？**
在「机器」页填写该机器的 SSH 用户/密码并点击「启用监控」。凭据仅加密存储在本机，软件不会修改被监控机器上的任何内容。

**Q：如何用手机访问？**
直接访问同一地址，界面自动适配；浏览器「添加到主屏幕」即可当应用使用。

## License

[MIT](LICENSE)

# FRPilot

> Full-stack monitoring & operations console for FRP (frps/frpc) tunnel networks.

[English](README.md) | [简体中文](README.zh-CN.md)

`FRPilot` (formerly `frp-monitor`) is a lightweight monitoring and operations tool deployed on your **frps server**. It automatically discovers every frpc machine connected to the tunnel, collects system / GPU / security / cron / port metrics over SSH, and provides traffic bandwidth analysis, one-click health checks, one-click cache cleanup and AI-powered diagnosis. A single static binary with an embedded responsive web UI, plus a `frpm` CLI.

## Features

- **Automatic discovery** — enumerates all frpc machines (with their tunnel ports) via the frps dashboard API; no manual registration needed
- **Token baseline protection** — auto-reads the `auth.token` from the frps config file as a read-only baseline for the deployment, with drift detection across the fleet (prevents accidental token changes from dropping every frpc client)
- **Machine lifecycle management** — auto-discovered → pending → fill in SSH credentials (AES-encrypted at rest) → enable monitoring; machines without credentials are listed but never probed
- **Per-machine sudo password** — configure a sudo password per machine; collection and actions automatically elevate via `sudo -S` when the SSH user is not root, so root-only commands are no longer skipped
- **Fault-tolerant collection** — CPU (delta-based) / memory / disk / GPU (nvidia-smi) / load / NIC rates every 30s, plus slow modules (security, cron, ports) every 5min; a failing module never affects the others; missing security software shows a hint instead of an error
- **Security posture** — ClamAV (incl. signature freshness) / CrowdSec (decision count) / fail2ban (banned count) / rkhunter / UFW checked per machine
- **Scheduled tasks** — user crontabs, `/etc/crontab`, `/etc/cron.d/*`, cron.hourly/daily/weekly/monthly scripts and systemd timers in one view
- **Traffic & bandwidth direction** — cumulative traffic, live rates and share percentages per frpc; highlights which machine is consuming the bandwidth and flags rate spikes as anomalies
- **Bandwidth control (QoS)** — per-machine bandwidth balancing directly on the frps server via `tc` shaping (no frpc config changes, no session drops): **auto mode** fairly divides the egress/ingress budget among actively-used machines (a machine counts as one no matter how many SSH sessions, threshold + hysteresis to avoid flapping), **manual mode** sets fixed caps per machine; directions with no budget are left untouched; rules are cleaned up automatically on shutdown
- **One-click cleanup** — 5 built-in safe command sets (page cache / APT cache / journal vacuum / user caches / temp files) with risk levels, dry-run preview, custom command extension and human confirmation before execution; root-required items auto-elevate when a sudo password is configured
- **Security suite (health check + virus scan, one entry)** — threshold-based health assessment over the latest snapshot (CPU/memory/disk/GPU temp & VRAM/security/tunnel reachability) with scored reports (fail −10, warn −3; ≥90 pass, ≥70 warn, else fail) and stored history; virus scan runs the tools installed on each machine: ClamAV quick (common dirs) / full (/) / **signature update (freshclam)** plus rkhunter/chkrootkit rootkit checks; asynchronous background tasks with real-time per-directory progress
- **AI diagnosis** (OpenAI-compatible API) — explains every warn/fail item and suggests remediation steps in plain text; **diagnosis only, never executes** — the system prompt forbids commands and server-side heuristics flag any command-like content as "for reference only"
- **User system** — mandatory login; the first registered user becomes admin; configurable registration (open / approval / closed); two roles (admin / user); bcrypt hashing, account+IP brute-force lockout and a full audit trail
- **Live task progress** — cleanup/scan tasks show an animated progress bar and the current phase (e.g. "scanning /home (2/7)") in the task records view
- **Responsive web UI** — shadcn-inspired design system (unified control heights, strict alignment, ellipsis truncation, dropdown action menus); desktop and mobile friendly; installable as a home-screen app
- **CLI** — `frpm` shares the same API, permissions and audit logging as the web UI

## Architecture

```
Browser / frpm CLI
      │  HTTP(S)
      ▼
FRPilot (single binary, runs on the frps server)
 ├─ Embedded Web UI (Vue3 + ECharts)
 ├─ frps dashboard API client ──► machine discovery / traffic stats
 ├─ SSH collector (x/crypto/ssh) ──► frps host + each frpc via the frp tunnel
 ├─ Action executor (cleanup / security suite: health check + virus scan — async tasks with live progress)
 ├─ AI diagnosis client (OpenAI-compatible chat/completions)
 └─ SQLite (WAL mode, 30-day retention cleanup)
```

Data flows:

1. **Discovery & traffic** — frps dashboard API (default `127.0.0.1:8000`, auto-detected)
2. **Machine metrics** — SSH to the frps host, then hop through tunnel ports (`frps-IP:remotePort`) to each frpc
3. **Actions** — cleanup/health reuse the same SSH channel; every operation is written to the audit log

## Quick Start

### Prerequisites

- Linux (the machine running frps)
- root access during installation
- Only **one port** needs to be opened (default 8443)

### Install

```bash
# 1. Upload the binary and scripts to the frps server
scp frpilot install.sh uninstall.sh update.sh root@<frps-IP>:/tmp/frpm/

# 2. Install (creates a dedicated system user, systemd service, firewall rule)
ssh root@<frps-IP> 'bash /tmp/frpm/install.sh /tmp/frpm/frpilot'

# 3. Open TCP 8443 in your cloud security group (Aliyun etc.)
```

Then visit `http://<frps-IP>:8443`. **The first account to register becomes the administrator.**

### First-run configuration (admin)

1. **Settings** → enter the frps SSH connection info (host / port / user / password)
2. Click **"Auto-detect frps config"** → dashboard URL, credentials and the token baseline are read from the server automatically
3. **Machines** → click "Rescan frps" → all frpc machines appear automatically
4. Fill in SSH credentials for each machine (or repeat the same ones) → for non-root SSH users also set a **sudo password** (root-only collection/cleanup/scan commands auto-elevate) → click **"Enable monitoring"**

System metrics start within 30 seconds; security software, cron and port data are collected within 5 minutes.

## Usage

### Web UI

| Page | Purpose |
|------|---------|
| Machines | machine stats (total / monitoring / pending / frps clients / traffic), token baseline, machine list (SSH credentials + sudo password (⋮ menu), monitoring toggle, last seen), 24h trend charts, disk / security / cron / ports detail |
| Traffic | bandwidth direction chart, live rates, 24h history, anomaly flags |
| Actions | one-click cleanup (preview → confirm → execute), security suite (health report + history / virus scan: ClamAV, rootkit, signature update — live progress), AI diagnosis, task records with progress bars |
| Settings | frps connection, token baseline, registration mode, health thresholds, custom cleanup commands, AI provider |

### CLI (frpm)

```bash
frpm login --user <name>            # first login; token saved to ~/.config/frpmon/
frpm status                         # overview: machines / frps / token baseline
frpm machines list                  # machines and their state
frpm machines set-credentials <id|name> --user <user> --pass <pass> [--sudo-pass <sudo>]
frpm machines enable <id|name>      # enable monitoring (disable to stop)
frpm show <id|name>                 # machine snapshot: CPU/memory/disk/GPU
frpm security <id|name>             # security software status
frpm crontab <id|name>              # scheduled tasks
frpm ports <id|name>                # open ports
frpm traffic                        # traffic stats and bandwidth direction
frpm health <id|name>               # one-click health check
frpm cleanup <id|name> [--items a,b] # cleanup (preview by default, --execute to run)
frpm scan <id|name>... [--mode quick|full|rootkit|update]  # virus scan (default quick; update = refresh ClamAV signatures)
frpm diagnose <id|name>             # AI diagnosis
frpm tasks                          # task results with live progress (% + current phase)
frpm settings detect-frps           # auto-detect frps config
frpm settings verify-token          # verify token baseline consistency
frpm audit                          # audit log (admin)
frpm version
```

Every command supports `--json` output for scripting.

## Configuration

Config file: `/etc/frpilot/config.json` (generated at install; editable from the Settings page — hot-reloaded, no restart needed)

| Key | Description | Default |
|-----|-------------|---------|
| `listenAddr` | listen address | `0.0.0.0:8443` |
| `registration` | `open` / `approval` / `closed` | `open` |
| `frps.token` | **read-only token baseline** — auto-detected or set manually; drift triggers a warning; ciphertext lives in SQLite, never in config.json | auto-detected |
| `health.*` | health thresholds (CPU/memory/disk/GPU temp & VRAM/clamav DB age) | see below |
| `cleanupCustom` | custom cleanup commands (name/description/command/risk) | empty |
| `ai.*` | AI provider (URL/model/timeout); API key stored encrypted | disabled |

Default health thresholds: CPU warn/fail 70/85, memory 80/90, disk 75/85, GPU temperature 75/85 °C, GPU VRAM 85/95 %, ClamAV database 7 days, snapshot freshness 10 minutes.

Built-in cleanup items: `page_cache` (memory caches, low), `apt_cache` (APT archives, low), `journal` (journald vacuum to 100M, mid), `user_cache` (`~/.cache`, mid), `tmp_files` (/tmp files older than 1 day, high).

## AI Diagnosis

1. Settings → AI Diagnosis → enable
2. Enter an OpenAI-compatible provider URL (e.g. `https://api.deepseek.com`), model name and API key
3. On the Actions page, click **🤖 AI Diagnose** next to a health report

> Safety boundary: AI outputs analysis text only. The system prompt forbids command output, server-side heuristics flag command-like content, and **no execution path driven by AI exists anywhere in the system**.

## Security Model

- Mandatory login; bcrypt-hashed passwords; failed-login counters persisted in the database — 5 failures per account or 15 per source IP lock for 10 minutes (15-minute window), surviving restarts
- Browser sessions use an **HttpOnly session cookie** (XSS-resistant); the bearer token is no longer stored in localStorage. The CLI opts in via the `X-FRPilot-Client: cli` header to receive a bearer token
- Sensitive configuration (frps dashboard/SSH passwords, token baseline, AI API key) is AES-GCM encrypted in SQLite; **config.json never holds plaintext credentials** (legacy values are migrated automatically on upgrade)
- Machine SSH credentials and sudo passwords are AES-GCM encrypted at rest; the key file is separate from the database; APIs return masks only, never plaintext
- Changing a password revokes all of that user's old sessions; security response headers enabled (nosniff / X-Frame-Options / Referrer-Policy, etc.)
- All sensitive operations (login/register/cleanup/health/scan/settings/diagnosis) are written to the audit log with the operator identity
- systemd hardening: dedicated no-login user, `ProtectSystem=strict`, `NoNewPrivileges`, private tmp, etc.
- Read-only token baseline: changing the token in frps/frpc configs triggers a drift warning

## Operations

```bash
# Status
systemctl status frpilot

# Update (local file or URL; old binary auto-backed up, auto-rollback on failure)
bash /opt/frpilot/update.sh /path/to/new/frpilot
bash /opt/frpilot/update.sh https://example.com/frpilot

# Uninstall (full cleanup; --keep-data keeps the database)
bash /opt/frpilot/uninstall.sh
bash /opt/frpilot/uninstall.sh --keep-data
```

## Install Footprint

```
/opt/frpilot/          program directory (binary + ops scripts)
/etc/frpilot/          configuration (plus optional self-signed cert)
/var/lib/frpilot/      data (SQLite + encryption key)
/etc/systemd/system/       frpilot.service
```

**frps configuration files are never modified** (frps.ini/frps.toml are only read for verification); uninstalling leaves no system residue.

## Performance

- SQLite in WAL mode; metrics/traffic/audit/health/AI-history unified 30-day retention cleanup
- Measured under full load (collection loop + traffic polling + concurrent API): **~15 MB RSS, <1 % CPU**
- Collection cadence: fast metrics 30s, slow modules 5min, traffic polling 30s

## Development

```bash
# Backend
go build -o frpilot .          # requires Go 1.22+
go test ./...

# Frontend (build output is embedded into the binary)
cd webui
npm install
npm run dev                        # dev mode (proxies /api to 127.0.0.1:8443)
npm run build                      # rebuild, then recompile the backend to embed

# End-to-end browser regression (optional)
npm install playwright             # install Playwright, then run a script that
                                   # registers, logs in and visits every page
```

### Project layout

```
internal/
├── config/      config load / hot reload / token baseline
├── store/       SQLite (users/sessions/machines/metrics/traffic/audit/health/AI)
├── auth/        user system (register/approve/roles/lockout)
├── frpsapi/     frps dashboard API client
├── sshx/        SSH password auth and command execution
├── collector/   collectors (parsers + loop orchestration, module-level tolerance)
├── traffic/     traffic polling / bandwidth direction / anomaly detection
├── actions/     one-click cleanup (task model) / health check (scored)
├── ai/          AI diagnosis (OpenAI-compatible client + command detection)
├── registry/    machine registry (discovery/credentials/monitoring toggle)
├── web/         HTTP API + embedded frontend
└── cli/         frpm CLI (thin client, reuses server-side auth & audit)
deploy/          install.sh / uninstall.sh / update.sh / systemd unit
webui/           Vue3 + Vite + ECharts frontend
```

## FAQ

**Q: The browser shows a "connection not secure" warning / the page won't load.**
HTTP is the default. If your browser enforces HTTPS-First mode (Chrome "Always use secure connections"), turn it off in `chrome://settings/security`; or install with `INSTALL_TLS=1` to enable HTTPS with a self-signed certificate (accept/import the cert in the browser).

**Q: Port 8443 is reachable but the page renders incorrectly.**
Hard-refresh (`Cmd/Ctrl+Shift+R`) to clear stale cache — frontend assets are content-hashed and an old cached `index.html` may reference removed files.

**Q: All frpc clients dropped after a token change?**
`frpm settings verify-token` detects drift against the baseline. The baseline is set at deployment; if the token was intentionally changed, re-run "Auto-detect".

**Q: A machine shows "no credentials"?**
Fill in the SSH user/password on the Machines page and click "Enable monitoring". Credentials are encrypted locally and the software never modifies anything on the monitored machines.

**Q: Root-only operations (cleanup/scan) fail under a non-root SSH user?**
Configure a **sudo password** for that machine in "Machines → edit credentials" (or use root as the SSH user). Once set, collection, cleanup and virus-scan commands that need root automatically elevate via `sudo -S` instead of being skipped.

**Q: How long does a virus scan take?**
Quick scan (7 common directories) typically 10-30 minutes; full scan can take hours depending on the machine; rootkit check takes about 2-10 minutes; the "update signatures" mode refreshes via freshclam in roughly 1-10 minutes. Tasks run in the background — the task records view refreshes every 3 seconds and shows a live progress bar with the current directory being scanned. If a tool isn't installed, the task reports "ClamAV not detected" instead of failing; running the signature update before a scan is recommended so an outdated database doesn't cause missed detections.

**Q: How do I use it on my phone?**
Visit the same URL — the UI adapts automatically; use the browser's "Add to Home Screen" for an app-like experience.

## License

[MIT](LICENSE)

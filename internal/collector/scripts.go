package collector

// fastScript 快速指标采集（每次循环执行）：
// 系统信息 + CPU 统计 + 内存 + 磁盘 + 网卡流量计数器。
const fastScriptText = `
echo "===HOSTNAME==="; hostname 2>/dev/null
echo "===UPTIME==="; uptime 2>/dev/null || cat /proc/uptime 2>/dev/null
echo "===OS==="; grep '^PRETTY_NAME' /etc/os-release 2>/dev/null || echo unknown
echo "===KERNEL==="; uname -r 2>/dev/null
echo "===CPUCORES==="; nproc 2>/dev/null || grep -c '^processor' /proc/cpuinfo 2>/dev/null
echo "===LOAD==="; cat /proc/loadavg 2>/dev/null
echo "===MEM==="; grep -E '^(MemTotal|MemFree|MemAvailable):' /proc/meminfo 2>/dev/null
echo "===STAT==="; grep '^cpu ' /proc/stat 2>/dev/null
echo "===DISK==="; df -kP 2>/dev/null | grep -vE '^(tmpfs|devtmpfs|overlay|shm|udev)'
echo "===NET==="; cat /proc/net/dev 2>/dev/null
`

func fastScript() string { return fastScriptText }

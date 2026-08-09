package cli

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"
)

// Version 由 main 注入（frpm version / 部署脚本读取）。
var Version = "dev"

// newHTTP 构造 CLI 的 HTTP 客户端。
func newHTTP() *http.Client {
	return &http.Client{Timeout: 30 * time.Second}
}

// Run CLI 入口。args 为去掉程序名后的参数。
func Run(args []string) int {
	if len(args) == 0 {
		usage()
		return 0
	}
	server := os.Getenv("FRPMON_SERVER")
	if server == "" {
		server = "http://127.0.0.1:8443"
	}
	c := &Client{Server: server, HTTP: newHTTP()}

	cmd, rest := args[0], args[1:]
	jsonOut := false
	for i, a := range rest {
		if a == "--json" {
			jsonOut = true
			rest = append(rest[:i], rest[i+1:]...)
			break
		}
	}

	var err error
	switch cmd {
	case "login":
		err = cmdLogin(c, rest)
	case "status":
		err = cmdStatus(c, jsonOut)
	case "machines", "machine":
		err = cmdMachines(c, rest, jsonOut)
	case "show":
		err = cmdShow(c, rest, jsonOut)
	case "security":
		err = cmdSecurity(c, rest, jsonOut)
	case "crontab":
		err = cmdCrontab(c, rest, jsonOut)
	case "ports":
		err = cmdPorts(c, rest, jsonOut)
	case "traffic":
		err = cmdTraffic(c, rest, jsonOut)
	case "cleanup":
		err = cmdCleanup(c, rest, jsonOut)
	case "health":
		err = cmdHealth(c, rest, jsonOut)
	case "tasks":
		err = cmdTasks(c, rest, jsonOut)
	case "diagnose":
		err = cmdDiagnose(c, rest, jsonOut)
	case "discover":
		err = cmdDiscover(c, jsonOut)
	case "settings", "config":
		err = cmdSettings(c, rest, jsonOut)
	case "users":
		err = cmdUsers(c, rest, jsonOut)
	case "audit", "logs":
		err = cmdAudit(c, rest, jsonOut)
	case "version", "--version", "-V":
		fmt.Println(Version)
		return 0
	case "help", "-h", "--help":
		usage()
		return 0
	default:
		fmt.Fprintf(os.Stderr, "未知命令: %s\n\n", cmd)
		usage()
		return 2
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "错误:", err)
		return 1
	}
	return 0
}

func usage() {
	fmt.Print(`FRPilot 命令行工具 (frpm)

用法:
  frpm login [--server URL] [--user NAME]     登录并保存会话令牌
  frpm status                                  总览：机器/隧道/frps 状态
  frpm machines list                           列出全部机器与状态
  frpm machines set-credentials <id|name> --user <ssh用户> --pass <密码>
  frpm machines enable <id|name>               启用监控
  frpm machines disable <id|name>              停用监控
  frpm show <id|name>                          机器最新快照（CPU/内存/磁盘/GPU）
  frpm security <id|name>                      安全软件状态
  frpm crontab <id|name>                       定时任务列表
  frpm ports <id|name>                         端口开放情况
  frpm traffic [--hours N]                     流量统计与带宽流向（Top 机器/占比/异常）
  frpm cleanup <id|name> [--items a,b]         一键清理（默认预览，--execute 才执行）
  frpm health <id|name>                        一键体检（基于最新采集数据）
  frpm health --reports                        最近体检报告
  frpm tasks                                   查看清理任务执行结果
  frpm diagnose <id|name>                      AI 诊断（分析体检报告，不执行任何操作）
  frpm discover                                从 frps 重新扫描发现机器
  frpm settings show                           查看设置（token 基线状态）
  frpm settings test-frps                      测试 frps dashboard 连接
  frpm settings detect-frps                   一键自动读取 frps 全部配置（含 token 基线）
  frpm settings verify-token                   校验 frps token 与基线一致性
  frpm users list                              用户列表（管理员）
  frpm audit [--limit N]                       审计日志（管理员）
  frpm help                                    显示帮助

通用选项:
  --json       JSON 输出（脚本友好）
  --server     API 地址（默认 http://127.0.0.1:8443，或环境变量 FRPMON_SERVER）
`)
}

func printJSON(v any) {
	b, _ := json.MarshalIndent(v, "", "  ")
	fmt.Println(string(b))
}

func cmdLogin(c *Client, rest []string) error {
	user := ""
	for i := 0; i < len(rest); i++ {
		switch rest[i] {
		case "--user":
			if i+1 < len(rest) {
				user = rest[i+1]
				i++
			}
		case "--server":
			if i+1 < len(rest) {
				c.Server = rest[i+1]
				i++
			}
		}
	}
	if user == "" {
		fmt.Fprint(os.Stderr, "SSH 用户名: ")
		fmt.Scanln(&user)
		if user == "" {
			return fmt.Errorf("用户名不能为空")
		}
	}
	fmt.Fprint(os.Stderr, "密码: ")
	var pass string
	fmt.Scanln(&pass)
	token, err := c.Login(user, pass)
	if err != nil {
		return err
	}
	if err := SaveToken(token); err != nil {
		return err
	}
	fmt.Println("登录成功，令牌已保存到", TokenPath())
	return nil
}

func cmdStatus(c *Client, jsonOut bool) error {
	st, err := c.Status()
	if err != nil {
		return err
	}
	if jsonOut {
		printJSON(st)
		return nil
	}
	fmt.Println("== FRPilot 总览 ==")
	if m, ok := st["machines"].(map[string]any); ok {
		fmt.Printf("机器: 总计 %v | 待配置 %v | 已配置 %v | 监控中 %v | 停用 %v\n",
			m["total"], m["pending"], m["configured"], m["enabled"], m["disabled"])
	}
	if st["tokenSet"] == true {
		fmt.Println("token 基线: 已设置（原文不回显，只读）")
	} else {
		fmt.Println("token 基线: 未设置")
	}
	if frps, ok := st["frps"].(map[string]any); ok {
		fmt.Printf("frps: 版本 %v | 在线客户端 %v | 入流量 %s | 出流量 %s\n",
			frps["version"], frps["clients"], fmtBytes(frps["trafficIn"]), fmtBytes(frps["trafficOut"]))
	} else {
		fmt.Println("frps: 未配置或不可达")
	}
	return nil
}

func fmtBytes(v any) string {
	n, ok := v.(float64)
	if !ok {
		return "-"
	}
	units := []string{"B", "KB", "MB", "GB", "TB"}
	i := 0
	for n >= 1024 && i < len(units)-1 {
		n /= 1024
		i++
	}
	if i == 0 {
		return fmt.Sprintf("%.0f %s", n, units[i])
	}
	return fmt.Sprintf("%.1f %s", n, units[i])
}

func cmdMachines(c *Client, rest []string, jsonOut bool) error {
	sub := "list"
	if len(rest) > 0 {
		sub = rest[0]
		rest = rest[1:]
	}
	switch sub {
	case "list":
		ms, err := c.Machines()
		if err != nil {
			return err
		}
		if jsonOut {
			printJSON(ms)
			return nil
		}
		fmt.Printf("%-4s %-22s %-10s %-8s %-12s\n", "ID", "名称", "隧道端口", "状态", "凭据")
		for _, m := range ms {
			fmt.Printf("%-4v %-22s %-10v %-8s %-12v\n",
				m["id"], m["name"], m["tunnelPort"], m["status"], m["hasCredentials"])
		}
		return nil
	case "set-credentials":
		if len(rest) < 1 {
			return fmt.Errorf("用法: frpm machines set-credentials <id|name> --user <用户> --pass <密码>")
		}
		target, err := resolveID(c, rest[0])
		if err != nil {
			return err
		}
		user, pass := "", ""
		for i := 0; i < len(rest); i++ {
			switch rest[i] {
			case "--user":
				if i+1 < len(rest) {
					user = rest[i+1]
					i++
				}
			case "--pass":
				if i+1 < len(rest) {
					pass = rest[i+1]
					i++
				}
			}
		}
		if user == "" || pass == "" {
			return fmt.Errorf("需要 --user 和 --pass")
		}
		if err := c.SetCredentials(target, user, pass); err != nil {
			return err
		}
		fmt.Println("凭据已保存，可执行 enable 启用监控")
		return nil
	case "enable", "disable":
		if len(rest) < 1 {
			return fmt.Errorf("用法: frpm machines %s <id|name>", sub)
		}
		target, err := resolveID(c, rest[0])
		if err != nil {
			return err
		}
		if err := c.SetEnabled(target, sub == "enable"); err != nil {
			return err
		}
		fmt.Println("已" + map[bool]string{true: "启用", false: "停用"}[sub == "enable"] + "监控")
		return nil
	default:
		return fmt.Errorf("未知子命令: %s", sub)
	}
}

func resolveID(c *Client, target string) (string, error) {
	// 支持数字 id 或机器名
	if strings.TrimSpace(target) == "" {
		return "", fmt.Errorf("目标不能为空")
	}
	if isDigits(target) {
		return target, nil
	}
	ms, err := c.Machines()
	if err != nil {
		return "", err
	}
	for _, m := range ms {
		if m["name"] == target {
			return fmt.Sprintf("%v", m["id"]), nil
		}
	}
	return "", fmt.Errorf("找不到机器: %s", target)
}

func isDigits(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return s != ""
}

// cmdShow 展示机器最新快照（总览 + 系统指标）。
func cmdShow(c *Client, rest []string, jsonOut bool) error {
	if len(rest) < 1 {
		return fmt.Errorf("用法: frpm show <id|name>")
	}
	target, err := resolveID(c, rest[0])
	if err != nil {
		return err
	}
	snap, err := c.Snapshot(target)
	if err != nil {
		return err
	}
	if jsonOut {
		printJSON(snap)
		return nil
	}
	data, _ := snap["data"].(map[string]any)
	m, _ := snap["machine"].(map[string]any)
	if data == nil {
		fmt.Printf("机器 %v: 暂无采集数据（状态 %v，监控中: %v）\n", m["name"], m["status"], snap["collecting"])
		return nil
	}
	sys, _ := data["sys"].(map[string]any)
	fmt.Printf("== %v ==\n", m["name"])
	fmt.Printf("主机名: %v | OS: %v | 内核: %v\n", data["hostname"], sys["os"], sys["kernel"])
	fmt.Printf("CPU: %v%% (负载 %v) | 内存: %v | 磁盘最大: %v\n",
		sys["cpuPct"], sys["load1"], memPctOf(sys), diskPctOf(sys))
	if g, ok := data["gpu"].(map[string]any); ok && g["present"] == true {
		fmt.Printf("GPU: %v | 利用率 %v%% | 显存 %v/%v MiB | 温度 %v°C\n",
			g["name"], g["util"], g["memUsedMiB"], g["memTotalMiB"], g["tempC"])
	} else {
		fmt.Println("GPU: 无 GPU 或不支持")
	}
	fmt.Printf("隧道连通: %v | 采集时间: %v\n", data["tunnelUp"], data["ts"])
	return nil
}

func memPctOf(sys map[string]any) any {
	if sys == nil {
		return "-"
	}
	total, _ := sys["memTotalMB"].(float64)
	avail, _ := sys["memAvailMB"].(float64)
	if total <= 0 {
		return "-"
	}
	return fmt.Sprintf("%.1f%%", (total-avail)/total*100)
}

func diskPctOf(sys map[string]any) any {
	disks, _ := sys["disk"].([]any)
	maxPct := -1.0
	mount := ""
	for _, raw := range disks {
		d, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		p, _ := d["usePct"].(float64)
		if p > maxPct {
			maxPct = p
			mount, _ = d["mount"].(string)
		}
	}
	if maxPct < 0 {
		return "-"
	}
	return fmt.Sprintf("%.1f%% (%s)", maxPct, mount)
}

func cmdSecurity(c *Client, rest []string, jsonOut bool) error {
	if len(rest) < 1 {
		return fmt.Errorf("用法: frpm security <id|name>")
	}
	target, err := resolveID(c, rest[0])
	if err != nil {
		return err
	}
	snap, err := c.Snapshot(target)
	if err != nil {
		return err
	}
	data, _ := snap["data"].(map[string]any)
	if jsonOut {
		printJSON(data["security"])
		return nil
	}
	sec, _ := data["security"].([]any)
	if len(sec) == 0 {
		fmt.Println("暂无安全软件数据（监控未启用或尚未采集）")
		return nil
	}
	fmt.Printf("%-14s %-8s %-10s %s\n", "软件", "已安装", "服务状态", "详情")
	for _, raw := range sec {
		it := raw.(map[string]any)
		inst := "是"
		if it["installed"] != true {
			inst = "否"
		}
		ver, _ := it["version"].(string)
		det, _ := it["detail"].(string)
		extra := ver
		if det != "" {
			if extra != "" {
				extra += " | "
			}
			extra += det
		}
		fmt.Printf("%-14s %-8s %-10v %s\n", it["name"], inst, it["active"], extra)
	}
	return nil
}

func cmdCrontab(c *Client, rest []string, jsonOut bool) error {
	if len(rest) < 1 {
		return fmt.Errorf("用法: frpm crontab <id|name>")
	}
	target, err := resolveID(c, rest[0])
	if err != nil {
		return err
	}
	snap, err := c.Snapshot(target)
	if err != nil {
		return err
	}
	data, _ := snap["data"].(map[string]any)
	if jsonOut {
		printJSON(data["cron"])
		return nil
	}
	cron, _ := data["cron"].([]any)
	if len(cron) == 0 {
		fmt.Println("暂无定时任务数据")
		return nil
	}
	fmt.Printf("%-20s %-8s %s\n", "来源", "用户", "任务")
	for _, raw := range cron {
		it := raw.(map[string]any)
		fmt.Printf("%-20s %-8v %v\n", it["source"], it["user"], it["line"])
	}
	return nil
}

func cmdPorts(c *Client, rest []string, jsonOut bool) error {
	if len(rest) < 1 {
		return fmt.Errorf("用法: frpm ports <id|name>")
	}
	target, err := resolveID(c, rest[0])
	if err != nil {
		return err
	}
	snap, err := c.Snapshot(target)
	if err != nil {
		return err
	}
	data, _ := snap["data"].(map[string]any)
	if jsonOut {
		printJSON(data["ports"])
		return nil
	}
	ports, _ := data["ports"].([]any)
	if len(ports) == 0 {
		fmt.Println("暂无端口数据（注意：需要 root 权限查看全部端口）")
		return nil
	}
	fmt.Printf("%-10s %s\n", "端口", "进程")
	for _, raw := range ports {
		it := raw.(map[string]any)
		fmt.Printf("%-10v %v\n", it["port"], it["process"])
	}
	return nil
}

// cmdTraffic 展示带宽流向与流量统计。
func cmdTraffic(c *Client, rest []string, jsonOut bool) error {
	hours := 24
	for i := 0; i < len(rest); i++ {
		if rest[i] == "--hours" && i+1 < len(rest) {
			fmt.Sscanf(rest[i+1], "%d", &hours)
			i++
		}
	}
	snap, err := c.Traffic()
	if err != nil {
		return err
	}
	if jsonOut {
		printJSON(snap)
		return nil
	}
	flows, _ := snap["flows"].([]any)
	if len(flows) == 0 {
		fmt.Println("暂无流量数据（frps dashboard 未配置或轮询未开始）")
		return nil
	}
	fmt.Println("== 带宽流向 ==")
	fmt.Printf("%-20s %-14s %-14s %-10s %-10s %-8s %s\n", "机器", "累计入流量", "累计出流量", "入速率", "出速率", "入占比", "状态")
	for _, raw := range flows {
		f := raw.(map[string]any)
		status := ""
		if f["anomaly"] == true {
			status = "⚠ 突增"
		}
		fmt.Printf("%-20s %-14s %-14s %-10s %-10s %-8v %s\n",
			f["proxy"], fmtBytes(f["inBytes"]), fmtBytes(f["outBytes"]),
			fmtRate(f["rateIn"]), fmtRate(f["rateOut"]),
			fmt.Sprintf("%v%%", f["pctIn"]), status)
	}
	if top, ok := snap["top"].(map[string]any); ok && top["proxy"] != nil {
		fmt.Printf("\n带宽主要流向: %s（入 %s + 出 %s）\n",
			top["proxy"], fmtRate(top["rateIn"]), fmtRate(top["rateOut"]))
	}
	if anom, ok := snap["anomalies"].([]any); ok && len(anom) > 0 {
		fmt.Printf("\n⚠ 流量异常: %d 台机器出现速率突增\n", len(anom))
	}
	return nil
}

func fmtRate(v any) string {
	n, ok := v.(float64)
	if !ok {
		return "-"
	}
	return fmtBytes(n) + "/s"
}

// cmdCleanup 一键清理：先预览（dry-run），--execute 才真正执行。
func cmdCleanup(c *Client, rest []string, jsonOut bool) error {
	if len(rest) < 1 {
		return fmt.Errorf("用法: frpm cleanup <id|name> [--items item1,item2] [--execute]")
	}
	target, err := resolveID(c, rest[0])
	if err != nil {
		return err
	}
	items := []string{}
	execute := false
	for i := 1; i < len(rest); i++ {
		switch rest[i] {
		case "--items":
			if i+1 < len(rest) {
				for _, s := range strings.Split(rest[i+1], ",") {
					if s = strings.TrimSpace(s); s != "" {
						items = append(items, s)
					}
				}
				i++
			}
		case "--execute":
			execute = true
		}
	}
	if !execute {
		fmt.Println("== 预览（dry-run，不会执行任何清理）==")
		var resp struct {
			Results []map[string]any `json:"results"`
		}
		if err := c.do("POST", "/api/actions/cleanup/preview",
			map[string]any{"machineIds": []string{target}, "itemIds": items}, &resp); err != nil {
			return err
		}
		for _, r := range resp.Results {
			fmt.Printf("[%s] %s\n%s\n", r["itemName"], r["status"], r["output"])
		}
		fmt.Println("\n确认无误请加 --execute 执行")
		return nil
	}
	var resp struct {
		Task map[string]any `json:"task"`
	}
	if err := c.do("POST", "/api/actions/cleanup",
		map[string]any{"machineIds": []string{target}, "itemIds": items}, &resp); err != nil {
		return err
	}
	fmt.Printf("任务 #%v 已开始执行（可用 frpm tasks 查看结果）\n", resp.Task["id"])
	return nil
}

// cmdHealth 一键体检 + 历史报告。
func cmdHealth(c *Client, rest []string, jsonOut bool) error {
	if len(rest) > 0 && rest[0] == "--reports" {
		var resp struct {
			Reports []map[string]any `json:"reports"`
		}
		if err := c.do("GET", "/api/actions/health/reports?limit=10", nil, &resp); err != nil {
			return err
		}
		if jsonOut {
			printJSON(resp.Reports)
			return nil
		}
		fmt.Printf("%-20s %-14s %-6s %-5s %s\n", "时间", "机器", "评分", "结论", "详情")
		for _, r := range resp.Reports {
			fmt.Printf("%-20v %-14v %-6v %-5v %s\n", r["ts"], r["machine"], r["score"], r["overall"],
				fmt.Sprint(r["itemsJson"])[:60])
		}
		return nil
	}
	if len(rest) < 1 {
		return fmt.Errorf("用法: frpm health <id|name> 或 frpm health --reports")
	}
	target, err := resolveID(c, rest[0])
	if err != nil {
		return err
	}
	var resp struct {
		Report map[string]any `json:"report"`
	}
	if err := c.do("POST", "/api/actions/health/"+target, struct{}{}, &resp); err != nil {
		return err
	}
	rep := resp.Report
	if jsonOut {
		printJSON(rep)
		return nil
	}
	fmt.Printf("== %v 体检报告 ==\n评分: %v/100 | 结论: %v\n\n", rep["machine"], rep["score"], rep["overall"])
	items, _ := rep["items"].([]any)
	for _, raw := range items {
		it := raw.(map[string]any)
		mark := map[string]string{"pass": "✅", "warn": "⚠", "fail": "❌"}[fmt.Sprint(it["status"])]
		fmt.Printf("%s [%-8s] %-28s %s\n", mark, it["category"], it["title"], it["detail"])
	}
	return nil
}

// cmdTasks 查看执行任务。
func cmdTasks(c *Client, rest []string, jsonOut bool) error {
	var resp struct {
		Tasks []map[string]any `json:"tasks"`
	}
	if err := c.do("GET", "/api/actions/tasks?limit=5", nil, &resp); err != nil {
		return err
	}
	if jsonOut {
		printJSON(resp.Tasks)
		return nil
	}
	for _, t := range resp.Tasks {
		fmt.Printf("任务 #%v [%s] %s 于 %v 由 %s 发起\n", t["id"], t["status"], t["type"], t["createdAt"], t["operator"])
		results, _ := t["results"].([]any)
		for _, raw := range results {
			r := raw.(map[string]any)
			mark := map[string]string{"ok": "✅", "failed": "❌", "skipped": "⏭"}[fmt.Sprint(r["status"])]
			fmt.Printf("  %s %-20s %-24s %s\n", mark, r["machine"], r["itemName"], r["duration"])
		}
	}
	return nil
}

// cmdDiagnose 调用 AI 对机器体检报告做诊断分析（只读，不执行任何操作）。
func cmdDiagnose(c *Client, rest []string, jsonOut bool) error {
	if len(rest) < 1 {
		return fmt.Errorf("用法: frpm diagnose <id|name>")
	}
	target, err := resolveID(c, rest[0])
	if err != nil {
		return err
	}
	var resp struct {
		Diagnosis map[string]any `json:"diagnosis"`
	}
	if err := c.do("POST", "/api/ai/diagnose/"+target, struct{}{}, &resp); err != nil {
		return err
	}
	d := resp.Diagnosis
	if jsonOut {
		printJSON(d)
		return nil
	}
	fmt.Printf("== %v AI 诊断（体检 %v/%v）==\n", d["machine"], d["reportScore"], d["reportOverall"])
	if d["flagged"] == true {
		fmt.Println("⚠ 输出疑似包含命令内容（仅供参考，系统不会执行任何操作）")
	}
	fmt.Println(d["text"])
	return nil
}

func cmdDiscover(c *Client, jsonOut bool) error {
	res, err := c.Discover()
	if err != nil {
		return err
	}
	if jsonOut {
		printJSON(res)
		return nil
	}
	fmt.Printf("扫描完成：共 %v 个隧道\n", res["total"])
	if n, ok := res["newFound"].([]any); ok && len(n) > 0 {
		fmt.Println("新增机器:", strings.Join(toStrings(n), ", "))
	} else {
		fmt.Println("新增机器: 无")
	}
	return nil
}

func toStrings(v []any) []string {
	out := make([]string, 0, len(v))
	for _, x := range v {
		out = append(out, fmt.Sprintf("%v", x))
	}
	return out
}

func cmdSettings(c *Client, rest []string, jsonOut bool) error {
	sub := "show"
	if len(rest) > 0 {
		sub = rest[0]
	}
	switch sub {
	case "show":
		s, err := c.Settings()
		if err != nil {
			return err
		}
		if jsonOut {
			printJSON(s)
			return nil
		}
		frps, _ := s["frps"].(map[string]any)
		fmt.Println("== 设置 ==")
		fmt.Printf("注册模式: %v\n", s["registration"])
		fmt.Printf("frps dashboard: %v (用户 %v)\n", frps["dashboardUrl"], frps["dashboardUser"])
		fmt.Printf("frps SSH: %v:%v (%v)\n", frps["sshHost"], frps["sshPort"], frps["sshUser"])
		if frps["tokenSet"] == true {
			fmt.Println("token 基线: 已设置（原文不回显，只读；漂移检测以此为准）")
		} else {
			fmt.Println("token 基线: 未设置（可执行 frpm settings detect-frps 一键自动检测）")
		}
		return nil
	case "test-frps":
		res, err := c.TestFrps()
		if err != nil {
			return err
		}
		if jsonOut {
			printJSON(res)
			return nil
		}
		fmt.Printf("连接成功: frps %v, 在线客户端 %v, 当前连接 %v\n",
			res["version"], res["clients"], res["curConns"])
		return nil
	case "detect-frps":
		var res map[string]any
		if err := c.do("POST", "/api/settings/detect-frps", struct{}{}, &res); err != nil {
			return err
		}
		if jsonOut {
			printJSON(res)
			return nil
		}
		fmt.Printf("自动检测完成：\n  配置路径: %v\n  bindPort: %v\n  dashboardPort: %v\n  dashboardUser: %v\n  token 基线: %v\n",
			res["configPath"], res["bindPort"], res["dashboardPort"], res["dashboardUser"], map[bool]string{true: "已安全保存", false: "未设置"}[res["tokenSet"] == true])
		return nil
	case "verify-token":
		res, err := c.VerifyToken()
		if err != nil {
			return err
		}
		if jsonOut {
			printJSON(res)
			return nil
		}
		fmt.Println("token 校验通过：当前 frps token 与基线一致（原文不回显）")
		return nil
	default:
		return fmt.Errorf("未知子命令: %s", sub)
	}
}

func cmdUsers(c *Client, rest []string, jsonOut bool) error {
	if len(rest) > 0 && rest[0] == "list" {
		rest = rest[1:]
	}
	var resp struct {
		Users []map[string]any `json:"users"`
	}
	if err := c.do("GET", "/api/users", nil, &resp); err != nil {
		return err
	}
	if jsonOut {
		printJSON(resp.Users)
		return nil
	}
	sort.Slice(resp.Users, func(i, j int) bool {
		return fmt.Sprint(resp.Users[i]["id"]) < fmt.Sprint(resp.Users[j]["id"])
	})
	fmt.Printf("%-4s %-16s %-8s %-10s\n", "ID", "用户名", "角色", "状态")
	for _, u := range resp.Users {
		fmt.Printf("%-4v %-16s %-8s %-10v\n", u["id"], u["username"], u["role"], u["status"])
	}
	return nil
}

func cmdAudit(c *Client, rest []string, jsonOut bool) error {
	limit := 50
	for i := 0; i < len(rest); i++ {
		if rest[i] == "--limit" && i+1 < len(rest) {
			fmt.Sscanf(rest[i+1], "%d", &limit)
			i++
		}
	}
	logs, err := c.Audit(limit)
	if err != nil {
		return err
	}
	if jsonOut {
		printJSON(logs)
		return nil
	}
	fmt.Printf("%-20s %-14s %-18s %-10s\n", "时间", "用户", "操作", "目标")
	for _, l := range logs {
		fmt.Printf("%-20v %-14v %-18v %-10v\n", l["ts"], l["username"], l["action"], l["target"])
	}
	return nil
}

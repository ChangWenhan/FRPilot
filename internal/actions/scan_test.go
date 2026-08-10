package actions

import (
	"strings"
	"testing"
)

func TestSummarizeScanClam(t *testing.T) {
	raw := `/home/alice/downloads/evil.exe: FOUND
/etc/evil/backdoor.so: FOUND.1

----------- SCAN SUMMARY -----------
Known viruses: 8636285
Engine version: 0.103.10
Scanned directories: 3213
Scanned files: 12345
Infected files: 2
Data scanned: 234.5 MB
Time: 15 sec
`
	got := summarizeScan(ScanModeQuick, raw)
	for _, want := range []string{"Infected files: 2", "发现 2 个威胁", "evil.exe", "backdoor.so"} {
		if !strings.Contains(got, want) {
			t.Errorf("summarizeScan 缺少 %q\n---\n%s", want, got)
		}
	}
}

func TestSummarizeScanClamClean(t *testing.T) {
	raw := "----------- SCAN SUMMARY -----------\nScanned files: 999\nInfected files: 0\nTime: 5 sec\n"
	got := summarizeScan(ScanModeQuick, raw)
	if !strings.Contains(got, "未发现威胁") {
		t.Errorf("干净机器应提示未发现威胁, got:\n%s", got)
	}
}

func TestSummarizeScanNoClam(t *testing.T) {
	got := summarizeScan(ScanModeFull, "__NO_CLAMAV__")
	if !strings.Contains(got, "未检测到 ClamAV") {
		t.Errorf("got: %s", got)
	}
}

// 回归：clamscan 非零退出（如不支持的选项、扫描出错、发现病毒）不得误报"未检测到 ClamAV"。
func TestSummarizeScanClamNonZeroExit(t *testing.T) {
	raw := "clamscan: unrecognized option `--no-banner'\nERROR: Unknown option passed\n"
	got := summarizeScan(ScanModeQuick, raw)
	if strings.Contains(got, "未检测到 ClamAV") {
		t.Errorf("选项错误不得误报工具缺失\n---\n%s", got)
	}
}

func TestGuardedCmd(t *testing.T) {
	cmd := guardedCmd(`command -v clamscan`, `clamscan -r -i /etc`, "__NO_CLAMAV__")
	if !strings.Contains(cmd, "if command -v clamscan") {
		t.Errorf("guardedCmd 应先用 command -v 探测: %s", cmd)
	}
	if strings.Contains(cmd, "|| echo") {
		t.Errorf("guardedCmd 不应使用 || 兜底（会把非零退出码误判为工具缺失）: %s", cmd)
	}
	if !strings.HasSuffix(cmd, "exit 0") {
		t.Errorf("guardedCmd 应以 exit 0 结尾，避免非零退出码被当作 SSH 中断: %s", cmd)
	}
}

func TestSummarizeScanRootkit(t *testing.T) {
	raw := `[RKHUNTER]
Checking for possible rootkits ...
  Checking for rootkit files and directories ... [ Warning ]
  Warning: Possible rootkit: /usr/lib/evil
[CHKROOTKIT]
suspicious /usr/bin/evil: INFECTED
`
	got := summarizeScan(ScanModeRootkit, raw)
	for _, want := range []string{"3 个警告/感染项", "Possible rootkit", "INFECTED"} {
		if !strings.Contains(got, want) {
			t.Errorf("rootkit 摘要缺少 %q\n---\n%s", want, got)
		}
	}
}

func TestSummarizeScanUpdate(t *testing.T) {
	raw := `ClamAV update process started at Mon Aug 10 10:00:00 2026
Downloading daily.cvd [100%]
Database updated (version: 28100, sigs: 1000000)
`
	got := summarizeScan(ScanModeUpdate, raw)
	for _, want := range []string{"Database updated", "Downloading"} {
		if !strings.Contains(got, want) {
			t.Errorf("病毒库更新摘要缺少 %q\n---\n%s", want, got)
		}
	}
	if strings.Contains(got, "未检测到") {
		t.Errorf("更新成功输出不应提示工具缺失\n---\n%s", got)
	}
}

func TestSummarizeScanUpdateNoFreshclam(t *testing.T) {
	got := summarizeScan(ScanModeUpdate, "__NO_FRESHCLAM__")
	if !strings.Contains(got, "未检测到 freshclam") {
		t.Errorf("got: %s", got)
	}
}

// 回归：freshclam 守护进程占用日志锁时，应给出守护进程自动更新的提示而非原始报错。
func TestSummarizeScanUpdateLockConflict(t *testing.T) {
	raw := `ERROR: Failed to lock the log file /var/log/clamav/freshclam.log: Resource temporarily unavailable
ERROR: Problem with internal logger (UpdateLogFile = /var/log/clamav/freshclam.log).
ERROR: initialize: libfreshclam init failed.
ERROR: Initialization error!
`
	got := summarizeScan(ScanModeUpdate, raw)
	if !strings.Contains(got, "freshclam 守护进程") {
		t.Errorf("锁冲突应提示守护进程自动更新\n---\n%s", got)
	}
}

func TestScanPhases(t *testing.T) {
	quick := scanPhases(ScanModeQuick)
	if len(quick) != 7 {
		t.Fatalf("quick 应有 7 个目录阶段, got %d", len(quick))
	}
	if !strings.Contains(quick[0].label, "/etc") || !strings.Contains(quick[0].label, "1/7") {
		t.Fatalf("quick 阶段标签异常: %s", quick[0].label)
	}
	for _, ph := range quick {
		if !strings.Contains(ph.cmd, "clamscan") || ph.marker != "__NO_CLAMAV__" {
			t.Fatalf("quick 阶段命令异常: %s", ph.cmd)
		}
	}
	full := scanPhases(ScanModeFull)
	for _, ph := range full {
		if strings.Contains(ph.cmd, "/proc") || strings.Contains(ph.cmd, "/sys") {
			t.Fatalf("full 不应包含虚拟文件系统目录: %s", ph.cmd)
		}
	}
	rootkit := scanPhases(ScanModeRootkit)
	if len(rootkit) != 2 || !strings.Contains(rootkit[0].cmd, "rkhunter") || !strings.Contains(rootkit[1].cmd, "chkrootkit") {
		t.Fatalf("rootkit 阶段异常: %+v", rootkit)
	}
	upd := scanPhases(ScanModeUpdate)
	if len(upd) != 1 || !strings.Contains(upd[0].cmd, "freshclam") || upd[0].marker != "__NO_FRESHCLAM__" {
		t.Fatalf("update 阶段异常: %+v", upd)
	}
}

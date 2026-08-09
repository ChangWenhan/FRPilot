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
}

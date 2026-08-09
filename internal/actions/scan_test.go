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

func TestScanCommandModes(t *testing.T) {
	cmd, _ := scanCommand(ScanModeQuick)
	if !strings.Contains(cmd, "clamscan") || !strings.Contains(cmd, "__NO_CLAMAV__") {
		t.Fatalf("quick 命令异常: %s", cmd)
	}
	cmd, _ = scanCommand(ScanModeFull)
	if !strings.Contains(cmd, "exclude-dir=/proc") {
		t.Fatalf("full 命令应排除 /proc: %s", cmd)
	}
	cmd, _ = scanCommand(ScanModeRootkit)
	if !strings.Contains(cmd, "rkhunter") || !strings.Contains(cmd, "chkrootkit") {
		t.Fatalf("rootkit 命令异常: %s", cmd)
	}
}

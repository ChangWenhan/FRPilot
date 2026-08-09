package sshx

import (
	"bytes"
	"fmt"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
)

// Conn 一个 SSH 会话包装（密码认证）。
type Conn struct {
	client *ssh.Client
}

// Dial 建立 SSH 连接。
func Dial(host string, port int, user, pass string) (*Conn, error) {
	cfg := &ssh.ClientConfig{
		User:            user,
		Auth:            []ssh.AuthMethod{ssh.Password(pass)},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         8 * time.Second,
	}
	addr := fmt.Sprintf("%s:%d", host, port)
	client, err := ssh.Dial("tcp", addr, cfg)
	if err != nil {
		return nil, err
	}
	return &Conn{client: client}, nil
}

// Run 在远端执行一条命令（非交互），返回 stdout 与 stderr。
func (c *Conn) Run(cmd string, timeout time.Duration) (string, string, error) {
	sess, err := c.client.NewSession()
	if err != nil {
		return "", "", err
	}
	defer sess.Close()
	var out, errOut bytes.Buffer
	sess.Stdout = &out
	sess.Stderr = &errOut
	if timeout > 0 {
		if err := sess.RequestPty("vt100", 40, 80, nil); err == nil {
			// 有 pty 时按时间关闭会话，防命令卡死
			go func() {
				time.Sleep(timeout)
				_ = sess.Close()
			}()
		}
	}
	if err := sess.Run(cmd); err != nil {
		return out.String(), errOut.String(), err
	}
	return out.String(), errOut.String(), nil
}

// RunSudo 以 sudo 提升权限执行命令（非交互，密码经 stdin 传给 sudo -S）。
// sudoPass 为空时退化为普通 Run；密码不回显、不落日志。
func (c *Conn) RunSudo(cmd, sudoPass string, timeout time.Duration) (string, string, error) {
	if sudoPass == "" {
		return c.Run(cmd, timeout)
	}
	wrapped := fmt.Sprintf("echo %s | sudo -S -p '' -- bash -c %s", shellQuote(sudoPass), shellQuote(cmd))
	return c.Run(wrapped, timeout)
}

// shellQuote 单引号包裹并转义内嵌单引号，可安全嵌入 bash 命令行。
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func (c *Conn) Close() error { return c.client.Close() }

// FindFrpsConfigPath 通过 SSH 定位 frps 的实际配置文件路径：
// 1) 从 frps 进程命令行提取 -c 参数；2) 兜底常见安装路径。
func FindFrpsConfigPath(c *Conn) (string, error) {
	cmd := `ps aux 2>/dev/null | grep -E '[f]rps' | grep -oE '\-c[ =][^ ]+' | head -1 | sed 's/^-c[ =]//'; true`
	out, _, _ := c.Run(cmd, 10*time.Second)
	if p := strings.TrimSpace(out); p != "" && strings.Contains(p, "frps") {
		return p, nil
	}
	// 兜底：常见安装路径
	probe := `for f in /etc/frp/frps.toml /etc/frp/frps.ini /usr/local/etc/frps.ini /opt/frp*/frps.ini /opt/frp*/frps.toml /root/frp*/frps.ini /root/frp*/frps.toml /home/*/frp*/frps.ini /home/*/frp*/frps.toml; do [ -f "$f" ] && echo "$f" && break; done`
	out2, _, _ := c.Run(probe, 10*time.Second)
	if p := strings.TrimSpace(out2); p != "" {
		return p, nil
	}
	return "", fmt.Errorf("未能自动定位 frps 配置文件，请在设置中手动填写 configPath")
}

// VerifyTokenInFrpsIni 通过 SSH 读取 frps 配置文件，校验 auth token 是否等于基线。
// 兼容 frps.ini（旧格式 token = x）与 frps.toml（auth.token = "x"）；
// 找不到 token 视为漂移。
// 返回 (是否一致, 读到的 token 值, 错误)
func VerifyTokenInFrpsIni(c *Conn, configPath string, baseline string) (bool, string, error) {
	tomlPath := configPath
	if i := strings.LastIndex(tomlPath, "."); i > 0 {
		tomlPath = tomlPath[:i] + ".toml"
	}
	cmd := fmt.Sprintf(`cat "%s" 2>/dev/null; cat "%s" 2>/dev/null`, configPath, tomlPath)
	out, _, err := c.Run(cmd, 10*time.Second)
	if err != nil {
		return false, "", err
	}
	token := extractConfigValue(out, "token")
	return token == baseline, token, nil
}

// extractConfigValue 从 frps 配置文本中提取指定 key 的值。
// 兼容 INI 格式（token = x）与 TOML 格式（auth.token = "x"）。
// key 匹配规则：等号前的 key 与目标相等，或以 ".<key>" 结尾（如 auth.token）。
func extractConfigValue(content, key string) string {
	sep := "." + key
	for _, line := range bytes.Split([]byte(content), []byte("\n")) {
		s := bytes.TrimSpace(line)
		if len(s) == 0 || bytes.HasPrefix(s, []byte("#")) {
			continue
		}
		eq := bytes.IndexByte(s, '=')
		if eq <= 0 {
			continue
		}
		k := strings.TrimSpace(string(s[:eq]))
		if k != key && !strings.HasSuffix(k, sep) {
			continue
		}
		v := stringsTrimQuotes(string(bytes.TrimSpace(s[eq+1:])))
		if v != "" {
			return v
		}
	}
	return ""
}

// ExtractFrpsConfigValues 解析 frps 配置文本中的常用字段（INI/TOML 双格式）。
// 返回 map：bindPort/dashboardPort/dashboardUser/dashboardPwd/token。
func ExtractFrpsConfigValues(content string) map[string]string {
	out := map[string]string{}
	for _, k := range []string{"bind_port", "bindPort", "dashboard_port", "dashboardPort",
		"dashboard_user", "dashboardUser", "dashboard_pwd", "dashboardPwd", "token"} {
		if v := extractConfigValue(content, k); v != "" {
			out[k] = v
		}
	}
	return out
}

func stringsTrimQuotes(s string) string {
	if len(s) >= 2 && (s[0] == '"' || s[0] == '\'') && s[len(s)-1] == s[0] {
		return s[1 : len(s)-1]
	}
	return s
}

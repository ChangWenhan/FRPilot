package registry

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"frpmon/internal/config"
	"frpmon/internal/frpsapi"
	"frpmon/internal/sshx"
	"frpmon/internal/store"
)

// Service 机器注册表：自动发现 + 生命周期管理。
type Service struct {
	db  *store.DB
	cfg *config.Manager
}

func NewService(db *store.DB, cfg *config.Manager) *Service {
	return &Service{db: db, cfg: cfg}
}

type DiscoveryResult struct {
	Total    int      `json:"total"`
	NewFound []string `json:"newFound"`
	Online   []string `json:"online"`
	Offline  []string `json:"offline"`
}

// Discover 从 frps dashboard 拉取全部 tcp 代理，自动建档。
// 新机器 → pending（待配置），已存在的机器更新最近在线时间。
func (s *Service) Discover(client *frpsapi.Client) (*DiscoveryResult, error) {
	proxies, err := client.Proxies()
	if err != nil {
		return nil, err
	}
	res := &DiscoveryResult{Total: len(proxies)}
	for _, p := range proxies {
		m, isNew, err := s.db.UpsertMachineFromDiscovery(p.Name, p.Conf.RemotePort)
		if err != nil {
			return nil, err
		}
		if isNew {
			res.NewFound = append(res.NewFound, p.Name)
		}
		if p.Status == "online" {
			res.Online = append(res.Online, p.Name)
			s.db.TouchMachine(m.ID, true, false)
		} else {
			res.Offline = append(res.Offline, p.Name)
		}
	}
	return res, nil
}

// UpdateCredentials 填写某机器的 SSH 凭据并落库（加密存储）。
// 凭据就绪后状态 pending → configured，监控开关需单独开启。
func (s *Service) UpdateCredentials(id int64, sshUser, sshPass string) error {
	if strings.TrimSpace(sshUser) == "" {
		return errors.New("SSH 用户不能为空")
	}
	if sshPass == "" {
		// UI 不再回显旧密码；编辑用户名时留空表示保留原来的密文。
		m, err := s.db.GetMachineByID(id)
		if err != nil {
			return err
		}
		if m.SSHPassEnc == "" {
			return errors.New("SSH 密码不能为空")
		}
		return s.db.SetMachineCredentials(id, strings.TrimSpace(sshUser), m.SSHPassEnc)
	}
	enc, err := s.db.EncryptSecret(sshPass)
	if err != nil {
		return err
	}
	return s.db.SetMachineCredentials(id, strings.TrimSpace(sshUser), enc)
}

// SetEnabled 开/关监控。开启要求凭据已配置（store 层校验）。
func (s *Service) SetEnabled(id int64, enabled bool) (*store.Machine, error) {
	return s.db.SetMachineEnabled(id, enabled)
}

// VerifyFrpsTokenBaseline 通过 SSH 读 frps 配置文件，校验 token 是否仍等于
// 该部署的基线（配置中存储的 token）。发现漂移返回 error（前端告警）。
func (s *Service) VerifyFrpsTokenBaseline() (string, error) {
	f := s.cfg.Get().Frps
	port := f.SSHPort
	if port == 0 {
		port = 22
	}
	conn, err := sshx.Dial(f.SSHHost, port, f.SSHUser, f.SSHPass)
	if err != nil {
		return "", fmt.Errorf("SSH 连接 frps 失败: %w", err)
	}
	defer conn.Close()
	path := f.ConfigPath
	if path == "" {
		path, err = sshx.FindFrpsConfigPath(conn)
		if err != nil {
			return "", err
		}
	}
	baseline := f.Token
	ok, token, err := sshx.VerifyTokenInFrpsIni(conn, path, baseline)
	if err != nil {
		return "", err
	}
	if !ok {
		return "", fmt.Errorf("token 漂移：frps 配置中的 token 与当前基线不一致（基线为部署时设定，若确实更换了 token 请重新检测）")
	}
	return token, nil
}

// AutoDetectFrpsInfo 一键自动检测：通过 SSH 定位 frps 进程的实际配置文件，
// 读取 bind_port / dashboard_port / dashboard_user / dashboard_pwd / token，
// 全部自动写入配置（dashboard 默认走 127.0.0.1，token 即该部署基线）。
// 返回检测到的字段摘要。
func (s *Service) AutoDetectFrpsInfo() (map[string]string, error) {
	f := s.cfg.Get().Frps
	port := f.SSHPort
	if port == 0 {
		port = 22
	}
	if f.SSHHost == "" || f.SSHUser == "" {
		return nil, errors.New("请先配置 frps 的 SSH 连接信息（主机/端口/用户/密码）")
	}
	conn, err := sshx.Dial(f.SSHHost, port, f.SSHUser, f.SSHPass)
	if err != nil {
		return nil, fmt.Errorf("SSH 连接 frps 失败: %w", err)
	}
	defer conn.Close()

	path, err := sshx.FindFrpsConfigPath(conn)
	if err != nil {
		return nil, err
	}
	out, _, err := conn.Run(fmt.Sprintf(`cat "%s"`, path), 10*time.Second)
	if err != nil {
		return nil, fmt.Errorf("读取 frps 配置失败: %w", err)
	}
	vals := sshx.ExtractFrpsConfigValues(out)

	dashPort := firstNonEmpty(vals["dashboard_port"], vals["dashboardPort"])
	user := firstNonEmpty(vals["dashboard_user"], vals["dashboardUser"])
	pwd := firstNonEmpty(vals["dashboard_pwd"], vals["dashboardPwd"])
	token := firstNonEmpty(vals["token"], config.DefaultTokenValue)

	dashURL := "http://127.0.0.1:" + dashPort
	if dashPort == "" {
		dashURL = ""
	}

	err = s.cfg.Update(func(c *config.AppConfig) {
		c.Frps.ConfigPath = path
		c.Frps.DashboardURL = dashURL
		c.Frps.DashboardUser = user
		c.Frps.DashboardPass = pwd
		c.Frps.Token = token
	})
	if err != nil {
		return nil, err
	}
	return map[string]string{
		"configPath":    path,
		"bindPort":      firstNonEmpty(vals["bind_port"], vals["bindPort"]),
		"dashboardPort": dashPort,
		"dashboardUser": user,
		"token":         token,
	}, nil
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

// NewFrpsClient 根据当前配置构造 dashboard 客户端（使用该部署的 token 基线）。
func NewFrpsClient(cfg *config.Manager) (*frpsapi.Client, error) {
	f := cfg.Get().Frps
	base := f.DashboardURL
	if base == "" {
		return nil, errors.New("未配置 frps dashboard 地址")
	}
	return frpsapi.NewClient(base, f.DashboardUser, f.DashboardPass, f.Token), nil
}

// SuggestSSHUser 本机部署时探测当前用户作为默认 SSH 用户（部署向导用）。
func SuggestSSHUser() string {
	return os.Getenv("USER")
}

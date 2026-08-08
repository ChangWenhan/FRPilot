package config

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
)

// DefaultTokenValue 是默认的 frps auth token（仅当 frps 配置中无 token 时兜底使用）。
// 每个部署的 token 基线由软件从 frps 配置文件自动读取并存入本地配置，
// 不作为硬编码常量强制使用。
const DefaultTokenValue = "12345"

type FrpsConfig struct {
	// Dashboard API 访问信息（软件部署在 frps 本机时走 127.0.0.1）
	DashboardURL  string `json:"dashboardUrl"`
	DashboardUser string `json:"dashboardUser"`
	DashboardPass string `json:"dashboardPass"`
	// frps 本机 SSH 信息（用于读 frps.ini 校验 token、部署自检）
	SSHHost string `json:"sshHost"`
	SSHPort int    `json:"sshPort"`
	SSHUser string `json:"sshUser"`
	SSHPass string `json:"sshPass"`
	// Token 是该部署的 auth token 基线：由「自动检测」从 frps 配置文件读取，
	// 或部署时手动设置；设置后 UI 只读展示，漂移检测以此为准。
	Token string `json:"token"`
	// 探测到的 frps 配置路径
	ConfigPath string `json:"configPath"`
}

// TlsConfig HTTPS 支持（自签名证书部署时由 install.sh 生成）。
type TlsConfig struct {
	Enabled bool   `json:"enabled"`
	Cert    string `json:"cert"`
	Key     string `json:"key"`
}

// AiConfig AI 诊断配置（OpenAI 兼容协议）。
// API Key 不存此结构（加密存 settings 表），仅存 Provider/Model 等非敏感项。
type AiConfig struct {
	Enabled     bool   `json:"enabled"`
	ProviderURL string `json:"providerUrl"` // 如 https://api.deepseek.com/v1
	Model       string `json:"model"`       // 如 deepseek-chat
	TimeoutSec  int    `json:"timeoutSec"`
}

// CustomCleanupItem 设置中可追加的自定义清理命令。
type CustomCleanupItem struct {
	Name    string `json:"name"`
	Desc    string `json:"desc"`
	Command string `json:"command"`
	Risk    string `json:"risk"` // low | mid | high
}

// HealthThresholds 一键体检阈值（可配置）。
type HealthThresholds struct {
	CPUWarn        float64 `json:"cpuWarn"`
	CPUFail        float64 `json:"cpuFail"`
	MemWarn        float64 `json:"memWarn"`
	MemFail        float64 `json:"memFail"`
	DiskWarn       float64 `json:"diskWarn"`
	DiskFail       float64 `json:"diskFail"`
	GPUTempWarn    float64 `json:"gpuTempWarn"`
	GPUTempFail    float64 `json:"gpuTempFail"`
	GPUMemWarn     float64 `json:"gpuMemWarn"`
	GPUMemFail     float64 `json:"gpuMemFail"`
	ClamDbMaxDays  int     `json:"clamDbMaxDays"`
	SnapshotMaxAge int     `json:"snapshotMaxAgeMin"` // 快照超过该分钟数视为数据过期
}

type AppConfig struct {
	ListenAddr      string            `json:"listenAddr"`
	DataDir         string            `json:"dataDir"`
	ConfigPath      string            `json:"-"`
	Frps            FrpsConfig        `json:"frps"`
	Registration    string            `json:"registration"` // open | approval | closed
	SessionTTLDays  int               `json:"sessionTTLDays"`
	LoginMaxFails   int               `json:"loginMaxFails"`
	LoginLockMinute int               `json:"loginLockMinutes"`
	Health          HealthThresholds  `json:"health"`
	CleanupCustom   []CustomCleanupItem `json:"cleanupCustom"`
	AI              AiConfig          `json:"ai"`
	TLS             TlsConfig         `json:"tls"`
	Version         string            `json:"-"`
}

func DefaultConfig(dataDir string) *AppConfig {
	return &AppConfig{
		ListenAddr:      "0.0.0.0:8443",
		DataDir:         dataDir,
		ConfigPath:      filepath.Join(dataDir, "config.json"),
		Registration:    "open",
		SessionTTLDays:  7,
		LoginMaxFails:   5,
		LoginLockMinute: 10,
		Health: HealthThresholds{
			CPUWarn: 70, CPUFail: 85,
			MemWarn: 80, MemFail: 90,
			DiskWarn: 75, DiskFail: 85,
			GPUTempWarn: 75, GPUTempFail: 85,
			GPUMemWarn: 85, GPUMemFail: 95,
			ClamDbMaxDays: 7, SnapshotMaxAge: 10,
		},
		AI: AiConfig{
			Enabled: false, ProviderURL: "", Model: "",
			TimeoutSec: 60,
		},
	}
}

// Manager 持有关配置并提供热修改能力（写内存 + 落盘，无需重启）。
type Manager struct {
	mu     sync.RWMutex
	cfg    *AppConfig
	onSave func() error
}

func NewManager(cfg *AppConfig) *Manager {
	return &Manager{cfg: cfg}
}

func LoadOrCreate(dataDir string) (*Manager, error) {
	return LoadOrCreateAt(dataDir, "")
}

// LoadOrCreateAt 加载或创建配置。configPath 为空时使用 <dataDir>/config.json
// （生产部署可通过 --config 指定 /etc/FRPilot/config.json）。
func LoadOrCreateAt(dataDir, configPath string) (*Manager, error) {
	cfg := DefaultConfig(dataDir)
	if configPath != "" {
		cfg.ConfigPath = configPath
	}
	if err := os.MkdirAll(dataDir, 0o750); err != nil {
		return nil, err
	}
	path := cfg.ConfigPath
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return nil, err
	}
	if b, err := os.ReadFile(path); err == nil {
		var loaded AppConfig
		if err := json.Unmarshal(b, &loaded); err != nil {
			return nil, err
		}
		loaded.DataDir = dataDir
		loaded.ConfigPath = path
		cfg = &loaded
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	} else {
		if err := os.WriteFile(path, []byte("{}"), 0o600); err != nil {
			return nil, err
		}
	}
	m := NewManager(cfg)
	return m, nil
}

func (m *Manager) Get() *AppConfig {
	m.mu.RLock()
	defer m.mu.RUnlock()
	cp := *m.cfg
	return &cp
}

// Update 原子替换配置并立即持久化（热修改：进程内生效，无需重启）。
func (m *Manager) Update(fn func(*AppConfig)) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	fn(m.cfg)
	if err := m.persist(); err != nil {
		return err
	}
	if m.onSave != nil {
		return m.onSave()
	}
	return nil
}

func (m *Manager) OnSave(fn func() error) { m.mu.Lock(); m.onSave = fn; m.mu.Unlock() }

func (m *Manager) persist() error {
	b, err := json.MarshalIndent(m.cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(m.cfg.ConfigPath, b, 0o600)
}

// VerifyTokenBaseline 校验给定 token 是否等于该部署的基线（配置中存储的 token）。
// frps.ini / 各 frpc 配置中读到的 token 都必须与此一致，否则视为漂移。
func VerifyTokenBaseline(got, baseline string) bool { return got == baseline }

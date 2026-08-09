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
	// 敏感值由 config.SyncEncryptedSecrets 写入 SQLite 的加密 settings；
	// 这些字段只在进程内保留，永远不通过 config.json 序列化。
	DashboardPass string `json:"-"`
	// frps 本机 SSH 信息（用于读 frps.ini 校验 token、部署自检）
	SSHHost string `json:"sshHost"`
	SSHPort int    `json:"sshPort"`
	SSHUser string `json:"sshUser"`
	SSHPass string `json:"-"`
	// Token 是该部署的 auth token 基线：由「自动检测」从 frps 配置文件读取，
	// 或部署时手动设置；设置后 UI 只读展示，漂移检测以此为准。
	Token string `json:"-"`
	// 探测到的 frps 配置路径
	ConfigPath string `json:"configPath"`
}

// frpsConfigJSON 同时承担两件事：
//  1. 兼容读取旧版本 config.json 中的明文密码/token，供启动时迁移；
//  2. 通过 FrpsConfig.MarshalJSON 确保新写入的 config.json 不再包含敏感值。
//
// 不要把敏感字段直接加回 FrpsConfig 的 json tag，否则一次热保存就会把
// 运行时凭据重新写回明文配置文件。
type frpsConfigJSON struct {
	DashboardURL  string `json:"dashboardUrl"`
	DashboardUser string `json:"dashboardUser"`
	DashboardPass string `json:"dashboardPass"`
	SSHHost       string `json:"sshHost"`
	SSHPort       int    `json:"sshPort"`
	SSHUser       string `json:"sshUser"`
	SSHPass       string `json:"sshPass"`
	Token         string `json:"token"`
	ConfigPath    string `json:"configPath"`
}

func (f FrpsConfig) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		DashboardURL  string `json:"dashboardUrl"`
		DashboardUser string `json:"dashboardUser"`
		SSHHost       string `json:"sshHost"`
		SSHPort       int    `json:"sshPort"`
		SSHUser       string `json:"sshUser"`
		ConfigPath    string `json:"configPath"`
	}{
		DashboardURL:  f.DashboardURL,
		DashboardUser: f.DashboardUser,
		SSHHost:       f.SSHHost,
		SSHPort:       f.SSHPort,
		SSHUser:       f.SSHUser,
		ConfigPath:    f.ConfigPath,
	})
}

func (f *FrpsConfig) UnmarshalJSON(data []byte) error {
	var v frpsConfigJSON
	if err := json.Unmarshal(data, &v); err != nil {
		return err
	}
	*f = FrpsConfig{
		DashboardURL:  v.DashboardURL,
		DashboardUser: v.DashboardUser,
		DashboardPass: v.DashboardPass,
		SSHHost:       v.SSHHost,
		SSHPort:       v.SSHPort,
		SSHUser:       v.SSHUser,
		SSHPass:       v.SSHPass,
		Token:         v.Token,
		ConfigPath:    v.ConfigPath,
	}
	return nil
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
	ListenAddr      string              `json:"listenAddr"`
	DataDir         string              `json:"dataDir"`
	ConfigPath      string              `json:"-"`
	Frps            FrpsConfig          `json:"frps"`
	Registration    string              `json:"registration"` // open | approval | closed
	SessionTTLDays  int                 `json:"sessionTTLDays"`
	LoginMaxFails   int                 `json:"loginMaxFails"`
	LoginLockMinute int                 `json:"loginLockMinutes"`
	LoginIPMaxFails int                 `json:"loginIPMaxFails"`
	LoginWindowMin  int                 `json:"loginWindowMinutes"`
	Health          HealthThresholds    `json:"health"`
	CleanupCustom   []CustomCleanupItem `json:"cleanupCustom"`
	AI              AiConfig            `json:"ai"`
	TLS             TlsConfig           `json:"tls"`
	Version         string              `json:"-"`
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
		LoginIPMaxFails: 15,
		LoginWindowMin:  15,
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
	onSave func(*AppConfig) error
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
		applyDefaults(&loaded, cfg)
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
	candidate := cloneConfig(m.cfg)
	fn(candidate)
	if m.onSave != nil {
		if err := m.onSave(candidate); err != nil {
			m.mu.Unlock()
			return err
		}
	}
	if err := m.persist(candidate); err != nil {
		m.mu.Unlock()
		return err
	}
	*m.cfg = *candidate
	m.mu.Unlock()
	return nil
}

// OnSave 在配置通过校验、写入磁盘前调用。回调拿到的是候选配置，适合把
// 敏感字段同步到外部加密存储；回调失败时不会替换内存中的配置。
func (m *Manager) OnSave(fn func(*AppConfig) error) { m.mu.Lock(); m.onSave = fn; m.mu.Unlock() }

func (m *Manager) persist(cfg *AppConfig) error {
	b, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(cfg.ConfigPath), ".config.json-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, cfg.ConfigPath)
}

func cloneConfig(src *AppConfig) *AppConfig {
	cp := *src
	cp.CleanupCustom = append([]CustomCleanupItem(nil), src.CleanupCustom...)
	return &cp
}

func applyDefaults(cfg, defaults *AppConfig) {
	if cfg.ListenAddr == "" {
		cfg.ListenAddr = defaults.ListenAddr
	}
	if cfg.Registration == "" {
		cfg.Registration = defaults.Registration
	}
	if cfg.SessionTTLDays <= 0 {
		cfg.SessionTTLDays = defaults.SessionTTLDays
	}
	if cfg.LoginMaxFails <= 0 {
		cfg.LoginMaxFails = defaults.LoginMaxFails
	}
	if cfg.LoginLockMinute <= 0 {
		cfg.LoginLockMinute = defaults.LoginLockMinute
	}
	if cfg.LoginIPMaxFails <= 0 {
		cfg.LoginIPMaxFails = defaults.LoginIPMaxFails
	}
	if cfg.LoginWindowMin <= 0 {
		cfg.LoginWindowMin = defaults.LoginWindowMin
	}
	if cfg.Frps.SSHPort <= 0 {
		cfg.Frps.SSHPort = defaults.Frps.SSHPort
	}
	if cfg.Health.CPUWarn <= 0 {
		cfg.Health.CPUWarn = defaults.Health.CPUWarn
	}
	if cfg.Health.CPUFail <= 0 {
		cfg.Health.CPUFail = defaults.Health.CPUFail
	}
	if cfg.Health.MemWarn <= 0 {
		cfg.Health.MemWarn = defaults.Health.MemWarn
	}
	if cfg.Health.MemFail <= 0 {
		cfg.Health.MemFail = defaults.Health.MemFail
	}
	if cfg.Health.DiskWarn <= 0 {
		cfg.Health.DiskWarn = defaults.Health.DiskWarn
	}
	if cfg.Health.DiskFail <= 0 {
		cfg.Health.DiskFail = defaults.Health.DiskFail
	}
	if cfg.Health.GPUTempWarn <= 0 {
		cfg.Health.GPUTempWarn = defaults.Health.GPUTempWarn
	}
	if cfg.Health.GPUTempFail <= 0 {
		cfg.Health.GPUTempFail = defaults.Health.GPUTempFail
	}
	if cfg.Health.GPUMemWarn <= 0 {
		cfg.Health.GPUMemWarn = defaults.Health.GPUMemWarn
	}
	if cfg.Health.GPUMemFail <= 0 {
		cfg.Health.GPUMemFail = defaults.Health.GPUMemFail
	}
	if cfg.Health.ClamDbMaxDays <= 0 {
		cfg.Health.ClamDbMaxDays = defaults.Health.ClamDbMaxDays
	}
	if cfg.Health.SnapshotMaxAge <= 0 {
		cfg.Health.SnapshotMaxAge = defaults.Health.SnapshotMaxAge
	}
	if cfg.AI.TimeoutSec <= 0 {
		cfg.AI.TimeoutSec = defaults.AI.TimeoutSec
	}
}

// VerifyTokenBaseline 校验给定 token 是否等于该部署的基线（配置中存储的 token）。
// frps.ini / 各 frpc 配置中读到的 token 都必须与此一致，否则视为漂移。
func VerifyTokenBaseline(got, baseline string) bool { return got == baseline }

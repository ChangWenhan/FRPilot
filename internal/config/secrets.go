package config

import (
	"fmt"

	"frpmon/internal/store"
)

// Secret setting keys are kept separate from the public config JSON. The value
// stored in settings is AES-GCM encrypted with the data directory key.
const (
	SecretDashboardPass = "frps_dashboard_pass"
	SecretSSHPass       = "frps_ssh_pass"
	SecretToken         = "frps_token"
)

// SyncEncryptedSecrets migrates legacy plaintext values from config.json to
// the encrypted settings store, hydrates the runtime config, and installs the
// save hook used by hot updates. The public config serializer omits these
// fields, so the final config.json contains no passwords or tokens.
func SyncEncryptedSecrets(m *Manager, db *store.DB) error {
	current := m.Get().Frps

	var err error
	current.DashboardPass, err = loadOrMigrateSecret(db, SecretDashboardPass, current.DashboardPass)
	if err != nil {
		return fmt.Errorf("迁移 frps dashboard 密码失败: %w", err)
	}
	current.SSHPass, err = loadOrMigrateSecret(db, SecretSSHPass, current.SSHPass)
	if err != nil {
		return fmt.Errorf("迁移 frps SSH 密码失败: %w", err)
	}
	current.Token, err = loadOrMigrateSecret(db, SecretToken, current.Token)
	if err != nil {
		return fmt.Errorf("迁移 frps token 失败: %w", err)
	}

	m.OnSave(func(candidate *AppConfig) error {
		f := candidate.Frps
		for key, value := range map[string]string{
			SecretDashboardPass: f.DashboardPass,
			SecretSSHPass:       f.SSHPass,
			SecretToken:         f.Token,
		} {
			if err := saveSecretIfChanged(db, key, value); err != nil {
				return fmt.Errorf("保存敏感配置 %q 失败: %w", key, err)
			}
		}
		return nil
	})

	// This update both hydrates the in-memory values and rewrites the public
	// config atomically, removing any legacy plaintext fields.
	return m.Update(func(c *AppConfig) {
		c.Frps.DashboardPass = current.DashboardPass
		c.Frps.SSHPass = current.SSHPass
		c.Frps.Token = current.Token
	})
}

func loadOrMigrateSecret(db *store.DB, key, legacy string) (string, error) {
	enc, err := db.GetSetting(key)
	if err != nil {
		return "", err
	}
	if enc != "" {
		return db.DecryptSecret(enc)
	}
	if legacy == "" {
		return "", nil
	}
	if err := saveSecretIfChanged(db, key, legacy); err != nil {
		return "", err
	}
	return legacy, nil
}

func saveSecretIfChanged(db *store.DB, key, plain string) error {
	if plain == "" {
		return nil
	}
	old, err := db.GetSetting(key)
	if err != nil {
		return err
	}
	if old != "" {
		decoded, err := db.DecryptSecret(old)
		if err != nil {
			return err
		}
		if decoded == plain {
			return nil
		}
	}
	enc, err := db.EncryptSecret(plain)
	if err != nil {
		return err
	}
	return db.SetSetting(key, enc)
}

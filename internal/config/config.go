package config

import (
	"os"
	"path/filepath"

	coreconfig "github.com/aeon022/missionctl-core/config"
	"github.com/spf13/viper"
)

type Config struct {
	DefaultAccount string `mapstructure:"default_account"`
	DefaultFrom    string `mapstructure:"default_from"`
	InboxMailbox   string `mapstructure:"inbox_mailbox"`
	SyncCount      int    `mapstructure:"sync_count"` // messages to sync per account
	DataDir        string `mapstructure:"data_dir"`
	LicenseKey     string `mapstructure:"license_key"`
	LicenseStatus  string `mapstructure:"license_status"`
}

// defaultPolarOrgID is aeon022's Polar.sh organization — shared across the
// missionctl suite, same as postctl's.
const defaultPolarOrgID = "aa792ea4-650e-492e-a955-9b3d564e943e"

// IsPro reports whether a valid Pro/Bundle license is active on this
// machine — gates the AI draft-reply feature (the `a` key).
func IsPro() bool {
	return Active.LicenseStatus == "active"
}

func PolarOrgID() string {
	if v := viper.GetString("polar_org_id"); v != "" {
		return v
	}
	return defaultPolarOrgID
}

// SetLicense persists the license key/status to
// ~/.config/mailctl/config.yaml and updates Active immediately.
func SetLicense(key, status string) error {
	viper.Set("license_key", key)
	viper.Set("license_status", status)
	Active.LicenseKey = key
	Active.LicenseStatus = status
	home, _ := os.UserHomeDir()
	cfgDir := filepath.Join(home, ".config", "mailctl")
	if err := os.MkdirAll(cfgDir, 0755); err != nil {
		return err
	}
	return viper.WriteConfigAs(filepath.Join(cfgDir, "config.yaml"))
}

var Active Config

func Load() error {
	home, _ := os.UserHomeDir()
	cfgDir := filepath.Join(home, ".config", "mailctl")
	_ = os.MkdirAll(cfgDir, 0755)

	viper.SetConfigName("config")
	viper.SetConfigType("yaml")
	viper.AddConfigPath(cfgDir)
	viper.SetEnvPrefix("MAILCTL")
	viper.AutomaticEnv()

	viper.SetDefault("default_account", "")
	viper.SetDefault("default_from", "")
	viper.SetDefault("inbox_mailbox", "INBOX")
	viper.SetDefault("sync_count", 100)

	if err := viper.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			return err
		}
		_ = viper.WriteConfigAs(filepath.Join(cfgDir, "config.yaml"))
	}
	return viper.Unmarshal(&Active)
}

// DBPathOverride, when non-empty, overrides DBPath()'s return value. Used by tests
// to point at a temporary database instead of the real one on disk.
var DBPathOverride string

// DBPath returns the database file path. DBPathOverride (test-only) wins
// if set; otherwise data_dir (viper key, also settable via
// MAILCTL_DATA_DIR) points it at a user-chosen directory — e.g. inside
// iCloud Drive or Dropbox — resolved via coreconfig.ResolveDir; with
// neither set, the private default (~/Library/Application Support/mailctl)
// is unchanged from before this existed.
func DBPath() string {
	if DBPathOverride != "" {
		return DBPathOverride
	}
	if dir := viper.GetString("data_dir"); dir != "" {
		resolved, _ := coreconfig.ResolveDir("mailctl", dir)
		return filepath.Join(resolved, "mailctl.db")
	}
	home, _ := os.UserHomeDir()
	dir := filepath.Join(home, "Library", "Application Support", "mailctl")
	_ = os.MkdirAll(dir, 0755)
	return filepath.Join(dir, "mailctl.db")
}

// Shared reports whether DBPath currently resolves to a user-configured
// directory (data_dir) rather than the tool's private default.
func Shared() bool {
	return DBPathOverride == "" && viper.GetString("data_dir") != ""
}

// LastSyncedPath is the marker file (see missionctl-core/lastsync) tracking
// when a sync last completed, for the TUI's "synced Xh ago" indicator.
func LastSyncedPath() string {
	home, _ := os.UserHomeDir()
	dir := filepath.Join(home, "Library", "Application Support", "mailctl")
	_ = os.MkdirAll(dir, 0755)
	return filepath.Join(dir, "last_synced")
}

// UIStatePath is where the TUI persists small preferences (last active
// account tab, last unread-only filter) — see missionctl-core/uistate.
func UIStatePath() string {
	home, _ := os.UserHomeDir()
	dir := filepath.Join(home, "Library", "Application Support", "mailctl")
	_ = os.MkdirAll(dir, 0755)
	return filepath.Join(dir, "ui_state.json")
}

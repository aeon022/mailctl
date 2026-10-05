package config

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"

	coreconfig "github.com/aeon022/missionctl-core/config"
	"github.com/aeon022/missionctl-core/licensing"
)

// settings is this tool's config store (replaces the former global viper).
var settings = coreconfig.NewStore("config")

type Config struct {
	DataDir          string `yaml:"data_dir"`
	LicenseKey       string `yaml:"license_key"`
	LicenseStatus    string `yaml:"license_status"`
	LicenseBenefitID string `yaml:"license_benefit_id"`
}

// bundleBenefitID and mailctlBenefitID identify the missionctl Bundle's and
// mailctl's own individual-product license-key benefits in Polar. Both
// start empty (the mailctl-only product doesn't exist in Polar yet) — see
// licensing.Result.Grants: empty IDs fall back to "any active key under
// our org grants access", so this is a no-op until both are filled in
// once the individual product is created and its benefit ID is known.
const (
	bundleBenefitID  = "de1be860-1dfc-43da-99a8-206fb2573f09"
	mailctlBenefitID = "1b50057f-0f52-4b31-a89b-9d77eda8abae"
)

// IsPro reports whether a valid Pro/Bundle or mailctl-only license is
// active on this machine — gates the AI draft-reply feature (the `a` key).
func IsPro() bool {
	result := licensing.Result{Status: Active.LicenseStatus, BenefitID: Active.LicenseBenefitID}
	return result.Grants(mailctlBenefitID, bundleBenefitID)
}

func PolarOrgID() string {
	if v := settings.GetString("polar_org_id"); v != "" {
		return v
	}
	return licensing.DefaultOrgID
}

// SetLicense persists the license key/status/benefit to
// ~/.config/mailctl/config.yaml and updates Active immediately.
func SetLicense(key, status, benefitID string) error {
	settings.Set("license_key", key)
	settings.Set("license_status", status)
	settings.Set("license_benefit_id", benefitID)
	Active.LicenseKey = key
	Active.LicenseStatus = status
	Active.LicenseBenefitID = benefitID
	home, _ := os.UserHomeDir()
	cfgDir := filepath.Join(home, ".config", "mailctl")
	if err := os.MkdirAll(cfgDir, 0755); err != nil {
		return err
	}
	return settings.Write(filepath.Join(cfgDir, "config.yaml"))
}

var Active Config

// appSupportDirFor returns the OS-appropriate application-support directory
// for the given goos/home — a pure function (goos as a parameter, not
// runtime.GOOS directly) so both branches are unit-testable from a single
// compiled test binary, regardless of which OS actually runs the test.
func appSupportDirFor(goos, home string) string {
	if goos == "linux" {
		if xdg := os.Getenv("XDG_DATA_HOME"); xdg != "" {
			return filepath.Join(xdg, "mailctl")
		}
		return filepath.Join(home, ".local", "share", "mailctl")
	}
	return filepath.Join(home, "Library", "Application Support", "mailctl")
}

func appSupportDir() string {
	home, _ := os.UserHomeDir()
	return appSupportDirFor(runtime.GOOS, home)
}

func Load() error {
	home, _ := os.UserHomeDir()
	cfgDir := filepath.Join(home, ".config", "mailctl")
	_ = os.MkdirAll(cfgDir, 0755)

	settings.SetEnvPrefix("MAILCTL")
	settings.AddPath(cfgDir)

	if err := settings.Read(); err != nil {
		if !errors.Is(err, coreconfig.ErrNotFound) {
			return err
		}
		_ = settings.Write(filepath.Join(cfgDir, "config.yaml"))
	}
	return settings.Unmarshal(&Active)
}

// DBPathOverride, when non-empty, overrides DBPath()'s return value. Used by tests
// to point at a temporary database instead of the real one on disk.
var DBPathOverride string

// DBPath returns the database file path. DBPathOverride (test-only) wins
// if set; otherwise data_dir (config key, also settable via
// MAILCTL_DATA_DIR) points it at a user-chosen directory — e.g. inside
// iCloud Drive or Dropbox — resolved via coreconfig.ResolveDir; with
// neither set, the private default (~/Library/Application Support/mailctl)
// is unchanged from before this existed.
func DBPath() string {
	if DBPathOverride != "" {
		return DBPathOverride
	}
	if dir := settings.GetString("data_dir"); dir != "" {
		resolved, _ := coreconfig.ResolveDir("mailctl", dir)
		return filepath.Join(resolved, "mailctl.db")
	}
	dir := appSupportDir()
	_ = os.MkdirAll(dir, 0755)
	return filepath.Join(dir, "mailctl.db")
}

// Shared reports whether DBPath currently resolves to a user-configured
// directory (data_dir) rather than the tool's private default.
func Shared() bool {
	return DBPathOverride == "" && settings.GetString("data_dir") != ""
}

// appFile returns the path to name inside mailctl's private app-support
// directory, creating that directory if needed.
func appFile(name string) string {
	dir := appSupportDir()
	_ = os.MkdirAll(dir, 0755)
	return filepath.Join(dir, name)
}

// LastSyncedPath is the marker file (see missionctl-core/lastsync) tracking
// when a sync last completed, for the TUI's "synced Xh ago" indicator.
func LastSyncedPath() string {
	return appFile("last_synced")
}

// UIStatePath is where the TUI persists small preferences (last active
// account tab, last unread-only filter) — see missionctl-core/uistate.
func UIStatePath() string {
	return appFile("ui_state.json")
}

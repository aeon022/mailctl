package config

import (
	"os"
	"path/filepath"

	"github.com/spf13/viper"
)

type Config struct {
	DefaultAccount string `mapstructure:"default_account"`
	DefaultFrom    string `mapstructure:"default_from"`
	InboxMailbox   string `mapstructure:"inbox_mailbox"`
	SyncCount      int    `mapstructure:"sync_count"` // messages to sync per account
}

var Active Config

func Load() error {
	home, _ := os.UserHomeDir()
	cfgDir := filepath.Join(home, ".config", "mailctl")
	_ = os.MkdirAll(cfgDir, 0755)

	viper.SetConfigName("config")
	viper.SetConfigType("yaml")
	viper.AddConfigPath(cfgDir)

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

func DBPath() string {
	home, _ := os.UserHomeDir()
	dir := filepath.Join(home, "Library", "Application Support", "mailctl")
	_ = os.MkdirAll(dir, 0755)
	return filepath.Join(dir, "mailctl.db")
}

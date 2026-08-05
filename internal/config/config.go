package config

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/viper"
)

// Config holds all configuration
type Config struct {
	Defaults Defaults `mapstructure:"defaults"`
	Cache    Cache    `mapstructure:"cache"`
	Network  Network  `mapstructure:"network"`
	Auth     Auth     `mapstructure:"auth"`
}

type Defaults struct {
	Platform      string `mapstructure:"platform"`
	Connections   int    `mapstructure:"connections"`
	Jobs          int    `mapstructure:"jobs"`
	MinSplitSize  string `mapstructure:"min_split_size"`
	MaxTries      int    `mapstructure:"max_tries"`
	RetryWait     string `mapstructure:"retry_wait"`
	CheckIntegrity bool  `mapstructure:"check_integrity"`
}

type Cache struct {
	Dir              string `mapstructure:"dir"`
	MaxSize          string `mapstructure:"max_size"`
	KeepAfterSuccess bool   `mapstructure:"keep_after_success"`
}

type Network struct {
	Proxy          string `mapstructure:"proxy"`
	Timeout        string `mapstructure:"timeout"`
	ConnectTimeout string `mapstructure:"connect_timeout"`
	UserAgent      string `mapstructure:"user_agent"`
}

type Auth struct {
	Registries map[string]RegistryAuth `mapstructure:"registries"`
}

type RegistryAuth struct {
	Username    string `mapstructure:"username"`
	Password    string `mapstructure:"password"`
	PasswordEnv string `mapstructure:"password_env"`
}

// Load loads configuration from file
// Priority: --config flag > ./dpull.yaml > ~/.dpull/config.yaml
func Load(configFile string) (*Config, error) {
	v := viper.New()

	// Set defaults
	v.SetDefault("defaults.platform", "")
	v.SetDefault("defaults.connections", 8)
	v.SetDefault("defaults.jobs", 3)
	v.SetDefault("defaults.min_split_size", "20M")
	v.SetDefault("defaults.max_tries", 10)
	v.SetDefault("defaults.retry_wait", "5s")
	v.SetDefault("defaults.check_integrity", true)

	v.SetDefault("cache.dir", defaultCacheDir())
	v.SetDefault("cache.max_size", "100G")
	v.SetDefault("cache.keep_after_success", false)

	v.SetDefault("network.proxy", "")
	v.SetDefault("network.timeout", "60s")
	v.SetDefault("network.connect_timeout", "15s")
	v.SetDefault("network.user_agent", "dpull/1.0")

	// Config file paths
	v.SetConfigName("config")
	v.SetConfigType("yaml")

	if configFile != "" {
		// Explicit config file
		v.SetConfigFile(configFile)
	} else {
		// Search in current dir and ~/.dpull/
		v.AddConfigPath(".")
		v.AddConfigPath(filepath.Join(os.Getenv("HOME"), ".dpull"))
	}

	// Read config file (optional)
	if err := v.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			return nil, fmt.Errorf("read config: %w", err)
		}
		// Config file not found is okay, use defaults
	}

	// Bind environment variables
	// Proxy precedence: flag > config > env
	v.SetEnvPrefix("DPULL")
	v.AutomaticEnv()

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("unmarshal config: %w", err)
	}

	// If no proxy in config, check HTTP_PROXY/HTTPS_PROXY
	if cfg.Network.Proxy == "" {
		if proxy := os.Getenv("HTTP_PROXY"); proxy != "" {
			cfg.Network.Proxy = proxy
		} else if proxy := os.Getenv("HTTPS_PROXY"); proxy != "" {
			cfg.Network.Proxy = proxy
		}
	}

	return &cfg, nil
}

func defaultCacheDir() string {
	home := os.Getenv("HOME")
	if home == "" {
		home = "."
	}
	return filepath.Join(home, ".dpull", "cache")
}

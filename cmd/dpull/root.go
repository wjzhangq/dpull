package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"github.com/wjzhangq/dpull/pkg/version"
)

var (
	cfgFile      string
	cacheDir     string
	proxy        string
	jobs         int
	conns        int
	minSplit     int64
	maxRetries   int
	progressMode string
	platform     string
	mirrorHost   string
	mirrorPath   string
	autoMirror   bool
	plainHTTP    bool
)

var rootCmd = &cobra.Command{
	Use:   "dpull [flags] IMAGE",
	Short: "Docker image downloader with multi-connection resume support",
	Long: `dpull downloads Docker images with byte-level resume capability.

Supports multi-connection downloads, mirror rewriting, and proxy configuration.
Downloaded images can be loaded with 'docker load -i <file>'.

Examples:
  # Basic pull
  dpull nginx:1.27

  # With proxy
  dpull --proxy http://127.0.0.1:7890 nginx:1.27

  # Pull through a named mirror. docker.io is tried first unless -m is set.
  dpull -m 1ms nginx:1.27

  # Rename the image stored in the tar
  dpull -m docker.1ms.run --name mynginx:1.27 nginx:1.27

  # Multi-connection download
  dpull -x 16 -j 5 docker.io/lmsysorg/sglang:v0.5.15`,
	Version: version.String(),
	RunE:    runPull,
	Args:    cobra.ExactArgs(1),
}

func init() {
	// Config file
	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default: ./dpull.yaml or ~/.dpull/config.yaml)")

	// Cache directory
	home, _ := os.UserHomeDir()
	defaultCache := filepath.Join(home, ".dpull", "cache")
	rootCmd.PersistentFlags().StringVar(&cacheDir, "cache-dir", defaultCache, "cache directory for blobs and state")

	// Network flags
	rootCmd.PersistentFlags().StringVar(&proxy, "proxy", "", "HTTP proxy URL (overrides HTTP_PROXY env)")
	rootCmd.PersistentFlags().IntVarP(&jobs, "jobs", "j", 3, "max concurrent blob downloads")
	rootCmd.PersistentFlags().IntVarP(&conns, "connections", "x", 8, "connections per blob")
	rootCmd.PersistentFlags().Int64Var(&minSplit, "min-split-size", 20*1024*1024, "minimum size to enable splitting (bytes)")
	rootCmd.PersistentFlags().IntVar(&maxRetries, "max-retries", 10, "max retries per piece")

	// Progress display
	rootCmd.PersistentFlags().StringVar(&progressMode, "progress", "bar", "progress mode: bar, plain, json, none")

	// Platform selection
	rootCmd.PersistentFlags().StringVar(&platform, "platform", "", "target platform (e.g., linux/amd64, linux/arm64)")

	// Mirror configuration. The address is a registry host, optionally with a
	// path prefix. The image repository is appended, so -m docker.1ms.run
	// fetches docker.1ms.run/library/nginx. --mirror-path is only for a layout
	// that cannot be written as a prefix.
	rootCmd.PersistentFlags().StringVarP(&mirrorHost, "mirror", "m", "", "mirror to use (1ms, xuanyuan, daocloud, hubfast, dockerproxy, or a host)")
	rootCmd.PersistentFlags().StringVar(&mirrorPath, "mirror-path", "", "advanced path template; leave empty to use the mirror address as-is")
	rootCmd.PersistentFlags().BoolVar(&autoMirror, "auto-mirror", true, "if docker.io is unreachable, try the built-in mirrors")

	// Plain HTTP (for local testing)
	rootCmd.PersistentFlags().BoolVar(&plainHTTP, "plain-http", false, "use HTTP instead of HTTPS (insecure, for testing)")

	// Bind flags to viper
	viper.BindPFlag("cache_dir", rootCmd.PersistentFlags().Lookup("cache-dir"))
	viper.BindPFlag("network.proxy", rootCmd.PersistentFlags().Lookup("proxy"))
	viper.BindPFlag("download.jobs", rootCmd.PersistentFlags().Lookup("jobs"))
	viper.BindPFlag("download.connections", rootCmd.PersistentFlags().Lookup("connections"))
	viper.BindPFlag("download.min_split_size", rootCmd.PersistentFlags().Lookup("min-split-size"))
	viper.BindPFlag("download.max_retries", rootCmd.PersistentFlags().Lookup("max-retries"))
	viper.BindPFlag("ui.progress", rootCmd.PersistentFlags().Lookup("progress"))
	viper.BindPFlag("platform", rootCmd.PersistentFlags().Lookup("platform"))
	viper.BindPFlag("mirror.endpoint", rootCmd.PersistentFlags().Lookup("mirror"))
	viper.BindPFlag("mirror.path_template", rootCmd.PersistentFlags().Lookup("mirror-path"))

	// Add subcommands
	rootCmd.AddCommand(mirrorsCmd)
	rootCmd.AddCommand(infoCmd)
	rootCmd.AddCommand(lsCmd)
	rootCmd.AddCommand(resumeCmd)
	rootCmd.AddCommand(rmCmd)
	rootCmd.AddCommand(configCmd)
	rootCmd.AddCommand(versionCmd)

	// Set PersistentPreRunE after rootCmd is fully initialized
	rootCmd.PersistentPreRunE = func(cmd *cobra.Command, args []string) error {
		return initConfig()
	}
}

func initConfig() error {
	if cfgFile != "" {
		viper.SetConfigFile(cfgFile)
	} else {
		// Look for config in current directory
		viper.AddConfigPath(".")
		viper.SetConfigName("dpull")

		// Also look in ~/.dpull/
		if home, err := os.UserHomeDir(); err == nil {
			viper.AddConfigPath(filepath.Join(home, ".dpull"))
		}
	}

	viper.SetConfigType("yaml")
	viper.AutomaticEnv()

	// Proxy env variable mapping
	viper.BindEnv("network.proxy", "HTTP_PROXY", "HTTPS_PROXY", "http_proxy", "https_proxy")

	if err := viper.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			return fmt.Errorf("read config: %w", err)
		}
	}

	// Apply config values to globals if flags weren't explicitly set
	cmd := rootCmd
	if !cmd.PersistentFlags().Changed("cache-dir") {
		cacheDir = viper.GetString("cache_dir")
	}
	if !cmd.PersistentFlags().Changed("proxy") {
		proxy = viper.GetString("network.proxy")
	}
	if !cmd.PersistentFlags().Changed("jobs") {
		jobs = viper.GetInt("download.jobs")
		if jobs == 0 {
			jobs = 3
		}
	}
	if !cmd.PersistentFlags().Changed("connections") {
		conns = viper.GetInt("download.connections")
		if conns == 0 {
			conns = 8
		}
	}
	if !cmd.PersistentFlags().Changed("progress") {
		progressMode = viper.GetString("ui.progress")
		if progressMode == "" {
			progressMode = "bar"
		}
	}

	return nil
}

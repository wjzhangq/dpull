package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var configCmd = &cobra.Command{
	Use:   "config",
	Short: "Manage configuration",
}

var configInitCmd = &cobra.Command{
	Use:   "init",
	Short: "Create a default configuration file",
	RunE:  runConfigInit,
}

var configShowCmd = &cobra.Command{
	Use:   "show",
	Short: "Display current configuration",
	RunE:  runConfigShow,
}

func init() {
	configCmd.AddCommand(configInitCmd)
	configCmd.AddCommand(configShowCmd)
}

func runConfigInit(cmd *cobra.Command, args []string) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("get home dir: %w", err)
	}

	configDir := filepath.Join(home, ".dpull")
	configPath := filepath.Join(configDir, "config.yaml")

	// Check if config already exists
	if _, err := os.Stat(configPath); err == nil {
		return fmt.Errorf("config file already exists: %s (remove it first if you want to recreate)", configPath)
	}

	// Create directory
	if err := os.MkdirAll(configDir, 0755); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}

	// Default config template
	defaultConfig := `# dpull configuration file

# Default settings
defaults:
  platform: ""              # Target platform (e.g., linux/amd64, linux/arm64)
  connections: 8            # Connections per blob
  jobs: 3                   # Max concurrent blob downloads
  min_split_size: "20M"     # Minimum size to enable splitting
  max_tries: 10             # Max retries per piece
  check_integrity: true     # Verify sha256 after download

# Cache settings
cache:
  dir: ~/.dpull/cache       # Cache directory
  max_size: "100G"          # Max cache size (not enforced yet)
  keep_after_success: false # Keep blobs after successful tar creation

# Network settings
network:
  proxy: ""                 # HTTP proxy (e.g., http://127.0.0.1:7890)
  timeout: "60s"            # Request timeout
  connect_timeout: "15s"    # Connection timeout
  user_agent: "dpull/1.0"   # User agent

# Authentication (optional)
# Credentials here are used only if not found in ~/.docker/config.json
auth:
  registries: {}
    # docker.io:
    #   username: "myuser"
    #   password: "mypass"
    # ghcr.io:
    #   username: "myuser"
    #   password_env: "GITHUB_TOKEN"

# Mirror configuration (optional)
# mirror:
#   endpoint: "mirror.example.com"
#   path_template: "{registry}/{repo}"
`

	// Write config file
	if err := os.WriteFile(configPath, []byte(defaultConfig), 0644); err != nil {
		return fmt.Errorf("write config: %w", err)
	}

	fmt.Printf("Created config file: %s\n", configPath)
	fmt.Println("\nEdit this file to customize your settings.")
	return nil
}

func runConfigShow(cmd *cobra.Command, args []string) error {
	fmt.Println("Current configuration:")
	fmt.Println()

	// Show config file location
	if viper.ConfigFileUsed() != "" {
		fmt.Printf("Config file: %s\n", viper.ConfigFileUsed())
	} else {
		fmt.Println("Config file: (none, using defaults)")
	}
	fmt.Println()

	// Show key settings
	fmt.Printf("Cache directory:  %s\n", cacheDir)
	fmt.Printf("Proxy:            %s\n", proxyOrNone(proxy))
	fmt.Printf("Jobs:             %d\n", jobs)
	fmt.Printf("Connections:      %d\n", conns)
	fmt.Printf("Min split size:   %d bytes\n", minSplit)
	fmt.Printf("Max retries:      %d\n", maxRetries)
	fmt.Printf("Progress mode:    %s\n", progressMode)
	fmt.Printf("Platform:         %s\n", platformOrDefault(platform))

	if mirrorHost != "" {
		fmt.Println()
		fmt.Printf("Mirror endpoint:  %s\n", mirrorHost)
		fmt.Printf("Mirror path:      %s\n", mirrorPath)
	}

	return nil
}

func proxyOrNone(p string) string {
	if p == "" {
		return "(none)"
	}
	return p
}

func platformOrDefault(p string) string {
	if p == "" {
		return "(auto-detect)"
	}
	return p
}

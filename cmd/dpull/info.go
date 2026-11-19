package main

import (
	"context"
	"fmt"
	"runtime"
	"time"

	"github.com/docker/go-units"
	"github.com/spf13/cobra"
	"github.com/wjzhangq/dpull/internal/ref"
)

var infoCmd = &cobra.Command{
	Use:   "info IMAGE",
	Short: "Display image information without downloading",
	Args:  cobra.ExactArgs(1),
	RunE:  runInfo,
}

func runInfo(cmd *cobra.Command, args []string) error {
	canonical := args[0]

	// Parse reference
	imageRef, err := ref.Parse(canonical)
	if err != nil {
		return fmt.Errorf("parse reference: %w", err)
	}

	// Determine platform
	targetPlatform := platform
	if targetPlatform == "" {
		targetPlatform = fmt.Sprintf("%s/%s", runtime.GOOS, runtime.GOARCH)
	}

	regClient := setupRegistryClient(imageRef)

	fmt.Printf("Fetching manifest for %s...\n\n", canonical)
	fetchRef, manifest, _, err := prepareFetch(context.Background(), regClient, imageRef, targetPlatform)
	if err != nil {
		return err
	}

	// Fetch config for additional details
	config, err := regClient.GetConfig(fetchRef, manifest.Config.Digest)
	if err != nil {
		fmt.Fprintf(cmd.OutOrStderr(), "warn: fetch config: %v\n", err)
		config = nil
	}

	// Display info
	fmt.Printf("Image:      %s\n", canonical)
	fmt.Printf("Digest:     %s\n", manifest.Digest)
	printPlatformChoice(targetPlatform, manifest)
	if config != nil && manifest.Platform == "" {
		fmt.Printf("Platform:   %s/%s\n", config.OS, config.Architecture)
	}
	if config != nil && !config.Created.IsZero() {
		fmt.Printf("Created:    %s\n", config.Created.Format(time.RFC3339))
	}
	fmt.Printf("\n")
	fmt.Printf("Layers:     %d\n", len(manifest.Layers))
	fmt.Printf("Total Size: %s (compressed)\n", units.HumanSize(float64(manifest.TotalSize())))

	if len(manifest.Layers) > 0 {
		fmt.Printf("\nLayers:\n")
		for i, layer := range manifest.Layers {
			fmt.Printf("  %2d. %s  %s\n", i+1, layer.Digest, units.HumanSize(float64(layer.Size)))
		}
	}

	// Test mirror connectivity if configured
	if mirrorHost != "" {
		fmt.Printf("\nTesting mirror: %s\n", mirrorHost)
		// For simplicity, just report that mirror is configured
		// Full connectivity test would require making a test blob request
		fmt.Printf("  Mirror configured (test with actual download)\n")
	}

	return nil
}

package main

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/wjzhangq/dpull/internal/mirror"
)

var mirrorsCmd = &cobra.Command{
	Use:   "mirrors",
	Short: "List built-in Docker Hub mirrors",
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Printf("%-14s  %-28s  %-10s  %s\n", "NAME", "HOST", "TIER", "TITLE")
		for _, site := range mirror.Builtin() {
			tier := "backup"
			if site.Tier == mirror.TierPrimary {
				tier = "primary"
			}
			fmt.Printf("%-14s  %-28s  %-10s  %s\n", site.Name, site.Host, tier, site.Title)
		}
		fmt.Println()
		fmt.Println("Force one mirror:  dpull -m 1ms nginx:1.27")
		fmt.Println("Any other host:    dpull -m docker.example.com nginx:1.27")
		if autoMirror {
			fmt.Println("Auto-mirror is on: docker.io is tried first, then these mirrors.")
		} else {
			fmt.Println("Auto-mirror is off.")
		}
		return nil
	},
}

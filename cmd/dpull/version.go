package main

import (
	"github.com/spf13/cobra"
	"github.com/wjzhangq/dpull/pkg/version"
)

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print version information",
	Run: func(cmd *cobra.Command, args []string) {
		cmd.Println(version.String())
	},
}

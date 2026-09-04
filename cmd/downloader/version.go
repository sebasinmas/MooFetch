package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

var (
	Version   = "dev"
	Commit    = "none"
	BuildDate = "unknown"
)

func formatVersion() string {
	return fmt.Sprintf("GoDownloader v%s (commit: %s, built: %s)", Version, Commit, BuildDate)
}

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Muestra la versión instalada de GoDownloader",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println(formatVersion())
	},
}

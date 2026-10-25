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
	return fmt.Sprintf("MooFetch v%s (commit: %s, built: %s)", Version, Commit, BuildDate)
}

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Muestra la versión, el commit y la fecha de compilación",
	// Global flags are irrelevant here; keep its help short.
	Annotations: map[string]string{localOnlyAnnotation: "1"},
	Run: func(_ *cobra.Command, _ []string) {
		fmt.Println(formatVersion())
	},
}

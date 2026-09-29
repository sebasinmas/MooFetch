package main

import (
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// resetFlags restores every rootCmd/runCmd flag (and the package-level flag
// variables bound to them) to its default, now and when the test ends, so
// tests sharing the global Cobra commands do not leak state (-count=N safe).
func resetFlags(t *testing.T) {
	t.Helper()
	reset := func() {
		for _, c := range []*cobra.Command{rootCmd, runCmd} {
			for _, fs := range []*pflag.FlagSet{c.Flags(), c.PersistentFlags()} {
				fs.VisitAll(func(f *pflag.Flag) {
					if err := f.Value.Set(f.DefValue); err != nil {
						t.Fatalf("reset flag %s: %v", f.Name, err)
					}
					f.Changed = false
				})
			}
		}
	}
	reset()
	t.Cleanup(reset)
}

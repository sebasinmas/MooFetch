package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestCobra_HelpOutput(t *testing.T) {
	buf := new(bytes.Buffer)
	rootCmd.SetOut(buf)
	rootCmd.SetErr(buf)
	rootCmd.SetArgs([]string{"--help"})

	err := rootCmd.Execute()
	if err != nil {
		t.Fatalf("expected --help to execute successfully, got: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "Usage:") {
		t.Errorf("expected help output to contain 'Usage:'")
	}
	if !strings.Contains(out, "run") {
		t.Errorf("expected help output to contain 'run' subcommand")
	}
	if !strings.Contains(out, "version") {
		t.Errorf("expected help output to contain 'version' subcommand")
	}
	if !strings.Contains(out, "--concurrency") {
		t.Errorf("expected help output to contain '--concurrency'")
	}
}

func TestCobra_VersionSubcommand(t *testing.T) {
	out := formatVersion()
	if !strings.Contains(out, "MooFetch v") {
		t.Errorf("expected formatVersion to contain 'MooFetch v', got %q", out)
	}
}

func TestCobra_RunMissingURLs(t *testing.T) {
	buf := new(bytes.Buffer)
	rootCmd.SetOut(buf)
	rootCmd.SetErr(buf)
	rootCmd.SetArgs([]string{"run", "--cookie", "test", "--urls", ""})

	err := rootCmd.Execute()
	if err == nil {
		t.Fatalf("expected error when running run command without URLs, got nil")
	}
}

func TestCobra_HelpCleanup(t *testing.T) {
	for _, args := range [][]string{{"--help"}, {"run", "--help"}} {
		buf := new(bytes.Buffer)
		rootCmd.SetOut(buf)
		rootCmd.SetErr(buf)
		rootCmd.SetArgs(args)
		err := rootCmd.Execute()
		// Cobra keeps the parsed --help flag between executions; reset it so
		// later tests sharing rootCmd do not print help instead of running.
		resetHelpFlags(t)
		if err != nil {
			t.Fatalf("%v: %v", args, err)
		}
		out := buf.String()
		for _, unwanted := range []string{"completion", "--logger", "--plain"} {
			if strings.Contains(out, unwanted) {
				t.Errorf("%v: help must not contain %q", args, unwanted)
			}
		}
		if !strings.Contains(out, "Examples:") || !strings.Contains(out, "moofetch") {
			t.Errorf("%v: help must include examples", args)
		}
		for _, want := range []string{"--output", "--cookie", "--headless"} {
			if !strings.Contains(out, want) {
				t.Errorf("%v: help must contain %q", args, want)
			}
		}
	}
}

func resetHelpFlags(t *testing.T) {
	t.Helper()
	for _, c := range []*cobra.Command{rootCmd, runCmd} {
		if f := c.Flags().Lookup("help"); f != nil {
			if err := f.Value.Set("false"); err != nil {
				t.Fatal(err)
			}
			f.Changed = false
		}
	}
}

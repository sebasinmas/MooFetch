package main

import (
	"bytes"
	"strings"
	"testing"
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
	if !strings.Contains(out, "GoDownloader v") {
		t.Errorf("expected formatVersion to contain 'GoDownloader v', got %q", out)
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

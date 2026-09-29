package main

import (
	"bytes"
	"context"
	"os"
	"strings"
	"testing"
)

func TestPipedExecution_RunWithDemo(t *testing.T) {
	resetFlags(t)
	// Simulate stdin by writing to a pipe
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("failed to create pipe: %v", err)
	}

	_, _ = w.WriteString("https://campusvirtual.ufro.cl/mod/resource/view.php?id=888\n")
	_ = w.Close()

	oldStdin := os.Stdin
	os.Stdin = r
	defer func() {
		os.Stdin = oldStdin
		_ = r.Close()
	}()

	tempDir := t.TempDir()

	rootCmd.SetArgs([]string{
		"run",
		"--cookie", "demo_token",
		"--output", tempDir,
		"--demo",
		"--headless",
	})

	var outBuf bytes.Buffer
	rootCmd.SetOut(&outBuf)
	rootCmd.SetErr(&outBuf)

	err = Execute(context.Background())
	if err != nil {
		t.Fatalf("unexpected error during piped run: %v", err)
	}

	// Verify file was downloaded
	entries, err := os.ReadDir(tempDir)
	if err != nil {
		t.Fatalf("failed to read temp dir: %v", err)
	}

	if len(entries) == 0 {
		t.Fatalf("expected downloaded file in %s, found none", tempDir)
	}

	var foundPDF bool
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".pdf") {
			foundPDF = true
			break
		}
	}
	if !foundPDF {
		t.Fatalf("expected .pdf file in %s, got: %+v", tempDir, entries)
	}
}

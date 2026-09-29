package demo_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sebasinmas/MooFetch/internal/domain"
	"github.com/sebasinmas/MooFetch/internal/plugins/demo"
)

type downloaderContract interface {
	Name() string
	CanHandle(rawURL string) bool
	Download(ctx context.Context, task domain.Task, progress domain.ProgressFunc) (*domain.Result, error)
}

func TestDemoPlugin_Interface(_ *testing.T) {
	var _ downloaderContract = demo.New()
}

func TestDemoPlugin_CanHandle(t *testing.T) {
	p := demo.New()
	if !p.CanHandle("https://campusvirtual.ufro.cl/mod/resource/view.php?id=123") {
		t.Errorf("expected CanHandle to return true for any URL")
	}
	if !p.CanHandle("arbitrary-string") {
		t.Errorf("expected CanHandle to return true for arbitrary string")
	}
}

func TestDemoPlugin_ExtractPDFResource(t *testing.T) {
	tests := []struct {
		url      string
		taskID   int
		expected string
	}{
		{
			url:      "https://campusvirtual.ufro.cl/mod/resource/view.php?id=123/Apunte%201.pdf",
			taskID:   1,
			expected: "Apunte 1.pdf",
		},
		{
			url:      "https://campusvirtual.ufro.cl/mod/resource/view.php?id=456&file=Guia_02.pdf",
			taskID:   2,
			expected: "Guia_02.pdf",
		},
		{
			url:      "https://campusvirtual.ufro.cl/mod/resource/view.php?id=789",
			taskID:   3,
			expected: "Recurso_789.pdf",
		},
		{
			url:      "https://example.com/material/Clase_03_Intro.pdf",
			taskID:   4,
			expected: "Clase_03_Intro.pdf",
		},
		{
			url:      "Calculo_I_Syllabus.pdf",
			taskID:   5,
			expected: "Calculo_I_Syllabus.pdf",
		},
		{
			url:      "apunte-algebra",
			taskID:   6,
			expected: "apunte-algebra.pdf",
		},
	}

	for _, tc := range tests {
		actual := demo.ExtractPDFResource(tc.url, tc.taskID)
		if actual != tc.expected {
			t.Errorf("ExtractPDFResource(%q, %d) = %q; want %q", tc.url, tc.taskID, actual, tc.expected)
		}
	}
}

func TestDemoPlugin_Download(t *testing.T) {
	tempDir := t.TempDir()
	p := demo.New(
		demo.WithStepDelay(1*time.Millisecond),
		demo.WithWriteDummyFiles(true),
	)

	task := domain.Task{
		ID:        1,
		URL:       "https://campusvirtual.ufro.cl/mod/resource/view.php?id=101/Syllabus_2026.pdf",
		Cookie:    "demo_cookie",
		OutputDir: tempDir,
	}

	var progressUpdates int
	res, err := p.Download(context.Background(), task, func(u domain.ProgressUpdate) {
		progressUpdates++
		if u.Filename != "Syllabus_2026.pdf" {
			t.Errorf("unexpected filename in update: %s", u.Filename)
		}
	})

	if err != nil {
		t.Fatalf("unexpected error during demo download: %v", err)
	}

	if res.Filename != "Syllabus_2026.pdf" {
		t.Errorf("expected res.Filename 'Syllabus_2026.pdf', got %q", res.Filename)
	}

	if progressUpdates < 10 {
		t.Errorf("expected at least 10 progress updates, got %d", progressUpdates)
	}

	// Verify dummy file created
	filePath := filepath.Join(tempDir, "Syllabus_2026.pdf")
	content, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("expected file to exist: %v", err)
	}
	if !strings.HasPrefix(string(content), "%PDF-1.4") {
		t.Errorf("expected valid PDF header in dummy file")
	}
}

func TestDemoPlugin_Cancellation(t *testing.T) {
	p := demo.New(demo.WithStepDelay(100 * time.Millisecond))
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	task := domain.Task{
		ID:        2,
		URL:       "https://campusvirtual.ufro.cl/mod/resource/view.php?id=102",
		OutputDir: t.TempDir(),
	}

	_, err := p.Download(ctx, task, nil)
	if err == nil {
		t.Errorf("expected context cancellation error, got nil")
	}
}

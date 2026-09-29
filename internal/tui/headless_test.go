package tui_test

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"moofetch/internal/domain"
	"moofetch/internal/kernel"
	"moofetch/internal/tui"
)

type mockPlugin struct {
	canHandle bool
	dlErr     error
	filename  string
	bytes     int64
}

func (m *mockPlugin) Name() string { return "mock" }
func (m *mockPlugin) CanHandle(rawURL string) bool {
	return m.canHandle
}
func (m *mockPlugin) Download(ctx context.Context, task domain.Task, emitProgress domain.ProgressFunc) (*domain.Result, error) {
	if m.dlErr != nil {
		return nil, m.dlErr
	}
	if emitProgress != nil {
		emitProgress(domain.ProgressUpdate{
			TaskID:     task.ID,
			URL:        task.URL,
			Filename:   m.filename,
			BytesRead:  m.bytes,
			TotalBytes: m.bytes,
		})
	}
	return &domain.Result{
		TaskID:     task.ID,
		URL:        task.URL,
		Filename:   m.filename,
		BytesRead:  m.bytes,
		TotalBytes: m.bytes,
	}, nil
}

func TestRunHeadlessProgress(t *testing.T) {
	t.Parallel()

	plugin := &mockPlugin{
		canHandle: true,
		filename:  "test.pdf",
		bytes:     2048,
	}

	k := kernel.New(
		kernel.WithConcurrency(2),
		kernel.WithPlugins([]kernel.DownloaderPlugin{plugin}),
	)

	tasks := []domain.Task{
		{ID: 1, URL: "https://example.com/file1.pdf"},
		{ID: 2, URL: "https://example.com/file2.pdf"},
	}

	var buf bytes.Buffer
	results, err := tui.RunHeadlessProgress(context.Background(), k, tasks, &buf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}

	output := buf.String()
	if !strings.Contains(output, "Iniciando descarga de 2 archivo(s)") {
		t.Errorf("expected start banner, got:\n%s", output)
	}
	if !strings.Contains(output, "Completado: test.pdf (2.0 KB)") {
		t.Errorf("expected completion message, got:\n%s", output)
	}
	if !strings.Contains(output, "Finalizado: 2 completadas, 0 fallidas") {
		t.Errorf("expected summary message, got:\n%s", output)
	}
}

func TestRunHeadlessProgress_WithFailure(t *testing.T) {
	t.Parallel()

	plugin := &mockPlugin{
		canHandle: true,
		dlErr:     errors.New("connection reset"),
	}

	k := kernel.New(
		kernel.WithConcurrency(1),
		kernel.WithPlugins([]kernel.DownloaderPlugin{plugin}),
	)

	tasks := []domain.Task{
		{ID: 1, URL: "https://example.com/broken.pdf"},
	}

	var buf bytes.Buffer
	results, err := tui.RunHeadlessProgress(context.Background(), k, tasks, &buf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(results) != 1 || results[0].Err == nil {
		t.Fatalf("expected 1 failed result, got %+v", results)
	}

	output := buf.String()
	if !strings.Contains(output, "Error: https://example.com/broken.pdf") {
		t.Errorf("expected error log in output, got:\n%s", output)
	}
	if !strings.Contains(output, "Finalizado: 0 completadas, 1 fallidas") {
		t.Errorf("expected summary of failures, got:\n%s", output)
	}
}

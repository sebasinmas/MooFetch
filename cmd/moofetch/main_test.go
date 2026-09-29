package main

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/sebasinmas/MooFetch/internal/domain"
	"github.com/sebasinmas/MooFetch/internal/kernel"
	"github.com/sebasinmas/MooFetch/internal/plugins/demo"
	"github.com/sebasinmas/MooFetch/internal/tui"
)

func TestDemoFlow_EndToEnd(t *testing.T) {
	tempDir := t.TempDir()

	// 1. Simulate demo form data with arbitrary token and links
	rawURLs := `
https://campusvirtual.ufro.cl/mod/resource/view.php?id=9901/01_Clase_Intro.pdf
campusvirtual.ufro.cl/mod/resource/view.php?id=9902/02_Guia_Practica.pdf
https://campusvirtual.ufro.cl/mod/resource/view.php?id=9903
Custom_Apunte.pdf
`
	urls := tui.CleanURLsDemo(rawURLs)
	if len(urls) != 4 {
		t.Fatalf("expected 4 URLs parsed, got %d", len(urls))
	}

	form := &tui.FormData{
		Cookie: "cualquier_token_showcase",
		URLs:   urls,
	}

	// 2. Create tasks
	tasks := createTasks(form, tempDir)
	if len(tasks) != 4 {
		t.Fatalf("expected 4 tasks, got %d", len(tasks))
	}

	// 3. Init kernel with demo plugin (fast step delay for test)
	demoPlugin := demo.New(
		demo.WithStepDelay(1*time.Millisecond),
		demo.WithWriteDummyFiles(true),
	)
	k := kernel.New(
		kernel.WithConcurrency(2),
		kernel.WithPlugins([]kernel.DownloaderPlugin{demoPlugin}),
	)

	var (
		mu     sync.Mutex
		events []domain.Event
	)
	results := k.Dispatch(context.Background(), tasks, func(ev domain.Event) {
		mu.Lock()
		events = append(events, ev)
		mu.Unlock()
	})

	if len(results) != 4 {
		t.Fatalf("expected 4 results, got %d", len(results))
	}

	for _, r := range results {
		if r.Err != nil {
			t.Errorf("task %d failed: %v", r.TaskID, r.Err)
		}
		if r.BytesRead <= 0 {
			t.Errorf("task %d: expected positive BytesRead, got %d", r.TaskID, r.BytesRead)
		}
	}

	expectedFilenames := []string{
		"01_Clase_Intro.pdf",
		"02_Guia_Practica.pdf",
		"Recurso_9903.pdf",
		"Custom_Apunte.pdf",
	}

	for i, exp := range expectedFilenames {
		if results[i].Filename != exp {
			t.Errorf("task %d: expected filename %q, got %q", i+1, exp, results[i].Filename)
		}
	}
}

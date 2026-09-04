package tui

import (
	"context"
	"fmt"
	"io"
	"os"
	"sync"

	"godownloader/internal/domain"
	"godownloader/internal/kernel"
)

// RunHeadlessProgress executes the tasks using kernel.Dispatch while streaming
// plain-text progress notifications to the specified writer (or os.Stderr if nil).
// It does not launch any Bubble Tea programs or TUI elements, making it suitable
// for non-interactive shells, scripts, and Unix pipelines.
func RunHeadlessProgress(ctx context.Context, k *kernel.Kernel, tasks []domain.Task, out io.Writer) ([]domain.Result, error) {
	if out == nil {
		out = os.Stderr
	}

	total := len(tasks)
	if total == 0 {
		return nil, nil
	}

	fmt.Fprintf(out, "🚀 Iniciando descarga de %d archivo(s)...\n", total)

	var mu sync.Mutex
	onEvent := func(ev domain.Event) {
		mu.Lock()
		defer mu.Unlock()

		switch ev.Type {
		case domain.EventTaskStarted:
			fmt.Fprintf(out, "[%d/%d] ▶ Descargando: %s\n", ev.TaskID, total, ev.URL)
		case domain.EventTaskCompleted:
			fmt.Fprintf(out, "[%d/%d] ✓ Completado: %s (%s)\n", ev.TaskID, total, ev.Filename, formatBytes(ev.Bytes))
		case domain.EventTaskFailed:
			fmt.Fprintf(out, "[%d/%d] ✗ Error: %s (%v)\n", ev.TaskID, total, ev.URL, ev.Err)
		}
	}

	results := k.Dispatch(ctx, tasks, onEvent)

	var successCount, failureCount int
	for _, res := range results {
		if res.Err != nil {
			failureCount++
		} else {
			successCount++
		}
	}

	fmt.Fprintf(out, "🏁 Finalizado: %d completadas, %d fallidas (total: %d)\n", successCount, failureCount, total)
	return results, nil
}

// Package main provides the CLI entrypoint for MooFetch.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/sebasinmas/MooFetch/internal/domain"
	"github.com/sebasinmas/MooFetch/internal/kernel"
	"github.com/sebasinmas/MooFetch/internal/logger"
	"github.com/sebasinmas/MooFetch/internal/plugins/demo"
	"github.com/sebasinmas/MooFetch/internal/plugins/moodle"
	"github.com/sebasinmas/MooFetch/internal/tui"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := Execute(ctx); err != nil {
		code := DetermineExitCode(ctx, err)
		if !errors.Is(err, context.Canceled) && !errors.Is(err, tui.ErrFormAborted) {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		}
		os.Exit(code)
	}
}

func setupLogger(logPath string, concurrency int, outputDir string, form *tui.FormData, isDemo bool) (*logger.Logger, string) {
	if logPath == "" {
		return nil, ""
	}

	l, err := logger.New(logPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Advertencia: no se pudo inicializar el registro de depuración: %v\n", err)
		return nil, ""
	}

	if isDemo {
		l.Printf("MooFetch inicializado en MODO DEMO. Concurrencia: %d | Directorio: %s", concurrency, outputDir)
	} else {
		l.Printf("MooFetch inicializado. Concurrencia: %d | Directorio: %s", concurrency, outputDir)
	}
	l.Printf("Cookie de sesión: %s", logger.RedactCookie(form.Cookie))
	l.Printf("Total de URLs en cola: %d", len(form.URLs))
	return l, logPath
}

func createTasks(form *tui.FormData, outputDir string) []domain.Task {
	tasks := make([]domain.Task, len(form.URLs))
	for i, u := range form.URLs {
		tasks[i] = domain.Task{
			ID:        i + 1,
			URL:       u,
			Cookie:    form.Cookie,
			OutputDir: outputDir,
		}
	}
	return tasks
}

func initKernel(concurrency int, l *logger.Logger, isDemo bool) *kernel.Kernel {
	opts := []kernel.Option{
		kernel.WithConcurrency(concurrency),
	}
	if isDemo {
		opts = append(opts,
			kernel.WithPlugins([]kernel.DownloaderPlugin{
				demo.New(demo.WithLogger(l)),
			}),
		)
	} else {
		opts = append(opts,
			kernel.WithPlugins([]kernel.DownloaderPlugin{
				moodle.New(moodle.WithLogger(l)),
			}),
		)
	}
	if l != nil {
		opts = append(opts, kernel.WithLogger(l))
	}
	return kernel.New(opts...)
}

func handleCompletion(results []domain.Result, logPath string) error {
	var failedResults []domain.Result
	for _, res := range results {
		if res.Err != nil {
			failedResults = append(failedResults, res)
		}
	}

	if logPath != "" {
		fmt.Printf("\n📄 Registro de depuración guardado en: %s\n", logPath)
	}

	if len(failedResults) > 0 {
		return &BatchError{Results: failedResults}
	}
	return nil
}

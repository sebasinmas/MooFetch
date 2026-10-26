// Package main provides the CLI entrypoint for MooFetch.
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
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
	code := realMain(ctx, os.Stderr)
	stop()
	os.Exit(code)
}

// realMain executes the CLI and returns the POSIX exit code, reporting
// errors on stderr. Split from main so it can be tested without os.Exit.
func realMain(ctx context.Context, stderr io.Writer) int {
	err := Execute(ctx)
	if err == nil {
		return ExitSuccess
	}
	if !errors.Is(err, context.Canceled) && !errors.Is(err, tui.ErrFormAborted) {
		_, _ = fmt.Fprintf(stderr, "Error: %v\n", err)
	}
	return DetermineExitCode(ctx, err)
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

// createTasks builds one task per file. On the API route (form.Items) each
// task carries the token in Task.Token, never in the URL, and is saved in its
// own sub-folder; the sub-folder is checked to stay inside outputDir.
func createTasks(form *tui.FormData, outputDir string) ([]domain.Task, error) {
	if len(form.Items) == 0 {
		tasks := make([]domain.Task, len(form.URLs))
		for i, u := range form.URLs {
			tasks[i] = domain.Task{
				ID:        i + 1,
				URL:       u,
				Cookie:    form.Cookie,
				OutputDir: outputDir,
			}
		}
		return tasks, nil
	}
	tasks := make([]domain.Task, len(form.Items))
	taken := map[string]int{} // folder + file name -> how many already use it
	for i, it := range form.Items {
		orig := fileNameOfURL(it.URL)
		sub, name := fitPath(pathGOOS, outputDir, it.SubDir, orig)
		dir, err := safeOutputDir(outputDir, sub)
		if err != nil {
			return nil, err
		}
		tasks[i] = domain.Task{
			ID:        i + 1,
			URL:       it.URL,
			Cookie:    form.Cookie,
			Token:     form.Token,
			TokenHost: tokenHostFor(form),
			OutputDir: dir,
		}
		// Only override the server's file name when it had to change.
		if final := uniqueName(taken, dir, name); final != orig {
			tasks[i].FileName = final
		}
	}
	return tasks, nil
}

// uniqueName returns name, or "name (2)", "name (3)"... when another task of
// the same folder already uses it, so two files never overwrite each other. An
// empty name (unknown) is returned as is.
func uniqueName(taken map[string]int, dir, name string) string {
	if name == "" {
		return ""
	}
	key := strings.ToLower(filepath.Join(dir, name))
	taken[key]++
	n := taken[key]
	if n == 1 {
		return name
	}
	ext := filepath.Ext(name)
	return fmt.Sprintf("%s (%d)%s", strings.TrimSuffix(name, ext), n, ext)
}

// tokenHostFor is the only host a web-service token may be sent to: the campus
// the user logged in to. Without a token there is nothing to pin.
func tokenHostFor(form *tui.FormData) string {
	if form.Token == "" {
		return ""
	}
	return strings.ToLower(form.Campus)
}

// safeOutputDir joins outputDir and subDir, refusing any result that is not
// inside outputDir (defense in depth: the wizard already sanitizes names).
func safeOutputDir(outputDir, subDir string) (string, error) {
	dir := filepath.Join(outputDir, filepath.FromSlash(subDir))
	rel, err := filepath.Rel(outputDir, dir)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("la subcarpeta %q queda fuera de la carpeta de salida", subDir)
	}
	return dir, nil
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

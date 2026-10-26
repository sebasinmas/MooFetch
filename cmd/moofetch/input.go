package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/sebasinmas/MooFetch/internal/domain"
	"github.com/sebasinmas/MooFetch/internal/tui"
)

// splitNonEmpty splits s by sep and drops blank, trimmed items.
func splitNonEmpty(s, sep string) []string {
	var out []string
	for _, p := range strings.Split(s, sep) {
		if trimmed := strings.TrimSpace(p); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

// readLines returns the non-blank, trimmed lines of r.
func readLines(r io.Reader) ([]string, error) {
	var lines []string
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		if trimmed := strings.TrimSpace(scanner.Text()); trimmed != "" {
			lines = append(lines, trimmed)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("error al leer URLs desde stdin: %w", err)
	}
	return lines, nil
}

// collectURLs gathers raw URL candidates from a comma separated list, a file
// (one per line) and, when none of those produced anything and readStdin is
// set, from stdin. The result is not yet cleaned (see cleanURLList).
func collectURLs(csv, file string, readStdin bool, stdin io.Reader) ([]string, error) {
	var raw []string
	if csv != "" {
		raw = append(raw, splitNonEmpty(csv, ",")...)
	}
	if file != "" {
		content, err := os.ReadFile(file)
		if err != nil {
			return nil, fmt.Errorf("no se pudo leer el archivo de URLs: %w", err)
		}
		raw = append(raw, splitNonEmpty(string(content), "\n")...)
	}
	if len(raw) == 0 && readStdin {
		lines, err := readLines(stdin)
		if err != nil {
			return nil, err
		}
		raw = lines
	}
	return raw, nil
}

// cleanURLList extracts valid URLs from raw candidates. Demo mode accepts
// any http(s)-looking URL so the simulator can run without a real campus.
func cleanURLList(raw []string, demo bool) []string {
	joined := strings.Join(raw, "\n")
	if demo {
		return tui.CleanURLsDemo(joined)
	}
	return tui.CleanURLs(joined)
}

// resolveCookie returns the session cookie for a non-interactive run.
// Demo mode never needs one and passes the flag through untouched. Otherwise
// the precedence is --cookie, browser detection (--uni/--domain),
// MOODLE_SESSION; missingMsg is the usage error when none is available.
func resolveCookie(ctx context.Context, demo bool, explicit, missingMsg string) (string, error) {
	if demo {
		return explicit, nil
	}
	cookie, err := resolveHeadlessCookie(ctx, explicit)
	if err != nil {
		return "", err
	}
	if cookie == "" {
		return "", fmt.Errorf("%s", missingMsg)
	}
	return cookie, nil
}

// execute runs the download pipeline shared by the root command and `run`:
// logger, tasks, kernel, progress (TUI or headless) and final report.
func execute(ctx context.Context, form *tui.FormData) error {
	appLogger, logPath := setupLogger(flagLogPath, flagConcurrency, flagOutputDir, form, flagDemo)
	if appLogger != nil {
		defer func() { _ = appLogger.Close() }()
	}

	tasks, err := createTasks(form, flagOutputDir)
	if err != nil {
		return err
	}
	k := initKernel(flagConcurrency, appLogger, flagDemo)

	var results []domain.Result
	if shouldUseHeadless() {
		results, err = tui.RunHeadlessProgress(ctx, k, tasks, os.Stderr)
	} else {
		results, err = tui.RunProgressUI(ctx, k, tasks, logPath)
	}
	if err != nil {
		return fmt.Errorf("error durante la ejecución de las descargas: %w", err)
	}
	return handleCompletion(results, logPath)
}

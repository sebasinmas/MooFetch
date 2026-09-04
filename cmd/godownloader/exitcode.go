package main

import (
	"context"
	"errors"
	"strings"

	"godownloader/internal/domain"
	"godownloader/internal/plugins/moodle"
	"godownloader/internal/tui"
)

const (
	ExitSuccess     = 0
	ExitGeneralErr  = 1
	ExitUsageErr    = 2   // Flags o parámetros inválidos
	ExitAuthErr     = 77  // EX_NOPERM (Fallo de autenticación)
	ExitInterrupted = 130 // 128 + SIGINT (Ctrl+C)
)

// BatchError represents failures encountered across download tasks.
type BatchError struct {
	Results []domain.Result
}

func (e *BatchError) Error() string {
	var errStrs []string
	for _, r := range e.Results {
		if r.Err != nil {
			errStrs = append(errStrs, r.Err.Error())
		}
	}
	if len(errStrs) == 0 {
		return "batch completed with errors"
	}
	return strings.Join(errStrs, "; ")
}

// DetermineExitCode maps errors and execution contexts to standard POSIX exit codes.
func DetermineExitCode(ctx context.Context, err error) int {
	if err == nil {
		return ExitSuccess
	}

	// 1. Interrupted (Ctrl+C, SIGINT, SIGTERM, form aborted)
	if isInterrupted(ctx, err) {
		return ExitInterrupted
	}

	// 2. BatchError inspecting tasks
	var batchErr *BatchError
	if errors.As(err, &batchErr) {
		return determineBatchExitCode(batchErr)
	}

	// 3. Direct Auth error
	if isAuthError(err) {
		return ExitAuthErr
	}

	// 4. Usage error (unknown flags, missing required flags/arguments)
	if isUsageError(err) {
		return ExitUsageErr
	}

	// 5. Default General error
	return ExitGeneralErr
}

func isInterrupted(ctx context.Context, err error) bool {
	return errors.Is(err, context.Canceled) || errors.Is(err, tui.ErrFormAborted) || (ctx != nil && ctx.Err() != nil)
}

func determineBatchExitCode(batchErr *BatchError) int {
	hasAuthErr := false
	hasCancelErr := false
	hasOtherErr := false
	for _, r := range batchErr.Results {
		if r.Err == nil {
			continue
		}
		if errors.Is(r.Err, context.Canceled) {
			hasCancelErr = true
		} else if isAuthError(r.Err) {
			hasAuthErr = true
		} else {
			hasOtherErr = true
		}
	}
	if hasCancelErr && !hasOtherErr {
		return ExitInterrupted
	}
	if hasAuthErr && !hasOtherErr {
		return ExitAuthErr
	}
	return ExitGeneralErr
}

func isAuthError(err error) bool {
	return domain.IsFatalAuth(err) || errors.Is(err, moodle.ErrAuthenticationFailed)
}

func isUsageError(err error) bool {
	errStr := strings.ToLower(err.Error())
	usageKeywords := []string{
		"unknown flag",
		"unknown shorthand flag",
		"flag needs an argument",
		"required flag",
		"no se especificaron urls válidas",
		"se requiere cookie de sesión",
		"no se detectaron urls válidas",
	}
	for _, kw := range usageKeywords {
		if strings.Contains(errStr, kw) {
			return true
		}
	}
	return false
}

package main

import (
	"context"
	"errors"
	"strings"

	"godownloader/internal/kernel"
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
	Results []kernel.Result
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
func DetermineExitCode(err error, ctx context.Context) int {
	if err == nil {
		return ExitSuccess
	}

	// 1. Interrupted (Ctrl+C, SIGINT, SIGTERM, form aborted)
	if errors.Is(err, context.Canceled) || errors.Is(err, tui.ErrFormAborted) || (ctx != nil && ctx.Err() != nil) {
		return ExitInterrupted
	}

	// 2. BatchError inspecting tasks
	var batchErr *BatchError
	if errors.As(err, &batchErr) {
		hasAuthErr := false
		hasCancelErr := false
		hasOtherErr := false
		for _, r := range batchErr.Results {
			if r.Err != nil {
				if errors.Is(r.Err, context.Canceled) {
					hasCancelErr = true
				} else if kernel.IsFatalAuth(r.Err) || errors.Is(r.Err, moodle.ErrAuthenticationFailed) {
					hasAuthErr = true
				} else {
					hasOtherErr = true
				}
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

	// 3. Direct Auth error
	if kernel.IsFatalAuth(err) || errors.Is(err, moodle.ErrAuthenticationFailed) {
		return ExitAuthErr
	}

	// 4. Usage error (unknown flags, missing required flags/arguments)
	errStr := strings.ToLower(err.Error())
	if strings.Contains(errStr, "unknown flag") ||
		strings.Contains(errStr, "unknown shorthand flag") ||
		strings.Contains(errStr, "flag needs an argument") ||
		strings.Contains(errStr, "required flag") ||
		strings.Contains(errStr, "no se especificaron urls válidas") ||
		strings.Contains(errStr, "se requiere cookie de sesión") ||
		strings.Contains(errStr, "no se detectaron urls válidas") {
		return ExitUsageErr
	}

	// 5. Default General error
	return ExitGeneralErr
}

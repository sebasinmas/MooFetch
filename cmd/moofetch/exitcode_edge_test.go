package main

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/sebasinmas/MooFetch/internal/auth"
	"github.com/sebasinmas/MooFetch/internal/domain"
	"github.com/sebasinmas/MooFetch/internal/tui"
)

func TestDetermineExitCode_EdgeCases(t *testing.T) {
	canceled, cancel := context.WithCancel(context.Background())
	cancel()

	tests := []struct {
		name string
		ctx  context.Context
		err  error
		want int
	}{
		{"nil error", context.Background(), nil, ExitSuccess},
		{"nil ctx with general error", nil, errors.New("disk full"), ExitGeneralErr}, //nolint:staticcheck // nil ctx is intentional
		{"canceled ctx overrides usage error", canceled, errors.New("unknown flag: --x"), ExitInterrupted},
		{"wrapped context.Canceled", context.Background(), fmt.Errorf("run: %w", context.Canceled), ExitInterrupted},
		{"wrapped form abort", context.Background(), fmt.Errorf("form: %w", tui.ErrFormAborted), ExitInterrupted},
		{"wrapped fatal auth", context.Background(), fmt.Errorf("x: %w", domain.ErrAuthenticationFailed), ExitAuthErr},
		{"unknown university is usage", context.Background(), fmt.Errorf("%w: %q", auth.ErrUnknownUniversity, "zzz"), ExitUsageErr},
		{"invalid domain is usage", context.Background(), fmt.Errorf("%w: %q", auth.ErrInvalidDomain, "bad"), ExitUsageErr},
		{"usage keyword is case-insensitive", context.Background(), errors.New("Unknown Flag: --Foo"), ExitUsageErr},
		{"empty batch error", context.Background(), &BatchError{}, ExitGeneralErr},
		{"batch: auth plus other error is general", context.Background(), &BatchError{Results: []domain.Result{
			{Err: domain.ErrAuthenticationFailed}, {Err: errors.New("500")},
		}}, ExitGeneralErr},
		{"batch: cancel plus other error is general", context.Background(), &BatchError{Results: []domain.Result{
			{Err: context.Canceled}, {Err: errors.New("500")},
		}}, ExitGeneralErr},
		{"batch: only successes and auth", context.Background(), &BatchError{Results: []domain.Result{
			{}, {Err: domain.ErrAuthenticationFailed},
		}}, ExitAuthErr},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := DetermineExitCode(tc.ctx, tc.err); got != tc.want {
				t.Errorf("got %d, want %d", got, tc.want)
			}
		})
	}
}

func TestBatchError_EmptyMessage(t *testing.T) {
	if got := (&BatchError{Results: []domain.Result{{}}}).Error(); got != "batch completed with errors" {
		t.Errorf("got %q", got)
	}
}

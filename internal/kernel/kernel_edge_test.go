package kernel_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sebasinmas/MooFetch/internal/domain"
	"github.com/sebasinmas/MooFetch/internal/kernel"
)

func mkTasks(n int, prefix string) []domain.Task {
	tasks := make([]domain.Task, n)
	for i := range tasks {
		tasks[i] = domain.Task{ID: i + 1, URL: fmt.Sprintf("%s/%d", prefix, i)}
	}
	return tasks
}

func TestKernel_Dispatch_NonPositiveConcurrencyStillRuns(t *testing.T) {
	for _, limit := range []int{0, -1, -100} {
		p := &mockPlugin{name: "p", prefix: "https://p.test"}
		k := kernel.New(kernel.WithConcurrency(limit), kernel.WithPlugins([]kernel.DownloaderPlugin{p}))
		results := k.Dispatch(context.Background(), mkTasks(4, "https://p.test"), nil)
		if len(results) != 4 {
			t.Fatalf("limit %d: got %d results", limit, len(results))
		}
		for i, r := range results {
			if r.Err != nil {
				t.Errorf("limit %d: task %d failed: %v", limit, i, r.Err)
			}
		}
	}
}

func TestKernel_Dispatch_NilHandlerAndEmptyBatch(t *testing.T) {
	p := &mockPlugin{name: "p", prefix: "https://p.test"}
	k := kernel.New(kernel.WithPlugins([]kernel.DownloaderPlugin{p}))

	if res := k.Dispatch(context.Background(), nil, nil); len(res) != 0 {
		t.Errorf("nil tasks: got %d results", len(res))
	}
	if res := k.Dispatch(context.Background(), []domain.Task{}, nil); len(res) != 0 {
		t.Errorf("empty tasks: got %d results", len(res))
	}
	// nil handler must not panic on success, on failure, or on unroutable URLs.
	res := k.Dispatch(context.Background(), []domain.Task{{ID: 1, URL: "https://p.test/a"}, {ID: 2, URL: "gopher://x"}}, nil)
	if res[0].Err != nil || !errors.Is(res[1].Err, kernel.ErrNoPluginFound) {
		t.Errorf("unexpected results: %+v", res)
	}
	// Pre-cancelled context with nil handler.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	res = k.Dispatch(ctx, mkTasks(2, "https://p.test"), nil)
	for _, r := range res {
		if !errors.Is(r.Err, context.Canceled) {
			t.Errorf("want Canceled, got %v", r.Err)
		}
	}
}

func TestKernel_Dispatch_DuplicateURLsAreProcessedIndependently(t *testing.T) {
	p := &mockPlugin{name: "p", prefix: "https://p.test"}
	k := kernel.New(kernel.WithConcurrency(2), kernel.WithPlugins([]kernel.DownloaderPlugin{p}))
	tasks := []domain.Task{
		{ID: 1, URL: "https://p.test/same"},
		{ID: 2, URL: "https://p.test/same"},
		{ID: 3, URL: "https://p.test/same"},
	}
	results := k.Dispatch(context.Background(), tasks, nil)
	if got := atomic.LoadInt64(&p.callCount); got != 3 {
		t.Errorf("kernel must not dedupe (dedupe is the caller's job): %d calls", got)
	}
	for i, r := range results {
		if r.TaskID != tasks[i].ID || r.Err != nil {
			t.Errorf("result %d mismatched: %+v", i, r)
		}
	}
}

func TestKernel_Dispatch_ResultOrderMatchesInput(t *testing.T) {
	p := &mockPlugin{name: "p", prefix: "https://p.test", delay: time.Millisecond}
	k := kernel.New(kernel.WithConcurrency(8), kernel.WithPlugins([]kernel.DownloaderPlugin{p}))
	tasks := mkTasks(50, "https://p.test")
	for i, r := range k.Dispatch(context.Background(), tasks, nil) {
		if r.TaskID != tasks[i].ID {
			t.Fatalf("index %d holds task %d", i, r.TaskID)
		}
	}
}

// selectivePlugin fails per-URL with a configured error.
type selectivePlugin struct {
	errs map[string]error
}

func (s *selectivePlugin) Name() string          { return "sel" }
func (s *selectivePlugin) CanHandle(string) bool { return true }
func (s *selectivePlugin) Download(ctx context.Context, task domain.Task, _ domain.ProgressFunc) (*domain.Result, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := s.errs[task.URL]; err != nil {
		return nil, err
	}
	return &domain.Result{TaskID: task.ID, URL: task.URL, Filename: "f", BytesRead: 1, TotalBytes: 1}, nil
}

func TestKernel_Dispatch_MixedResultsWithoutAuthErrorsDoNotTripBreaker(t *testing.T) {
	s := &selectivePlugin{errs: map[string]error{
		"u/1": errors.New("500"),
		"u/3": errors.New("timeout"),
	}}
	k := kernel.New(kernel.WithConcurrency(1), kernel.WithPlugins([]kernel.DownloaderPlugin{s}))
	tasks := []domain.Task{{ID: 1, URL: "u/0"}, {ID: 2, URL: "u/1"}, {ID: 3, URL: "u/2"}, {ID: 4, URL: "u/3"}, {ID: 5, URL: "u/4"}}
	res := k.Dispatch(context.Background(), tasks, nil)
	var failed, ok int
	for _, r := range res {
		if r.Err != nil {
			failed++
		} else {
			ok++
		}
	}
	if failed != 2 || ok != 3 {
		t.Errorf("failed=%d ok=%d, want 2/3", failed, ok)
	}
}

func TestKernel_Dispatch_AuthErrorMidBatchSkipsRestAndKeepsEarlierSuccesses(t *testing.T) {
	s := &selectivePlugin{errs: map[string]error{
		"u/2": fmt.Errorf("x: %w", domain.ErrAuthenticationFailed),
	}}
	k := kernel.New(kernel.WithConcurrency(1), kernel.WithPlugins([]kernel.DownloaderPlugin{s}))
	tasks := []domain.Task{{ID: 1, URL: "u/0"}, {ID: 2, URL: "u/1"}, {ID: 3, URL: "u/2"}, {ID: 4, URL: "u/3"}, {ID: 5, URL: "u/4"}}

	var mu sync.Mutex
	failedEvents := map[int]bool{}
	res := k.Dispatch(context.Background(), tasks, func(e domain.Event) {
		if e.Type == domain.EventTaskFailed {
			mu.Lock()
			failedEvents[e.TaskID] = true
			mu.Unlock()
		}
	})

	if res[0].Err != nil || res[1].Err != nil {
		t.Errorf("earlier tasks should have succeeded: %+v %+v", res[0], res[1])
	}
	for i := 2; i < 5; i++ {
		if !domain.IsFatalAuth(res[i].Err) {
			t.Errorf("task %d: want fatal auth (own or propagated), got %v", i+1, res[i].Err)
		}
		if !failedEvents[i+1] {
			t.Errorf("task %d: missing failure event", i+1)
		}
	}
}

func TestKernel_Dispatch_CancelMidBatchFillsEveryResult(t *testing.T) {
	started := make(chan struct{}, 16)
	p := &blockingPlugin{started: started}
	k := kernel.New(kernel.WithConcurrency(2), kernel.WithPlugins([]kernel.DownloaderPlugin{p}))
	ctx, cancel := context.WithCancel(context.Background())

	tasks := mkTasks(10, "https://b.test")
	done := make(chan []domain.Result, 1)
	go func() { done <- k.Dispatch(ctx, tasks, nil) }()

	<-started
	<-started
	cancel()

	select {
	case res := <-done:
		if len(res) != len(tasks) {
			t.Fatalf("got %d results", len(res))
		}
		for i, r := range res {
			if !errors.Is(r.Err, context.Canceled) {
				t.Errorf("task %d: want context.Canceled, got %v", i, r.Err)
			}
			if r.TaskID != tasks[i].ID {
				t.Errorf("task %d: TaskID %d", i, r.TaskID)
			}
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Dispatch did not return after cancellation")
	}
}

type blockingPlugin struct{ started chan struct{} }

func (b *blockingPlugin) Name() string          { return "block" }
func (b *blockingPlugin) CanHandle(string) bool { return true }
func (b *blockingPlugin) Download(ctx context.Context, _ domain.Task, _ domain.ProgressFunc) (*domain.Result, error) {
	b.started <- struct{}{}
	<-ctx.Done()
	return nil, ctx.Err()
}

func TestKernel_NoPluginsAndNilRegistry(t *testing.T) {
	k := kernel.New()
	if _, err := k.ResolvePlugin("https://x.test"); !errors.Is(err, kernel.ErrNoPluginFound) {
		t.Errorf("want ErrNoPluginFound, got %v", err)
	}
	res := k.Dispatch(context.Background(), []domain.Task{{ID: 1, URL: "https://x.test"}}, nil)
	if !errors.Is(res[0].Err, kernel.ErrNoPluginFound) {
		t.Errorf("want ErrNoPluginFound, got %v", res[0].Err)
	}
	// WithRegistry(nil) must be ignored.
	k = kernel.New(kernel.WithRegistry(nil))
	if _, err := k.ResolvePlugin("x"); !errors.Is(err, kernel.ErrNoPluginFound) {
		t.Errorf("got %v", err)
	}
}

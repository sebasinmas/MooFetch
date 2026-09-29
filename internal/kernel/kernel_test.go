package kernel_test

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sebasinmas/MooFetch/internal/domain"
	"github.com/sebasinmas/MooFetch/internal/kernel"
)

type mockPlugin struct {
	name      string
	prefix    string
	delay     time.Duration
	failWith  error
	callCount int64
}

func (m *mockPlugin) Name() string {
	return m.name
}

func (m *mockPlugin) CanHandle(rawURL string) bool {
	return strings.HasPrefix(rawURL, m.prefix)
}

func (m *mockPlugin) Download(ctx context.Context, task domain.Task, progress domain.ProgressFunc) (*domain.Result, error) {
	atomic.AddInt64(&m.callCount, 1)

	if m.delay > 0 {
		select {
		case <-time.After(m.delay):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}

	if progress != nil {
		progress(domain.ProgressUpdate{
			TaskID:     task.ID,
			URL:        task.URL,
			Filename:   "mock.pdf",
			BytesRead:  100,
			TotalBytes: 100,
		})
	}

	if m.failWith != nil {
		return nil, m.failWith
	}

	return &domain.Result{
		TaskID:     task.ID,
		URL:        task.URL,
		Filename:   "mock.pdf",
		BytesRead:  100,
		TotalBytes: 100,
		Err:        nil,
	}, nil
}

func TestRegistry_RegisterAndRetrieve(t *testing.T) {
	t.Parallel()
	r := kernel.NewRegistry()

	p1 := &mockPlugin{name: "p1", prefix: "https://p1.test"}
	r.Register(p1)

	// Idempotent test
	r.Register(p1)

	registered := r.Plugins()
	if len(registered) != 1 {
		t.Fatalf("expected 1 plugin registered, got %d", len(registered))
	}
	if registered[0].Name() != "p1" {
		t.Errorf("expected plugin name 'p1', got '%s'", registered[0].Name())
	}
}

func TestRegistry_NilPanic(t *testing.T) {
	t.Parallel()
	r := kernel.NewRegistry()
	defer func() {
		if rec := recover(); rec == nil {
			t.Errorf("expected panic when registering nil plugin, got none")
		}
	}()
	r.Register(nil)
}

func TestKernel_ResolvePlugin(t *testing.T) {
	t.Parallel()
	p1 := &mockPlugin{name: "moodle", prefix: "https://campusvirtual.ufro.cl"}
	p2 := &mockPlugin{name: "canvas", prefix: "https://canvas.edu"}

	k := kernel.New(kernel.WithPlugins([]kernel.DownloaderPlugin{p1, p2}))

	resolved, err := k.ResolvePlugin("https://campusvirtual.ufro.cl/resource/123")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resolved.Name() != "moodle" {
		t.Errorf("expected 'moodle', got '%s'", resolved.Name())
	}

	resolvedCanvas, err := k.ResolvePlugin("https://canvas.edu/courses/456")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resolvedCanvas.Name() != "canvas" {
		t.Errorf("expected 'canvas', got '%s'", resolvedCanvas.Name())
	}

	_, err = k.ResolvePlugin("https://unsupported.com/file.pdf")
	if !errors.Is(err, kernel.ErrNoPluginFound) {
		t.Errorf("expected ErrNoPluginFound, got %v", err)
	}
}

func TestKernel_Dispatch_SuccessAndEvents(t *testing.T) {
	t.Parallel()
	p := &mockPlugin{name: "test-plugin", prefix: "https://test.com"}
	k := kernel.New(
		kernel.WithPlugins([]kernel.DownloaderPlugin{p}),
		kernel.WithConcurrency(2),
	)

	tasks := []domain.Task{
		{ID: 1, URL: "https://test.com/file1.pdf"},
		{ID: 2, URL: "https://test.com/file2.pdf"},
	}

	var startedCount, completedCount, progressCount int64
	handler := func(ev domain.Event) {
		switch ev.Type {
		case domain.EventTaskStarted:
			atomic.AddInt64(&startedCount, 1)
		case domain.EventTaskProgress:
			atomic.AddInt64(&progressCount, 1)
		case domain.EventTaskCompleted:
			atomic.AddInt64(&completedCount, 1)
		}
	}

	results := k.Dispatch(context.Background(), tasks, handler)
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}

	for _, res := range results {
		if res.Err != nil {
			t.Errorf("task %d unexpected error: %v", res.TaskID, res.Err)
		}
		if res.Filename != "mock.pdf" {
			t.Errorf("expected filename 'mock.pdf', got '%s'", res.Filename)
		}
	}

	if atomic.LoadInt64(&startedCount) != 2 {
		t.Errorf("expected 2 started events, got %d", startedCount)
	}
	if atomic.LoadInt64(&completedCount) != 2 {
		t.Errorf("expected 2 completed events, got %d", completedCount)
	}
	if atomic.LoadInt64(&progressCount) != 2 {
		t.Errorf("expected 2 progress events, got %d", progressCount)
	}
}

func TestKernel_Dispatch_TaskFailure(t *testing.T) {
	t.Parallel()
	expectedErr := errors.New("network failure")
	p := &mockPlugin{name: "failing", prefix: "https://fail.com", failWith: expectedErr}
	k := kernel.New(kernel.WithPlugins([]kernel.DownloaderPlugin{p}))

	tasks := []domain.Task{
		{ID: 1, URL: "https://fail.com/res1"},
	}

	var failedCount int64
	handler := func(ev domain.Event) {
		if ev.Type == domain.EventTaskFailed {
			atomic.AddInt64(&failedCount, 1)
		}
	}

	results := k.Dispatch(context.Background(), tasks, handler)
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if !errors.Is(results[0].Err, expectedErr) {
		t.Errorf("expected %v, got %v", expectedErr, results[0].Err)
	}
	if atomic.LoadInt64(&failedCount) != 1 {
		t.Errorf("expected 1 failed event, got %d", failedCount)
	}
}

func TestKernel_Dispatch_ContextCancellation(t *testing.T) {
	t.Parallel()
	p := &mockPlugin{name: "slow", prefix: "https://slow.com", delay: 100 * time.Millisecond}
	k := kernel.New(
		kernel.WithPlugins([]kernel.DownloaderPlugin{p}),
		kernel.WithConcurrency(1),
	)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	tasks := []domain.Task{
		{ID: 1, URL: "https://slow.com/slow1"},
	}

	results := k.Dispatch(ctx, tasks, nil)
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if !errors.Is(results[0].Err, context.Canceled) {
		t.Errorf("expected context.Canceled, got %v", results[0].Err)
	}
}

type concurrencyTrackingPlugin struct {
	onStart func()
	onEnd   func()
}

func (c *concurrencyTrackingPlugin) Name() string            { return "tracker" }
func (c *concurrencyTrackingPlugin) CanHandle(_ string) bool { return true }
func (c *concurrencyTrackingPlugin) Download(ctx context.Context, task domain.Task, _ domain.ProgressFunc) (*domain.Result, error) {
	if c.onStart != nil {
		c.onStart()
	}
	defer func() {
		if c.onEnd != nil {
			c.onEnd()
		}
	}()
	time.Sleep(2 * time.Millisecond)
	return &domain.Result{TaskID: task.ID, URL: task.URL, Filename: "file.pdf"}, nil
}

func TestKernel_Dispatch_BoundedWorkerPool(t *testing.T) {
	t.Parallel()
	var currentActive int64
	var maxActive int64

	const concurrency = 3
	const totalTasks = 100

	plugin := &concurrencyTrackingPlugin{
		onStart: func() {
			curr := atomic.AddInt64(&currentActive, 1)
			for {
				max := atomic.LoadInt64(&maxActive)
				if curr <= max || atomic.CompareAndSwapInt64(&maxActive, max, curr) {
					break
				}
			}
		},
		onEnd: func() {
			atomic.AddInt64(&currentActive, -1)
		},
	}

	k := kernel.New(
		kernel.WithPlugins([]kernel.DownloaderPlugin{plugin}),
		kernel.WithConcurrency(concurrency),
	)

	tasks := make([]domain.Task, totalTasks)
	for i := 0; i < totalTasks; i++ {
		tasks[i] = domain.Task{ID: i + 1, URL: "https://track.com/file"}
	}

	results := k.Dispatch(context.Background(), tasks, nil)
	if len(results) != totalTasks {
		t.Fatalf("expected %d results, got %d", totalTasks, len(results))
	}

	for _, r := range results {
		if r.Err != nil {
			t.Fatalf("unexpected task error: %v", r.Err)
		}
	}

	if peak := atomic.LoadInt64(&maxActive); peak > concurrency {
		t.Errorf("expected maximum %d concurrent workers, but peak was %d", concurrency, peak)
	}
}

func TestKernel_Dispatch_BatchContextExpiration(t *testing.T) {
	t.Parallel()
	const totalTasks = 50
	plugin := &mockPlugin{
		name:   "slow",
		prefix: "https://slow.com",
		delay:  20 * time.Millisecond,
	}

	k := kernel.New(
		kernel.WithPlugins([]kernel.DownloaderPlugin{plugin}),
		kernel.WithConcurrency(2),
	)

	tasks := make([]domain.Task, totalTasks)
	for i := 0; i < totalTasks; i++ {
		tasks[i] = domain.Task{ID: i + 1, URL: "https://slow.com/file"}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	results := k.Dispatch(ctx, tasks, nil)
	if len(results) != totalTasks {
		t.Fatalf("expected %d results, got %d", totalTasks, len(results))
	}

	var canceledCount int
	for _, r := range results {
		if errors.Is(r.Err, context.DeadlineExceeded) || errors.Is(r.Err, context.Canceled) {
			canceledCount++
		}
	}

	if canceledCount == 0 {
		t.Errorf("expected at least some tasks to be canceled due to deadline, but none were")
	}
}

func TestKernel_Dispatch_CircuitBreakerAuth(t *testing.T) {
	t.Parallel()
	plugin := &mockPlugin{
		name:     "moodle-mock",
		prefix:   "https://moodle.test",
		failWith: domain.ErrAuthenticationFailed,
	}

	k := kernel.New(
		kernel.WithPlugins([]kernel.DownloaderPlugin{plugin}),
		kernel.WithConcurrency(1), // Concurrency 1 ensures task 1 executes first
	)

	tasks := []domain.Task{
		{ID: 1, URL: "https://moodle.test/file1"},
		{ID: 2, URL: "https://moodle.test/file2"},
		{ID: 3, URL: "https://moodle.test/file3"},
		{ID: 4, URL: "https://moodle.test/file4"},
		{ID: 5, URL: "https://moodle.test/file5"},
	}

	results := k.Dispatch(context.Background(), tasks, nil)
	if len(results) != len(tasks) {
		t.Fatalf("expected %d results, got %d", len(tasks), len(results))
	}

	// Task 1 failed with ErrAuthenticationFailed
	if !errors.Is(results[0].Err, domain.ErrAuthenticationFailed) {
		t.Fatalf("expected task 1 error to be ErrAuthenticationFailed, got: %v", results[0].Err)
	}

	// The plugin should only have been called ONCE because circuit breaker tripped immediately
	if atomic.LoadInt64(&plugin.callCount) != 1 {
		t.Errorf("expected plugin to be called exactly 1 time, but was called %d times", plugin.callCount)
	}

	// Subsequent tasks should be marked with authentication failure or cancellation
	for i := 1; i < len(tasks); i++ {
		if results[i].Err == nil {
			t.Errorf("task %d was expected to fail due to circuit breaker, but had no error", results[i].TaskID)
		}
		if !domain.IsFatalAuth(results[i].Err) && !errors.Is(results[i].Err, context.Canceled) {
			t.Errorf("task %d expected auth/canceled error, got %v", results[i].TaskID, results[i].Err)
		}
	}
}

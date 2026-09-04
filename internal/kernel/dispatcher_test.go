package kernel_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"

	"godownloader/internal/kernel"
	"godownloader/internal/plugins/moodle"
)

// TestDispatcher_CircuitBreaker_PromptScenario tests the exact circuit breaker scenario:
// An httptest.NewServer returns 200 for the first request and 403 for the second request.
// The worker pool must immediately trip the circuit breaker and abort the remaining queued URLs
// without issuing any further HTTP requests.
func TestDispatcher_CircuitBreaker_PromptScenario(t *testing.T) {
	t.Parallel()

	var requestCount int64
	dummyData := bytes.Repeat([]byte("%PDF-1.4\n"), 100)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		current := atomic.AddInt64(&requestCount, 1)
		switch current {
		case 1:
			// First request succeeds
			w.Header().Set("Content-Type", "application/pdf")
			w.Header().Set("Content-Length", fmt.Sprintf("%d", len(dummyData)))
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(dummyData)
		case 2:
			// Second request fails with fatal authentication error (403 Forbidden)
			http.Error(w, "Forbidden: Session Expired", http.StatusForbidden)
		default:
			// Subsequent requests should NEVER happen because the circuit breaker stopped the pool
			http.Error(w, "Circuit breaker failed to abort queue", http.StatusInternalServerError)
		}
	}))
	defer server.Close()

	tempDir := t.TempDir()
	plugin := moodle.New()
	k := kernel.New(
		kernel.WithPlugins([]kernel.DownloaderPlugin{plugin}),
		kernel.WithConcurrency(1), // Concurrency 1 ensures deterministic sequence: Task 1 -> Task 2 -> Breaker
	)

	tasks := []kernel.Task{
		{ID: 1, URL: server.URL + "/res1.pdf", OutputDir: tempDir},
		{ID: 2, URL: server.URL + "/res2.pdf", OutputDir: tempDir},
		{ID: 3, URL: server.URL + "/res3.pdf", OutputDir: tempDir},
		{ID: 4, URL: server.URL + "/res4.pdf", OutputDir: tempDir},
		{ID: 5, URL: server.URL + "/res5.pdf", OutputDir: tempDir},
	}

	results := k.Dispatch(context.Background(), tasks, nil)
	if len(results) != len(tasks) {
		t.Fatalf("expected %d results, got %d", len(tasks), len(results))
	}

	totalReqs := atomic.LoadInt64(&requestCount)
	if totalReqs != 2 {
		t.Fatalf("circuit breaker failed: expected exactly 2 HTTP requests, but server received %d requests", totalReqs)
	}

	// Task 1 succeeded
	if results[0].Err != nil {
		t.Errorf("task 1 expected success, got error: %v", results[0].Err)
	}

	// Task 2 failed with fatal auth
	if !kernel.IsFatalAuth(results[1].Err) {
		t.Errorf("task 2 expected IsFatalAuth to be true, got: %v", results[1].Err)
	}

	// Tasks 3, 4, 5 were aborted without network activity
	for i := 2; i < len(tasks); i++ {
		if results[i].Err == nil {
			t.Errorf("task %d was expected to be aborted by circuit breaker, but had no error", results[i].TaskID)
		}
		if !kernel.IsFatalAuth(results[i].Err) && !errors.Is(results[i].Err, context.Canceled) {
			t.Errorf("task %d expected fatal auth or cancellation drain error, got: %v", results[i].TaskID, results[i].Err)
		}
	}
}

// TestDispatcher_CircuitBreaker_HTTPStatusesTDT runs a Table-Driven Test across different
// HTTP responses to confirm fatal auth responses (403, 303 to login, 401) immediately trip the
// circuit breaker, whereas non-fatal errors (500) do NOT trip it and allow remaining tasks to proceed.
func TestDispatcher_CircuitBreaker_HTTPStatusesTDT(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name                 string
		firstStatusHandler  func(w http.ResponseWriter, r *http.Request)
		expectCircuitBreaker bool
		expectedServerReqs   int64
	}{
		{
			name: "403 Forbidden trips circuit breaker",
			firstStatusHandler: func(w http.ResponseWriter, _ *http.Request) {
				http.Error(w, "Forbidden", http.StatusForbidden)
			},
			expectCircuitBreaker: true,
			expectedServerReqs:   1, // Only task 1 executes; remaining tasks aborted immediately
		},
		{
			name: "303 Redirect to login page trips circuit breaker",
			firstStatusHandler: func(w http.ResponseWriter, r *http.Request) {
				http.Redirect(w, r, "/login/index.php", http.StatusSeeOther)
			},
			expectCircuitBreaker: true,
			expectedServerReqs:   1,
		},
		{
			name: "401 Unauthorized trips circuit breaker",
			firstStatusHandler: func(w http.ResponseWriter, _ *http.Request) {
				http.Error(w, "Unauthorized", http.StatusUnauthorized)
			},
			expectCircuitBreaker: true,
			expectedServerReqs:   1,
		},
		{
			name: "500 Internal Server Error does NOT trip circuit breaker (resilience)",
			firstStatusHandler: func(w http.ResponseWriter, _ *http.Request) {
				http.Error(w, "Server Error", http.StatusInternalServerError)
			},
			expectCircuitBreaker: false,
			expectedServerReqs:   3, // Task 1 fails (500), but tasks 2 and 3 continue to be processed!
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var reqCount int64
			dummyBytes := bytes.Repeat([]byte("%PDF-1.4 header\n"), 50)

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				count := atomic.AddInt64(&reqCount, 1)
				if count == 1 {
					tc.firstStatusHandler(w, r)
					return
				}
				// Subsequent requests succeed with 200 OK
				w.Header().Set("Content-Type", "application/pdf")
				w.Header().Set("Content-Length", fmt.Sprintf("%d", len(dummyBytes)))
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write(dummyBytes)
			}))
			defer server.Close()

			tempDir := t.TempDir()
			plugin := moodle.New()
			k := kernel.New(
				kernel.WithPlugins([]kernel.DownloaderPlugin{plugin}),
				kernel.WithConcurrency(1),
			)

			tasks := []kernel.Task{
				{ID: 1, URL: server.URL + "/item1.pdf", OutputDir: tempDir},
				{ID: 2, URL: server.URL + "/item2.pdf", OutputDir: tempDir},
				{ID: 3, URL: server.URL + "/item3.pdf", OutputDir: tempDir},
			}

			results := k.Dispatch(context.Background(), tasks, nil)
			if len(results) != 3 {
				t.Fatalf("expected 3 results, got %d", len(results))
			}

			actualReqs := atomic.LoadInt64(&reqCount)
			if actualReqs != tc.expectedServerReqs {
				t.Errorf("expected %d server requests, but got %d", tc.expectedServerReqs, actualReqs)
			}

			if tc.expectCircuitBreaker {
				// Task 1 was fatal auth error
				if !kernel.IsFatalAuth(results[0].Err) {
					t.Errorf("expected fatal auth error on task 1, got %v", results[0].Err)
				}
				// Task 2 and 3 should have been aborted
				for i := 1; i < 3; i++ {
					if results[i].Err == nil {
						t.Errorf("task %d was expected to be aborted by breaker", results[i].TaskID)
					}
				}
			} else {
				// 500 error on task 1, but task 2 and 3 should have succeeded!
				if results[0].Err == nil {
					t.Errorf("task 1 expected error on 500 status")
				}
				if results[1].Err != nil {
					t.Errorf("task 2 expected success after non-fatal 500 error, got: %v", results[1].Err)
				}
				if results[2].Err != nil {
					t.Errorf("task 3 expected success after non-fatal 500 error, got: %v", results[2].Err)
				}
			}
		})
	}
}

// channelSyncPlugin enables deterministic bounded worker pool testing without time.Sleep.
type channelSyncPlugin struct {
	name          string
	activeWorkers int64
	peakWorkers   int64
	workerStarted chan struct{}
	workerRelease chan struct{}
}

func (p *channelSyncPlugin) Name() string            { return p.name }
func (p *channelSyncPlugin) CanHandle(_ string) bool { return true }
func (p *channelSyncPlugin) Download(ctx context.Context, task kernel.Task, _ kernel.ProgressFunc) (*kernel.Result, error) {
	curr := atomic.AddInt64(&p.activeWorkers, 1)

	// Update peak concurrency atomically
	for {
		peak := atomic.LoadInt64(&p.peakWorkers)
		if curr <= peak || atomic.CompareAndSwapInt64(&p.peakWorkers, peak, curr) {
			break
		}
	}

	// Notify test controller that a worker has entered active state
	p.workerStarted <- struct{}{}

	// Wait for explicit test controller release or context cancellation
	select {
	case <-p.workerRelease:
	case <-ctx.Done():
		atomic.AddInt64(&p.activeWorkers, -1)
		return nil, ctx.Err()
	}

	atomic.AddInt64(&p.activeWorkers, -1)
	return &kernel.Result{
		TaskID:   task.ID,
		URL:      task.URL,
		Filename: fmt.Sprintf("task_%d.pdf", task.ID),
	}, nil
}

// TestDispatcher_BoundedWorkers_DeterministicChannels verifies that the worker pool strictly
// respects the configured concurrency limit using channel handshakes instead of sleeps.
func TestDispatcher_BoundedWorkers_DeterministicChannels(t *testing.T) {
	t.Parallel()

	const maxWorkers = 3
	const totalTasks = 9

	p := &channelSyncPlugin{
		name:          "bounded-sync",
		workerStarted: make(chan struct{}, totalTasks),
		workerRelease: make(chan struct{}),
	}

	k := kernel.New(
		kernel.WithPlugins([]kernel.DownloaderPlugin{p}),
		kernel.WithConcurrency(maxWorkers),
	)

	tasks := make([]kernel.Task, totalTasks)
	for i := 0; i < totalTasks; i++ {
		tasks[i] = kernel.Task{ID: i + 1, URL: fmt.Sprintf("https://test.local/task/%d", i+1)}
	}

	var results []kernel.Result
	var wg sync.WaitGroup
	wg.Add(1)

	go func() {
		defer wg.Done()
		results = k.Dispatch(context.Background(), tasks, nil)
	}()

	// Exactly maxWorkers (3) should start and enter the active phase
	for i := 0; i < maxWorkers; i++ {
		<-p.workerStarted
	}

	// Verify active count is exactly 3
	if active := atomic.LoadInt64(&p.activeWorkers); active != maxWorkers {
		t.Fatalf("expected exactly %d active workers, got %d", maxWorkers, active)
	}

	// Verify NO 4th worker entered (channel must be empty)
	select {
	case <-p.workerStarted:
		t.Fatalf("concurrency violation: more than %d workers were started simultaneously", maxWorkers)
	default:
		// Passed: worker pool is strictly bounded
	}

	// Release 1 worker, then exactly 1 new worker should start
	p.workerRelease <- struct{}{}
	<-p.workerStarted

	if peak := atomic.LoadInt64(&p.peakWorkers); peak > maxWorkers {
		t.Errorf("peak concurrency (%d) exceeded configured limit (%d)", peak, maxWorkers)
	}

	// Release the remaining 8 workers
	for i := 0; i < totalTasks-1; i++ {
		p.workerRelease <- struct{}{}
		if i < totalTasks-maxWorkers-1 {
			<-p.workerStarted
		}
	}

	wg.Wait()

	if len(results) != totalTasks {
		t.Fatalf("expected %d results, got %d", totalTasks, len(results))
	}

	for _, r := range results {
		if r.Err != nil {
			t.Errorf("task %d unexpected error: %v", r.TaskID, r.Err)
		}
	}

	if peak := atomic.LoadInt64(&p.peakWorkers); peak > maxWorkers {
		t.Errorf("peak concurrency %d exceeded limit %d", peak, maxWorkers)
	}
}

// TestDispatcher_GracefulShutdown_CleansPartFile simulates context cancellation (SIGTERM)
// in the middle of an active download and verifies deterministically that:
// 1. The temporary .godownload.part file is deleted from disk.
// 2. The temporary file is NEVER renamed to the final destination file.
// 3. The output directory remains 100% clean without disk garbage.
func TestDispatcher_GracefulShutdown_CleansPartFile(t *testing.T) {
	t.Parallel()

	clientWroteFirstChunk := make(chan struct{}, 1)
	unblockServer := make(chan struct{})

	dummyContent := bytes.Repeat([]byte("A"), 1024*1024) // 1MB payload

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/pdf")
		w.Header().Set("Content-Length", fmt.Sprintf("%d", len(dummyContent)))
		w.WriteHeader(http.StatusOK)

		// Write initial chunk and flush immediately so client creates .part file on disk
		_, _ = w.Write(dummyContent[:1024])
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}

		select {
		case clientWroteFirstChunk <- struct{}{}:
		default:
		}

		// Hold connection open until cancelled or released
		select {
		case <-r.Context().Done():
		case <-unblockServer:
		}
	}))
	defer server.Close()
	defer func() {
		select {
		case <-unblockServer:
		default:
			close(unblockServer)
		}
	}()

	tempDir := t.TempDir()
	plugin := moodle.New()
	k := kernel.New(
		kernel.WithPlugins([]kernel.DownloaderPlugin{plugin}),
		kernel.WithConcurrency(1),
	)

	ctx, cancel := context.WithCancel(context.Background())

	task := kernel.Task{
		ID:        1,
		URL:       server.URL + "/document_sigterm.pdf",
		OutputDir: tempDir,
	}

	finalPath := filepath.Join(tempDir, "document_sigterm.pdf")
	partPath := finalPath + ".godownload.part"

	var cancelInvoked atomic.Bool

	// progress handler detects when client has actively written bytes to disk
	onEvent := func(ev kernel.Event) {
		if ev.Type == kernel.EventTaskProgress && ev.Bytes > 0 {
			if cancelInvoked.CompareAndSwap(false, true) {
				// Verify .part file DOES exist on disk mid-download
				if _, err := os.Stat(partPath); err != nil {
					t.Errorf("expected .part file to exist mid-download, but got error: %v", err)
				}

				// Simulate immediate SIGTERM context cancellation
				cancel()
			}
		}
	}

	results := k.Dispatch(ctx, []kernel.Task{task}, onEvent)

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}

	// 1. Task must report cancellation error
	if !errors.Is(results[0].Err, context.Canceled) {
		t.Errorf("expected result error to be context.Canceled, got: %v", results[0].Err)
	}

	// 2. Target file must NOT exist (atomic rename was aborted)
	if _, err := os.Stat(finalPath); !os.IsNotExist(err) {
		t.Errorf("ATOMIC BREACH: final destination file %s was created despite cancellation!", finalPath)
	}

	// 3. Temporary .part file must have been deleted by defer cleanup
	if _, err := os.Stat(partPath); !os.IsNotExist(err) {
		t.Errorf("CLEANUP BREACH: temporary file %s was NOT deleted after cancellation!", partPath)
	}

	// 4. Output directory must be completely clean
	entries, err := os.ReadDir(tempDir)
	if err != nil {
		t.Fatalf("failed reading temp directory: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("expected tempDir to have 0 files remaining, found %d files: %+v", len(entries), entries)
	}
}

// TestDispatcher_GracefulShutdown_ConcurrentMultipleWorkersCleansPartFiles verifies that when
// multiple downloads are running in parallel, a context cancellation cleanly purges all .part files
// without leaving stray artifacts.
func TestDispatcher_GracefulShutdown_ConcurrentMultipleWorkersCleansPartFiles(t *testing.T) {
	t.Parallel()

	const workers = 3
	unblockServer := make(chan struct{})
	var progressCount int64

	dummyContent := bytes.Repeat([]byte("B"), 512*1024)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/pdf")
		w.Header().Set("Content-Length", fmt.Sprintf("%d", len(dummyContent)))
		w.WriteHeader(http.StatusOK)

		_, _ = w.Write(dummyContent[:512])
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}

		select {
		case <-r.Context().Done():
		case <-unblockServer:
		}
	}))
	defer server.Close()
	defer func() {
		select {
		case <-unblockServer:
		default:
			close(unblockServer)
		}
	}()

	tempDir := t.TempDir()
	plugin := moodle.New()
	k := kernel.New(
		kernel.WithPlugins([]kernel.DownloaderPlugin{plugin}),
		kernel.WithConcurrency(workers),
	)

	ctx, cancel := context.WithCancel(context.Background())

	tasks := make([]kernel.Task, workers)
	for i := 0; i < workers; i++ {
		tasks[i] = kernel.Task{
			ID:        i + 1,
			URL:       fmt.Sprintf("%s/concurrent_%d.pdf", server.URL, i+1),
			OutputDir: tempDir,
		}
	}

	var cancelCalled atomic.Bool
	onEvent := func(ev kernel.Event) {
		if ev.Type == kernel.EventTaskProgress {
			count := atomic.AddInt64(&progressCount, 1)
			if count >= int64(workers) {
				if cancelCalled.CompareAndSwap(false, true) {
					cancel()
				}
			}
		}
	}

	results := k.Dispatch(ctx, tasks, onEvent)
	if len(results) != workers {
		t.Fatalf("expected %d results, got %d", workers, len(results))
	}

	for _, res := range results {
		if !errors.Is(res.Err, context.Canceled) {
			t.Errorf("task %d: expected context.Canceled, got %v", res.TaskID, res.Err)
		}
	}

	// Verify NO files or .part files remain in tempDir
	entries, err := os.ReadDir(tempDir)
	if err != nil {
		t.Fatalf("failed to read tempDir: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("expected 0 files in tempDir after cancellation, but found %d entries: %+v", len(entries), entries)
	}
}

// Package kernel implements the static microkernel orchestrator, plugin registry, and concurrent task dispatcher.
package kernel

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"

	"godownloader/internal/logger"
)

var (
	// ErrNoPluginFound is returned when no registered plugin can handle the specified URL.
	ErrNoPluginFound = errors.New("no registered plugin can handle the provided URL")
	// ErrNilPlugin is returned when attempting to register a nil plugin.
	ErrNilPlugin = errors.New("cannot register a nil plugin")
	// ErrAuthenticationFailed indicates invalid or expired credentials/session that aborts the batch.
	ErrAuthenticationFailed = errors.New("authentication failed")
)

// FatalAuthError is an interface implemented by errors that represent unrecoverable auth failures.
type FatalAuthError interface {
	IsFatalAuth() bool
}

// IsFatalAuth checks if an error represents an unrecoverable authentication failure.
func IsFatalAuth(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, ErrAuthenticationFailed) {
		return true
	}
	var fa FatalAuthError
	if errors.As(err, &fa) && fa.IsFatalAuth() {
		return true
	}
	return strings.Contains(strings.ToLower(err.Error()), "authentication failed")
}

// Task represents an individual download unit dispatched by the kernel.
type Task struct {
	ID        int
	URL       string
	Cookie    string
	OutputDir string
}

// Result captures the final status and outcome of a download task.
type Result struct {
	TaskID     int
	URL        string
	Filename   string
	BytesRead  int64
	TotalBytes int64
	Err        error
}

// ProgressUpdate contains real-time stream information for a running task.
type ProgressUpdate struct {
	TaskID     int
	URL        string
	Filename   string
	BytesRead  int64
	TotalBytes int64
}

// ProgressFunc is a callback invoked during payload transfer.
type ProgressFunc func(update ProgressUpdate)

// EventType categorizes dispatcher event notifications.
type EventType int

const (
	// EventTaskStarted indicates a task was picked up by an active worker.
	EventTaskStarted EventType = iota
	// EventTaskProgress indicates byte transfer progress for a task.
	EventTaskProgress
	// EventTaskCompleted indicates a task finished successfully.
	EventTaskCompleted
	// EventTaskFailed indicates a task terminated with an error.
	EventTaskFailed
)

// Event represents an atomic status transition sent to kernel observers.
type Event struct {
	Type     EventType
	TaskID   int
	URL      string
	Filename string
	Bytes    int64
	Total    int64
	Err      error
}

// EventHandler receives real-time download events from the kernel dispatcher.
type EventHandler func(event Event)

// DownloaderPlugin defines the microkernel contract that all domain download handlers must fulfill.
type DownloaderPlugin interface {
	Name() string
	CanHandle(rawURL string) bool
	Download(ctx context.Context, task Task, progress ProgressFunc) (*Result, error)
}

// Registry stores and manages available downloader plugins in an isolated, thread-safe instance.
type Registry struct {
	mu      sync.RWMutex
	plugins []DownloaderPlugin
}

// NewRegistry creates an empty plugin Registry.
func NewRegistry() *Registry {
	return &Registry{}
}

// Register adds a new plugin to the registry instance.
func (r *Registry) Register(p DownloaderPlugin) {
	if p == nil {
		panic(ErrNilPlugin)
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	for _, existing := range r.plugins {
		if existing.Name() == p.Name() {
			return // Idempotent registration
		}
	}
	r.plugins = append(r.plugins, p)
}

// Plugins returns a copy of all registered plugins in the registry.
func (r *Registry) Plugins() []DownloaderPlugin {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make([]DownloaderPlugin, len(r.plugins))
	copy(result, r.plugins)
	return result
}

// Resolve finds the first registered plugin that declares it can handle the given URL.
func (r *Registry) Resolve(rawURL string) (DownloaderPlugin, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	for _, p := range r.plugins {
		if p.CanHandle(rawURL) {
			return p, nil
		}
	}
	return nil, fmt.Errorf("%w: %s", ErrNoPluginFound, rawURL)
}

// Kernel orchestrates URL dispatching and concurrency management across registered plugins.
type Kernel struct {
	registry    *Registry
	concurrency int
	logger      *logger.Logger
}

// Option configures a Kernel instance.
type Option func(*Kernel)

// WithLogger associates a debug logger with the Kernel.
func WithLogger(l *logger.Logger) Option {
	return func(k *Kernel) {
		k.logger = l
	}
}

// WithConcurrency configures the maximum number of parallel downloads.
func WithConcurrency(limit int) Option {
	return func(k *Kernel) {
		if limit > 0 {
			k.concurrency = limit
		}
	}
}

// WithRegistry sets a custom plugin Registry instance on the Kernel.
func WithRegistry(r *Registry) Option {
	return func(k *Kernel) {
		if r != nil {
			k.registry = r
		}
	}
}

// WithPlugins registers the provided plugins into the Kernel's registry.
func WithPlugins(custom []DownloaderPlugin) Option {
	return func(k *Kernel) {
		if k.registry == nil {
			k.registry = NewRegistry()
		}
		for _, p := range custom {
			k.registry.Register(p)
		}
	}
}

// New creates an initialized Kernel instance using either configured options or an empty registry.
func New(opts ...Option) *Kernel {
	k := &Kernel{
		registry:    NewRegistry(),
		concurrency: 5, // Sensible default to prevent DDoS/throttling
	}

	for _, opt := range opts {
		opt(k)
	}

	return k
}

// ResolvePlugin finds the first registered plugin that declares it can handle the given URL.
func (k *Kernel) ResolvePlugin(rawURL string) (DownloaderPlugin, error) {
	if k.registry == nil {
		return nil, fmt.Errorf("%w: %s", ErrNoPluginFound, rawURL)
	}
	return k.registry.Resolve(rawURL)
}

type indexedTask struct {
	idx  int
	task Task
}

// Dispatch executes the given list of tasks concurrently using a bounded worker pool,
// constrained by the kernel's concurrency limit. If any task encounters a fatal authentication
// failure (IsFatalAuth), a circuit breaker triggers early batch cancellation.
func (k *Kernel) Dispatch(ctx context.Context, tasks []Task, onEvent EventHandler) []Result {
	results := make([]Result, len(tasks))
	if len(tasks) == 0 {
		return results
	}

	if err := ctx.Err(); err != nil {
		for i, t := range tasks {
			results[i] = Result{
				TaskID: t.ID,
				URL:    t.URL,
				Err:    err,
			}
			emitEvent(onEvent, Event{
				Type:   EventTaskFailed,
				TaskID: t.ID,
				URL:    t.URL,
				Err:    err,
			})
		}
		return results
	}

	dispatchCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	tasksChan := make(chan indexedTask, len(tasks))
	for i, t := range tasks {
		tasksChan <- indexedTask{idx: i, task: t}
	}
	close(tasksChan)

	concurrency := k.concurrency
	if concurrency <= 0 {
		concurrency = 1
	}
	numWorkers := concurrency
	if len(tasks) < numWorkers {
		numWorkers = len(tasks)
	}

	var authFailed atomic.Bool
	var fatalAuthErr error
	var authErrMu sync.Mutex

	var wg sync.WaitGroup
	for w := 0; w < numWorkers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-dispatchCtx.Done():
					return
				case item, ok := <-tasksChan:
					if !ok {
						return
					}
					if err := dispatchCtx.Err(); err != nil {
						return
					}

					res := k.executeTask(dispatchCtx, item.task, onEvent)
					results[item.idx] = res

					if IsFatalAuth(res.Err) {
						if authFailed.CompareAndSwap(false, true) {
							authErrMu.Lock()
							fatalAuthErr = res.Err
							authErrMu.Unlock()

							if k.logger != nil {
								k.logger.Printf("Lote cancelado tempranamente por fallo de autenticación: %v", res.Err)
							}
							cancel()
						}
						return
					}
				}
			}
		}()
	}

	wg.Wait()

	// Drain any remaining unexecuted tasks if context was cancelled
	if dispatchCtx.Err() != nil {
		authErrMu.Lock()
		activeAuthErr := fatalAuthErr
		authErrMu.Unlock()

		drainErr := dispatchCtx.Err()
		if activeAuthErr != nil {
			drainErr = fmt.Errorf("%w: batch canceled early due to authentication failure", activeAuthErr)
		}

		for item := range tasksChan {
			results[item.idx] = Result{
				TaskID: item.task.ID,
				URL:    item.task.URL,
				Err:    drainErr,
			}
			emitEvent(onEvent, Event{
				Type:   EventTaskFailed,
				TaskID: item.task.ID,
				URL:    item.task.URL,
				Err:    drainErr,
			})
		}
	}

	return results
}

func (k *Kernel) executeTask(ctx context.Context, task Task, onEvent EventHandler) Result {
	if k.logger != nil {
		k.logger.LogTaskStart(task.ID, task.URL)
	}

	emitEvent(onEvent, Event{
		Type:   EventTaskStarted,
		TaskID: task.ID,
		URL:    task.URL,
	})

	plugin, err := k.ResolvePlugin(task.URL)
	if err != nil {
		if k.logger != nil {
			k.logger.LogTaskError(task.ID, task.URL, err)
		}
		res := Result{
			TaskID: task.ID,
			URL:    task.URL,
			Err:    err,
		}
		emitEvent(onEvent, Event{
			Type:   EventTaskFailed,
			TaskID: task.ID,
			URL:    task.URL,
			Err:    err,
		})
		return res
	}

	progressWrapper := func(u ProgressUpdate) {
		emitEvent(onEvent, Event{
			Type:     EventTaskProgress,
			TaskID:   u.TaskID,
			URL:      u.URL,
			Filename: u.Filename,
			Bytes:    u.BytesRead,
			Total:    u.TotalBytes,
		})
	}

	res, err := plugin.Download(ctx, task, progressWrapper)
	if err != nil {
		if k.logger != nil {
			k.logger.LogTaskError(task.ID, task.URL, err)
		}
		failureResult := Result{
			TaskID: task.ID,
			URL:    task.URL,
			Err:    err,
		}
		emitEvent(onEvent, Event{
			Type:   EventTaskFailed,
			TaskID: task.ID,
			URL:    task.URL,
			Err:    err,
		})
		return failureResult
	}

	if k.logger != nil {
		k.logger.LogTaskSuccess(res.TaskID, res.Filename, res.BytesRead)
	}

	emitEvent(onEvent, Event{
		Type:     EventTaskCompleted,
		TaskID:   res.TaskID,
		URL:      res.URL,
		Filename: res.Filename,
		Bytes:    res.BytesRead,
		Total:    res.TotalBytes,
	})
	return *res
}

func emitEvent(handler EventHandler, event Event) {
	if handler != nil {
		handler(event)
	}
}

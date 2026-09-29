// Package kernel implements the static microkernel orchestrator, plugin registry, and concurrent task dispatcher.
package kernel

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/sebasinmas/MooFetch/internal/domain"
	"github.com/sebasinmas/MooFetch/internal/logger"
)

var (
	// ErrNoPluginFound is returned when no registered plugin can handle the specified URL.
	ErrNoPluginFound = errors.New("no registered plugin can handle the provided URL")
	// ErrNilPlugin is returned when attempting to register a nil plugin.
	ErrNilPlugin = errors.New("cannot register a nil plugin")
)

// Task re-exports domain.Task.
type Task = domain.Task

// Result re-exports domain.Result.
type Result = domain.Result

// ProgressUpdate re-exports domain.ProgressUpdate.
type ProgressUpdate = domain.ProgressUpdate

// ProgressFunc re-exports domain.ProgressFunc.
type ProgressFunc = domain.ProgressFunc

// EventType re-exports domain.EventType.
type EventType = domain.EventType

// Event re-exports domain.Event.
type Event = domain.Event

// EventHandler re-exports domain.EventHandler.
type EventHandler = domain.EventHandler

// FatalAuthError re-exports domain.FatalAuthError.
type FatalAuthError = domain.FatalAuthError

// Event constants re-exported from domain.
const (
	EventTaskStarted   = domain.EventTaskStarted
	EventTaskProgress  = domain.EventTaskProgress
	EventTaskCompleted = domain.EventTaskCompleted
	EventTaskFailed    = domain.EventTaskFailed
)

// Sentinel auth error and detector function re-exported from domain.
var (
	ErrAuthenticationFailed = domain.ErrAuthenticationFailed
	IsFatalAuth             = domain.IsFatalAuth
)

// DownloaderPlugin defines the consumer contract required by the kernel for all download handlers.
type DownloaderPlugin interface {
	Name() string
	CanHandle(rawURL string) bool
	Download(ctx context.Context, task domain.Task, progress domain.ProgressFunc) (*domain.Result, error)
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
	task domain.Task
}

// Dispatch executes the given list of tasks concurrently using a bounded worker pool,
// constrained by the kernel's concurrency limit. If any task encounters a fatal authentication
// failure (domain.IsFatalAuth), a circuit breaker triggers early batch cancellation.
func (k *Kernel) Dispatch(ctx context.Context, tasks []domain.Task, onEvent domain.EventHandler) []domain.Result {
	results := make([]domain.Result, len(tasks))
	if len(tasks) == 0 {
		return results
	}

	if err := ctx.Err(); err != nil {
		return handleImmediateCancellation(tasks, onEvent, err)
	}

	dispatchCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	tasksChan := make(chan indexedTask, len(tasks))
	for i, t := range tasks {
		tasksChan <- indexedTask{idx: i, task: t}
	}
	close(tasksChan)

	numWorkers := k.concurrency
	if numWorkers <= 0 {
		numWorkers = 1
	}
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
			k.worker(dispatchCtx, tasksChan, results, onEvent, &authFailed, &fatalAuthErr, &authErrMu, cancel)
		}()
	}

	wg.Wait()

	if dispatchCtx.Err() != nil {
		drainRemainingTasks(tasksChan, results, onEvent, readAuthErr(&fatalAuthErr, &authErrMu), dispatchCtx.Err())
	}

	return results
}

func handleImmediateCancellation(tasks []domain.Task, onEvent domain.EventHandler, err error) []domain.Result {
	results := make([]domain.Result, len(tasks))
	for i, t := range tasks {
		results[i] = domain.Result{
			TaskID: t.ID,
			URL:    t.URL,
			Err:    err,
		}
		emitEvent(onEvent, domain.Event{
			Type:   domain.EventTaskFailed,
			TaskID: t.ID,
			URL:    t.URL,
			Err:    err,
		})
	}
	return results
}

func (k *Kernel) worker(
	ctx context.Context,
	tasksChan <-chan indexedTask,
	results []domain.Result,
	onEvent domain.EventHandler,
	authFailed *atomic.Bool,
	fatalAuthErr *error,
	authErrMu *sync.Mutex,
	cancel context.CancelFunc,
) {
	for {
		select {
		case <-ctx.Done():
			return
		case item, ok := <-tasksChan:
			if !ok {
				return
			}
			if ctx.Err() != nil {
				// The item was already dequeued, so drainRemainingTasks cannot see it:
				// record it here or its result would stay zero-valued.
				failSkippedTask(item, results, onEvent, readAuthErr(fatalAuthErr, authErrMu), ctx.Err())
				return
			}

			res := k.executeTask(ctx, item.task, onEvent)
			results[item.idx] = res

			if domain.IsFatalAuth(res.Err) {
				k.tripAuthBreaker(res.Err, authFailed, fatalAuthErr, authErrMu, cancel)
				return
			}
		}
	}
}

func (k *Kernel) tripAuthBreaker(err error, authFailed *atomic.Bool, fatalAuthErr *error, authErrMu *sync.Mutex, cancel context.CancelFunc) {
	if !authFailed.CompareAndSwap(false, true) {
		return
	}
	authErrMu.Lock()
	*fatalAuthErr = err
	authErrMu.Unlock()

	if k.logger != nil {
		k.logger.Printf("Lote cancelado tempranamente por fallo de autenticación: %v", err)
	}
	cancel()
}

func readAuthErr(errp *error, mu *sync.Mutex) error {
	mu.Lock()
	defer mu.Unlock()
	return *errp
}

func drainRemainingTasks(
	tasksChan <-chan indexedTask,
	results []domain.Result,
	onEvent domain.EventHandler,
	activeAuthErr error,
	cancelErr error,
) {
	for item := range tasksChan {
		failSkippedTask(item, results, onEvent, activeAuthErr, cancelErr)
	}
}

// failSkippedTask marks a task that will never run as failed, using the auth
// error as the cause when the circuit breaker tripped and cancelErr otherwise.
func failSkippedTask(item indexedTask, results []domain.Result, onEvent domain.EventHandler, activeAuthErr, cancelErr error) {
	err := cancelErr
	if activeAuthErr != nil {
		err = fmt.Errorf("%w: batch canceled early due to authentication failure", activeAuthErr)
	}
	results[item.idx] = domain.Result{
		TaskID: item.task.ID,
		URL:    item.task.URL,
		Err:    err,
	}
	emitEvent(onEvent, domain.Event{
		Type:   domain.EventTaskFailed,
		TaskID: item.task.ID,
		URL:    item.task.URL,
		Err:    err,
	})
}

func (k *Kernel) executeTask(ctx context.Context, task domain.Task, onEvent domain.EventHandler) domain.Result {
	if k.logger != nil {
		k.logger.LogTaskStart(task.ID, task.URL)
	}

	emitEvent(onEvent, domain.Event{
		Type:   domain.EventTaskStarted,
		TaskID: task.ID,
		URL:    task.URL,
	})

	plugin, err := k.ResolvePlugin(task.URL)
	if err != nil {
		if k.logger != nil {
			k.logger.LogTaskError(task.ID, task.URL, err)
		}
		res := domain.Result{
			TaskID: task.ID,
			URL:    task.URL,
			Err:    err,
		}
		emitEvent(onEvent, domain.Event{
			Type:   domain.EventTaskFailed,
			TaskID: task.ID,
			URL:    task.URL,
			Err:    err,
		})
		return res
	}

	progressWrapper := func(u domain.ProgressUpdate) {
		emitEvent(onEvent, domain.Event{
			Type:     domain.EventTaskProgress,
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
		failureResult := domain.Result{
			TaskID: task.ID,
			URL:    task.URL,
			Err:    err,
		}
		emitEvent(onEvent, domain.Event{
			Type:   domain.EventTaskFailed,
			TaskID: task.ID,
			URL:    task.URL,
			Err:    err,
		})
		return failureResult
	}

	if k.logger != nil {
		k.logger.LogTaskSuccess(res.TaskID, res.Filename, res.BytesRead)
	}

	emitEvent(onEvent, domain.Event{
		Type:     domain.EventTaskCompleted,
		TaskID:   res.TaskID,
		URL:      res.URL,
		Filename: res.Filename,
		Bytes:    res.BytesRead,
		Total:    res.TotalBytes,
	})
	return *res
}

func emitEvent(handler domain.EventHandler, event domain.Event) {
	if handler != nil {
		handler(event)
	}
}

package kernel_test

import (
	"bytes"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/sebasinmas/MooFetch/internal/domain"
	"github.com/sebasinmas/MooFetch/internal/kernel"
	"github.com/sebasinmas/MooFetch/internal/plugins/moodle"
)

// Integration of kernel.Dispatch with the real moodle plugin against an
// httptest campus. External test package: kernel production code still has no
// dependency on any plugin.

var batchPDF = []byte("%PDF-1.4\n1 0 obj\n<< /Type /Catalog >>\nendobj\ntrailer\n<< /Root 1 0 R >>\n%%EOF")

func campusServer(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/good.pdf", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/pdf")
		w.Header().Set("Content-Length", fmt.Sprint(len(batchPDF)))
		_, _ = w.Write(batchPDF)
	})
	mux.HandleFunc("/good2.pdf", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/pdf")
		_, _ = w.Write(batchPDF)
	})
	mux.HandleFunc("/truncated.pdf", func(w http.ResponseWriter, _ *http.Request) {
		// Declares more than it sends; the server drops the connection afterwards.
		w.Header().Set("Content-Type", "application/pdf")
		w.Header().Set("Content-Length", fmt.Sprint(len(batchPDF)*4))
		_, _ = w.Write(batchPDF[:20])
	})
	mux.HandleFunc("/missing.pdf", func(w http.ResponseWriter, _ *http.Request) { http.NotFound(w, nil) })
	mux.HandleFunc("/boom.pdf", func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	})
	mux.HandleFunc("/denied.pdf", func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "denied", http.StatusForbidden)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func dirNames(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	sort.Strings(names)
	return names
}

type eventLog struct {
	mu     sync.Mutex
	events []domain.Event
}

func (l *eventLog) handle(e domain.Event) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.events = append(l.events, e)
}

func (l *eventLog) count(tp domain.EventType, taskID int) int {
	l.mu.Lock()
	defer l.mu.Unlock()
	n := 0
	for _, e := range l.events {
		if e.Type == tp && e.TaskID == taskID {
			n++
		}
	}
	return n
}

func TestDispatchMoodle_MixedBatch_GoodSavedRestReportedNoStrayFiles(t *testing.T) {
	srv := campusServer(t)
	dir := t.TempDir()
	k := kernel.New(
		kernel.WithConcurrency(4),
		kernel.WithPlugins([]kernel.DownloaderPlugin{moodle.New(moodle.WithInactivityTimeout(5 * time.Second))}),
	)

	paths := []string{"/good.pdf", "/truncated.pdf", "/missing.pdf", "/boom.pdf"}
	tasks := make([]domain.Task, len(paths))
	for i, p := range paths {
		tasks[i] = domain.Task{ID: i + 1, URL: srv.URL + p, Cookie: "MoodleSession=abc", OutputDir: dir}
	}

	var log eventLog
	results := k.Dispatch(t.Context(), tasks, log.handle)

	if len(results) != len(tasks) {
		t.Fatalf("results = %d, want %d", len(results), len(tasks))
	}
	// Results are positional: results[i] belongs to tasks[i].
	for i, r := range results {
		if r.TaskID != tasks[i].ID {
			t.Errorf("results[%d].TaskID = %d, want %d", i, r.TaskID, tasks[i].ID)
		}
	}

	if results[0].Err != nil {
		t.Fatalf("good task failed: %v", results[0].Err)
	}
	if results[0].Filename != "good.pdf" || results[0].BytesRead != int64(len(batchPDF)) {
		t.Errorf("good result = %+v", results[0])
	}
	got, err := os.ReadFile(filepath.Join(dir, "good.pdf"))
	if err != nil || !bytes.Equal(got, batchPDF) {
		t.Errorf("good.pdf not saved intact (err=%v)", err)
	}

	if results[1].Err == nil {
		t.Errorf("truncated task must fail")
	}
	for _, i := range []int{2, 3} {
		if !errors.Is(results[i].Err, moodle.ErrUnexpectedStatus) {
			t.Errorf("results[%d].Err = %v, want ErrUnexpectedStatus", i, results[i].Err)
		}
	}
	for i, r := range results {
		if domain.IsFatalAuth(r.Err) {
			t.Errorf("results[%d]: non-auth failure classified as fatal auth: %v", i, r.Err)
		}
	}

	if names := dirNames(t, dir); len(names) != 1 || names[0] != "good.pdf" {
		t.Errorf("output dir = %v, want only good.pdf", names)
	}

	if log.count(domain.EventTaskCompleted, 1) != 1 {
		t.Errorf("task 1 must emit exactly one completed event")
	}
	for _, id := range []int{2, 3, 4} {
		if log.count(domain.EventTaskFailed, id) != 1 || log.count(domain.EventTaskCompleted, id) != 0 {
			t.Errorf("task %d must emit exactly one failed and no completed event", id)
		}
	}
}

// A 403 is a fatal auth error: the circuit breaker must stop the batch, the
// task already finished stays on disk, and the cancelled ones leave nothing.
func TestDispatchMoodle_AuthFailureStopsBatchWithoutStrayFiles(t *testing.T) {
	srv := campusServer(t)
	dir := t.TempDir()
	k := kernel.New(
		kernel.WithConcurrency(1), // deterministic order
		kernel.WithPlugins([]kernel.DownloaderPlugin{moodle.New()}),
	)

	paths := []string{"/good.pdf", "/denied.pdf", "/good2.pdf"}
	tasks := make([]domain.Task, len(paths))
	for i, p := range paths {
		tasks[i] = domain.Task{ID: i + 1, URL: srv.URL + p, OutputDir: dir}
	}

	results := k.Dispatch(t.Context(), tasks, nil)

	if results[0].Err != nil {
		t.Fatalf("first task should succeed before the breaker trips: %v", results[0].Err)
	}
	if !domain.IsFatalAuth(results[1].Err) {
		t.Errorf("403 must be a fatal auth error, got %v", results[1].Err)
	}
	if results[2].Err == nil {
		t.Errorf("task after the breaker must be reported as failed/cancelled")
	}
	if names := dirNames(t, dir); len(names) != 1 || names[0] != "good.pdf" {
		t.Errorf("output dir = %v, want only good.pdf", names)
	}
}

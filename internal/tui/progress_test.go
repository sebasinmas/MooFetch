package tui

import (
	"context"
	"errors"
	"io"
	"regexp"
	"runtime"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/sebasinmas/MooFetch/internal/domain"
	"github.com/sebasinmas/MooFetch/internal/kernel"
)

func TestProgressModel_RenderVisual(t *testing.T) {
	tasks := []domain.Task{
		{ID: 1, URL: "https://campusvirtual.ufro.cl/mod/resource/view.php?id=101/Clase_01.pdf"},
		{ID: 2, URL: "https://campusvirtual.ufro.cl/mod/resource/view.php?id=102/Guia_02.pdf"},
		{ID: 3, URL: "https://campusvirtual.ufro.cl/mod/resource/view.php?id=103/Lectura.pdf"},
	}

	m := newProgressModel(context.Background(), nil, tasks, "")
	m.done = true
	m.totalDuration = 2340 * time.Millisecond
	m.results = []domain.Result{
		{TaskID: 1, URL: tasks[0].URL, Filename: "Clase_01.pdf", BytesRead: 2450000, TotalBytes: 2450000},
		{TaskID: 2, URL: tasks[1].URL, Filename: "Guia_02.pdf", BytesRead: 4890000, TotalBytes: 4890000},
		{TaskID: 3, URL: tasks[2].URL, Filename: "Lectura.pdf", BytesRead: 1780000, TotalBytes: 1780000},
	}

	out := m.View()
	if !strings.Contains(out, "MooFetch") {
		t.Errorf("expected view to contain 'MooFetch'")
	}
	if !strings.Contains(out, "Descargas completadas") {
		t.Errorf("expected view to contain 'Descargas completadas'")
	}
	if !strings.Contains(out, "SebaSinMas") {
		t.Errorf("expected view to contain 'SebaSinMas'")
	}
}

type blockingPlugin struct{}

func (b *blockingPlugin) Name() string            { return "blocking" }
func (b *blockingPlugin) CanHandle(_ string) bool { return true }
func (b *blockingPlugin) Download(ctx context.Context, task domain.Task, _ domain.ProgressFunc) (*domain.Result, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(2 * time.Second):
		return &domain.Result{TaskID: task.ID, URL: task.URL}, nil
	}
}

func TestProgress_CancellationAndGoroutineDrain(t *testing.T) {
	k := kernel.New(
		kernel.WithConcurrency(2),
		kernel.WithPlugins([]kernel.DownloaderPlugin{&blockingPlugin{}}),
	)

	tasks := []domain.Task{
		{ID: 1, URL: "http://example.com/1"},
		{ID: 2, URL: "http://example.com/2"},
	}

	ctx, cancel := context.WithCancel(context.Background())

	// Cancel context after a short delay
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	initialGoroutines := runtime.NumGoroutine()

	results, err := runProgressUI(ctx, k, tasks, "", tea.WithInput(nil), tea.WithOutput(io.Discard))
	if err != nil {
		t.Fatalf("unexpected error running progress UI: %v", err)
	}

	// Verify all tasks report context.Canceled
	if len(results) != len(tasks) {
		t.Fatalf("expected %d results, got %d", len(tasks), len(results))
	}

	for _, r := range results {
		if !errors.Is(r.Err, context.Canceled) {
			t.Errorf("expected task %d error to be context.Canceled, got: %v", r.TaskID, r.Err)
		}
	}

	// Verify goroutines drain cleanly
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		if runtime.NumGoroutine() <= initialGoroutines+1 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	if leaked := runtime.NumGoroutine() - initialGoroutines; leaked > 1 {
		t.Errorf("detected %d potentially leaked goroutines after RunProgressUI", leaked)
	}
}

func TestProgress_CtrlCKeyCancels(t *testing.T) {
	m := newProgressModel(context.Background(), nil, nil, "")

	// Send Ctrl+C
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	if cmd == nil {
		t.Errorf("expected tea.Quit command on Ctrl+C")
	}

	// Context should be cancelled
	select {
	case <-m.ctx.Done():
		// OK
	default:
		t.Errorf("expected m.ctx to be cancelled after Ctrl+C")
	}
}

func TestProgressModel_NoColor(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	InitColorProfile()

	tasks := []domain.Task{
		{ID: 1, URL: "https://campusvirtual.ufro.cl/mod/resource/view.php?id=101/Clase_01.pdf"},
	}

	m := newProgressModel(context.Background(), nil, tasks, "")
	m.done = true
	m.totalDuration = 100 * time.Millisecond
	m.results = []domain.Result{
		{TaskID: 1, URL: tasks[0].URL, Filename: "Clase_01.pdf", BytesRead: 1000, TotalBytes: 1000},
	}

	out := m.View()
	if strings.Contains(out, "\x1b[38;") || strings.Contains(out, "\x1b[48;") {
		t.Errorf("expected no ANSI color sequences when NO_COLOR=1, got: %q", out)
	}
}

func TestHyperlinkBlock_WholeBoxIsOneLink(t *testing.T) {
	m := &progressModel{done: true}
	box := m.renderCompletoBox()

	const open = "\x1b]8;id=moofetch-support;https://ko-fi.com/sebasinmas\x1b\\"
	const closing = "\x1b]8;;\x1b\\"
	var linked int
	for _, line := range strings.Split(strings.TrimRight(box, "\n"), "\n") {
		if strings.TrimSpace(line) == "" {
			if strings.Contains(line, "\x1b]8") {
				t.Errorf("blank lines must not carry a link: %q", line)
			}
			continue
		}
		if !strings.HasPrefix(line, open) || !strings.HasSuffix(line, closing) {
			t.Errorf("every box line must be wrapped in the hyperlink, got %q", line)
		}
		linked++
	}
	if linked < 4 { // top border, two text lines, bottom border
		t.Errorf("expected the whole box to be linked, only %d lines were", linked)
	}
	plain := ansiStrip(box)
	for _, want := range []string{"completo", "SebaSinMas", "ko-fi.com/sebasinmas"} {
		if !strings.Contains(plain, want) {
			t.Errorf("box should show %q, got:\n%s", want, plain)
		}
	}
}

func TestHyperlinkBlock_SkipsEmptyLines(t *testing.T) {
	got := hyperlinkBlock("\nuno\n\ndos", "https://x.test")
	lines := strings.Split(got, "\n")
	if lines[0] != "" || lines[2] != "" {
		t.Errorf("empty lines must stay empty: %q", lines)
	}
	if !strings.Contains(lines[1], "https://x.test") || !strings.Contains(lines[3], "https://x.test") {
		t.Errorf("text lines must be linked: %q", lines)
	}
}

// ansiStrip removes CSI and OSC escape sequences.
func ansiStrip(s string) string {
	return regexp.MustCompile("\x1b\\[[0-9;]*[A-Za-z]|\x1b\\][^\x1b]*\x1b\\\\").ReplaceAllString(s, "")
}

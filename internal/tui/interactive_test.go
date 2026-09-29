package tui

import (
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/sebasinmas/MooFetch/internal/domain"
	"github.com/sebasinmas/MooFetch/internal/kernel"
)

// scriptedInputs makes every interactive program launched by the package read
// one scripted key sequence (in order) instead of the real terminal. Each key
// is written separately so Esc and Enter are not merged into one sequence. The
// pipe stays open until the test ends so Bubble Tea never sees an early EOF.
func scriptedInputs(t *testing.T, scripts ...[]string) {
	t.Helper()
	var (
		mu   sync.Mutex
		next int
		pws  []*io.PipeWriter
	)
	testInput = func() io.Reader {
		mu.Lock()
		defer mu.Unlock()
		var keys []string
		if next < len(scripts) {
			keys = scripts[next]
		}
		next++
		pr, pw := io.Pipe()
		pws = append(pws, pw)
		go func() {
			for _, k := range keys {
				if _, err := pw.Write([]byte(k)); err != nil {
					return
				}
			}
		}()
		return pr
	}
	t.Cleanup(func() {
		testInput = nil
		mu.Lock()
		defer mu.Unlock()
		for _, pw := range pws {
			_ = pw.Close()
		}
	})
}

const (
	keyEnter = "\r"
	keyEsc   = "\x1b"
	keyCtrlC = "\x03"
)

func TestRunInteractiveForm_DemoSubmit(t *testing.T) {
	// Enter skips the splash, Enter advances the (optional in demo) cookie
	// step, Esc then Enter submits the empty URL box (demo defaults apply).
	scriptedInputs(t, []string{keyEnter, keyEnter, keyEsc, keyEnter})
	data, err := RunInteractiveForm(true)
	if err != nil {
		t.Fatal(err)
	}
	if data.Cookie != "MoodleSession=demo_session_ufro_showcase" {
		t.Errorf("demo cookie default missing: %q", data.Cookie)
	}
}

func TestRunInteractiveForm_AbortOnSplash(t *testing.T) {
	scriptedInputs(t, []string{keyCtrlC})
	if _, err := RunInteractiveForm(true); !errors.Is(err, ErrFormAborted) {
		t.Fatalf("want ErrFormAborted, got %v", err)
	}
}

func TestRunInteractiveForm_AbortInForm(t *testing.T) {
	scriptedInputs(t, []string{keyEnter, keyCtrlC})
	if _, err := RunInteractiveForm(true); !errors.Is(err, ErrFormAborted) {
		t.Fatalf("want ErrFormAborted, got %v", err)
	}
}

func testAuthOptions(detect func(context.Context, string) (string, error)) *AuthOptions {
	return &AuthOptions{
		Universities: []University{{Name: "Uni", Domain: "campus.uni.cl"}},
		Detect:       detect,
	}
}

func TestRunInteractiveFormWithAuth_SkipsAuthStep(t *testing.T) {
	// nil options, missing Detect and demo mode all go straight to the plain form.
	for name, opts := range map[string]*AuthOptions{
		"nil opts":  nil,
		"no detect": {},
		"demo":      testAuthOptions(func(context.Context, string) (string, error) { t.Error("Detect must not run"); return "", nil }),
	} {
		t.Run(name, func(t *testing.T) {
			scriptedInputs(t, []string{keyCtrlC})
			if _, err := RunInteractiveFormWithAuth(context.Background(), opts, true); !errors.Is(err, ErrFormAborted) {
				t.Fatalf("want ErrFormAborted, got %v", err)
			}
		})
	}
}

func TestRunInteractiveFormWithAuth_DetectFlow(t *testing.T) {
	tests := []struct {
		name       string
		authKeys   []string
		detect     func(context.Context, string) (string, error)
		wantDetect string // domain Detect must receive; "" means not called
		wantNotice string
	}{
		{"detected", []string{keyEnter, keyEnter},
			func(_ context.Context, d string) (string, error) { return "MoodleSession=zzz", nil },
			"campus.uni.cl", "Cookie de sesión detectada."},
		{"detect fails -> manual paste", []string{keyEnter, keyEnter},
			func(context.Context, string) (string, error) { return "", errors.New("no browsers") },
			"campus.uni.cl", "Pégala manualmente"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var gotDomain string
			var notice strings.Builder
			opts := testAuthOptions(func(ctx context.Context, d string) (string, error) {
				gotDomain = d
				return tc.detect(ctx, d)
			})
			opts.Notice = &notice
			// auth step: pick first university + confirm; then the form is aborted.
			scriptedInputs(t, tc.authKeys, []string{keyCtrlC})
			_, err := RunInteractiveFormWithAuth(context.Background(), opts, false)
			if !errors.Is(err, ErrFormAborted) {
				t.Fatalf("want ErrFormAborted from second step, got %v", err)
			}
			if gotDomain != tc.wantDetect {
				t.Errorf("Detect domain = %q, want %q", gotDomain, tc.wantDetect)
			}
			if !strings.Contains(notice.String(), tc.wantNotice) {
				t.Errorf("notice %q must contain %q", notice.String(), tc.wantNotice)
			}
		})
	}
}

func TestRunInteractiveFormWithAuth_PresetDomainAndAbort(t *testing.T) {
	called := false
	opts := testAuthOptions(func(context.Context, string) (string, error) { called = true; return "", nil })
	opts.Domain = "campus.preset.cl"
	// Only the consent question is shown; Ctrl+C aborts it.
	scriptedInputs(t, []string{keyCtrlC})
	if _, err := RunInteractiveFormWithAuth(context.Background(), opts, false); !errors.Is(err, ErrFormAborted) {
		t.Fatalf("want ErrFormAborted, got %v", err)
	}
	if called {
		t.Error("Detect must not run after aborting the consent step")
	}
}

func TestResolveChosenDomain(t *testing.T) {
	validate := func(s string) (string, error) {
		if strings.Contains(s, " ") {
			return "", errors.New("bad")
		}
		return strings.ToLower(s), nil
	}
	tests := []struct {
		name         string
		opts         *AuthOptions
		choice, cust string
		want         string
	}{
		{"catalog choice", &AuthOptions{}, "campus.uni.cl", "ignored", "campus.uni.cl"},
		{"custom validated", &AuthOptions{ValidateDomain: validate}, otherDomain, "  CAMPUS.X.CL ", "campus.x.cl"},
		{"custom invalid", &AuthOptions{ValidateDomain: validate}, otherDomain, "a b", ""},
		{"custom empty", &AuthOptions{ValidateDomain: validate}, otherDomain, "  ", ""},
		{"custom without validator", &AuthOptions{}, otherDomain, " raw.cl ", "raw.cl"},
	}
	for _, tc := range tests {
		if got := resolveChosenDomain(tc.opts, tc.choice, tc.cust); got != tc.want {
			t.Errorf("%s: got %q want %q", tc.name, got, tc.want)
		}
	}
}

func TestBuildAuthGroups(t *testing.T) {
	var choice, custom string
	consent := true
	opts := &AuthOptions{Universities: []University{{Name: "U", Domain: "u.cl"}}}
	if g := buildAuthGroups(opts, "", &choice, &custom, &consent); len(g) != 2 {
		t.Errorf("no preset domain: want question + custom-domain groups, got %d", len(g))
	}
	if g := buildAuthGroups(opts, "u.cl", &choice, &custom, &consent); len(g) != 1 {
		t.Errorf("preset domain: want consent group only, got %d", len(g))
	}
}

func TestRunProgressUI_Wrapper_QuitKeyCancels(t *testing.T) {
	scriptedInputs(t, []string{"q"})
	k := kernel.New(kernel.WithPlugins([]kernel.DownloaderPlugin{&blockingPlugin{}}))
	tasks := []domain.Task{{ID: 1, URL: "http://example.com/a.pdf", OutputDir: t.TempDir()}}

	results, err := RunProgressUI(context.Background(), k, tasks, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || !errors.Is(results[0].Err, context.Canceled) {
		t.Fatalf("pressing q must cancel the batch, got %+v", results)
	}
}

func TestFormatProgress(t *testing.T) {
	tests := []struct {
		bytes, total int64
		want         string
	}{
		{0, 0, "0 B"},
		{2048, 0, "2.0 KB"},
		{512, 1024, "512 B / 1.0 KB (50%)"},
		{2048, 1024, "2.0 KB / 1.0 KB (100%)"},
	}
	for _, tc := range tests {
		if got := formatProgress(tc.bytes, tc.total); got != tc.want {
			t.Errorf("formatProgress(%d,%d) = %q, want %q", tc.bytes, tc.total, got, tc.want)
		}
	}
}

func TestTruncateString(t *testing.T) {
	if got := truncateString("short", 10); got != "short" {
		t.Errorf("got %q", got)
	}
	if got := truncateString("abcdefghij", 8); got != "abcde..." {
		t.Errorf("got %q", got)
	}
}

func TestSplash_UpdateStateMachine(t *testing.T) {
	next := &dummyNext{}
	s := NewSplash(next, WithDuration(80*time.Millisecond))

	// Window size is stored and forwarded downstream.
	if _, cmd := s.Update(tea.WindowSizeMsg{Width: 100, Height: 30}); cmd != nil {
		t.Error("resize must not schedule commands")
	}
	if s.width != 100 || s.height != 30 || !next.gotResize {
		t.Errorf("resize not applied: %d %d %v", s.width, s.height, next.gotResize)
	}

	// Ticks advance until the total is reached, then hand off to the next model.
	for i := 0; i < s.totalTicks-1; i++ {
		m, cmd := s.Update(tickMsg(time.Now()))
		if m != tea.Model(s) || cmd == nil {
			t.Fatalf("tick %d: must stay on splash and reschedule", i)
		}
	}
	if m, _ := s.Update(tickMsg(time.Now())); m != tea.Model(next) {
		t.Error("final tick must transition to next model")
	}

	// Unknown messages are ignored.
	if m, cmd := s.Update(struct{}{}); m != tea.Model(s) || cmd != nil {
		t.Error("unknown msg must be a no-op")
	}
}

func TestSplash_NoNextQuits(t *testing.T) {
	s := NewSplash(nil)
	m, cmd := s.transitionToNext()
	if m != tea.Model(s) || cmd == nil {
		t.Fatal("without next model the splash must quit")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Error("expected tea.QuitMsg")
	}
}

func TestSplash_CurrentStage(t *testing.T) {
	s := NewSplash(nil)
	s.stages = nil
	if got := s.currentStage(); got.Message != "Inicializando..." {
		t.Errorf("default stage = %+v", got)
	}
	s = NewSplash(nil)
	s.currentTick = s.totalTicks * 2 // beyond the end clamps to the last stage
	if got := s.currentStage(); got != s.stages[len(s.stages)-1] {
		t.Errorf("overflow must clamp to last stage, got %+v", got)
	}
	if s.tickCmd() == nil {
		t.Error("tickCmd must return a command")
	}
}

type dummyNext struct{ gotResize bool }

func (d *dummyNext) Init() tea.Cmd { return nil }
func (d *dummyNext) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if _, ok := msg.(tea.WindowSizeMsg); ok {
		d.gotResize = true
	}
	return d, nil
}
func (d *dummyNext) View() string { return "" }

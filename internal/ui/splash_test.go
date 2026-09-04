package ui_test

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"

	"godownloader/internal/ui"
)

type dummyModel struct {
	inited  bool
	updated bool
}

func (d *dummyModel) Init() tea.Cmd {
	d.inited = true
	return nil
}

func (d *dummyModel) Update(_ tea.Msg) (tea.Model, tea.Cmd) {
	d.updated = true
	return d, nil
}

func (d *dummyModel) View() string {
	return "dummy view"
}

func TestSplash_InitialState(t *testing.T) {
	dummy := &dummyModel{}
	splash := ui.NewSplash(dummy, ui.WithDuration(500*time.Millisecond))

	if splash.Aborted() {
		t.Errorf("expected aborted to be false initially")
	}

	cmd := splash.Init()
	if cmd == nil {
		t.Errorf("expected Init to return a non-nil batch command")
	}

	view := splash.View()
	t.Log("\n" + view)
	if !strings.Contains(view, "CampusFetch") && !strings.Contains(view, "█") {
		t.Errorf("expected view to contain banner, got: %s", view)
	}
	if !strings.Contains(view, "Presiona cualquier tecla") {
		t.Errorf("expected view to contain skip hint, got: %s", view)
	}
}

func TestSplash_SkipOnKeypress(t *testing.T) {
	dummy := &dummyModel{}
	splash := ui.NewSplash(dummy)

	// Sending an Enter key message should trigger instant transition to dummy
	nextModel, cmd := splash.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if nextModel != dummy {
		t.Errorf("expected transition to dummyModel upon keypress, got %T", nextModel)
	}
	if !dummy.inited {
		t.Errorf("expected dummyModel.Init() to have been called")
	}
	_ = cmd
}

func TestSplash_AbortOnCtrlC(t *testing.T) {
	dummy := &dummyModel{}
	splash := ui.NewSplash(dummy)

	m, cmd := splash.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	sm, ok := m.(*ui.SplashModel)
	if !ok {
		t.Fatalf("expected SplashModel, got %T", m)
	}
	if !sm.Aborted() {
		t.Errorf("expected Aborted() to be true after Ctrl+C")
	}
	if cmd == nil {
		t.Errorf("expected non-nil tea.Quit command")
	}
}

func TestSplash_ConfigureWithHuhForm(t *testing.T) {
	var inputVal string
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewInput().Value(&inputVal),
		),
	)

	splash := ui.NewSplash(form)
	if form.SubmitCmd == nil || form.CancelCmd == nil {
		t.Errorf("expected form SubmitCmd and CancelCmd to be initialized for tea.Quit")
	}

	m, _ := splash.Update(tea.KeyMsg{Type: tea.KeySpace})
	if m != form {
		t.Errorf("expected model transition to form, got %T", m)
	}
}

func TestSplash_CustomOptions(t *testing.T) {
	dummy := &dummyModel{}
	customBanner := "CUSTOM_BANNER_TEST"
	customSub := "CUSTOM_SUBTITLE_TEST"
	stages := []ui.SplashStage{
		{Icon: "🚀", Message: "Custom stage 1"},
	}

	splash := ui.NewSplash(dummy,
		ui.WithBanner(customBanner),
		ui.WithSubtitle(customSub),
		ui.WithStages(stages),
		ui.WithDuration(100*time.Millisecond),
	)

	view := splash.View()
	if !strings.Contains(view, customBanner) {
		t.Errorf("expected custom banner in view")
	}
	if !strings.Contains(view, customSub) {
		t.Errorf("expected custom subtitle in view")
	}
	if !strings.Contains(view, "Custom stage 1") {
		t.Errorf("expected custom stage in view")
	}
}

func TestSplash_WindowSize(t *testing.T) {
	dummy := &dummyModel{}
	splash := ui.NewSplash(dummy)

	m, cmd := splash.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	if cmd != nil {
		t.Errorf("expected nil command for window size")
	}
	if !dummy.updated {
		t.Errorf("expected window size message to be forwarded to dummy model")
	}
	view := m.View()
	if len(view) == 0 {
		t.Errorf("expected non-empty view")
	}
}

func TestSplash_NoColor(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	ui.InitColorProfile()

	dummy := &dummyModel{}
	splash := ui.NewSplash(dummy)
	view := splash.View()

	if strings.Contains(view, "\x1b[38;") || strings.Contains(view, "\x1b[48;") {
		t.Errorf("expected no ANSI color sequences when NO_COLOR=1, got: %q", view)
	}
}

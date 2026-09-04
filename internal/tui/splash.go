// Package tui provides modern, aesthetically rich terminal user interface components.
package tui

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

// InitColorProfile sets Lipgloss color profile to Ascii if NO_COLOR is defined.
func InitColorProfile() {
	if os.Getenv("NO_COLOR") != "" {
		lipgloss.SetColorProfile(termenv.Ascii)
	}
}

// SplashStage represents a discrete milestone during the boot sequence.
type SplashStage struct {
	Icon    string
	Message string
}

// Default splash stages simulating microkernel and module loading.
var defaultStages = []SplashStage{
	{Icon: "⚡", Message: "Inicializando microkernel GoDownloader..."},
	{Icon: "🔌", Message: "Cargando motor extractor Moodle v4+..."},
	{Icon: "🛡️", Message: "Configurando sandbox seguro para cookies de sesión..."},
	{Icon: "✨", Message: "¡Microkernel listo! Desplegando interfaz interactiva..."},
}

// Compact ASCII quadrant banner: "CampusFetch"
const defaultBanner = `  █▀▀ ▄▀█ █▀▄▀█ █▀█ █ █ █▀   █▀▀ █▀▀ ▀█▀ █▀▀ █ █
  █▄▄ █▀█ █ ▀ █ █▀▀ █▄█ ▄█   █▀  ██▄  █  █▄▄ █▀█`

// Lip Gloss styles tailored with adaptive colors for both light and dark backgrounds
var (
	cardBorder = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.AdaptiveColor{Light: "#5B32D6", Dark: "#7D56F4"}).
			Padding(1, 3).
			MarginTop(1).
			MarginBottom(1)

	logoStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.AdaptiveColor{Light: "#6B3AD4", Dark: "#BD93F9"})

	subtitleStyle = lipgloss.NewStyle().
			Foreground(lipgloss.AdaptiveColor{Light: "#007799", Dark: "#8BE9FD"}).
			Bold(true)

	statusIconStyle = lipgloss.NewStyle().
			Foreground(lipgloss.AdaptiveColor{Light: "#1B7B34", Dark: "#50FA7B"}).
			Bold(true)

	statusTextStyle = lipgloss.NewStyle().
			Foreground(lipgloss.AdaptiveColor{Light: "#111111", Dark: "#F8F8F2"}).
			Bold(true)

	gaugeFilledStyle = lipgloss.NewStyle().
				Foreground(lipgloss.AdaptiveColor{Light: "#5B32D6", Dark: "#7D56F4"})

	gaugeEmptyStyle = lipgloss.NewStyle().
			Foreground(lipgloss.AdaptiveColor{Light: "#CCCCCC", Dark: "#44475A"})

	percentStyle = lipgloss.NewStyle().
			Foreground(lipgloss.AdaptiveColor{Light: "#6B3AD4", Dark: "#BD93F9"}).
			Bold(true)

	hintStyle = lipgloss.NewStyle().
			Foreground(lipgloss.AdaptiveColor{Light: "#666666", Dark: "#6272A4"}).
			Italic(true)
)

type tickMsg time.Time

// Option allows customizing SplashModel behavior.
type Option func(*SplashModel)

// WithDuration configures the total lifespan of the splash animation.
func WithDuration(d time.Duration) Option {
	return func(m *SplashModel) {
		if d > 0 {
			m.totalDuration = d
		}
	}
}

// WithStages configures custom loading stages.
func WithStages(stages []SplashStage) Option {
	return func(m *SplashModel) {
		if len(stages) > 0 {
			m.stages = stages
		}
	}
}

// WithBanner overrides the default ASCII banner.
func WithBanner(banner string) Option {
	return func(m *SplashModel) {
		m.banner = banner
	}
}

// WithSubtitle overrides the default subtitle.
func WithSubtitle(sub string) Option {
	return func(m *SplashModel) {
		m.subtitle = sub
	}
}

// SplashModel is the Bubble Tea model managing the startup animation.
type SplashModel struct {
	spinner       spinner.Model
	stages        []SplashStage
	banner        string
	subtitle      string
	next          tea.Model
	totalDuration time.Duration
	tickInterval  time.Duration
	currentTick   int
	totalTicks    int
	width         int
	height        int
	aborted       bool
}

// NewSplash constructs a new SplashModel transitioning to nextModel upon completion.
func NewSplash(nextModel tea.Model, opts ...Option) *SplashModel {
	InitColorProfile()

	s := spinner.New()
	s.Spinner = spinner.MiniDot
	s.Style = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "#007799", Dark: "#8BE9FD"})

	totalDuration := 1200 * time.Millisecond // 1.2s default: non-blocking & snappy
	tickInterval := 40 * time.Millisecond   // ~25 FPS
	totalTicks := int(totalDuration / tickInterval)
	if totalTicks < 1 {
		totalTicks = 1
	}

	// If transitioning to a huh.Form, ensure callbacks signal tea.Quit
	if form, ok := nextModel.(*huh.Form); ok {
		form.SubmitCmd = tea.Quit
		form.CancelCmd = tea.Quit
	}

	m := &SplashModel{
		spinner:       s,
		stages:        defaultStages,
		banner:        defaultBanner,
		subtitle:      "⚡ High-Performance Concurrent Intranet Fetcher",
		next:          nextModel,
		totalDuration: totalDuration,
		tickInterval:  tickInterval,
		currentTick:   0,
		totalTicks:    totalTicks,
	}

	for _, opt := range opts {
		opt(m)
	}

	// Recalculate ticks if custom duration was passed
	m.totalTicks = int(m.totalDuration / m.tickInterval)
	if m.totalTicks < 1 {
		m.totalTicks = 1
	}

	return m
}

// Aborted returns true if the user interrupted the splash screen with Ctrl+C.
func (m *SplashModel) Aborted() bool {
	return m.aborted
}

// Init starts the spinner and non-blocking ticker concurrently.
func (m *SplashModel) Init() tea.Cmd {
	return tea.Batch(
		m.spinner.Tick,
		m.tickCmd(),
	)
}

func (m *SplashModel) tickCmd() tea.Cmd {
	return tea.Tick(m.tickInterval, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

// Update drives the state machine: ticks, animations, skip keys, and model transition.
func (m *SplashModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c":
			m.aborted = true
			return m, tea.Quit
		default:
			// Power-user escape hatch: any key skips splash instantly
			return m.transitionToNext()
		}

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		if m.next != nil {
			// Propagate window resize to downstream form
			m.next, _ = m.next.Update(msg)
		}
		return m, nil

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd

	case tickMsg:
		m.currentTick++
		if m.currentTick >= m.totalTicks {
			return m.transitionToNext()
		}
		return m, m.tickCmd()
	}

	return m, nil
}

// transitionToNext seamlessly hands control over to the downstream model (e.g. *huh.Form).
func (m *SplashModel) transitionToNext() (tea.Model, tea.Cmd) {
	if m.next != nil {
		return m.next, m.next.Init()
	}
	return m, tea.Quit
}

// currentStage determines which stage to display based on elapsed tick percentage.
func (m *SplashModel) currentStage() SplashStage {
	if len(m.stages) == 0 {
		return SplashStage{Icon: "⚡", Message: "Inicializando..."}
	}
	pct := float64(m.currentTick) / float64(m.totalTicks)
	idx := int(pct * float64(len(m.stages)))
	if idx >= len(m.stages) {
		idx = len(m.stages) - 1
	}
	return m.stages[idx]
}

// View renders the visual splash card.
func (m *SplashModel) View() string {
	stage := m.currentStage()

	// Progress percentage
	pct := (m.currentTick * 100) / m.totalTicks
	if pct > 100 {
		pct = 100
	}

	// Sleek mini progress bar (26 characters wide)
	const gaugeWidth = 26
	filledChars := (gaugeWidth * m.currentTick) / m.totalTicks
	if filledChars > gaugeWidth {
		filledChars = gaugeWidth
	}
	emptyChars := gaugeWidth - filledChars
	if emptyChars < 0 {
		emptyChars = 0
	}

	bar := gaugeFilledStyle.Render(strings.Repeat("━", filledChars)) +
		gaugeEmptyStyle.Render(strings.Repeat("━", emptyChars))

	progressLine := fmt.Sprintf("  %s %s %s",
		bar,
		percentStyle.Render(fmt.Sprintf("%3d%%", pct)),
		m.spinner.View(),
	)

	statusLine := fmt.Sprintf("  %s %s",
		statusIconStyle.Render(stage.Icon),
		statusTextStyle.Render(stage.Message),
	)

	hintLine := fmt.Sprintf("  %s",
		hintStyle.Render("Presiona cualquier tecla para omitir • [Ctrl+C para salir]"),
	)

	content := strings.Join([]string{
		logoStyle.Render(m.banner),
		subtitleStyle.Render("  " + m.subtitle),
		"",
		progressLine,
		statusLine,
		"",
		hintLine,
	}, "\n")

	renderedCard := cardBorder.Render(content)

	// Responsive horizontal centering if terminal width is known
	if m.width > 0 {
		return lipgloss.PlaceHorizontal(m.width, lipgloss.Center, renderedCard)
	}

	return renderedCard
}

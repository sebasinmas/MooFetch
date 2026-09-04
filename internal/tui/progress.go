package tui

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"godownloader/internal/kernel"
)

// InitColorProfile sets Lipgloss color profile to Ascii if NO_COLOR is defined.
func InitColorProfile() {
	if os.Getenv("NO_COLOR") != "" {
		lipgloss.SetColorProfile(termenv.Ascii)
	}
}

// Styling definitions with Lip Gloss and AdaptiveColor for light and dark backgrounds
var (
	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.AdaptiveColor{Light: "#FFFFFF", Dark: "#FAFAFA"}).
			Background(lipgloss.AdaptiveColor{Light: "#5B32D6", Dark: "#7D56F4"}).
			Padding(0, 1).
			MarginBottom(1)

	cardBorder = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.AdaptiveColor{Light: "#5B32D6", Dark: "#7D56F4"}).
			Padding(0, 2).
			MarginTop(1).
			Width(56)

	successBadge = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.AdaptiveColor{Light: "#1B7B34", Dark: "#50FA7B"})

	failedBadge = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.AdaptiveColor{Light: "#D32F2F", Dark: "#FF5555"})

	downloadingBadge = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.AdaptiveColor{Light: "#007799", Dark: "#8BE9FD"})

	pendingBadge = lipgloss.NewStyle().
			Foreground(lipgloss.AdaptiveColor{Light: "#666666", Dark: "#6272A4"})

	dimStyle = lipgloss.NewStyle().
			Foreground(lipgloss.AdaptiveColor{Light: "#777777", Dark: "#6272A4"})

	boldStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.AdaptiveColor{Light: "#111111", Dark: "#F8F8F2"})

	completoCard = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.AdaptiveColor{Light: "#D97706", Dark: "#FFB86C"}).
			Padding(0, 2).
			MarginTop(1).
			Width(56)

	authorHighlight = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.AdaptiveColor{Light: "#B8004F", Dark: "#FF2A85"}).
			Background(lipgloss.AdaptiveColor{Light: "#E0E0E0", Dark: "#282A36"}).
			Padding(0, 1)
)

type itemState struct {
	taskID   int
	url      string
	filename string
	status   kernel.EventType
	bytes    int64
	total    int64
	err      error
}

type kernelEventMsg kernel.Event
type allDoneMsg struct {
	results []kernel.Result
}

type progressModel struct {
	ctx           context.Context
	cancel        context.CancelFunc
	dispatcherWg  sync.WaitGroup
	resultsMu     sync.Mutex
	kernel        *kernel.Kernel
	tasks         []kernel.Task
	items         []itemState
	spinner       spinner.Model
	startTime     time.Time
	totalDuration time.Duration
	done          bool
	results       []kernel.Result
	eventChan     chan kernel.Event
	logFilePath   string
}

func newProgressModel(ctx context.Context, k *kernel.Kernel, tasks []kernel.Task, logFilePath string) *progressModel {
	InitColorProfile()

	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithCancel(ctx)

	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "#6B3AD4", Dark: "#BD93F9"})

	items := make([]itemState, len(tasks))
	for i, t := range tasks {
		items[i] = itemState{
			taskID:   t.ID,
			url:      t.URL,
			filename: fmt.Sprintf("file_%d", t.ID),
			status:   kernel.EventType(-1), // Initial pending state
		}
	}

	return &progressModel{
		ctx:         ctx,
		cancel:      cancel,
		kernel:      k,
		tasks:       tasks,
		items:       items,
		spinner:     s,
		startTime:   time.Now(),
		eventChan:   make(chan kernel.Event, 100),
		logFilePath: logFilePath,
	}
}

type cancelMsg struct{}

func (m *progressModel) Init() tea.Cmd {
	return tea.Batch(
		m.spinner.Tick,
		m.startDispatcher(),
		m.waitForEvents(),
		m.waitForCancellation(),
	)
}

func (m *progressModel) waitForCancellation() tea.Cmd {
	return func() tea.Msg {
		<-m.ctx.Done()
		return cancelMsg{}
	}
}

func (m *progressModel) startDispatcher() tea.Cmd {
	if m.kernel == nil {
		return nil
	}
	m.dispatcherWg.Add(1)
	return func() tea.Msg {
		defer m.dispatcherWg.Done()
		results := m.kernel.Dispatch(m.ctx, m.tasks, func(ev kernel.Event) {
			select {
			case m.eventChan <- ev:
			case <-m.ctx.Done():
			}
		})
		m.resultsMu.Lock()
		m.results = results
		m.resultsMu.Unlock()
		close(m.eventChan)
		return allDoneMsg{results: results}
	}
}

func (m *progressModel) waitForEvents() tea.Cmd {
	return func() tea.Msg {
		ev, ok := <-m.eventChan
		if !ok {
			return nil
		}
		return kernelEventMsg(ev)
	}
}

func (m *progressModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case cancelMsg:
		return m, tea.Quit

	case tea.KeyMsg:
		if msg.Type == tea.KeyCtrlC || msg.String() == "ctrl+c" || msg.String() == "q" {
			if m.cancel != nil {
				m.cancel()
			}
			return m, tea.Quit
		}
		if m.done {
			return m, tea.Quit
		}

	case spinner.TickMsg:
		if m.done {
			return m, nil
		}
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd

	case kernelEventMsg:
		m.applyEvent(kernel.Event(msg))
		return m, m.waitForEvents()

	case allDoneMsg:
		m.done = true
		m.totalDuration = time.Since(m.startTime).Round(time.Millisecond)
		m.resultsMu.Lock()
		m.results = msg.results
		m.resultsMu.Unlock()
		return m, nil
	}

	return m, nil
}

func (m *progressModel) applyEvent(ev kernel.Event) {
	for i := range m.items {
		if m.items[i].taskID == ev.TaskID {
			m.items[i].status = ev.Type
			if ev.Filename != "" {
				m.items[i].filename = ev.Filename
			}
			m.items[i].bytes = ev.Bytes
			m.items[i].total = ev.Total
			m.items[i].err = ev.Err
			break
		}
	}
}

func (m *progressModel) View() string {
	var b strings.Builder

	b.WriteString(titleStyle.Render("⚡ GoDownloader • Transferencia Concurrente de Recursos"))
	b.WriteString("\n")

	for _, it := range m.items {
		b.WriteString(m.renderItem(it))
		b.WriteString("\n")
	}

	if m.done {
		b.WriteString(m.renderSummary())
		b.WriteString(m.renderCompletoBox())
	}

	return b.String()
}

func (m *progressModel) renderItem(it itemState) string {
	var statusBadge string
	switch it.status {
	case kernel.EventTaskCompleted:
		statusBadge = successBadge.Render("  ✓ COMPLETADO ")
	case kernel.EventTaskFailed:
		statusBadge = failedBadge.Render("  ✗ ERROR      ")
	case kernel.EventTaskProgress:
		statusBadge = downloadingBadge.Render(fmt.Sprintf("%s DESCARGANDO", m.spinner.View()))
	case kernel.EventTaskStarted:
		statusBadge = downloadingBadge.Render(fmt.Sprintf("%s INICIANDO  ", m.spinner.View()))
	default:
		statusBadge = pendingBadge.Render("  • PENDIENTE  ")
	}

	filename := boldStyle.Render(truncateString(it.filename, 32))
	progressText := dimStyle.Render(formatProgress(it.bytes, it.total))

	if it.err != nil {
		progressText = failedBadge.Render(fmt.Sprintf("(%s)", it.err.Error()))
	}

	return fmt.Sprintf(" %s %-32s  %s", statusBadge, filename, progressText)
}

func (m *progressModel) renderSummary() string {
	var successful, failed int
	var totalBytes int64

	for _, r := range m.results {
		if r.Err == nil {
			successful++
			totalBytes += r.BytesRead
		} else {
			failed++
		}
	}

	duration := m.totalDuration
	if duration == 0 {
		duration = time.Since(m.startTime).Round(time.Millisecond)
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "Descargas completadas en %s\n\n", duration)
	fmt.Fprintf(&sb, "  Archivos totales: %d\n", len(m.tasks))
	fmt.Fprintf(&sb, "  %s:         %d\n", successBadge.Render("Exitosos"), successful)
	fmt.Fprintf(&sb, "  %s:          %d\n", failedBadge.Render("Fallidos"), failed)
	fmt.Fprintf(&sb, "  Transferido:      %s\n", formatBytes(totalBytes))

	if m.logFilePath != "" {
		fmt.Fprintf(&sb, "  Registro:         %s\n", boldStyle.Render(m.logFilePath))
	}

	if failed > 0 && m.logFilePath != "" {
		fmt.Fprintf(&sb, "\n  %s\n", failedBadge.Render("Revisa el registro de depuración para ver las causas."))
	}

	fmt.Fprintf(&sb, "\n  %s", dimStyle.Render("Presiona cualquier tecla para salir."))

	return cardBorder.Render(sb.String()) + "\n"
}

func (m *progressModel) renderCompletoBox() string {
	boxContent := fmt.Sprintf("🌭 Considera comprarle un completo al %s", authorHighlight.Render("SebaSinMas"))
	return completoCard.Render(boxContent) + "\n"
}

// RunProgressUI starts the Bubble Tea parallel download progress interface.
func RunProgressUI(ctx context.Context, k *kernel.Kernel, tasks []kernel.Task, logFilePath string) ([]kernel.Result, error) {
	model := newProgressModel(ctx, k, tasks, logFilePath)
	defer func() {
		if model.cancel != nil {
			model.cancel()
		}
		model.dispatcherWg.Wait()
	}()

	p := tea.NewProgram(model)
	finalModel, err := p.Run()
	if err != nil {
		return nil, fmt.Errorf("failed to run progress UI: %w", err)
	}

	if pm, ok := finalModel.(*progressModel); ok {
		pm.resultsMu.Lock()
		res := pm.results
		pm.resultsMu.Unlock()
		return res, nil
	}
	return nil, nil
}

func formatBytes(b int64) string {
	if b <= 0 {
		return "0 B"
	}
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(b)/float64(div), "KMGTPE"[exp])
}

func formatProgress(bytes, total int64) string {
	if total <= 0 {
		return formatBytes(bytes)
	}
	pct := float64(bytes) / float64(total) * 100.0
	if pct > 100 {
		pct = 100
	}
	return fmt.Sprintf("%s / %s (%.0f%%)", formatBytes(bytes), formatBytes(total), pct)
}

func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}

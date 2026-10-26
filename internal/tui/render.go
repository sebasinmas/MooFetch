package tui

import (
	"fmt"
	"os"
	"runtime"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/term"
)

// Environment switches (1 = on, 0 = off) that override the automatic choice.
const (
	envAltScreen = "MOOFETCH_ALTSCREEN"
	envASCII     = "MOOFETCH_ASCII"
)

// renderMode says how the interactive screens are drawn. Old Windows consoles
// (cmd, classic PowerShell) can leave stale rows behind when a screen changes
// in place, so there the forms use the alternate screen (every frame starts at
// the top-left corner) and plain ASCII glyphs whose width the console agrees on.
type renderMode struct {
	AltScreen bool
	ASCII     bool
	// Why explains each choice for `moofetch doctor`.
	Why string
}

// detectMode decides the render mode from the OS and the environment.
func detectMode(goos string, getenv func(string) string) renderMode {
	windows := goos == "windows"
	// Windows Terminal and terminals that announce themselves handle emoji and
	// in-place redraws; the classic console host does not set either variable.
	legacy := windows && getenv("WT_SESSION") == "" && getenv("TERM_PROGRAM") == ""

	m := renderMode{AltScreen: windows, ASCII: legacy}
	var why []string
	switch {
	case windows:
		why = append(why, "pantalla alternativa: Windows")
	default:
		why = append(why, "pantalla alternativa: no (solo Windows)")
	}
	if legacy {
		why = append(why, "ASCII: consola clásica de Windows")
	} else {
		why = append(why, "ASCII: no")
	}
	if v, ok := override(getenv, envAltScreen); ok {
		m.AltScreen = v
		why = append(why, fmt.Sprintf("%s=%s", envAltScreen, onOff(v)))
	}
	if v, ok := override(getenv, envASCII); ok {
		m.ASCII = v
		why = append(why, fmt.Sprintf("%s=%s", envASCII, onOff(v)))
	}
	m.Why = strings.Join(why, "; ")
	return m
}

func override(getenv func(string) string, name string) (value, set bool) {
	switch strings.TrimSpace(getenv(name)) {
	case "1", "true", "yes", "on":
		return true, true
	case "0", "false", "no", "off":
		return false, true
	}
	return false, false
}

func onOff(b bool) string {
	if b {
		return "1"
	}
	return "0"
}

// currentMode is read from the real environment on every use, so a switch set
// before launching takes effect and tests can change it with t.Setenv.
func currentMode() renderMode { return detectMode(runtime.GOOS, os.Getenv) }

// TerminalSummary describes the detected terminal for `moofetch doctor`.
func TerminalSummary() string {
	m := currentMode()
	w, h := terminalSize()
	return fmt.Sprintf("%dx%d · pantalla alternativa: %s · ASCII: %s (%s)", w, h, yesNo(m.AltScreen), yesNo(m.ASCII), m.Why)
}

func yesNo(b bool) string {
	if b {
		return "sí"
	}
	return "no"
}

// Fallback terminal size when stdout is not a terminal.
const (
	defaultWidth  = 80
	defaultHeight = 24
)

// terminalSize returns the size of the terminal on stdout.
func terminalSize() (width, height int) {
	w, h, err := term.GetSize(os.Stdout.Fd())
	if err != nil || w <= 0 || h <= 0 {
		return defaultWidth, defaultHeight
	}
	return w, h
}

// rowMargin leaves room for the form border, the selector and the check mark.
const (
	rowMargin = 10
	minRow    = 24
)

// rowWidth is the widest label that fits on one terminal row.
func rowWidth() int {
	w, _ := terminalSize()
	return max(w-rowMargin, minRow)
}

// fitRow cuts s to width cells with an ellipsis, so a row never wraps and the
// terminal never has to scroll or re-flow it.
func fitRow(s string, width int) string {
	if ansi.StringWidth(s) <= width {
		return s
	}
	return ansi.Truncate(s, width, "…")
}

// fitRows applies fitRow to each label.
func fitRows(labels []string) []string {
	w := rowWidth()
	out := make([]string, len(labels))
	for i, l := range labels {
		out[i] = fitRow(l, w)
	}
	return out
}

// brandText removes the emoji from a title when the console cannot be trusted
// with wide characters.
func brandText(s string) string {
	if currentMode().ASCII {
		return strings.ReplaceAll(s, "🐄 ", "")
	}
	return s
}

// chip renders text as a highlighted badge (background colour plus padding).
var (
	keyChip = lipgloss.NewStyle().Bold(true).Padding(0, 1).
		Foreground(lipgloss.AdaptiveColor{Light: "#FFFFFF", Dark: "#1E1E2E"}).
		Background(lipgloss.AdaptiveColor{Light: "#5A56E0", Dark: "#B4BEFE"})
	nameChip = lipgloss.NewStyle().Bold(true).Padding(0, 1).
			Foreground(lipgloss.AdaptiveColor{Light: "#FFFFFF", Dark: "#1E1E2E"}).
			Background(lipgloss.AdaptiveColor{Light: "#0E7C66", Dark: "#94E2D5"})
)

// courseChip highlights a course (or campus) name inside a title. It goes last
// in the title: a badge ends the title's own colour, so nothing may follow it.
func courseChip(name string) string {
	return nameChip.Render("«" + fitRow(name, rowWidth()-30) + "»")
}

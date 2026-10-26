package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func env(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestDetectMode(t *testing.T) {
	tests := []struct {
		name     string
		goos     string
		env      map[string]string
		wantAlt  bool
		wantText bool
	}{
		{"linux default", "linux", nil, false, false},
		{"mac default", "darwin", nil, false, false},
		{"windows classic console", "windows", nil, true, true},
		{"windows terminal", "windows", map[string]string{"WT_SESSION": "x"}, true, false},
		{"windows vscode", "windows", map[string]string{"TERM_PROGRAM": "vscode"}, true, false},
		{"force alt on linux", "linux", map[string]string{"MOOFETCH_ALTSCREEN": "1"}, true, false},
		{"disable alt on windows", "windows", map[string]string{"MOOFETCH_ALTSCREEN": "0"}, false, true},
		{"force ascii on linux", "linux", map[string]string{"MOOFETCH_ASCII": "on"}, false, true},
		{"disable ascii on classic", "windows", map[string]string{"MOOFETCH_ASCII": "0"}, true, false},
		{"junk value ignored", "linux", map[string]string{"MOOFETCH_ASCII": "maybe"}, false, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m := detectMode(tc.goos, env(tc.env))
			if m.AltScreen != tc.wantAlt || m.ASCII != tc.wantText {
				t.Errorf("got alt=%v ascii=%v, want alt=%v ascii=%v", m.AltScreen, m.ASCII, tc.wantAlt, tc.wantText)
			}
			if m.Why == "" {
				t.Error("Why must explain the choice")
			}
		})
	}
}

func TestFitRowNeverExceedsWidth(t *testing.T) {
	long := "Guía de ejercicios resueltos de Cálculo Diferencial e Integral — Unidad 1/Subcarpeta/Otra (1.2 MB)"
	for _, w := range []int{24, 40, 80, 120} {
		got := fitRow(long, w)
		if ansi.StringWidth(got) > w {
			t.Errorf("width %d: %q is %d cells wide", w, got, ansi.StringWidth(got))
		}
	}
	if got := fitRow("corto", 40); got != "corto" {
		t.Errorf("short labels must not change, got %q", got)
	}
	if got := fitRow("日本語のとても長い名前のファイル.pdf", 20); ansi.StringWidth(got) > 20 {
		t.Errorf("wide runes: %q is %d cells", got, ansi.StringWidth(got))
	}
}

func TestFitRowsKeepsOrderAndCount(t *testing.T) {
	in := []string{"a", strings.Repeat("x", 500), "c"}
	got := fitRows(in)
	if len(got) != 3 || got[0] != "a" || got[2] != "c" || ansi.StringWidth(got[1]) > rowWidth() {
		t.Errorf("fitRows = %q", got)
	}
}

func TestBrandTextDropsEmojiInASCIIMode(t *testing.T) {
	t.Setenv("MOOFETCH_ASCII", "1")
	if got := brandText("🐄 MooFetch • Hola"); got != "MooFetch • Hola" {
		t.Errorf("got %q", got)
	}
	t.Setenv("MOOFETCH_ASCII", "0")
	if got := brandText("🐄 MooFetch • Hola"); !strings.HasPrefix(got, "🐄") {
		t.Errorf("emoji must stay when ASCII is off: %q", got)
	}
}

func TestShortenPath(t *testing.T) {
	tests := []struct{ in, want string }{
		{"Ejercicios", "Ejercicios"},
		{"/Ejercicios/Resueltos/", "Ejercicios/Resueltos"},
		{"Unidad 1/Guías de laboratorio/Semana 3/Entrega final/Resueltos", "Unidad 1/…/Resueltos"},
	}
	for _, tc := range tests {
		if got := shortenPath(tc.in, 30); got != tc.want {
			t.Errorf("shortenPath(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
	if got := shortenPath(strings.Repeat("a", 100), 20); ansi.StringWidth(got) > 20 {
		t.Errorf("single long segment must be cut: %q", got)
	}
}

func TestCourseChipKeepsName(t *testing.T) {
	got := ansi.Strip(courseChip("ICC730-1: CIBERSEGURIDAD"))
	if !strings.Contains(got, "«ICC730-1: CIBERSEGURIDAD»") {
		t.Errorf("chip = %q", got)
	}
}

func TestChipTitleEndsWithChip(t *testing.T) {
	got := chipTitle("🐄 MooFetch • Secciones de", "Cálculo", true)
	plain := ansi.Strip(got)
	if !strings.HasPrefix(plain, "🐄 MooFetch • Secciones de [MODO DEMO]") || !strings.Contains(plain, "«Cálculo»") {
		t.Errorf("title = %q", plain)
	}
}

func TestNewThemeASCIIMarkers(t *testing.T) {
	t.Setenv("MOOFETCH_ASCII", "1")
	th := newTheme()
	if got := th.Focused.SelectedPrefix.String(); got != "[x] " {
		t.Errorf("selected prefix = %q", got)
	}
	t.Setenv("MOOFETCH_ASCII", "0")
	if got := newTheme().Focused.SelectedPrefix.String(); got == "[x] " {
		t.Error("default theme must keep its own markers")
	}
}

func TestTerminalSummaryMentionsModes(t *testing.T) {
	got := TerminalSummary()
	for _, want := range []string{"pantalla alternativa", "ASCII"} {
		if !strings.Contains(got, want) {
			t.Errorf("summary %q lacks %q", got, want)
		}
	}
}

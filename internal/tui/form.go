// Package tui provides terminal user interfaces using Huh and Bubble Tea.
package tui

import (
	"errors"
	"net/url"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"
)

var (
	// ErrFormAborted indicates the user cancelled or exited the interactive form.
	ErrFormAborted = errors.New("formulario cancelado por el usuario")
)

// CleanURLs parses, sanitizes, and filters raw pasted multiline URL text.
// It trims whitespace, strips stray quotation marks or angle brackets,
// skips empty lines, and filters out non-HTTP/HTTPS URLs.
func CleanURLs(raw string) []string {
	lines := strings.Split(raw, "\n")
	var result []string
	seen := make(map[string]bool)

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		trimmed = strings.Trim(trimmed, `"'<>`)
		if trimmed == "" {
			continue
		}

		u, err := url.ParseRequestURI(trimmed)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			continue
		}

		if !seen[trimmed] {
			seen[trimmed] = true
			result = append(result, trimmed)
		}
	}

	return result
}

// CleanURLsDemo parses and sanitizes raw multiline URL or resource text for demo mode.
// It accepts any URLs, URLs missing schemes, or filename entries.
// If the input is empty or whitespace-only, it returns default sample UFRO PDF URLs.
func CleanURLsDemo(raw string) []string {
	lines := strings.Split(raw, "\n")
	var result []string
	seen := make(map[string]bool)

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		trimmed = strings.Trim(trimmed, `"'<>`)
		if trimmed == "" {
			continue
		}

		var formatted string
		if strings.HasPrefix(trimmed, "http://") || strings.HasPrefix(trimmed, "https://") {
			formatted = trimmed
		} else if strings.Contains(trimmed, ".") || strings.Contains(trimmed, "/") {
			formatted = "https://" + trimmed
		} else {
			formatted = "https://campusvirtual.ufro.cl/mod/resource/view.php?file=" + url.QueryEscape(trimmed)
		}

		if !seen[formatted] {
			seen[formatted] = true
			result = append(result, formatted)
		}
	}

	if len(result) == 0 {
		return []string{
			"https://campusvirtual.ufro.cl/mod/resource/view.php?id=12345/Clase_01_Introduccion.pdf",
			"https://campusvirtual.ufro.cl/mod/resource/view.php?id=12346/Guia_Ejercicios_01.pdf",
			"https://campusvirtual.ufro.cl/mod/resource/view.php?id=12347/Lectura_Complementaria.pdf",
		}
	}

	return result
}

// FormData contains the sanitized inputs gathered from the interactive Huh form.
type FormData struct {
	Cookie string
	URLs   []string
}

// FormController encapsulates the Huh form and allows retrieving the resulting FormData.
type FormController struct {
	Form    *huh.Form
	GetData func() *FormData
}

// NewInteractiveForm builds and returns the configured Huh form along with a data extractor.
// Optional isDemo flag enables flexible validation and showcase defaults.
func NewInteractiveForm(isDemo ...bool) *FormController {
	demoMode := len(isDemo) > 0 && isDemo[0]

	var (
		cookie  string
		rawURLs string
	)

	theme := huh.ThemeCharm()

	cookieValidator := func(s string) error {
		if strings.TrimSpace(s) == "" {
			return errors.New("la cookie de sesión no puede estar vacía")
		}
		return nil
	}
	if demoMode {
		cookieValidator = func(_ string) error {
			// In demo mode, any token (even empty) is valid
			return nil
		}
	}

	urlsValidator := func(s string) error {
		valid := CleanURLs(s)
		if len(valid) == 0 {
			return errors.New("por favor pega al menos una URL válida (HTTP/HTTPS)")
		}
		return nil
	}
	if demoMode {
		urlsValidator = func(_ string) error {
			// In demo mode, any URLs or empty (which defaults to samples) are accepted
			return nil
		}
	}

	step1Title := "Paso 1: Cookie de Sesión"
	step2Title := "Paso 2: Enlaces de los Recursos"
	if demoMode {
		step1Title = "Paso 1: Cookie de Sesión [MODO DEMO]"
		step2Title = "Paso 2: Enlaces de los Recursos [MODO DEMO]"
	}

	step1 := huh.NewGroup(
		huh.NewInput().
			Key("cookie").
			Title(step1Title).
			Description("Pega el token de tu cookie de sesión o el formato clave=valor:\n(ej., 65bmfu... o MoodleSession=65bmfu... - Guardado solo en memoria)").
			Placeholder("65bmfuq58ghdd1pgdop4208pl2").
			Value(&cookie).
			Validate(cookieValidator),
	)

	step2 := huh.NewGroup(
		huh.NewText().
			Key("urls").
			Title(step2Title).
			Description("Pega tus enlaces aquí (uno por línea, presiona Esc y luego Enter para enviar):").
			Placeholder("https://campusvirtual.ufro.cl/mod/resource/view.php?id=123/Apunte%201.pdf\nhttps://campusvirtual.ufro.cl/mod/resource/view.php?id=124/Guia%202.pdf").
			Value(&rawURLs).
			Lines(8).
			Validate(urlsValidator),
	)

	form := huh.NewForm(step1, step2).WithTheme(theme)

	return &FormController{
		Form: form,
		GetData: func() *FormData {
			finalCookie := NormalizeCookie(cookie)
			if demoMode && finalCookie == "" {
				finalCookie = "MoodleSession=demo_session_ufro_showcase"
			}

			var finalURLs []string
			if demoMode {
				finalURLs = CleanURLsDemo(rawURLs)
			} else {
				finalURLs = CleanURLs(rawURLs)
			}

			return &FormData{
				Cookie: finalCookie,
				URLs:   finalURLs,
			}
		},
	}
}

// RunInteractiveForm launches the startup splash screen and seamlessly transitions
// into the 2-step Huh form using Bubble Tea.
func RunInteractiveForm(isDemo ...bool) (*FormData, error) {
	ctrl := NewInteractiveForm(isDemo...)
	splash := NewSplash(ctrl.Form)

	p := tea.NewProgram(splash)
	if _, err := p.Run(); err != nil {
		return nil, err
	}

	if splash.Aborted() || ctrl.Form.State == huh.StateAborted {
		return nil, ErrFormAborted
	}

	return ctrl.GetData(), nil
}

// NormalizeCookie cleans and standardizes session cookies.
// It trims whitespace, removes surrounding quotes, strips leading "Cookie:" prefix,
// handles colon separators (e.g. MoodleSession:"..."), eliminates duplicate prefixes,
// and if a raw token without key=value is supplied, prefixes "MoodleSession=".
func NormalizeCookie(raw string) string {
	c := strings.TrimSpace(raw)
	c = strings.Trim(c, `"' `)

	// Strip "Cookie:" or "cookie: " if copied from curl or headers
	for strings.HasPrefix(strings.ToLower(c), "cookie:") {
		c = strings.TrimSpace(c[7:])
		c = strings.Trim(c, `"' `)
	}

	// Handle colon separators (e.g., MoodleSession:"..." or "MoodleSession": "...")
	if !strings.Contains(c, "=") && strings.Contains(c, ":") {
		parts := strings.SplitN(c, ":", 2)
		name := strings.Trim(strings.TrimSpace(parts[0]), `"' `)
		val := strings.Trim(strings.TrimSpace(parts[1]), `"' `)
		c = name + "=" + val
	}

	// Clean quotes and duplicate prefixes if key=value format
	if strings.Contains(c, "=") {
		parts := strings.SplitN(c, "=", 2)
		name := strings.Trim(strings.TrimSpace(parts[0]), `"' `)
		val := strings.Trim(strings.TrimSpace(parts[1]), `"' `)

		if strings.EqualFold(name, "moodlesession") {
			for strings.HasPrefix(strings.ToLower(val), "moodlesession=") || strings.HasPrefix(strings.ToLower(val), "moodlesession:") {
				val = strings.Trim(strings.TrimSpace(val[14:]), `"' `)
			}
		}
		c = name + "=" + val
	} else if c != "" {
		c = "MoodleSession=" + strings.Trim(c, `"' `)
	}

	return c
}

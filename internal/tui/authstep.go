package tui

import (
	"context"
	"fmt"
	"io"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"
)

// University is a selectable campus entry (kept UI-side so tui does not import auth).
type University struct {
	Name   string
	Domain string
}

// AuthOptions configures the optional browser-cookie detection step. All
// behaviour is injected by the composition root.
type AuthOptions struct {
	Universities   []University
	ValidateDomain func(string) (string, error)
	Detect         func(ctx context.Context, domain string) (string, error)
	// Scan, when set, replaces the "paste your links" step: the user gives a
	// course page, Scan lists its resources and they pick what to download.
	// pageURL is always on the chosen campus. Pasting links stays as a fallback.
	Scan func(ctx context.Context, pageURL, cookie string) ([]Resource, error)
	// Demo marks a guided showcase run: screens carry a [MODO DEMO] tag and
	// the consent screen says that no browser is read. Detect and Scan are
	// expected to be simulated by the caller.
	Demo bool
	// Domain, when set (from --uni/--domain), skips the university question.
	Domain string
	// Notice receives status messages (defaults to discarding).
	Notice io.Writer
}

const otherDomain = "\x00other"

// consentText is shown before any browser store is read.
const demoConsentText = "MODO DEMO: no se lee ningún navegador ni se hace ninguna petición.\nSe usará una cookie simulada para mostrar cómo funciona el asistente."

// demoTag is appended to screen titles in demo mode.
const demoTag = " [MODO DEMO]"

func tagged(title string, demo bool) string {
	if demo {
		return title + demoTag
	}
	return title
}

const consentText = "MooFetch leerá SOLO la cookie MoodleSession de tu campus en tus navegadores (Chrome, Chromium, Edge, Firefox, Opera, Brave y Safari en macOS).\nSe usa solo en memoria: no se guarda en disco ni se registra."

// RunInteractiveFormWithAuth shows the splash, then asks for the campus and the
// consent (one decision per screen), tries to detect the cookie and finally
// runs the cookie/URLs form. Any failure or refusal falls back to manual
// cookie pasting.
func RunInteractiveFormWithAuth(ctx context.Context, opts *AuthOptions, isDemo bool) (*FormData, error) {
	// Demo without a simulated scan keeps the plain two-step demo form.
	if opts == nil || opts.Detect == nil || (isDemo && opts.Scan == nil) {
		return RunInteractiveForm(isDemo)
	}
	notice := opts.Notice
	if notice == nil {
		notice = io.Discard
	}

	domain, consent, err := askAuthStep(opts)
	if err != nil {
		return nil, err
	}

	cookie := ""
	if consent && domain != "" {
		_, _ = fmt.Fprintf(notice, "Buscando la cookie de %s en tus navegadores...\n", domain)
		c, derr := opts.Detect(ctx, domain)
		if derr != nil {
			_, _ = fmt.Fprintf(notice, "%v Pégala manualmente.\n", derr)
		} else {
			cookie = c
			_, _ = fmt.Fprintln(notice, "Cookie de sesión detectada.")
		}
	}
	if opts.Scan != nil {
		return runScanFlow(ctx, opts, domain, cookie, notice, isDemo)
	}
	// The splash was already shown before the campus question.
	return runForm(newInteractiveForm(false, cookie), false)
}

// askAuthStep runs the splash followed by the campus and consent screens in a
// single Bubble Tea program (the splash wraps the form).
func askAuthStep(opts *AuthOptions) (domain string, consent bool, err error) {
	domain = opts.Domain
	choice := ""
	custom := ""
	consent = true

	form := newForm(buildAuthGroups(opts, domain, &choice, &custom, &consent)...)
	splash := NewSplash(form)
	p := tea.NewProgram(splash, programOptions()...)
	if _, err := p.Run(); err != nil {
		return "", false, err
	}
	if splash.Aborted() || form.State != huh.StateCompleted {
		return "", false, ErrFormAborted
	}

	if domain == "" {
		domain = resolveChosenDomain(opts, choice, custom)
	}
	return domain, consent, nil
}

// buildAuthGroups returns one group per screen: campus (skipped when the
// domain is preset), custom domain (only for "Otra…") and consent.
func buildAuthGroups(opts *AuthOptions, domain string, choice, custom *string, consent *bool) []*huh.Group {
	var groups []*huh.Group
	if domain == "" {
		options := make([]huh.Option[string], 0, len(opts.Universities)+1)
		for _, u := range opts.Universities {
			options = append(options, huh.NewOption(u.Name, u.Domain))
		}
		options = append(options, huh.NewOption("Otra…", otherDomain))
		groups = append(groups,
			huh.NewGroup(huh.NewSelect[string]().
				Title(tagged("🐄 ¿Cuál es tu universidad?", opts.Demo)).
				Options(options...).
				Value(choice)),
			huh.NewGroup(huh.NewInput().
				Title("Dominio de tu campus virtual").
				Placeholder("campus.miuniversidad.cl").
				Value(custom).
				Validate(func(s string) error {
					if opts.ValidateDomain == nil {
						return nil
					}
					_, verr := opts.ValidateDomain(s)
					return verr
				})).WithHideFunc(func() bool { return *choice != otherDomain }))
	}
	consentDesc := consentText
	if opts.Demo {
		consentDesc = demoConsentText
	}
	return append(groups, huh.NewGroup(huh.NewConfirm().
		Title(tagged("¿Detectar la cookie automáticamente desde tu navegador?", opts.Demo)).
		Description(consentDesc).
		Affirmative("Sí, detectar").
		Negative("No, pegar manualmente").
		Value(consent)))
}

func resolveChosenDomain(opts *AuthOptions, choice, custom string) string {
	if choice != otherDomain {
		return choice
	}
	domain := strings.TrimSpace(custom)
	if opts.ValidateDomain != nil && domain != "" {
		d, verr := opts.ValidateDomain(domain)
		if verr != nil {
			return ""
		}
		return d
	}
	return domain
}

// NewInteractiveFormWithCookie builds the form with an already detected cookie,
// skipping the manual cookie step (exported for tests and embedding).
func NewInteractiveFormWithCookie(isDemo bool, cookie string) *FormController {
	return newInteractiveForm(isDemo, cookie)
}

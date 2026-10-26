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
	// Login exchanges the campus username and password for a mobile-API token
	// (ADR 006). When nil the wizard keeps the browser-cookie flow only. The
	// password must not be kept, logged or put in errors; campus is the host.
	Login func(ctx context.Context, campus, user, pass string) (token string, err error)
	// Courses lists the user's courses. Expired tokens must return an error
	// wrapping domain.ErrAuthenticationFailed.
	Courses func(ctx context.Context, campus, token string) ([]Course, error)
	// Contents lists the sections and files of one course. URLs carry no token.
	Contents func(ctx context.Context, campus, token string, courseID int) ([]Section, error)
	// DefaultAuth preselects the entry method: "api" (default when Login is
	// set) or "cookie".
	DefaultAuth string
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
	title = brandText(title)
	if demo {
		return title + demoTag
	}
	return title
}

const consentText = "MooFetch leerá SOLO la cookie MoodleSession de tu campus en tus navegadores (Chrome, Chromium, Edge, Firefox, Opera, Brave y Safari en macOS).\nSe usa solo en memoria: no se guarda en disco ni se registra."

// RunInteractiveFormWithAuth shows the splash, then asks for the campus and
// how to sign in. With Login set the default is username and password through
// the mobile API (courses -> sections -> files); otherwise, or when chosen, it
// asks for the consent, tries to detect the browser cookie and runs the
// cookie/URLs form. Any failure or refusal falls back to manual pasting.
func RunInteractiveFormWithAuth(ctx context.Context, opts *AuthOptions, isDemo bool) (*FormData, error) {
	// Demo without a simulated scan keeps the plain two-step demo form.
	if opts == nil || (opts.Login == nil && (opts.Detect == nil || (isDemo && opts.Scan == nil))) {
		return RunInteractiveForm(isDemo)
	}
	notice := opts.Notice
	if notice == nil {
		notice = io.Discard
	}

	sel, err := askAuthStep(opts)
	if err != nil {
		return nil, err
	}

	if sel.method == authAPI {
		data, out, aerr := runAPIFlow(ctx, opts, sel.domain, notice, isDemo)
		if aerr != nil || data != nil {
			return data, aerr
		}
		if out == apiManual {
			return runForm(newInteractiveForm(isDemo, ""), false)
		}
		// The consent screen was skipped on the API route: ask it now.
		sel.consent = false
		if sel.domain != "" && opts.Detect != nil {
			if sel.consent, err = askDetectConsent(opts); err != nil {
				return nil, err
			}
		}
	}
	return runCookieFlow(ctx, opts, sel.domain, sel.consent, notice, isDemo)
}

// runCookieFlow detects (when allowed) the browser cookie and then scans a
// course page or asks for links.
func runCookieFlow(ctx context.Context, opts *AuthOptions, campus string, consent bool, notice io.Writer, isDemo bool) (*FormData, error) {
	cookie := ""
	if consent && campus != "" && opts.Detect != nil {
		_, _ = fmt.Fprintf(notice, "Buscando la cookie de %s en tus navegadores...\n", campus)
		c, derr := opts.Detect(ctx, campus)
		if derr != nil {
			_, _ = fmt.Fprintf(notice, "%v Pégala manualmente.\n", derr)
		} else {
			cookie = c
			_, _ = fmt.Fprintln(notice, "Cookie de sesión detectada.")
		}
	}
	if opts.Scan != nil {
		return runScanFlow(ctx, opts, campus, cookie, notice, isDemo)
	}
	// The splash was already shown before the campus question.
	return runForm(newInteractiveForm(false, cookie), false)
}

// authChoice is what the first screens of the wizard collected.
type authChoice struct {
	domain  string
	method  string // authAPI or authCookie ("" when Login is not set)
	consent bool
}

// askAuthStep runs the splash followed by the campus, sign-in method and
// consent screens in a single Bubble Tea program (the splash wraps the form).
func askAuthStep(opts *AuthOptions) (authChoice, error) {
	domain := opts.Domain
	choice := ""
	custom := ""
	consent := true
	method := ""
	if opts.Login != nil {
		method = authAPI
		if opts.DefaultAuth == authCookie {
			method = authCookie
		}
	}

	form := newForm(buildAuthGroups(opts, domain, &choice, &custom, &method, &consent)...)
	splash := NewSplash(form)
	p := tea.NewProgram(splash, programOptions()...)
	if _, err := p.Run(); err != nil {
		return authChoice{}, err
	}
	if splash.Aborted() || form.State != huh.StateCompleted {
		return authChoice{}, ErrFormAborted
	}

	if domain == "" {
		domain = resolveChosenDomain(opts, choice, custom)
	}
	return authChoice{domain: domain, method: method, consent: consent}, nil
}

// buildAuthGroups returns one group per screen: campus (skipped when the
// domain is preset), custom domain (only for "Otra…"), sign-in method (only
// when Login is set) and consent (hidden when signing in with the API).
func buildAuthGroups(opts *AuthOptions, domain string, choice, custom, method *string, consent *bool) []*huh.Group {
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
	if opts.Login != nil {
		groups = append(groups, huh.NewGroup(huh.NewSelect[string]().
			Title(tagged("¿Cómo quieres entrar?", opts.Demo)).
			Options(
				huh.NewOption("Usuario y contraseña del campus (recomendado)", authAPI),
				huh.NewOption("Cookie de mi navegador", authCookie),
			).
			Value(method)))
	}
	return append(groups, consentGroup(opts, consent).
		WithHideFunc(func() bool { return *method == authAPI }))
}

// consentGroup is the "detect the cookie from your browser?" screen.
func consentGroup(opts *AuthOptions, consent *bool) *huh.Group {
	consentDesc := consentText
	if opts.Demo {
		consentDesc = demoConsentText
	}
	return huh.NewGroup(huh.NewConfirm().
		Title(tagged("¿Detectar la cookie automáticamente desde tu navegador?", opts.Demo)).
		Description(consentDesc).
		Affirmative("Sí, detectar").
		Negative("No, pegar manualmente").
		Value(consent))
}

// askDetectConsent asks the cookie consent on its own, for when the API route
// fell back to the browser cookie.
func askDetectConsent(opts *AuthOptions) (bool, error) {
	consent := true
	if err := runHuh(newForm(consentGroup(opts, &consent))); err != nil {
		return false, err
	}
	return consent, nil
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

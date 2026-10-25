package tui

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"path"
	"strings"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"
	"github.com/sebasinmas/MooFetch/internal/domain"
)

// Resource is a downloadable item found by the page scanner. The user only
// ever sees Title; URL is what gets downloaded.
type Resource struct {
	Title string
	URL   string
}

// maxListHeight caps the resource list so long courses scroll instead of
// filling the terminal.
const maxListHeight = 16

// runScanFlow replaces the "paste your links" step: it asks for a course page,
// scans it, and lets the user untick what they do not want. Every resource
// starts selected. Pasting links by hand stays available as a fallback (empty
// page URL, or after a failed scan).
func runScanFlow(ctx context.Context, opts *AuthOptions, campus, cookie string, notice io.Writer) (*FormData, error) {
	pageURL := ""
	for {
		if cookie == "" {
			c, err := askCookie()
			if err != nil {
				return nil, err
			}
			cookie = c
		}

		var err error
		if pageURL, err = askPageURL(campus, pageURL); err != nil {
			return nil, err
		}
		if pageURL == "" {
			return runForm(newInteractiveForm(false, cookie), false)
		}

		picked, authFailed, err := scanAndPick(ctx, opts, pageURL, cookie, notice)
		if err != nil {
			return nil, err
		}
		if picked != nil {
			return &FormData{Cookie: NormalizeCookie(cookie), URLs: picked}, nil
		}
		if authFailed {
			cookie = "" // forces the cookie question again
		}

		manual, err := askRetryOrManual()
		if err != nil {
			return nil, err
		}
		if manual {
			return runForm(newInteractiveForm(false, cookie), false)
		}
	}
}

// scanAndPick scans pageURL and lets the user choose. A nil picked with a nil
// error means the attempt failed and the user was told why (authFailed marks an
// expired session).
func scanAndPick(ctx context.Context, opts *AuthOptions, pageURL, cookie string, notice io.Writer) (picked []string, authFailed bool, err error) {
	found, serr := scanWithSpinner(ctx, opts, pageURL, cookie)
	if errors.Is(serr, ErrFormAborted) {
		return nil, false, serr
	}
	if cerr := ctx.Err(); cerr != nil {
		return nil, false, cerr
	}
	switch {
	case serr == nil && len(found) > 0:
		picked, err = selectResources(found)
		return picked, false, err
	case serr == nil:
		_, _ = fmt.Fprintln(notice, "No se encontraron recursos descargables en esa página.")
	case errors.Is(serr, domain.ErrAuthenticationFailed):
		_, _ = fmt.Fprintln(notice, "Tu sesión no es válida o expiró. Inicia sesión de nuevo en el campus y pega la cookie nueva.")
		return nil, true, nil
	default:
		_, _ = fmt.Fprintf(notice, "No se pudo leer la página: %v\n", serr)
	}
	return nil, false, nil
}

// askCookie asks for the session cookie when detection failed or was refused.
func askCookie() (string, error) {
	var cookie string
	form := newForm(huh.NewGroup(
		huh.NewInput().
			Title("🐄 MooFetch • Cookie de sesión").
			Description("Pega tu cookie (ej. MoodleSession=65bmfu...). Solo se guarda en memoria.").
			Placeholder("65bmfuq58ghdd1pgdop4208pl2").
			Value(&cookie).
			Validate(func(s string) error {
				if NormalizeCookie(s) == "" {
					return errors.New("la cookie de sesión no puede estar vacía")
				}
				return nil
			}),
	))
	if err := runHuh(form); err != nil {
		return "", err
	}
	return NormalizeCookie(cookie), nil
}

// askPageURL asks for the course page to scan. Empty means "I will paste the
// links myself". The host must be the chosen campus: the cookie is sent there.
func askPageURL(campus, prefill string) (string, error) {
	pageURL := prefill
	placeholder := "https://campus.miuniversidad.cl/course/view.php?id=123"
	if campus != "" {
		placeholder = "https://" + campus + "/course/view.php?id=123"
	}
	form := newForm(huh.NewGroup(
		huh.NewInput().
			Title("🐄 MooFetch • ¿Qué página del curso quieres leer?").
			Description("Pega la URL del curso o de una sección. Déjalo vacío para pegar los enlaces a mano.").
			Placeholder(placeholder).
			Value(&pageURL).
			Validate(func(s string) error { return validatePageURL(s, campus) }),
	))
	if err := runHuh(form); err != nil {
		return "", err
	}
	return strings.TrimSpace(pageURL), nil
}

func validatePageURL(raw, campus string) error {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	u, err := url.ParseRequestURI(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" {
		return errors.New("pega una URL completa que empiece con https://")
	}
	if campus != "" {
		h := strings.ToLower(u.Hostname())
		if h != campus && !strings.HasSuffix(h, "."+campus) {
			return fmt.Errorf("la URL debe ser de %s: tu cookie solo se envía a tu campus", campus)
		}
	}
	return nil
}

// askRetryOrManual is shown after a failed or empty scan.
func askRetryOrManual() (manual bool, err error) {
	form := newForm(huh.NewGroup(
		huh.NewSelect[bool]().
			Title("¿Qué quieres hacer?").
			Options(
				huh.NewOption("Probar con otra URL", false),
				huh.NewOption("Pegar los enlaces a mano", true),
			).
			Value(&manual),
	))
	if err := runHuh(form); err != nil {
		return false, err
	}
	return manual, nil
}

// selectResources shows the resource names (never the links) with everything
// selected; the user unticks what they do not want.
func selectResources(found []Resource) ([]string, error) {
	labels := resourceLabels(found)
	options := make([]huh.Option[string], len(found))
	for i, r := range found {
		options[i] = huh.NewOption(labels[i], r.URL).Selected(true)
	}

	var picked []string
	form := newForm(huh.NewGroup(
		huh.NewMultiSelect[string]().
			Title(fmt.Sprintf("🐄 MooFetch • Elige qué descargar (%d recursos)", len(found))).
			Description("Todo viene marcado: desmarca lo que no quieras.").
			Options(options...).
			Height(min(len(found), maxListHeight) + 4).
			Filterable(true).
			Value(&picked).
			Validate(func(s []string) error {
				if len(s) == 0 {
					return errors.New("elige al menos un recurso")
				}
				return nil
			}),
	))
	if err := runHuh(form); err != nil {
		return nil, err
	}
	return picked, nil
}

// resourceLabels returns the display name of each resource: its title, or the
// file name when the title is empty, with " (2)", " (3)"... on repeated names
// so identical titles can be told apart without showing the link.
func resourceLabels(found []Resource) []string {
	labels := make([]string, len(found))
	seen := make(map[string]int, len(found))
	for i, r := range found {
		name := strings.Join(strings.Fields(r.Title), " ")
		if name == "" {
			name = fileNameOf(r.URL)
		}
		if name == "" {
			name = fmt.Sprintf("Recurso %d", i+1)
		}
		seen[name]++
		if n := seen[name]; n > 1 {
			name = fmt.Sprintf("%s (%d)", name, n)
		}
		labels[i] = name
	}
	return labels
}

func fileNameOf(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	base := path.Base(u.Path)
	if base == "." || base == "/" {
		return ""
	}
	if name, uerr := url.PathUnescape(base); uerr == nil {
		return name
	}
	return base
}

// scanning is the Bubble Tea model behind the "reading the page" spinner.
type scanning struct {
	spin    spinner.Model
	title   string
	found   []Resource
	err     error
	aborted bool
	cancel  context.CancelFunc
}

type scanDoneMsg struct {
	found []Resource
	err   error
}

func (m *scanning) Init() tea.Cmd { return m.spin.Tick }

func (m *scanning) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case scanDoneMsg:
		m.found, m.err = msg.found, msg.err
		return m, tea.Quit
	case tea.KeyMsg:
		if msg.String() == "ctrl+c" {
			m.aborted = true
			m.cancel()
			return m, tea.Quit
		}
	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spin, cmd = m.spin.Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m *scanning) View() string { return m.spin.View() + " " + m.title + "\n" }

// scanWithSpinner runs opts.Scan in the background while showing a spinner.
// Ctrl+C cancels the scan and returns ErrFormAborted.
func scanWithSpinner(ctx context.Context, opts *AuthOptions, pageURL, cookie string) ([]Resource, error) {
	sctx, cancel := context.WithCancel(ctx)
	defer cancel()

	m := &scanning{
		spin:   spinner.New(spinner.WithSpinner(spinner.Dot)),
		title:  "Leyendo la página del curso (pedimos de a una para no sobrecargar el campus)...",
		cancel: cancel,
	}
	progOpts := []tea.ProgramOption{}
	if testInput != nil {
		progOpts = append(progOpts, tea.WithInput(nil), tea.WithOutput(io.Discard))
	}
	p := tea.NewProgram(m, progOpts...)
	go func() {
		found, err := opts.Scan(sctx, pageURL, cookie)
		p.Send(scanDoneMsg{found: found, err: err})
	}()
	if _, err := p.Run(); err != nil {
		return nil, err
	}
	if m.aborted {
		return nil, ErrFormAborted
	}
	return m.found, m.err
}

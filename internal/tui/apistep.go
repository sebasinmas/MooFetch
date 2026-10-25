package tui

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"
	"github.com/sebasinmas/MooFetch/internal/domain"
)

// Course is an enrolled course as shown by the wizard.
type Course struct {
	ID   int
	Name string
}

// Section is a course section with its downloadable files.
type Section struct {
	ID     int
	Number int // position in the course, starting at 0
	Name   string
	Files  []FileItem
}

// FileItem is one downloadable file. URL never carries a secret: the token
// travels separately in FormData.Token.
type FileItem struct {
	Name   string
	URL    string
	SubDir string // folder path inside a "folder" activity ("" for resources)
	Size   int64
}

// Download is one chosen file with the sub-folder (relative to the output
// directory, "/"-separated, ending in "/") where it must be saved.
type Download struct {
	URL    string
	SubDir string
}

// ErrAPIUnavailable means the campus does not offer the mobile web service,
// so the wizard falls back to the browser-cookie flow.
var ErrAPIUnavailable = errors.New("el campus no ofrece la API móvil de Moodle para esta cuenta")

// Authentication methods offered by the wizard.
const (
	authAPI    = "api"
	authCookie = "cookie"
)

// apiOutcome says how the API stage of the wizard ended.
type apiOutcome int

const (
	apiDone    apiOutcome = iota // data is ready
	apiRelogin                   // the token expired: ask for the credentials again
	apiCookie                    // continue with the browser-cookie flow
	apiManual                    // continue by pasting links by hand
)

// Menu choices after a failure.
const (
	choiceRetry  = "retry"
	choiceCookie = "cookie"
	choiceManual = "manual"
)

// runAPIFlow drives credentials -> courses -> sections -> files. data is set
// only with apiDone; apiCookie and apiManual tell the caller which fallback to
// take. Nothing secret is ever written to notice.
func runAPIFlow(ctx context.Context, opts *AuthOptions, campus string, notice io.Writer, demo bool) (*FormData, apiOutcome, error) {
	if campus == "" {
		_, _ = fmt.Fprintln(notice, "No se pudo determinar el dominio del campus. Usaremos la cookie del navegador.")
		return nil, apiCookie, nil
	}
	user := ""
	for {
		var (
			token string
			out   apiOutcome
			err   error
		)
		token, user, out, err = apiLogin(ctx, opts, campus, user, notice, demo)
		if err != nil {
			return nil, out, err
		}
		if out != apiDone {
			return nil, out, nil
		}
		data, out, err := apiBrowse(ctx, opts, campus, token, notice, demo)
		if err != nil {
			return nil, out, err
		}
		if out == apiRelogin {
			continue
		}
		return data, out, nil
	}
}

// apiLogin asks for the credentials until the campus gives a token, or the
// user picks another way in. The returned user is kept to prefill a retry.
func apiLogin(ctx context.Context, opts *AuthOptions, campus, user string, notice io.Writer, demo bool) (token, lastUser string, out apiOutcome, err error) {
	for {
		u, pass, aerr := askCredentials(campus, user, demo)
		if aerr != nil {
			return "", user, apiDone, aerr
		}
		user = u
		tok, lerr := spinWhile(ctx, tagged("Iniciando sesión…", demo), func(c context.Context) (string, error) {
			return opts.Login(c, campus, u, pass)
		})
		if errors.Is(lerr, ErrFormAborted) {
			return "", user, apiDone, lerr
		}
		if cerr := ctx.Err(); cerr != nil {
			return "", user, apiDone, cerr
		}
		if lerr == nil && tok == "" {
			lerr = errors.New("el campus no entregó un token")
		}
		if lerr == nil {
			return tok, user, apiDone, nil
		}
		switch {
		case errors.Is(lerr, ErrAPIUnavailable):
			_, _ = fmt.Fprintln(notice, "Este campus no ofrece el acceso por usuario y contraseña. Usaremos la cookie de tu navegador.")
			return "", user, apiCookie, nil
		case errors.Is(lerr, domain.ErrAuthenticationFailed):
			_, _ = fmt.Fprintln(notice, "Usuario o contraseña incorrectos.")
		default:
			_, _ = fmt.Fprintf(notice, "No se pudo iniciar sesión: %s\n", scrub(lerr, pass, tok))
		}
		choice, merr := askFailureMenu("Reintentar con otros datos")
		if merr != nil {
			return "", user, apiDone, merr
		}
		if choice != choiceRetry {
			return "", user, outcomeOf(choice), nil
		}
	}
}

func outcomeOf(choice string) apiOutcome {
	if choice == choiceManual {
		return apiManual
	}
	return apiCookie
}

// scrub renders err without any of the secrets (defense in depth: the
// injected funcs must not leak them, but a message is never allowed to).
func scrub(err error, secrets ...string) string {
	msg := err.Error()
	for _, s := range secrets {
		if s != "" {
			msg = strings.ReplaceAll(msg, s, "***")
		}
	}
	return msg
}

// askCredentials asks for username and password (masked) on one screen.
func askCredentials(campus, prefillUser string, demo bool) (user, pass string, err error) {
	user = prefillUser
	notEmpty := func(msg string) func(string) error {
		return func(s string) error {
			if strings.TrimSpace(s) == "" {
				return errors.New(msg)
			}
			return nil
		}
	}
	form := newForm(huh.NewGroup(
		huh.NewInput().
			Title(tagged("🐄 MooFetch • Inicia sesión en "+campus, demo)).
			Description("Usa el mismo usuario y contraseña del campus. La contraseña no se guarda ni se registra.").
			Prompt("Usuario: ").
			Value(&user).
			Validate(notEmpty("escribe tu usuario")),
		huh.NewInput().
			Prompt("Contraseña: ").
			EchoMode(huh.EchoModePassword).
			Value(&pass).
			Validate(notEmpty("escribe tu contraseña")),
	))
	if err := runHuh(form); err != nil {
		return "", "", err
	}
	return strings.TrimSpace(user), pass, nil
}

// askFailureMenu is shown when the API route failed: retry, browser cookie or
// pasting the links by hand.
func askFailureMenu(retryLabel string) (string, error) {
	choice := choiceRetry
	form := newForm(huh.NewGroup(
		huh.NewSelect[string]().
			Title("¿Qué quieres hacer?").
			Options(
				huh.NewOption(retryLabel, choiceRetry),
				huh.NewOption("Usar la cookie de mi navegador", choiceCookie),
				huh.NewOption("Pegar los enlaces a mano", choiceManual),
			).
			Value(&choice),
	))
	if err := runHuh(form); err != nil {
		return "", err
	}
	return choice, nil
}

// apiBrowse lists the courses and lets the user choose courses, sections and
// files with the given token.
func apiBrowse(ctx context.Context, opts *AuthOptions, campus, token string, notice io.Writer, demo bool) (*FormData, apiOutcome, error) {
	for {
		courses, out, err := loadCourses(ctx, opts, campus, token, notice, demo)
		if err != nil || out != apiDone {
			return nil, out, err
		}
		chosen, err := selectCourses(courses, demo)
		if err != nil {
			return nil, apiDone, err
		}
		items, out, err := collectDownloads(ctx, opts, campus, token, chosen, notice, demo)
		if err != nil || out != apiDone {
			return nil, out, err
		}
		if len(items) == 0 {
			_, _ = fmt.Fprintln(notice, "No hay archivos para descargar en lo que elegiste. Elige otros cursos.")
			continue
		}
		urls := make([]string, len(items))
		for i, it := range items {
			urls[i] = it.URL
		}
		return &FormData{Token: token, Items: items, URLs: urls}, apiDone, nil
	}
}

// failedOutcome maps an API error (not an abort) to how the flow continues;
// ok is false for errors the caller must handle itself.
func failedOutcome(err error, notice io.Writer) (apiOutcome, bool) {
	switch {
	case errors.Is(err, domain.ErrAuthenticationFailed):
		_, _ = fmt.Fprintln(notice, "Tu sesión expiró. Vuelve a iniciar sesión.")
		return apiRelogin, true
	case errors.Is(err, ErrAPIUnavailable):
		_, _ = fmt.Fprintln(notice, "El campus no ofrece esta función por la API móvil. Usaremos la cookie de tu navegador.")
		return apiCookie, true
	}
	return apiDone, false
}

// interrupted reports an abort or a cancelled context from a spinner run.
func interrupted(ctx context.Context, err error) error {
	if errors.Is(err, ErrFormAborted) {
		return err
	}
	return ctx.Err()
}

// loadCourses fetches the course list, retrying on request.
func loadCourses(ctx context.Context, opts *AuthOptions, campus, token string, notice io.Writer, demo bool) ([]Course, apiOutcome, error) {
	for {
		courses, err := spinWhile(ctx, tagged("Cargando tus cursos…", demo), func(c context.Context) ([]Course, error) {
			return opts.Courses(c, campus, token)
		})
		if ierr := interrupted(ctx, err); ierr != nil {
			return nil, apiDone, ierr
		}
		if err == nil && len(courses) > 0 {
			return courses, apiDone, nil
		}
		if err == nil {
			_, _ = fmt.Fprintln(notice, "No apareces matriculado en ningún curso de este campus.")
		} else if out, ok := failedOutcome(err, notice); ok {
			return nil, out, nil
		} else {
			_, _ = fmt.Fprintf(notice, "No se pudieron leer tus cursos: %s\n", scrub(err, token))
		}
		choice, merr := askFailureMenu("Volver a intentarlo")
		if merr != nil {
			return nil, apiDone, merr
		}
		if choice != choiceRetry {
			return nil, outcomeOf(choice), nil
		}
	}
}

// selectCourses shows the course names with NONE selected (asking for every
// course at once would hammer the campus).
func selectCourses(courses []Course, demo bool) ([]Course, error) {
	names := make([]string, len(courses))
	for i, c := range courses {
		names[i] = courseName(c)
	}
	labels := dedupeLabels(names)
	options := make([]huh.Option[int], len(courses))
	for i := range courses {
		options[i] = huh.NewOption(labels[i], i)
	}
	var picked []int
	form := newForm(huh.NewGroup(
		huh.NewMultiSelect[int]().
			Title(tagged(fmt.Sprintf("🐄 MooFetch • Elige tus cursos (%d)", len(courses)), demo)).
			Description("Marca con espacio los cursos que quieres descargar (/ para filtrar).").
			Options(options...).
			Height(min(len(courses), maxListHeight) + 4).
			Filterable(true).
			Value(&picked).
			Validate(func(s []int) error {
				if len(s) == 0 {
					return errors.New("elige al menos un curso")
				}
				return nil
			}),
	))
	if err := runHuh(form); err != nil {
		return nil, err
	}
	chosen := make([]Course, len(picked))
	for i, idx := range picked {
		chosen[i] = courses[idx]
	}
	return chosen, nil
}

func courseName(c Course) string {
	if n := strings.Join(strings.Fields(c.Name), " "); n != "" {
		return n
	}
	return fmt.Sprintf("Curso %d", c.ID)
}

// collectDownloads loads the contents of each chosen course (one at a time)
// and lets the user pick sections and files.
func collectDownloads(ctx context.Context, opts *AuthOptions, campus, token string, courses []Course, notice io.Writer, demo bool) ([]Download, apiOutcome, error) {
	var items []Download
	for _, c := range courses {
		sections, err := spinWhile(ctx, tagged("Leyendo «"+courseName(c)+"»…", demo), func(cx context.Context) ([]Section, error) {
			return opts.Contents(cx, campus, token, c.ID)
		})
		if ierr := interrupted(ctx, err); ierr != nil {
			return nil, apiDone, ierr
		}
		if err != nil {
			if out, ok := failedOutcome(err, notice); ok {
				return nil, out, nil
			}
			_, _ = fmt.Fprintf(notice, "No se pudo leer «%s»: %s\n", courseName(c), scrub(err, token))
			continue
		}
		got, err := pickFromCourse(c, sections, notice, demo)
		if err != nil {
			return nil, apiDone, err
		}
		items = append(items, got...)
	}
	return items, apiDone, nil
}

// flatFile is a file together with the section it belongs to.
type flatFile struct {
	section Section
	file    FileItem
}

// pickFromCourse asks which sections and then which files of one course to
// download. Sections without files are not offered.
func pickFromCourse(c Course, sections []Section, notice io.Writer, demo bool) ([]Download, error) {
	var withFiles []Section
	for _, s := range sections {
		if len(s.Files) > 0 {
			withFiles = append(withFiles, s)
		}
	}
	if len(withFiles) == 0 {
		_, _ = fmt.Fprintf(notice, "«%s» no tiene archivos descargables.\n", courseName(c))
		return nil, nil
	}
	chosen, err := selectSections(c, withFiles, demo)
	if err != nil {
		return nil, err
	}
	var flat []flatFile
	for _, s := range chosen {
		for _, f := range s.Files {
			flat = append(flat, flatFile{section: s, file: f})
		}
	}
	if len(flat) == 0 {
		return nil, nil
	}
	picked, err := selectFiles(c, flat, demo)
	if err != nil {
		return nil, err
	}
	out := make([]Download, len(picked))
	for i, ff := range picked {
		out[i] = Download{URL: ff.file.URL, SubDir: downloadSubDir(c, ff.section, ff.file)}
	}
	return out, nil
}

func sectionName(s Section) string {
	if n := strings.Join(strings.Fields(s.Name), " "); n != "" {
		return n
	}
	return fmt.Sprintf("Sección %d", s.Number)
}

// selectSections shows "NN · Name (k archivos)" with every section selected.
func selectSections(c Course, sections []Section, demo bool) ([]Section, error) {
	options := make([]huh.Option[int], len(sections))
	for i, s := range sections {
		unit := "archivos"
		if len(s.Files) == 1 {
			unit = "archivo"
		}
		label := fmt.Sprintf("%02d · %s (%d %s)", s.Number, sectionName(s), len(s.Files), unit)
		options[i] = huh.NewOption(label, i).Selected(true)
	}
	var picked []int
	form := newForm(huh.NewGroup(
		huh.NewMultiSelect[int]().
			Title(tagged("🐄 MooFetch • Secciones de «"+courseName(c)+"»", demo)).
			Description("Todo viene marcado: desmarca lo que no quieras. Sin ninguna marcada se omite el curso.").
			Options(options...).
			Height(min(len(sections), maxListHeight) + 4).
			Filterable(true).
			Value(&picked),
	))
	if err := runHuh(form); err != nil {
		return nil, err
	}
	out := make([]Section, len(picked))
	for i, idx := range picked {
		out[i] = sections[idx]
	}
	return out, nil
}

// selectFiles shows the file names (never the links) with everything selected.
func selectFiles(c Course, flat []flatFile, demo bool) ([]flatFile, error) {
	files := make([]FileItem, len(flat))
	for i, ff := range flat {
		files[i] = ff.file
	}
	labels := fileLabels(files)
	options := make([]huh.Option[int], len(flat))
	for i := range flat {
		options[i] = huh.NewOption(labels[i], i).Selected(true)
	}
	var picked []int
	form := newForm(huh.NewGroup(
		huh.NewMultiSelect[int]().
			Title(tagged(fmt.Sprintf("🐄 MooFetch • Archivos de «%s» (%d)", courseName(c), len(flat)), demo)).
			Description("Todo viene marcado: desmarca lo que no quieras. Sin ninguno marcado se omite el curso.").
			Options(options...).
			Height(min(len(flat), maxListHeight) + 4).
			Filterable(true).
			Value(&picked),
	))
	if err := runHuh(form); err != nil {
		return nil, err
	}
	out := make([]flatFile, len(picked))
	for i, idx := range picked {
		out[i] = flat[idx]
	}
	return out, nil
}

// fileLabels returns the display text of each file: its name, " — folder" for
// files inside a folder activity, repeated names told apart with " (2)", and
// the human size. It never includes the URL.
func fileLabels(files []FileItem) []string {
	base := make([]string, len(files))
	for i, f := range files {
		name := strings.Join(strings.Fields(f.Name), " ")
		if name == "" {
			name = fileNameOf(f.URL)
		}
		if name == "" {
			name = fmt.Sprintf("Archivo %d", i+1)
		}
		if sub := strings.Trim(strings.Join(strings.Fields(f.SubDir), " "), `/\ `); sub != "" {
			name += " — " + sub
		}
		base[i] = name
	}
	labels := dedupeLabels(base)
	for i, f := range files {
		if f.Size > 0 {
			labels[i] += " (" + formatBytes(f.Size) + ")"
		}
	}
	return labels
}

// dedupeLabels appends " (2)", " (3)"... to repeated labels.
func dedupeLabels(names []string) []string {
	labels := make([]string, len(names))
	seen := make(map[string]int, len(names))
	for i, name := range names {
		seen[name]++
		if n := seen[name]; n > 1 {
			name = fmt.Sprintf("%s (%d)", name, n)
		}
		labels[i] = name
	}
	return labels
}

// downloadSubDir builds "<course>/<NN - section>/<folder segments>/" with every
// component made safe for the file system.
func downloadSubDir(c Course, s Section, f FileItem) string {
	parts := []string{
		safeDirName(courseName(c)),
		safeDirName(fmt.Sprintf("%02d - %s", s.Number, sectionName(s))),
	}
	segs := strings.FieldsFunc(f.SubDir, func(r rune) bool { return r == '/' || r == '\\' })
	for _, seg := range segs {
		if seg = strings.TrimSpace(seg); seg == "." || seg == ".." {
			continue
		}
		parts = append(parts, safeDirName(seg))
	}
	return strings.Join(parts, "/") + "/"
}

// busy is the Bubble Tea model behind the generic "working..." spinner.
type busy struct {
	spin    spinner.Model
	title   string
	aborted bool
	done    bool // finished or cancelled: render nothing so no spinner line is left behind
	cancel  context.CancelFunc
}

type busyDoneMsg struct{}

func (m *busy) Init() tea.Cmd { return m.spin.Tick }

func (m *busy) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case busyDoneMsg:
		m.done = true
		return m, tea.Quit
	case tea.KeyMsg:
		if msg.String() == "ctrl+c" {
			m.aborted = true
			m.done = true
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

func (m *busy) View() string {
	if m.done {
		return ""
	}
	return m.spin.View() + " " + m.title + "\n"
}

// spinWhile runs fn in the background while showing a spinner. Ctrl+C cancels
// fn's context and returns ErrFormAborted.
func spinWhile[T any](ctx context.Context, title string, fn func(context.Context) (T, error)) (T, error) {
	var zero T
	sctx, cancel := context.WithCancel(ctx)
	defer cancel()

	m := &busy{spin: spinner.New(spinner.WithSpinner(spinner.Dot)), title: title, cancel: cancel}
	progOpts := []tea.ProgramOption{}
	if testInput != nil {
		progOpts = append(progOpts, tea.WithInput(nil), tea.WithOutput(io.Discard))
	}
	p := tea.NewProgram(m, progOpts...)
	var (
		val T
		err error
	)
	go func() {
		val, err = fn(sctx)
		p.Send(busyDoneMsg{})
	}()
	if _, rerr := p.Run(); rerr != nil {
		return zero, rerr
	}
	if m.aborted {
		return zero, ErrFormAborted
	}
	return val, err
}

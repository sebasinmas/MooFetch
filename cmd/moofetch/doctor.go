package main

import (
	"context"
	"fmt"
	"io"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/sebasinmas/MooFetch/internal/auth"
	"github.com/sebasinmas/MooFetch/internal/tui"
	"github.com/spf13/cobra"
)

// maxDetailRunes limits the free-text error shown in the Detalle column.
const maxDetailRunes = 100

// universities is the catalog lookup used to pick a default campus. Tests
// replace it to exercise the ambiguous cases.
var universities = auth.Universities

var doctorCmd = &cobra.Command{
	Use:   "doctor",
	Short: "Diagnostica por qué no se encuentra la cookie en tus navegadores",
	Long: `Revisa, navegador por navegador y perfil por perfil, si MooFetch puede leer la
cookie de sesión de tu campus, y explica en cada caso qué pasó (cookie cifrada,
archivo en uso, sin sesión iniciada, navegador no instalado...).

El informe se escribe en stdout. No muestra valores de cookies ni envía nada
a ningún lado: solo nombres de navegador y perfil, el resultado y un motivo
breve. Puedes pegarlo tal cual al pedir ayuda.

El campus se indica con --uni o --domain; si el catálogo tiene una única
universidad, se usa esa. El comando termina con código 0 aunque no encuentre
la cookie, porque es solo un diagnóstico.`,
	Example: `  # Diagnosticar el campus de la UFRO
  moofetch doctor --uni ufro

  # Diagnosticar un campus propio
  moofetch doctor --domain campus.miuniversidad.cl`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		ctx := cmd.Context()
		if ctx == nil {
			ctx = context.Background()
		}
		domain, err := resolveDoctorDomain(flagUni, flagDomain)
		if err != nil {
			return err
		}
		return runDoctor(ctx, cmd.OutOrStdout(), cmd.ErrOrStderr(), domain, cookieProvider)
	},
}

// resolveDoctorDomain picks the campus domain from --uni/--domain, falling
// back to the only catalog entry when there is exactly one.
func resolveDoctorDomain(uni, domain string) (string, error) {
	d, err := auth.ResolveDomain(uni, domain)
	if err != nil {
		return "", err
	}
	if d != "" {
		return d, nil
	}
	if list := universities(); len(list) == 1 {
		return list[0].Domain, nil
	}
	// The wording starts with "dominio inválido" so that DetermineExitCode
	// maps it to the usage exit code.
	return "", fmt.Errorf("%w: indica el campus con --uni CLAVE o --domain HOST", auth.ErrInvalidDomain)
}

// runDoctor writes the diagnostic report to out. When provider cannot
// diagnose, it explains so on diag and succeeds without a report.
func runDoctor(ctx context.Context, out, diag io.Writer, domain string, provider auth.CookieProvider) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	d, ok := provider.(auth.Diagnoser)
	if !ok {
		_, _ = fmt.Fprintln(diag, "Esta compilación de MooFetch no incluye diagnóstico de navegadores.")
		return nil
	}
	reports := d.Diagnose(ctx, domain)
	if err := ctx.Err(); err != nil {
		return err
	}
	writeDoctorReport(out, domain, reports)
	return nil
}

// writeDoctorReport renders the header, the table and the summary.
func writeDoctorReport(w io.Writer, domain string, reports []auth.StoreReport) {
	_, _ = fmt.Fprintf(w, "MooFetch v%s | %s/%s | campus: %s\n", Version, runtime.GOOS, runtime.GOARCH, domain)
	_, _ = fmt.Fprintf(w, "Terminal: %s\n\n", tui.TerminalSummary())

	sorted := append([]auth.StoreReport(nil), reports...)
	sort.SliceStable(sorted, func(i, j int) bool {
		bi, bj := strings.ToLower(sorted[i].Browser), strings.ToLower(sorted[j].Browser)
		if bi != bj {
			return bi < bj
		}
		return strings.ToLower(sorted[i].Profile) < strings.ToLower(sorted[j].Profile)
	})

	present, missing := splitMissing(sorted)
	if len(present) > 0 {
		tw := tabwriter.NewWriter(w, 0, 0, 1, ' ', 0)
		writeRow(tw, "Navegador", "Perfil", "Resultado", "Estrategia", "Detalle")
		for _, r := range present {
			writeRow(tw, r.Browser, r.Profile, string(r.Outcome), string(r.Strategy), outcomeDetail(r))
		}
		_ = tw.Flush()
	}
	if len(missing) > 0 {
		_, _ = fmt.Fprintf(w, "\nNo instalados o sin perfil: %s.\n", strings.Join(missing, ", "))
	}

	_, _ = fmt.Fprintf(w, "\n%s\n%s\n", doctorSummary(present), doctorAdvice(sorted))
}

// splitMissing separates stores that exist from browsers that are not installed
// (reported once by name, so the table only shows what matters).
func splitMissing(sorted []auth.StoreReport) (present []auth.StoreReport, missing []string) {
	seen := map[string]bool{}
	inTable := map[string]bool{}
	for _, r := range sorted {
		if r.Outcome != auth.OutcomeMissing {
			present = append(present, r)
			inTable[strings.ToLower(strings.TrimSpace(r.Browser))] = true
		}
	}
	for _, r := range sorted {
		name := strings.TrimSpace(r.Browser)
		key := strings.ToLower(name)
		if r.Outcome == auth.OutcomeMissing && name != "" && !seen[key] && !inTable[key] {
			seen[key] = true
			missing = append(missing, name)
		}
	}
	return present, missing
}

// writeRow writes one table row, separating columns with " | ".
func writeRow(w io.Writer, cells ...string) {
	for i, c := range cells {
		c = strings.Join(strings.Fields(c), " ")
		if c == "" {
			c = "-"
		}
		cells[i] = c
	}
	_, _ = fmt.Fprintln(w, strings.Join(cells, "\t| "))
}

// outcomeDetail turns a report into a short, safe explanation.
func outcomeDetail(r auth.StoreReport) string {
	switch r.Outcome {
	case auth.OutcomeFound:
		return "cookie de sesión encontrada"
	case auth.OutcomeNoCookie:
		return "sin sesión en este navegador"
	case auth.OutcomeLocked:
		return "archivo en uso por el navegador"
	case auth.OutcomeEncrypted:
		return "el navegador protege sus cookies; usa Firefox o pega la cookie"
	case auth.OutcomeMissing:
		return "no instalado"
	case auth.OutcomeUnreadable:
		if r.Err == nil {
			return "no se pudo leer"
		}
		return "no se pudo leer: " + sanitizeError(r.Err.Error())
	default:
		return ""
	}
}

var (
	// pathRe matches a Windows or Unix path that starts a word. It runs until
	// a colon or quote so that directories with spaces are swallowed whole.
	pathRe = regexp.MustCompile(`(^|[\s"'(=])((?:[A-Za-z]:)?[\\/][^:"'<>|]*)`)
	// secretRe matches name=value pairs of session-like cookies.
	secretRe = regexp.MustCompile(`(?i)\b[\w-]*(?:session|cookie)[\w-]*=\S+`)
	// tokenRe matches long opaque strings that could be a bare cookie value.
	tokenRe = regexp.MustCompile(`\b[A-Za-z0-9+/_%-]{32,}\b`)
)

// sanitizeError shortens an error text: file paths are reduced to their base
// name, cookie-looking pairs are hidden and the result is trimmed.
func sanitizeError(msg string) string {
	msg = pathRe.ReplaceAllStringFunc(msg, func(m string) string {
		sub := pathRe.FindStringSubmatch(m)
		return sub[1] + pathBase(sub[2])
	})
	msg = secretRe.ReplaceAllString(msg, "<oculto>")
	msg = tokenRe.ReplaceAllString(msg, "<oculto>")
	msg = strings.Join(strings.Fields(msg), " ")
	if r := []rune(msg); len(r) > maxDetailRunes {
		msg = strings.TrimSpace(string(r[:maxDetailRunes-3])) + "..."
	}
	return msg
}

// pathBase returns the last element of a path with either separator.
func pathBase(p string) string {
	p = strings.TrimRight(strings.TrimSpace(p), `\/`)
	if i := strings.LastIndexAny(p, `\/`); i >= 0 {
		p = p[i+1:]
	}
	return p
}

func countOutcome(reports []auth.StoreReport, o auth.Outcome) int {
	n := 0
	for _, r := range reports {
		if r.Outcome == o {
			n++
		}
	}
	return n
}

func doctorSummary(reports []auth.StoreReport) string {
	found := countOutcome(reports, auth.OutcomeFound)
	return fmt.Sprintf("Resumen: cookie encontrada en %d de %d almacenes revisados (sin contar los no instalados).", found, len(reports))
}

// doctorAdvice gives one actionable recommendation based on the outcomes.
func doctorAdvice(reports []auth.StoreReport) string {
	if countOutcome(reports, auth.OutcomeFound) > 0 {
		return "Recomendación: todo en orden; ejecuta moofetch con --uni o --domain y usará esa cookie."
	}
	var tips []string
	if countOutcome(reports, auth.OutcomeLocked) > 0 {
		tips = append(tips, "cierra por completo el navegador (revisa también los procesos en segundo plano) y repite el diagnóstico")
	}
	if countOutcome(reports, auth.OutcomeEncrypted) > 0 {
		tips = append(tips, "inicia sesión en el campus con Firefox o pega la cookie con --cookie (-k)")
	}
	if countOutcome(reports, auth.OutcomeUnreadable) > 0 {
		tips = append(tips, "pega la cookie con --cookie (-k) o MOODLE_SESSION y comparte esta tabla al pedir ayuda")
	}
	if len(tips) == 0 && countOutcome(reports, auth.OutcomeNoCookie) > 0 {
		tips = append(tips, "inicia sesión en el campus con tu navegador y repite el diagnóstico")
	}
	if len(tips) == 0 {
		tips = append(tips, "no se encontró ningún navegador compatible; pega la cookie con --cookie (-k) o MOODLE_SESSION")
	}
	return "Recomendación: " + strings.Join(tips, "; o bien ") + "."
}

func init() {
	configureSubcommandHelp(doctorCmd)
	rootCmd.AddCommand(doctorCmd)
}

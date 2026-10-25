package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/sebasinmas/MooFetch/internal/plugins/moodle"
	"github.com/spf13/cobra"
)

var (
	flagScanDelay    time.Duration
	flagScanMaxPages int
)

var scanCmd = &cobra.Command{
	Use:   "scan <url-de-pagina>",
	Short: "Lista los recursos descargables de una página del campus",
	Long: `Lee una página del campus (curso o sección) con tu cookie de sesión y escribe en
stdout un enlace de recurso por línea, listo para canalizar a "moofetch run".

El comando es cuidadoso con el servidor: peticiones de una en una, con pausa
entre ellas (--delay), respeta Retry-After ante 429/503, se detiene al detectar
que la sesión expiró y no pide más de --max-pages páginas por ejecución.

La cookie se toma de -k, de la detección en el navegador (--uni/--domain) o de
MOODLE_SESSION, en ese orden.`,
	Example: `  # Listar los recursos de un curso
  moofetch scan "https://campusvirtual.ufro.cl/course/view.php?id=123" --uni ufro

  # Listar y descargar de una vez
  moofetch scan "$CURSO" --uni ufro | moofetch run -o ./apuntes --uni ufro`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		cookie, err := resolveCookie(cmd.Context(), false, flagCookie,
			"se requiere cookie de sesión (usa --cookie (-k), --uni/--domain para detectarla del navegador, o la variable MOODLE_SESSION)")
		if err != nil {
			return err
		}
		opts := moodle.ScanOptions{Delay: flagScanDelay, MaxPages: flagScanMaxPages}
		return runScan(cmd.Context(), moodle.New(), args[0], cookie, opts, os.Stdout, os.Stderr)
	},
}

// scanResources scans pageURL and returns the downloadable links, dropping
// folder entries (their files already come expanded as pluginfile links).
func scanResources(ctx context.Context, p *moodle.Plugin, pageURL, cookie string, opts moodle.ScanOptions) ([]moodle.Link, error) {
	moodle.UserAgentVersion = Version
	links, err := p.ScanPage(ctx, pageURL, cookie, opts)
	if err != nil {
		return nil, err
	}
	out := links[:0:0]
	for _, l := range links {
		if l.Kind != moodle.KindFolder {
			out = append(out, l)
		}
	}
	return out, nil
}

// runScan scans pageURL and writes one downloadable URL per line to out and a
// short summary to diag, so stdout stays clean for pipes.
func runScan(ctx context.Context, p *moodle.Plugin, pageURL, cookie string, opts moodle.ScanOptions, out, diag io.Writer) error {
	links, err := scanResources(ctx, p, pageURL, cookie, opts)
	if err != nil {
		return fmt.Errorf("no se pudo escanear la página: %w", err)
	}
	for _, l := range links {
		_, _ = fmt.Fprintln(out, l.URL)
		_, _ = fmt.Fprintf(diag, "  %-10s %s\n", l.Kind, l.Title)
	}
	_, _ = fmt.Fprintf(diag, "%d recursos encontrados\n", len(links))
	return nil
}

func init() {
	scanCmd.Flags().DurationVar(&flagScanDelay, "delay", 1500*time.Millisecond, "pausa entre peticiones")
	scanCmd.Flags().IntVar(&flagScanMaxPages, "max-pages", 30, "máximo de páginas a pedir")
	setFlagGroup(scanCmd.Flags(), groupDownload, "delay", "max-pages")
	rootCmd.AddCommand(scanCmd)
}

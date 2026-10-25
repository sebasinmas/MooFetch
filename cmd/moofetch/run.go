package main

import (
	"fmt"
	"os"

	"github.com/sebasinmas/MooFetch/internal/tui"
	"github.com/spf13/cobra"
)

var (
	flagRunURLs string
	flagRunFile string
)

var runCmd = &cobra.Command{
	Use:   "run",
	Short: "Descarga un lote de URLs sin asistente interactivo",
	Long: `Descarga un lote de URLs sin asistente, ideal para scripts y tuberías.

Las URLs se toman de --urls y --file; si no das ninguna y hay datos por
tubería (stdin), se leen de ahí. Una URL por línea o separadas por comas.

Origen de la cookie de sesión (en este orden):
  1. -k, --cookie
  2. detección en el navegador con --uni o --domain
  3. variable de entorno MOODLE_SESSION

Con --demo no se necesita cookie ni red.`,
	Example: `  # URLs separadas por comas
  moofetch run -u "https://campus.example/a.pdf,https://campus.example/b.pdf" -k "$MOODLE_SESSION"

  # Desde un archivo (una URL por línea)
  moofetch run -f urls.txt -o ./descargas

  # Desde una tubería
  cat urls.txt | moofetch run -c 10 -k "$MOODLE_SESSION"

  # Cookie detectada desde el navegador
  moofetch run --uni ufro -f urls.txt

  # Simulación sin red
  moofetch run --demo -f urls.txt -o /tmp/out`,
	RunE: func(cmd *cobra.Command, _ []string) error {
		raw, err := collectURLs(flagRunURLs, flagRunFile, !isTerminal(os.Stdin.Fd()), os.Stdin)
		if err != nil {
			return err
		}
		urls := cleanURLList(raw, flagDemo)
		if len(urls) == 0 {
			return fmt.Errorf("no se especificaron URLs válidas para descargar. Usa --urls (-u), --file (-f) o canaliza vía stdin")
		}

		cookie, err := resolveCookie(cmd.Context(), flagDemo, flagCookie,
			"se requiere cookie de sesión (usa --cookie (-k), --uni/--domain para detectarla del navegador, o la variable MOODLE_SESSION)")
		if err != nil {
			return err
		}
		return execute(cmd.Context(), &tui.FormData{Cookie: cookie, URLs: urls})
	},
}

func init() {
	runCmd.Flags().StringVarP(&flagRunURLs, "urls", "u", "", "URLs separadas por comas")
	runCmd.Flags().StringVarP(&flagRunFile, "file", "f", "", "archivo de texto con una URL por línea")
	setFlagGroup(runCmd.Flags(), groupInput, "urls", "file")
}

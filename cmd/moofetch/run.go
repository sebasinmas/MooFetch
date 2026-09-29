package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/sebasinmas/MooFetch/internal/domain"
	"github.com/sebasinmas/MooFetch/internal/tui"
	"github.com/spf13/cobra"
)

var (
	flagRunURLs string
	flagRunFile string
)

var runCmd = &cobra.Command{
	Use:   "run",
	Short: "Ejecuta descargas en modo directo sin asistente interactivo",
	Long: `El subcomando run permite descargar lotes de recursos especificando
las URLs y la cookie de sesión directamente mediante parámetros, archivos o tuberías Unix.`,
	Example: `  # URLs separadas por comas
  moofetch run -u "https://campus.example/a.pdf,https://campus.example/b.pdf" -k "$MOODLE_SESSION"

  # Desde un archivo (una URL por línea)
  moofetch run -f urls.txt -o ./descargas

  # Desde una tubería
  cat urls.txt | moofetch run -c 10 -k "$MOODLE_SESSION"

  # Simulación sin red
  moofetch run --demo -f urls.txt -o /tmp/out`,
	RunE: func(cmd *cobra.Command, _ []string) error {
		var rawURLs []string

		if flagRunURLs != "" {
			parts := strings.Split(flagRunURLs, ",")
			for _, p := range parts {
				if trimmed := strings.TrimSpace(p); trimmed != "" {
					rawURLs = append(rawURLs, trimmed)
				}
			}
		}

		if flagRunFile != "" {
			content, err := os.ReadFile(flagRunFile)
			if err != nil {
				return fmt.Errorf("no se pudo leer el archivo de URLs: %w", err)
			}
			lines := strings.Split(string(content), "\n")
			for _, l := range lines {
				if trimmed := strings.TrimSpace(l); trimmed != "" {
					rawURLs = append(rawURLs, trimmed)
				}
			}
		}

		// Read from stdin if no URLs provided via flags and stdin is piped
		if len(rawURLs) == 0 && !isTerminal(os.Stdin.Fd()) {
			scanner := bufio.NewScanner(os.Stdin)
			for scanner.Scan() {
				if trimmed := strings.TrimSpace(scanner.Text()); trimmed != "" {
					rawURLs = append(rawURLs, trimmed)
				}
			}
			if err := scanner.Err(); err != nil {
				return fmt.Errorf("error al leer URLs desde stdin: %w", err)
			}
		}

		var cleanURLs []string
		if flagDemo {
			cleanURLs = tui.CleanURLsDemo(strings.Join(rawURLs, "\n"))
		} else {
			cleanURLs = tui.CleanURLs(strings.Join(rawURLs, "\n"))
		}

		if len(cleanURLs) == 0 {
			return fmt.Errorf("no se especificaron URLs válidas para descargar. Usa --urls (-u), --file (-f) o canaliza vía stdin")
		}

		cookie := flagCookie
		if !flagDemo {
			var cerr error
			cookie, cerr = resolveHeadlessCookie(cmd.Context(), flagCookie)
			if cerr != nil {
				return cerr
			}
			if cookie == "" {
				return fmt.Errorf("se requiere cookie de sesión (usa --cookie (-k), --uni/--domain para detectarla del navegador, o la variable MOODLE_SESSION)")
			}
		}

		formData := &tui.FormData{
			Cookie: cookie,
			URLs:   cleanURLs,
		}

		appLogger, logPath := setupLogger(flagLogPath, flagConcurrency, flagOutputDir, formData, flagDemo)
		if appLogger != nil {
			defer func() { _ = appLogger.Close() }()
		}

		tasks := createTasks(formData, flagOutputDir)
		k := initKernel(flagConcurrency, appLogger, flagDemo)

		headless := shouldUseHeadless()

		var results []domain.Result
		var err error
		if headless {
			results, err = tui.RunHeadlessProgress(cmd.Context(), k, tasks, os.Stderr)
		} else {
			results, err = tui.RunProgressUI(cmd.Context(), k, tasks, logPath)
		}
		if err != nil {
			return fmt.Errorf("error durante la ejecución de las descargas: %w", err)
		}

		return handleCompletion(results, logPath)
	},
}

func init() {
	runCmd.Flags().StringVarP(&flagRunURLs, "urls", "u", "", "Lista de URLs separadas por comas")
	runCmd.Flags().StringVarP(&flagRunFile, "file", "f", "", "Archivo de texto con URLs (una por línea)")
}

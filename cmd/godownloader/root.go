package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/mattn/go-isatty"
	"github.com/spf13/cobra"
	"godownloader/internal/domain"
	"godownloader/internal/tui"
)

var (
	flagConcurrency int
	flagOutputDir   string
	flagDemo        bool
	flagLogPath     string
	flagCookie      string
	flagHeadless    bool
	flagPlain       bool
)

func isTerminal(fd uintptr) bool {
	return isatty.IsTerminal(fd) || isatty.IsCygwinTerminal(fd)
}

func shouldUseHeadless() bool {
	if flagHeadless || flagPlain {
		return true
	}
	if !isTerminal(os.Stdin.Fd()) || !isTerminal(os.Stdout.Fd()) {
		return true
	}
	return false
}

var rootCmd = &cobra.Command{
	Use:   "godownloader",
	Short: "GoDownloader: Descargas masivas y concurrentes de plataformas educativas (Moodle, etc.)",
	Long: `GoDownloader es una herramienta CLI de alto rendimiento diseñada para descargar
masivamente y en paralelo archivos PDF y material educativo de plataformas Moodle y similares,
inyectando cookies de sesión y resolviendo redirecciones HTTP 303 de forma transparente.`,
	RunE: func(cmd *cobra.Command, _ []string) error {
		headless := shouldUseHeadless()

		var formData *tui.FormData
		if !isTerminal(os.Stdin.Fd()) {
			// Input piped via stdin
			var rawURLs []string
			scanner := bufio.NewScanner(os.Stdin)
			for scanner.Scan() {
				if trimmed := strings.TrimSpace(scanner.Text()); trimmed != "" {
					rawURLs = append(rawURLs, trimmed)
				}
			}
			if err := scanner.Err(); err != nil {
				return fmt.Errorf("error al leer URLs desde stdin: %w", err)
			}

			var cleanURLs []string
			if flagDemo {
				cleanURLs = tui.CleanURLsDemo(strings.Join(rawURLs, "\n"))
			} else {
				cleanURLs = tui.CleanURLs(strings.Join(rawURLs, "\n"))
			}

			if len(cleanURLs) == 0 {
				return fmt.Errorf("no se detectaron URLs válidas desde la entrada estándar (stdin)")
			}

			cookie := flagCookie
			if cookie == "" && !flagDemo {
				cookie = os.Getenv("MOODLE_SESSION")
				if cookie == "" {
					return fmt.Errorf("se detectó entrada por tubería (stdin), pero falta la cookie de sesión. Usa --cookie (-k) o la variable MOODLE_SESSION")
				}
			}

			formData = &tui.FormData{
				Cookie: cookie,
				URLs:   cleanURLs,
			}
		} else {
			var err error
			formData, err = tui.RunInteractiveForm(flagDemo)
			if err != nil {
				if errors.Is(err, tui.ErrFormAborted) {
					fmt.Fprintln(os.Stderr, "\nDescarga cancelada por el usuario.")
					return tui.ErrFormAborted
				}
				return fmt.Errorf("error en formulario interactivo: %w", err)
			}
		}

		appLogger, logPath := setupLogger(flagLogPath, flagConcurrency, flagOutputDir, formData, flagDemo)
		if appLogger != nil {
			defer func() { _ = appLogger.Close() }()
		}

		tasks := createTasks(formData, flagOutputDir)
		k := initKernel(flagConcurrency, appLogger, flagDemo)

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
	rootCmd.Version = Version
	rootCmd.SetVersionTemplate(formatVersion() + "\n")

	rootCmd.PersistentFlags().IntVarP(&flagConcurrency, "concurrency", "c", 5, "Número de descargas concurrentes")
	rootCmd.PersistentFlags().StringVarP(&flagOutputDir, "output", "o", ".", "Directorio donde guardar los archivos descargados")
	rootCmd.PersistentFlags().BoolVarP(&flagDemo, "demo", "d", false, "Ejecutar en modo demostración para showcases")
	rootCmd.PersistentFlags().StringVarP(&flagLogPath, "log", "l", "", "Ruta de archivo para el registro de depuración (ej. debug.txt)")
	rootCmd.PersistentFlags().StringVar(&flagLogPath, "logger", "", "Alias para --log")
	_ = rootCmd.PersistentFlags().MarkHidden("logger")

	rootCmd.PersistentFlags().StringVarP(&flagCookie, "cookie", "k", "", "Cookie de sesión (ej. MoodleSession=...)")
	rootCmd.PersistentFlags().BoolVar(&flagHeadless, "headless", false, "Ejecutar en modo headless sin TUI (salida de progreso en stderr)")
	rootCmd.PersistentFlags().BoolVar(&flagPlain, "plain", false, "Alias para --headless")
	_ = rootCmd.PersistentFlags().MarkHidden("plain")

	rootCmd.AddCommand(versionCmd)
	rootCmd.AddCommand(runCmd)
}

func Execute(ctx context.Context) error {
	tui.InitColorProfile()
	return rootCmd.ExecuteContext(ctx)
}

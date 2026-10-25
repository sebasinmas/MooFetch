package main

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/mattn/go-isatty"
	"github.com/sebasinmas/MooFetch/internal/tui"
	"github.com/spf13/cobra"
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
	Use:   "moofetch",
	Short: "Descarga masiva de archivos desde campus Moodle usando tu sesión",
	Long: `MooFetch descarga en paralelo los archivos (PDF y otro material) de un campus
Moodle usando la cookie de sesión de tu navegador.

Modos de uso:
  Interactivo   sin argumentos y en una terminal, abre un asistente paso a paso.
  Headless      si los datos llegan por tubería (stdin) o usas --headless, no
                hay interfaz: el progreso va a stderr y stdout queda limpio.
  Subcomando    "moofetch run" toma las URLs de flags, archivo o tubería.

Origen de la cookie de sesión (en este orden):
  1. -k, --cookie
  2. detección en el navegador con --uni o --domain
  3. variable de entorno MOODLE_SESSION`,
	Example: `  # Asistente interactivo
  moofetch

  # URLs por tubería (la cookie viene de MOODLE_SESSION)
  export MOODLE_SESSION="MoodleSession=abc123"
  cat urls.txt | moofetch -o ./descargas

  # Cookie detectada desde el navegador
  moofetch run --uni ufro -f urls.txt

  # Salida limpia para scripts (progreso en stderr)
  grep -o 'https://[^ ]*' pagina.html | moofetch --headless -k "$MOODLE_SESSION"`,
	RunE: func(cmd *cobra.Command, _ []string) error {
		if isTerminal(os.Stdin.Fd()) {
			return runInteractive(cmd)
		}
		return runFromStdin(cmd)
	},
}

// runFromStdin downloads the URLs piped through stdin without any prompt.
func runFromStdin(cmd *cobra.Command) error {
	raw, err := collectURLs("", "", true, os.Stdin)
	if err != nil {
		return err
	}
	urls := cleanURLList(raw, flagDemo)
	if len(urls) == 0 {
		return fmt.Errorf("no se detectaron URLs válidas desde la entrada estándar (stdin)")
	}

	cookie, err := resolveCookie(cmd.Context(), flagDemo, flagCookie,
		"se detectó entrada por tubería (stdin), pero falta la cookie de sesión. Usa --cookie (-k) o la variable MOODLE_SESSION")
	if err != nil {
		return err
	}
	return execute(cmd.Context(), &tui.FormData{Cookie: cookie, URLs: urls})
}

// runInteractive shows the wizard and then downloads what the user entered.
func runInteractive(cmd *cobra.Command) error {
	opts, err := authOptions()
	if err != nil {
		return err
	}
	form, err := tui.RunInteractiveFormWithAuth(cmd.Context(), opts, flagDemo)
	if err != nil {
		if errors.Is(err, tui.ErrFormAborted) {
			fmt.Fprintln(os.Stderr, "\nDescarga cancelada por el usuario.")
			return tui.ErrFormAborted
		}
		return fmt.Errorf("error en formulario interactivo: %w", err)
	}
	return execute(cmd.Context(), form)
}

func init() {
	rootCmd.CompletionOptions.DisableDefaultCmd = true
	rootCmd.Version = Version
	rootCmd.SetVersionTemplate(formatVersion() + "\n")

	pf := rootCmd.PersistentFlags()

	// Descarga
	pf.StringVarP(&flagOutputDir, "output", "o", ".", "carpeta donde se guardan los archivos")
	pf.IntVarP(&flagConcurrency, "concurrency", "c", 5, "descargas simultáneas")

	// Sesión
	pf.StringVarP(&flagCookie, "cookie", "k", "", "cookie de sesión (ej. MoodleSession=abc123)")
	pf.StringVar(&flagUni, "uni", "", "universidad del catálogo, para leer la cookie del navegador (ej. ufro)")
	pf.StringVar(&flagDomain, "domain", "", "dominio del campus, para leer la cookie del navegador (ej. campus.ejemplo.cl)")

	// Salida
	pf.BoolVar(&flagHeadless, "headless", false, "sin interfaz, con el progreso en stderr")
	pf.StringVarP(&flagLogPath, "log", "l", "", "archivo de registro de depuración (ej. debug.txt)")

	// Otros
	pf.BoolVarP(&flagDemo, "demo", "d", false, "simula las descargas, sin usar la red")

	// Hidden aliases kept for backward compatibility with existing scripts.
	pf.StringVar(&flagLogPath, "logger", "", "alias de --log")
	_ = pf.MarkHidden("logger")
	pf.BoolVar(&flagPlain, "plain", false, "alias de --headless")
	_ = pf.MarkHidden("plain")

	setFlagGroup(pf, groupDownload, "output", "concurrency")
	setFlagGroup(pf, groupSession, "cookie", "uni", "domain")
	setFlagGroup(pf, groupOutput, "headless", "log")
	setFlagGroup(pf, groupOther, "demo")

	rootCmd.AddCommand(versionCmd)
	rootCmd.AddCommand(runCmd)
	configureHelp(rootCmd)
	configureSubcommandHelp(runCmd)
	configureSubcommandHelp(versionCmd)
}

func Execute(ctx context.Context) error {
	tui.InitColorProfile()
	return rootCmd.ExecuteContext(ctx)
}

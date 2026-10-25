package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

func helpFor(t *testing.T, args ...string) string {
	t.Helper()
	resetFlags(t)
	buf := new(bytes.Buffer)
	rootCmd.SetOut(buf)
	rootCmd.SetErr(buf)
	rootCmd.SetArgs(args)
	err := rootCmd.Execute()
	resetHelpFlags(t)
	if err != nil {
		t.Fatalf("%v: %v", args, err)
	}
	return buf.String()
}

func TestHelp_RootHasFlagGroupsInOrder(t *testing.T) {
	out := helpFor(t, "--help")
	last := -1
	for _, h := range []string{"\nDescarga:\n", "\nSesión:\n", "\nSalida:\n", "\nOtros:\n"} {
		i := strings.Index(out, h)
		if i < 0 {
			t.Fatalf("missing group %q in:\n%s", strings.TrimSpace(h), out)
		}
		if i < last {
			t.Errorf("group %q out of order", strings.TrimSpace(h))
		}
		last = i
	}
	for _, want := range []string{
		"-o, --output DIR", "-c, --concurrency N", "(por defecto 5)",
		"-k, --cookie COOKIE", "--uni CLAVE", "--domain HOST",
		"--headless", "-l, --log ARCHIVO", "-d, --demo",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("help must contain %q", want)
		}
	}
}

func TestHelp_FlagsLandInTheirGroup(t *testing.T) {
	out := helpFor(t, "--help")
	section := func(name string) string {
		start := strings.Index(out, "\n"+name+":\n")
		rest := out[start+1:]
		if end := strings.Index(rest, "\n\n"); end >= 0 {
			rest = rest[:end]
		}
		return rest
	}
	cases := map[string][]string{
		"Descarga": {"--output", "--concurrency"},
		"Sesión":   {"--cookie", "--uni", "--domain"},
		"Salida":   {"--headless", "--log"},
		"Otros":    {"--demo", "--help"},
	}
	for group, flags := range cases {
		s := section(group)
		for _, f := range flags {
			if !strings.Contains(s, f) {
				t.Errorf("flag %s should be in group %s:\n%s", f, group, s)
			}
		}
	}
}

func TestHelp_RootHasExitCodes(t *testing.T) {
	out := helpFor(t, "--help")
	if !strings.Contains(out, "Códigos de salida:") {
		t.Fatalf("root help must have exit codes section:\n%s", out)
	}
	for _, code := range []string{"0 ", "1 ", "2 ", "77 ", "130 "} {
		if !strings.Contains(out, "\n  "+code) {
			t.Errorf("exit code %q missing", code)
		}
	}
	if sub := helpFor(t, "run", "--help"); strings.Contains(sub, "Códigos de salida:") {
		t.Error("exit codes belong to the root help only")
	}
}

func TestHelp_RunShowsInputGroupAndGlobalGroups(t *testing.T) {
	out := helpFor(t, "run", "--help")
	for _, want := range []string{"\nEntrada:\n", "--urls", "--file", "\nDescarga:\n", "\nSesión:\n", "-k, --cookie"} {
		if !strings.Contains(out, want) {
			t.Errorf("run help must contain %q", want)
		}
	}
	if strings.Contains(out, "Global Flags") {
		t.Error("flat 'Global Flags' list must be gone")
	}
}

func TestHelp_VersionAndHelpCommand(t *testing.T) {
	if out := helpFor(t, "version", "--help"); strings.Contains(out, "--concurrency") {
		t.Errorf("version help should not list download flags:\n%s", out)
	}
	if out := helpFor(t, "help", "run"); !strings.Contains(out, "--urls") {
		t.Errorf("'help run' must show run help:\n%s", out)
	}
	if out := helpFor(t, "help", "nope"); !strings.Contains(out, "Comando desconocido") {
		t.Errorf("unknown help topic must be reported:\n%s", out)
	}
}

func TestHelp_AliasesStayHiddenButParse(t *testing.T) {
	out := helpFor(t, "--help")
	for _, hidden := range []string{"--plain", "--logger"} {
		if strings.Contains(out, hidden) {
			t.Errorf("%s must stay hidden", hidden)
		}
	}
	resetFlags(t)
	if err := rootCmd.ParseFlags([]string{"--plain", "--logger", "x.log", "-c", "3", "-o", "d", "-l", "y.log", "-d", "--headless", "-k", "k", "--uni", "ufro", "--domain", "h.cl"}); err != nil {
		t.Fatalf("flags must keep parsing: %v", err)
	}
	if !flagPlain || flagConcurrency != 3 || flagOutputDir != "d" || flagLogPath != "y.log" || !flagDemo || !flagHeadless || flagCookie != "k" || flagUni != "ufro" || flagDomain != "h.cl" {
		t.Error("flag variables not bound to expected values")
	}
	for short, long := range map[string]string{"c": "concurrency", "o": "output", "d": "demo", "l": "log", "k": "cookie"} {
		f := rootCmd.PersistentFlags().ShorthandLookup(short)
		if f == nil || f.Name != long {
			t.Errorf("shorthand -%s must map to --%s", short, long)
		}
	}
}

func TestSetFlagGroup_UnknownFlagPanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("expected panic for unknown flag")
		}
	}()
	setFlagGroup(pflag.NewFlagSet("x", pflag.ContinueOnError), groupOther, "nope")
}

func TestGroupedFlags_UngroupedFlagsFallIntoOtros(t *testing.T) {
	c := &cobra.Command{Use: "scan"}
	c.Flags().String("zzz-extra", "", "extra de prueba")
	out := groupedFlags(c)
	if !strings.Contains(out, "Otros:\n") || !strings.Contains(out, "--zzz-extra") {
		t.Errorf("ungrouped flag must be listed under Otros:\n%s", out)
	}
}

func TestCollectURLs(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "urls.txt")
	if err := os.WriteFile(file, []byte("https://a/x.pdf\n\n  https://b/y.pdf  \n"), 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := collectURLs(" https://c/z.pdf , ,https://d/w.pdf", file, true, strings.NewReader("https://stdin/ignored.pdf"))
	if err != nil || len(got) != 4 || got[0] != "https://c/z.pdf" || got[3] != "https://b/y.pdf" {
		t.Fatalf("flags+file must win and skip stdin: %v %v", got, err)
	}

	got, err = collectURLs("", "", true, strings.NewReader("\n https://s/1.pdf \nhttps://s/2.pdf\n"))
	if err != nil || len(got) != 2 {
		t.Fatalf("stdin fallback: %v %v", got, err)
	}

	got, err = collectURLs("", "", false, strings.NewReader("https://s/1.pdf"))
	if err != nil || len(got) != 0 {
		t.Fatalf("stdin must not be read when disabled: %v %v", got, err)
	}

	if _, err = collectURLs("", filepath.Join(dir, "missing.txt"), false, nil); err == nil || !strings.Contains(err.Error(), "no se pudo leer el archivo de URLs") {
		t.Fatalf("missing file error: %v", err)
	}
}

type failingReader struct{}

func (failingReader) Read([]byte) (int, error) { return 0, os.ErrClosed }

func TestReadLines_ReportsReadError(t *testing.T) {
	if _, err := readLines(failingReader{}); err == nil || !strings.Contains(err.Error(), "error al leer URLs desde stdin") {
		t.Fatalf("expected stdin read error, got %v", err)
	}
}

func TestCleanURLList(t *testing.T) {
	raw := []string{"https://campus.example/a.pdf", "basura"}
	if got := cleanURLList(raw, false); len(got) != 1 {
		t.Errorf("real mode: %v", got)
	}
	if got := cleanURLList(nil, true); len(got) == 0 {
		t.Error("demo mode falls back to sample URLs on empty input")
	}
}

func TestResolveCookie(t *testing.T) {
	ctx := context.Background()
	if got, err := resolveCookie(ctx, true, "raw", "msg"); err != nil || got != "raw" {
		t.Errorf("demo passes flag through: %q %v", got, err)
	}
	t.Setenv("MOODLE_SESSION", "")
	if _, err := resolveCookie(ctx, false, "", "falta la cookie"); err == nil || err.Error() != "falta la cookie" {
		t.Errorf("missing cookie must use caller message: %v", err)
	}
	if got, err := resolveCookie(ctx, false, "tok", "msg"); err != nil || got != "MoodleSession=tok" {
		t.Errorf("explicit cookie is normalized: %q %v", got, err)
	}
}

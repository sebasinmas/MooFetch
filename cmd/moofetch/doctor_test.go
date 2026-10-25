package main

import (
	"bytes"
	"context"
	"errors"
	"runtime"
	"strings"
	"testing"

	"github.com/sebasinmas/MooFetch/internal/auth"
)

// fakeDiagnoser is a CookieProvider that also implements auth.Diagnoser.
type fakeDiagnoser struct {
	reports []auth.StoreReport
	domain  string
	cancel  context.CancelFunc
}

func (*fakeDiagnoser) Find(context.Context, string) (string, error) { return "", errors.New("n/a") }

func (f *fakeDiagnoser) Diagnose(_ context.Context, domain string) []auth.StoreReport {
	f.domain = domain
	if f.cancel != nil {
		f.cancel()
	}
	return f.reports
}

func allOutcomeReports() []auth.StoreReport {
	return []auth.StoreReport{
		{Browser: "Firefox", Profile: "default-release", Outcome: auth.OutcomeFound, Strategy: auth.StrategyShared},
		{Browser: "Edge", Profile: "Profile 1", Outcome: auth.OutcomeEncrypted, Strategy: auth.StrategyShared},
		{Browser: "Chrome", Profile: "Profile 2", Outcome: auth.OutcomeNoCookie, Strategy: auth.StrategyShared},
		{Browser: "Chrome", Profile: "Default", Outcome: auth.OutcomeLocked, Strategy: auth.StrategyCopy},
		{Browser: "Brave", Profile: "Default", Outcome: auth.OutcomeMissing, Strategy: auth.StrategyNone},
		{Browser: "Opera", Profile: "Default", Outcome: auth.OutcomeUnreadable, Strategy: auth.StrategyCopy,
			Err: errors.New(`open C:\Users\Juan Perez\AppData\Local\Opera\Network\Cookies: unexpected end`)},
	}
}

func lineWith(t *testing.T, out, needle string) string {
	t.Helper()
	for _, l := range strings.Split(out, "\n") {
		if strings.Contains(l, needle) {
			return l
		}
	}
	t.Fatalf("no line with %q in:\n%s", needle, out)
	return ""
}

func TestDoctor_TableAllOutcomes(t *testing.T) {
	var out, diag bytes.Buffer
	fd := &fakeDiagnoser{reports: allOutcomeReports()}
	if err := runDoctor(context.Background(), &out, &diag, "campusvirtual.ufro.cl", fd); err != nil {
		t.Fatalf("runDoctor: %v", err)
	}
	if fd.domain != "campusvirtual.ufro.cl" {
		t.Errorf("domain passed to Diagnose = %q", fd.domain)
	}
	if diag.Len() != 0 {
		t.Errorf("nothing expected on diag, got %q", diag.String())
	}
	s := out.String()
	t.Log("\n" + s)

	header := strings.SplitN(s, "\n", 2)[0]
	for _, want := range []string{"MooFetch v" + Version, runtime.GOOS + "/" + runtime.GOARCH, "campus: campusvirtual.ufro.cl"} {
		if !strings.Contains(header, want) {
			t.Errorf("header %q lacks %q", header, want)
		}
	}
	cols := lineWith(t, s, "Navegador")
	for _, c := range []string{"Navegador", "Perfil", "Resultado", "Estrategia", "Detalle"} {
		if !strings.Contains(cols, c) {
			t.Errorf("column %q missing", c)
		}
	}
	for needle, detail := range map[string]string{
		"encontrada": "cookie de sesión encontrada",
		"cifrado":    "el navegador protege sus cookies; usa Firefox o pega la cookie",
		"sin-cookie": "sin sesión en este navegador",
		"bloqueado":  "archivo en uso por el navegador",
		"ausente":    "no instalado",
		"ilegible":   "no se pudo leer: open Cookies",
	} {
		if l := lineWith(t, s, "| "+needle); !strings.Contains(l, detail) {
			t.Errorf("row %s lacks detail %q: %q", needle, detail, l)
		}
	}
	if !strings.Contains(s, "cookie encontrada en 1 de 6 almacenes") {
		t.Errorf("summary missing:\n%s", s)
	}
	if !strings.Contains(s, "Recomendación: todo en orden") {
		t.Errorf("a recommendation was expected:\n%s", s)
	}
}

func TestDoctor_RowsAreAligned(t *testing.T) {
	var out bytes.Buffer
	writeDoctorReport(&out, "campus.ejemplo.cl", allOutcomeReports())
	// Every table row has its first and second separators in the same column.
	var pos1, pos2 = -1, -1
	for _, l := range strings.Split(out.String(), "\n") {
		if strings.Count(l, "| ") < 4 || strings.HasPrefix(l, "MooFetch") {
			continue
		}
		i := strings.Index(l, "| ")
		j := i + 2 + strings.Index(l[i+2:], "| ")
		if pos1 < 0 {
			pos1, pos2 = len([]rune(l[:i])), len([]rune(l[:j]))
		}
		if len([]rune(l[:i])) != pos1 || len([]rune(l[:j])) != pos2 {
			t.Errorf("row not aligned: %q", l)
		}
	}
	if pos1 < 0 {
		t.Fatal("no table rows found")
	}
}

func TestDoctor_SortsByBrowserThenProfile(t *testing.T) {
	var out bytes.Buffer
	reports := allOutcomeReports()
	orig := append([]auth.StoreReport(nil), reports...)
	writeDoctorReport(&out, "x.cl", reports)
	s := out.String()
	idx := func(browser, profile string) int {
		for n, l := range strings.Split(s, "\n") {
			f := strings.Fields(l)
			if len(f) > 0 && f[0] == browser && strings.Contains(l, profile) {
				return n
			}
		}
		t.Fatalf("row %s/%s not found", browser, profile)
		return -1
	}
	order := []int{
		idx("Brave", "Default"), idx("Chrome", "Default"), idx("Chrome", "Profile 2"),
		idx("Edge", "Profile 1"), idx("Firefox", "default-release"), idx("Opera", "Default"),
	}
	for i := 1; i < len(order); i++ {
		if order[i] <= order[i-1] {
			t.Fatalf("rows out of order: %v", order)
		}
	}
	for i := range reports {
		if reports[i].Browser != orig[i].Browser || reports[i].Profile != orig[i].Profile {
			t.Fatal("input slice must not be reordered")
		}
	}
}

func TestSanitizeError(t *testing.T) {
	cookie := "MoodleSession=abcDEF123456 other"
	long := strings.Repeat("x ", 200)
	cases := []struct {
		name, in string
		want     []string
		deny     []string
	}{
		{"windows path with spaces", `open C:\Users\Juan Perez\AppData\Cookies: acceso denegado`,
			[]string{"open Cookies: acceso denegado"}, []string{"Juan", "Users", `\`}},
		{"unix path", `read /home/maria/.config/google-chrome/Default/Cookies: permission denied`,
			[]string{"read Cookies: permission denied"}, []string{"maria", "/home"}},
		{"quoted path", `unable to open database file "/tmp/x y/cookies.sqlite"`,
			[]string{`"cookies.sqlite"`}, []string{"/tmp"}},
		{"slash inside a word is kept", "lectura y/o escritura", []string{"y/o"}, nil},
		{"cookie pair hidden", "bad header " + cookie, []string{"bad header <oculto>"}, []string{"abcDEF123456"}},
		{"opaque token hidden", "value 0123456789abcdef0123456789abcdef0123 end", []string{"value <oculto> end"}, []string{"0123456789abcdef"}},
		{"whitespace collapsed", "a\n\tb   c", []string{"a b c"}, []string{"\n", "\t"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := sanitizeError(tc.in)
			for _, w := range tc.want {
				if !strings.Contains(got, w) {
					t.Errorf("sanitizeError(%q) = %q, want it to contain %q", tc.in, got, w)
				}
			}
			for _, d := range tc.deny {
				if strings.Contains(got, d) {
					t.Errorf("sanitizeError(%q) = %q, must not contain %q", tc.in, got, d)
				}
			}
		})
	}

	got := sanitizeError(long)
	if n := len([]rune(got)); n > maxDetailRunes || !strings.HasSuffix(got, "...") {
		t.Errorf("long error not trimmed to %d: %d %q", maxDetailRunes, n, got)
	}
}

func TestDoctor_NoCookieLeaksInOutput(t *testing.T) {
	reports := []auth.StoreReport{{
		Browser: "Chrome", Profile: "Default", Outcome: auth.OutcomeUnreadable, Strategy: auth.StrategyCopy,
		Err: errors.New("parse /home/u/Cookies: MoodleSessionabc=SECRETVALUE123 failed"),
	}}
	var out bytes.Buffer
	writeDoctorReport(&out, "campus.ejemplo.cl", reports)
	if strings.Contains(out.String(), "SECRETVALUE123") || strings.Contains(out.String(), "/home/u") {
		t.Errorf("sensitive content leaked:\n%s", out.String())
	}
}

func TestDoctor_OutcomeDetailEdgeCases(t *testing.T) {
	if got := outcomeDetail(auth.StoreReport{Outcome: auth.OutcomeUnreadable}); got != "no se pudo leer" {
		t.Errorf("ilegible without error = %q", got)
	}
	if got := outcomeDetail(auth.StoreReport{Outcome: "raro"}); got != "" {
		t.Errorf("unknown outcome detail = %q", got)
	}
	var out bytes.Buffer
	writeDoctorReport(&out, "x.cl", []auth.StoreReport{{Browser: "B", Outcome: "raro"}})
	if l := lineWith(t, out.String(), "raro"); !strings.Contains(l, "B ") || !strings.HasSuffix(l, "| -") {
		t.Errorf("empty cells must render as '-': %q", l)
	}
}

func TestDoctorAdvice(t *testing.T) {
	adv := func(os ...auth.Outcome) string {
		var rs []auth.StoreReport
		for _, o := range os {
			rs = append(rs, auth.StoreReport{Outcome: o})
		}
		return doctorAdvice(rs)
	}
	cases := []struct {
		name string
		got  string
		want []string
	}{
		{"found", adv(auth.OutcomeFound, auth.OutcomeLocked), []string{"todo en orden"}},
		{"locked", adv(auth.OutcomeLocked), []string{"cierra por completo"}},
		{"encrypted", adv(auth.OutcomeEncrypted), []string{"Firefox", "--cookie"}},
		{"locked and encrypted", adv(auth.OutcomeLocked, auth.OutcomeEncrypted), []string{"cierra por completo", "o bien", "Firefox"}},
		{"unreadable", adv(auth.OutcomeUnreadable), []string{"comparte esta tabla"}},
		{"only no cookie", adv(auth.OutcomeNoCookie, auth.OutcomeMissing), []string{"inicia sesión en el campus"}},
		{"nothing at all", adv(), []string{"ningún navegador compatible"}},
		{"only missing", adv(auth.OutcomeMissing), []string{"ningún navegador compatible"}},
	}
	for _, tc := range cases {
		for _, w := range tc.want {
			if !strings.Contains(tc.got, w) {
				t.Errorf("%s: %q lacks %q", tc.name, tc.got, w)
			}
		}
		if !strings.HasPrefix(tc.got, "Recomendación: ") {
			t.Errorf("%s: unexpected prefix %q", tc.name, tc.got)
		}
	}
}

func TestResolveDoctorDomain(t *testing.T) {
	t.Run("flag domain", func(t *testing.T) {
		if d, err := resolveDoctorDomain("", "https://Campus.Ejemplo.cl/x"); err != nil || d != "campus.ejemplo.cl" {
			t.Fatalf("got %q %v", d, err)
		}
	})
	t.Run("flag uni", func(t *testing.T) {
		if d, err := resolveDoctorDomain("UFRO", ""); err != nil || d != "campusvirtual.ufro.cl" {
			t.Fatalf("got %q %v", d, err)
		}
	})
	t.Run("single university default", func(t *testing.T) {
		if d, err := resolveDoctorDomain("", ""); err != nil || d != "campusvirtual.ufro.cl" {
			t.Fatalf("got %q %v", d, err)
		}
	})
	t.Run("ambiguous", func(t *testing.T) {
		old := universities
		universities = func() []auth.University {
			return []auth.University{{Key: "a", Domain: "a.cl"}, {Key: "b", Domain: "b.cl"}}
		}
		t.Cleanup(func() { universities = old })
		_, err := resolveDoctorDomain("", "")
		if err == nil || DetermineExitCode(context.Background(), err) != ExitUsageErr {
			t.Fatalf("want usage error, got %v", err)
		}
	})
	t.Run("empty catalog", func(t *testing.T) {
		old := universities
		universities = func() []auth.University { return nil }
		t.Cleanup(func() { universities = old })
		if _, err := resolveDoctorDomain("", ""); err == nil {
			t.Fatal("want error")
		}
	})
	t.Run("unknown university", func(t *testing.T) {
		_, err := resolveDoctorDomain("nope", "")
		if !errors.Is(err, auth.ErrUnknownUniversity) || DetermineExitCode(context.Background(), err) != ExitUsageErr {
			t.Fatalf("got %v", err)
		}
	})
	t.Run("invalid domain", func(t *testing.T) {
		_, err := resolveDoctorDomain("", "no es un host")
		if !errors.Is(err, auth.ErrInvalidDomain) {
			t.Fatalf("got %v", err)
		}
	})
}

func TestRunDoctor_NotADiagnoser(t *testing.T) {
	var out, diag bytes.Buffer
	if err := runDoctor(context.Background(), &out, &diag, "x.cl", fakeCookies{}); err != nil {
		t.Fatalf("must succeed (exit 0): %v", err)
	}
	if out.Len() != 0 {
		t.Errorf("stdout must stay empty, got %q", out.String())
	}
	if !strings.Contains(diag.String(), "no incluye diagnóstico") {
		t.Errorf("message = %q", diag.String())
	}
	diag.Reset()
	if err := runDoctor(context.Background(), &out, &diag, "x.cl", nil); err != nil || !strings.Contains(diag.String(), "no incluye diagnóstico") {
		t.Errorf("nil provider: %v %q", err, diag.String())
	}
}

func TestRunDoctor_Cancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var out, diag bytes.Buffer
	fd := &fakeDiagnoser{reports: allOutcomeReports()}
	err := runDoctor(ctx, &out, &diag, "x.cl", fd)
	if !errors.Is(err, context.Canceled) || DetermineExitCode(ctx, err) != ExitInterrupted {
		t.Fatalf("pre-canceled: %v", err)
	}
	if fd.domain != "" {
		t.Error("Diagnose must not run on a canceled context")
	}

	ctx2, cancel2 := context.WithCancel(context.Background())
	defer cancel2()
	fd2 := &fakeDiagnoser{reports: allOutcomeReports(), cancel: cancel2}
	err = runDoctor(ctx2, &out, &diag, "x.cl", fd2)
	if !errors.Is(err, context.Canceled) || out.Len() != 0 {
		t.Fatalf("canceled during diagnosis: err=%v out=%q", err, out.String())
	}
}

// resetDoctorHelp clears the --help flag that Cobra leaves set on doctorCmd
// after printing help (resetFlags only covers rootCmd and runCmd).
func resetDoctorHelp(t *testing.T) {
	t.Helper()
	if f := doctorCmd.Flags().Lookup("help"); f != nil {
		if err := f.Value.Set("false"); err != nil {
			t.Fatal(err)
		}
		f.Changed = false
	}
}

// runDoctorViaRoot executes "moofetch doctor" through Cobra, keeping stdout
// and stderr in separate buffers.
func runDoctorViaRoot(t *testing.T, p auth.CookieProvider, args ...string) (stdout, stderr string, err error) {
	t.Helper()
	resetFlags(t)
	resetDoctorHelp(t)
	oldP := cookieProvider
	cookieProvider = p
	t.Cleanup(func() { cookieProvider = oldP; flagUni, flagDomain = "", "" })
	var o, e bytes.Buffer
	rootCmd.SetOut(&o)
	rootCmd.SetErr(&e)
	rootCmd.SetArgs(append([]string{"doctor"}, args...))
	err = rootCmd.ExecuteContext(context.Background())
	rootCmd.SetOut(nil)
	rootCmd.SetErr(nil)
	rootCmd.SetArgs(nil)
	return o.String(), e.String(), err
}

func TestDoctorCommand_StdoutHasOnlyTheReport(t *testing.T) {
	fd := &fakeDiagnoser{reports: allOutcomeReports()}
	stdout, stderr, err := runDoctorViaRoot(t, fd, "--domain", "campus.ejemplo.cl")
	if err != nil {
		t.Fatalf("doctor: %v", err)
	}
	if fd.domain != "campus.ejemplo.cl" {
		t.Errorf("domain = %q", fd.domain)
	}
	if !strings.HasPrefix(stdout, "MooFetch v") || !strings.HasSuffix(stdout, ".\n") {
		t.Errorf("stdout must be exactly the report:\n%s", stdout)
	}
	if stderr != "" {
		t.Errorf("stderr must be empty on success: %q", stderr)
	}
	for _, l := range strings.Split(strings.TrimSpace(stdout), "\n") {
		for _, r := range l {
			if r > 0x024F { // beyond Latin Extended-B: no emojis or box drawing
				t.Fatalf("non-latin rune %q in %q", r, l)
			}
		}
	}
}

func TestDoctorCommand_DefaultsToSingleUniversity(t *testing.T) {
	fd := &fakeDiagnoser{}
	stdout, _, err := runDoctorViaRoot(t, fd)
	if err != nil {
		t.Fatalf("doctor: %v", err)
	}
	if fd.domain != "campusvirtual.ufro.cl" {
		t.Errorf("domain = %q", fd.domain)
	}
	if !strings.Contains(stdout, "0 de 0 almacenes") || !strings.Contains(stdout, "ningún navegador compatible") {
		t.Errorf("empty report summary missing:\n%s", stdout)
	}
}

func TestDoctorCommand_NotADiagnosertExitsZero(t *testing.T) {
	stdout, stderr, err := runDoctorViaRoot(t, fakeCookies{}, "--uni", "ufro")
	if err != nil {
		t.Fatalf("must exit 0: %v", err)
	}
	if stdout != "" || !strings.Contains(stderr, "no incluye diagnóstico") {
		t.Errorf("stdout=%q stderr=%q", stdout, stderr)
	}
}

func TestDoctorCommand_UsageErrors(t *testing.T) {
	_, _, err := runDoctorViaRoot(t, &fakeDiagnoser{}, "--uni", "inexistente")
	if err == nil || DetermineExitCode(context.Background(), err) != ExitUsageErr {
		t.Errorf("unknown university: %v", err)
	}
	_, _, err = runDoctorViaRoot(t, &fakeDiagnoser{}, "sobrante")
	if err == nil {
		t.Error("positional args must be rejected")
	}
}

func TestDoctorCommand_ListedInRootHelp(t *testing.T) {
	out := helpFor(t, "--help")
	if !strings.Contains(out, "doctor") || !strings.Contains(out, "Diagnostica por qué no se encuentra la cookie") {
		t.Errorf("root help must list doctor:\n%s", out)
	}
	t.Cleanup(func() { resetDoctorHelp(t) })
	sub := helpFor(t, "doctor", "--help")
	for _, want := range []string{"--uni CLAVE", "--domain HOST", "moofetch doctor --uni ufro", "muestra esta ayuda"} {
		if !strings.Contains(sub, want) {
			t.Errorf("doctor help lacks %q:\n%s", want, sub)
		}
	}
}

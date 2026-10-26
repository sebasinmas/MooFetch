package tui

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/sebasinmas/MooFetch/internal/domain"
)

const (
	canaryPass  = "S3cr3t-PW-canary"
	canaryToken = "TOKEN-canary-9f8e7d"
)

var (
	apiAuthKeys   = []string{keyEnter, keyEnter, keyEnter}             // splash, campus, method (API is the default)
	credKeys      = []string{"alumno", keyEnter, canaryPass, keyEnter} // username, password
	apiTestCourse = []Course{{10, "Programación I"}, {20, "Cálculo II"}, {30, "Física"}}
)

func apiTestContents(courseID int) []Section {
	switch courseID {
	case 10:
		return []Section{
			{ID: 1, Number: 0, Name: "General", Files: []FileItem{{Name: "Programa", URL: "https://campus.uni.cl/webservice/pluginfile.php/1/Programa.pdf", Size: 2516582}}},
			{ID: 2, Number: 1, Name: "Tema 1: Intro", Files: []FileItem{
				{Name: "Clase 1", URL: "https://campus.uni.cl/webservice/pluginfile.php/2/Clase1.pdf"},
				{Name: "Guía", URL: "https://campus.uni.cl/webservice/pluginfile.php/3/Guia.pdf", SubDir: "Ejercicios/Resueltos"},
			}},
			{ID: 3, Number: 2, Name: "Vacía"},
		}
	case 30:
		return []Section{{ID: 9, Number: 0, Name: "General", Files: []FileItem{{Name: "Apunte", URL: "https://campus.uni.cl/webservice/pluginfile.php/9/Apunte.pdf"}}}}
	}
	return nil
}

type apiRecorder struct {
	logins   []string // "campus|user"
	courses  int
	contents []int
}

// apiOptions returns options whose Login/Courses/Contents succeed, recording the calls.
func apiOptions(rec *apiRecorder, notice *strings.Builder) *AuthOptions {
	opts := testAuthOptions(func(context.Context, string) (string, error) { return "MoodleSession=zzz", nil })
	opts.Notice = notice
	opts.Scan = func(context.Context, string, string) ([]Resource, error) { return sampleResources[:1], nil }
	opts.Login = func(_ context.Context, campus, user, pass string) (string, error) {
		rec.logins = append(rec.logins, campus+"|"+user)
		if pass != canaryPass {
			return "", domain.ErrAuthenticationFailed
		}
		return canaryToken, nil
	}
	opts.Courses = func(_ context.Context, _, token string) ([]Course, error) {
		rec.courses++
		if token != canaryToken {
			return nil, domain.ErrAuthenticationFailed
		}
		return apiTestCourse, nil
	}
	opts.Contents = func(_ context.Context, _, _ string, id int) ([]Section, error) {
		rec.contents = append(rec.contents, id)
		return apiTestContents(id), nil
	}
	return opts
}

func TestAPIFlow_HappyPath(t *testing.T) {
	var rec apiRecorder
	var notice strings.Builder
	opts := apiOptions(&rec, &notice)
	scriptedInputs(t, apiAuthKeys, credKeys,
		[]string{keySpc, keyDown, keyDown, keySpc, keyEnter}, // courses 1 and 3
		[]string{keyEnter},         // course 10: all sections
		[]string{keySpc, keyEnter}, // course 10: untick "Programa"
		[]string{keyEnter},         // course 30: sections
		[]string{keyEnter},         // course 30: files
	)

	data, err := RunInteractiveFormWithAuth(context.Background(), opts, false)
	if err != nil {
		t.Fatal(err)
	}
	want := []Download{
		{URL: "https://campus.uni.cl/webservice/pluginfile.php/2/Clase1.pdf", SubDir: "Programación I/01 - Tema 1_ Intro/"},
		{URL: "https://campus.uni.cl/webservice/pluginfile.php/3/Guia.pdf", SubDir: "Programación I/01 - Tema 1_ Intro/Ejercicios/Resueltos/"},
		{URL: "https://campus.uni.cl/webservice/pluginfile.php/9/Apunte.pdf", SubDir: "Física/00 - General/"},
	}
	if data.Campus == "" {
		t.Error("the API route must report the campus the token belongs to")
	}
	if !reflect.DeepEqual(data.Items, want) {
		t.Errorf("Items = %+v, want %+v", data.Items, want)
	}
	wantURLs := []string{want[0].URL, want[1].URL, want[2].URL}
	if !reflect.DeepEqual(data.URLs, wantURLs) {
		t.Errorf("URLs = %v, want %v", data.URLs, wantURLs)
	}
	if data.Token != canaryToken || data.Cookie != "" {
		t.Errorf("token/cookie = %q/%q", data.Token, data.Cookie)
	}
	if !reflect.DeepEqual(rec.logins, []string{"campus.uni.cl|alumno"}) {
		t.Errorf("logins = %v", rec.logins)
	}
	if !reflect.DeepEqual(rec.contents, []int{10, 30}) {
		t.Errorf("contents must be requested only for chosen courses, in order: %v", rec.contents)
	}
	if rec.courses != 1 {
		t.Errorf("courses loaded %d times", rec.courses)
	}
}

func TestAPIFlow_CoursesStartUnmarkedAndNeedOne(t *testing.T) {
	var rec apiRecorder
	var notice strings.Builder
	opts := apiOptions(&rec, &notice)
	// Enter with nothing marked is rejected (stays on the screen); then mark
	// the 2nd (no files: the wizard returns to the course list, aborted here).
	scriptedInputs(t, apiAuthKeys, credKeys,
		[]string{keyEnter, keyDown, keySpc, keyEnter},
		[]string{keyCtrlC},
	)
	if _, err := RunInteractiveFormWithAuth(context.Background(), opts, false); !errors.Is(err, ErrFormAborted) {
		t.Fatalf("want ErrFormAborted, got %v", err)
	}
	if !reflect.DeepEqual(rec.contents, []int{20}) {
		t.Errorf("only the 2nd course must be requested, got %v", rec.contents)
	}
}

func TestAPIFlow_CourseWithoutFilesIsReportedAndSkipped(t *testing.T) {
	var rec apiRecorder
	var notice strings.Builder
	opts := apiOptions(&rec, &notice)
	// courses 2 (no files) and 3: the first is reported and skipped, the second proceeds.
	scriptedInputs(t, apiAuthKeys, credKeys,
		[]string{keyDown, keySpc, keyDown, keySpc, keyEnter},
		[]string{keyEnter}, []string{keyEnter},
	)
	data, err := RunInteractiveFormWithAuth(context.Background(), opts, false)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(notice.String(), "«Cálculo II» no tiene archivos descargables.") {
		t.Errorf("notice = %q", notice.String())
	}
	if len(data.Items) != 1 || data.Items[0].SubDir != "Física/00 - General/" {
		t.Errorf("Items = %+v", data.Items)
	}
}

func TestAPIFlow_NoFilesAnywhereGoesBackToCourses(t *testing.T) {
	var rec apiRecorder
	var notice strings.Builder
	opts := apiOptions(&rec, &notice)
	scriptedInputs(t, apiAuthKeys, credKeys,
		[]string{keyDown, keySpc, keyEnter}, // course 20, empty
		[]string{keyCtrlC},                  // back on the course list: abort
	)
	if _, err := RunInteractiveFormWithAuth(context.Background(), opts, false); !errors.Is(err, ErrFormAborted) {
		t.Fatalf("want ErrFormAborted, got %v", err)
	}
	if !strings.Contains(notice.String(), "No hay archivos para descargar") {
		t.Errorf("notice = %q", notice.String())
	}
	if rec.courses != 2 {
		t.Errorf("the course list must be shown again, courses = %d", rec.courses)
	}
}

func TestAPIFlow_UnmarkEverythingSkipsTheCourse(t *testing.T) {
	var rec apiRecorder
	var notice strings.Builder
	opts := apiOptions(&rec, &notice)
	// course 10: untick both sections; course 30 normal
	scriptedInputs(t, apiAuthKeys, credKeys,
		[]string{keySpc, keyDown, keyDown, keySpc, keyEnter},
		[]string{keySpc, keyDown, keySpc, keyEnter}, // sections of 10: untick both
		[]string{keyEnter}, []string{keyEnter},
	)
	data, err := RunInteractiveFormWithAuth(context.Background(), opts, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(data.Items) != 1 || !strings.HasPrefix(data.Items[0].SubDir, "Física/") {
		t.Errorf("Items = %+v", data.Items)
	}
}

func TestAPIFlow_InvalidCredentialsRetry(t *testing.T) {
	var rec apiRecorder
	var notice strings.Builder
	opts := apiOptions(&rec, &notice)
	scriptedInputs(t, apiAuthKeys,
		[]string{"alumno", keyEnter, "mala", keyEnter}, // wrong password
		[]string{keyEnter},                       // menu: retry
		[]string{keyEnter, canaryPass, keyEnter}, // user prefilled, right password
		[]string{keySpc, keyEnter},               // course 10
		[]string{keyEnter}, []string{keyEnter},
	)
	data, err := RunInteractiveFormWithAuth(context.Background(), opts, false)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(notice.String(), "Usuario o contraseña incorrectos") {
		t.Errorf("notice = %q", notice.String())
	}
	if !reflect.DeepEqual(rec.logins, []string{"campus.uni.cl|alumno", "campus.uni.cl|alumno"}) {
		t.Errorf("logins = %v (the user name must be prefilled on retry)", rec.logins)
	}
	if data.Token != canaryToken || len(data.Items) != 3 {
		t.Errorf("data = %+v", data)
	}
}

func TestAPIFlow_InvalidCredentialsToCookie(t *testing.T) {
	var rec apiRecorder
	var notice strings.Builder
	opts := apiOptions(&rec, &notice)
	scriptedInputs(t, apiAuthKeys,
		[]string{"alumno", keyEnter, "mala", keyEnter},
		[]string{keyDown, keyEnter}, // menu: use the browser cookie
		[]string{keyEnter},          // consent
		pageKeys,
		[]string{keyEnter}, // pick everything
	)
	data, err := RunInteractiveFormWithAuth(context.Background(), opts, false)
	if err != nil {
		t.Fatal(err)
	}
	if data.Cookie != "MoodleSession=zzz" || data.Token != "" || len(data.URLs) != 1 || len(data.Items) != 0 {
		t.Errorf("cookie route data = %+v", data)
	}
}

func TestAPIFlow_InvalidCredentialsToManualLinks(t *testing.T) {
	var rec apiRecorder
	var notice strings.Builder
	opts := apiOptions(&rec, &notice)
	scriptedInputs(t, apiAuthKeys,
		[]string{"alumno", keyEnter, "mala", keyEnter},
		[]string{keyDown, keyDown, keyEnter},                                          // menu: paste links by hand
		[]string{"abc123", keyEnter, "https://campus.uni.cl/a.pdf", keyEsc, keyEnter}, // cookie + links form
	)
	data, err := RunInteractiveFormWithAuth(context.Background(), opts, false)
	if err != nil {
		t.Fatal(err)
	}
	if data.Cookie != "MoodleSession=abc123" || !reflect.DeepEqual(data.URLs, []string{"https://campus.uni.cl/a.pdf"}) {
		t.Errorf("manual data = %+v", data)
	}
}

func TestAPIFlow_APIUnavailableFallsBackToCookie(t *testing.T) {
	var rec apiRecorder
	var notice strings.Builder
	opts := apiOptions(&rec, &notice)
	opts.Login = func(context.Context, string, string, string) (string, error) { return "", ErrAPIUnavailable }
	scriptedInputs(t, apiAuthKeys, credKeys,
		[]string{keyEnter}, // consent
		pageKeys,
		[]string{keyEnter},
	)
	data, err := RunInteractiveFormWithAuth(context.Background(), opts, false)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(notice.String(), "no ofrece") {
		t.Errorf("notice = %q", notice.String())
	}
	if data.Cookie != "MoodleSession=zzz" {
		t.Errorf("cookie = %q", data.Cookie)
	}
}

func TestAPIFlow_APIUnavailableWithoutDetectGoesToManualCookie(t *testing.T) {
	var rec apiRecorder
	var notice strings.Builder
	opts := apiOptions(&rec, &notice)
	opts.Detect = nil
	opts.Scan = nil
	opts.Login = func(context.Context, string, string, string) (string, error) { return "", ErrAPIUnavailable }
	scriptedInputs(t, apiAuthKeys, credKeys,
		[]string{"abc", keyEnter, "https://campus.uni.cl/a.pdf", keyEsc, keyEnter},
	)
	data, err := RunInteractiveFormWithAuth(context.Background(), opts, false)
	if err != nil {
		t.Fatal(err)
	}
	if data.Cookie != "MoodleSession=abc" {
		t.Errorf("cookie = %q", data.Cookie)
	}
}

func TestAPIFlow_UnexpectedLoginErrorOffersMenu(t *testing.T) {
	var rec apiRecorder
	var notice strings.Builder
	opts := apiOptions(&rec, &notice)
	opts.Login = func(context.Context, string, string, string) (string, error) {
		return "", errors.New("sin conexión")
	}
	scriptedInputs(t, apiAuthKeys, credKeys, []string{keyCtrlC})
	if _, err := RunInteractiveFormWithAuth(context.Background(), opts, false); !errors.Is(err, ErrFormAborted) {
		t.Fatalf("want ErrFormAborted, got %v", err)
	}
	if !strings.Contains(notice.String(), "No se pudo iniciar sesión: sin conexión") {
		t.Errorf("notice = %q", notice.String())
	}
}

func TestAPIFlow_EmptyTokenIsAnError(t *testing.T) {
	var rec apiRecorder
	var notice strings.Builder
	opts := apiOptions(&rec, &notice)
	opts.Login = func(context.Context, string, string, string) (string, error) { return "", nil }
	scriptedInputs(t, apiAuthKeys, credKeys, []string{keyCtrlC})
	if _, err := RunInteractiveFormWithAuth(context.Background(), opts, false); !errors.Is(err, ErrFormAborted) {
		t.Fatalf("want ErrFormAborted, got %v", err)
	}
	if !strings.Contains(notice.String(), "no entregó un token") {
		t.Errorf("notice = %q", notice.String())
	}
}

func TestAPIFlow_ExpiredTokenInCoursesGoesBackToCredentials(t *testing.T) {
	var rec apiRecorder
	var notice strings.Builder
	opts := apiOptions(&rec, &notice)
	base := opts.Courses
	opts.Courses = func(c context.Context, campus, token string) ([]Course, error) {
		if rec.courses == 0 {
			rec.courses++
			return nil, fmt.Errorf("token vencido: %w", domain.ErrAuthenticationFailed)
		}
		return base(c, campus, token)
	}
	scriptedInputs(t, apiAuthKeys, credKeys,
		credKeys, // asked again
		[]string{keySpc, keyEnter},
		[]string{keyEnter}, []string{keyEnter},
	)
	data, err := RunInteractiveFormWithAuth(context.Background(), opts, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(rec.logins) != 2 {
		t.Errorf("logins = %v, want two", rec.logins)
	}
	if !strings.Contains(notice.String(), "Tu sesión expiró") {
		t.Errorf("notice = %q", notice.String())
	}
	if len(data.Items) != 3 {
		t.Errorf("Items = %+v", data.Items)
	}
}

func TestAPIFlow_ExpiredTokenInContentsGoesBackToCredentials(t *testing.T) {
	var rec apiRecorder
	var notice strings.Builder
	opts := apiOptions(&rec, &notice)
	expired := true
	opts.Contents = func(_ context.Context, _, _ string, id int) ([]Section, error) {
		rec.contents = append(rec.contents, id)
		if expired {
			expired = false
			return nil, domain.ErrAuthenticationFailed
		}
		return apiTestContents(id), nil
	}
	scriptedInputs(t, apiAuthKeys, credKeys,
		[]string{keySpc, keyEnter},
		credKeys,
		[]string{keySpc, keyEnter},
		[]string{keyEnter}, []string{keyEnter},
	)
	data, err := RunInteractiveFormWithAuth(context.Background(), opts, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(rec.logins) != 2 || len(data.Items) != 3 {
		t.Errorf("logins = %v, items = %+v", rec.logins, data.Items)
	}
}

func TestAPIFlow_ContentsErrorSkipsThatCourse(t *testing.T) {
	var rec apiRecorder
	var notice strings.Builder
	opts := apiOptions(&rec, &notice)
	opts.Contents = func(_ context.Context, _, token string, id int) ([]Section, error) {
		if id == 10 {
			return nil, errors.New("falló " + token)
		}
		return apiTestContents(id), nil
	}
	scriptedInputs(t, apiAuthKeys, credKeys,
		[]string{keySpc, keyDown, keyDown, keySpc, keyEnter},
		[]string{keyEnter}, []string{keyEnter},
	)
	data, err := RunInteractiveFormWithAuth(context.Background(), opts, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(data.Items) != 1 {
		t.Errorf("Items = %+v", data.Items)
	}
	if !strings.Contains(notice.String(), "No se pudo leer «Programación I»") {
		t.Errorf("notice = %q", notice.String())
	}
}

func TestAPIFlow_ContentsAPIUnavailableFallsBack(t *testing.T) {
	var rec apiRecorder
	var notice strings.Builder
	opts := apiOptions(&rec, &notice)
	opts.Contents = func(context.Context, string, string, int) ([]Section, error) { return nil, ErrAPIUnavailable }
	scriptedInputs(t, apiAuthKeys, credKeys,
		[]string{keySpc, keyEnter},
		[]string{keyEnter}, // consent
		pageKeys, []string{keyEnter},
	)
	data, err := RunInteractiveFormWithAuth(context.Background(), opts, false)
	if err != nil {
		t.Fatal(err)
	}
	if data.Cookie == "" || data.Token != "" {
		t.Errorf("data = %+v", data)
	}
}

func TestAPIFlow_CoursesErrorsAndEmptyListOfferTheMenu(t *testing.T) {
	tests := map[string]struct {
		courses func() ([]Course, error)
		notice  string
	}{
		"error": {func() ([]Course, error) { return nil, errors.New("503") }, "No se pudieron leer tus cursos: 503"},
		"empty": {func() ([]Course, error) { return nil, nil }, "ningún curso"},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			var rec apiRecorder
			var notice strings.Builder
			opts := apiOptions(&rec, &notice)
			opts.Courses = func(context.Context, string, string) ([]Course, error) { return tc.courses() }
			scriptedInputs(t, apiAuthKeys, credKeys,
				[]string{keyDown, keyDown, keyEnter}, // menu: manual links
				[]string{keyCtrlC},                   // the manual form is aborted
			)
			if _, err := RunInteractiveFormWithAuth(context.Background(), opts, false); !errors.Is(err, ErrFormAborted) {
				t.Fatalf("want ErrFormAborted, got %v", err)
			}
			if !strings.Contains(notice.String(), tc.notice) {
				t.Errorf("notice = %q", notice.String())
			}
		})
	}
}

func TestAPIFlow_CoursesRetryReloadsWithoutNewLogin(t *testing.T) {
	var rec apiRecorder
	var notice strings.Builder
	opts := apiOptions(&rec, &notice)
	base := opts.Courses
	calls := 0
	opts.Courses = func(c context.Context, campus, token string) ([]Course, error) {
		calls++
		if calls == 1 {
			return nil, errors.New("timeout")
		}
		return base(c, campus, token)
	}
	scriptedInputs(t, apiAuthKeys, credKeys,
		[]string{keyEnter}, // menu: retry
		[]string{keyCtrlC},
	)
	if _, err := RunInteractiveFormWithAuth(context.Background(), opts, false); !errors.Is(err, ErrFormAborted) {
		t.Fatalf("want ErrFormAborted, got %v", err)
	}
	if calls != 2 || len(rec.logins) != 1 {
		t.Errorf("calls = %d, logins = %v", calls, rec.logins)
	}
}

func TestAPIFlow_CookieIsDefaultWhenAsked(t *testing.T) {
	var rec apiRecorder
	var notice strings.Builder
	opts := apiOptions(&rec, &notice)
	opts.DefaultAuth = "cookie"
	// splash, campus, method (cookie preselected), consent -> cookie flow
	scriptedInputs(t, []string{keyEnter, keyEnter, keyEnter, keyEnter}, pageKeys, []string{keyEnter})
	data, err := RunInteractiveFormWithAuth(context.Background(), opts, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(rec.logins) != 0 || data.Cookie != "MoodleSession=zzz" {
		t.Errorf("Login must not run: %v, cookie %q", rec.logins, data.Cookie)
	}
}

func TestAPIFlow_ChooseCookieOnTheMethodScreen(t *testing.T) {
	var rec apiRecorder
	var notice strings.Builder
	opts := apiOptions(&rec, &notice)
	scriptedInputs(t, []string{keyEnter, keyEnter, keyDown, keyEnter, keyEnter}, pageKeys, []string{keyEnter})
	data, err := RunInteractiveFormWithAuth(context.Background(), opts, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(rec.logins) != 0 || data.Cookie != "MoodleSession=zzz" {
		t.Errorf("Login must not run: %v, cookie %q", rec.logins, data.Cookie)
	}
}

func TestAPIFlow_CustomDomainReachesLogin(t *testing.T) {
	var rec apiRecorder
	var notice strings.Builder
	opts := apiOptions(&rec, &notice)
	opts.Universities = nil
	opts.ValidateDomain = func(s string) (string, error) {
		if s == "" {
			return "", errors.New("vacío")
		}
		return s, nil
	}
	// splash; "Otra…" is the only option; the typed domain reaches Login.
	scriptedInputs(t, []string{keyEnter, keyEnter, "campus.otro.cl", keyEnter, keyEnter}, credKeys,
		[]string{keySpc, keyEnter}, []string{keyEnter}, []string{keyEnter})
	data, err := RunInteractiveFormWithAuth(context.Background(), opts, false)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(rec.logins, []string{"campus.otro.cl|alumno"}) || len(data.Items) != 3 {
		t.Errorf("logins = %v items = %+v", rec.logins, data.Items)
	}
}

func TestRunAPIFlow_EmptyCampusFallsBackToCookie(t *testing.T) {
	var notice strings.Builder
	opts := &AuthOptions{Login: func(context.Context, string, string, string) (string, error) {
		t.Error("Login must not run without a campus")
		return "", nil
	}}
	data, out, err := runAPIFlow(context.Background(), opts, "", &notice, false)
	if data != nil || out != apiCookie || err != nil {
		t.Fatalf("got %v %v %v", data, out, err)
	}
	if !strings.Contains(notice.String(), "dominio") {
		t.Errorf("notice = %q", notice.String())
	}
}

func TestAPIFlow_AbortPaths(t *testing.T) {
	tests := map[string][][]string{
		"on the method screen": {{keyEnter, keyEnter, keyCtrlC}},
		"on credentials":       {apiAuthKeys, {keyCtrlC}},
		"on the failure menu":  {apiAuthKeys, {"alumno", keyEnter, "mala", keyEnter}, {keyCtrlC}},
		"on courses":           {apiAuthKeys, credKeys, {keyCtrlC}},
		"on sections":          {apiAuthKeys, credKeys, {keySpc, keyEnter}, {keyCtrlC}},
		"on files":             {apiAuthKeys, credKeys, {keySpc, keyEnter}, {keyEnter}, {keyCtrlC}},
	}
	for name, scripts := range tests {
		t.Run(name, func(t *testing.T) {
			var rec apiRecorder
			var notice strings.Builder
			opts := apiOptions(&rec, &notice)
			scriptedInputs(t, scripts...)
			if _, err := RunInteractiveFormWithAuth(context.Background(), opts, false); !errors.Is(err, ErrFormAborted) {
				t.Fatalf("want ErrFormAborted, got %v", err)
			}
		})
	}
}

func TestAPIFlow_ConsentAbortAfterFallback(t *testing.T) {
	var rec apiRecorder
	var notice strings.Builder
	opts := apiOptions(&rec, &notice)
	opts.Login = func(context.Context, string, string, string) (string, error) { return "", ErrAPIUnavailable }
	scriptedInputs(t, apiAuthKeys, credKeys, []string{keyCtrlC})
	if _, err := RunInteractiveFormWithAuth(context.Background(), opts, false); !errors.Is(err, ErrFormAborted) {
		t.Fatalf("want ErrFormAborted, got %v", err)
	}
}

func TestAPIFlow_ContextCancelledDuringLogin(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	var rec apiRecorder
	var notice strings.Builder
	opts := apiOptions(&rec, &notice)
	opts.Login = func(context.Context, string, string, string) (string, error) {
		cancel()
		return "", context.Canceled
	}
	scriptedInputs(t, apiAuthKeys, credKeys)
	if _, err := RunInteractiveFormWithAuth(ctx, opts, false); !errors.Is(err, context.Canceled) {
		t.Fatalf("want context.Canceled, got %v", err)
	}
}

func TestAPIFlow_ContextCancelledDuringCoursesAndContents(t *testing.T) {
	for name, patch := range map[string]func(*AuthOptions, context.CancelFunc){
		"courses": func(o *AuthOptions, cancel context.CancelFunc) {
			o.Courses = func(context.Context, string, string) ([]Course, error) { cancel(); return nil, context.Canceled }
		},
		"contents": func(o *AuthOptions, cancel context.CancelFunc) {
			o.Contents = func(context.Context, string, string, int) ([]Section, error) { cancel(); return nil, context.Canceled }
		},
	} {
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			var rec apiRecorder
			var notice strings.Builder
			opts := apiOptions(&rec, &notice)
			patch(opts, cancel)
			scriptedInputs(t, apiAuthKeys, credKeys, []string{keySpc, keyEnter})
			if _, err := RunInteractiveFormWithAuth(ctx, opts, false); !errors.Is(err, context.Canceled) {
				t.Fatalf("want context.Canceled, got %v", err)
			}
		})
	}
}

// The password and the token must never reach the notices, the errors or the
// data that later becomes download URLs.
func TestAPIFlow_SecretsNeverLeak(t *testing.T) {
	t.Run("happy path", func(t *testing.T) {
		var rec apiRecorder
		var notice strings.Builder
		opts := apiOptions(&rec, &notice)
		scriptedInputs(t, apiAuthKeys, credKeys,
			[]string{keySpc, keyEnter}, []string{keyEnter}, []string{keyEnter})
		data, err := RunInteractiveFormWithAuth(context.Background(), opts, false)
		if err != nil {
			t.Fatal(err)
		}
		assertNoSecrets(t, notice.String())
		for _, u := range data.URLs {
			assertNoSecrets(t, u)
		}
		for _, it := range data.Items {
			assertNoSecrets(t, it.URL)
			assertNoSecrets(t, it.SubDir)
		}
		assertNoSecrets(t, data.Cookie)
	})

	t.Run("leaky errors are scrubbed", func(t *testing.T) {
		var rec apiRecorder
		var notice strings.Builder
		opts := apiOptions(&rec, &notice)
		opts.Login = func(_ context.Context, _, _, pass string) (string, error) {
			return "", errors.New("falló con clave " + pass)
		}
		scriptedInputs(t, apiAuthKeys, credKeys, []string{keyCtrlC})
		_, err := RunInteractiveFormWithAuth(context.Background(), opts, false)
		if !errors.Is(err, ErrFormAborted) {
			t.Fatalf("want ErrFormAborted, got %v", err)
		}
		assertNoSecrets(t, notice.String())
		assertNoSecrets(t, err.Error())
		if !strings.Contains(notice.String(), "***") {
			t.Errorf("the secret should have been masked: %q", notice.String())
		}
	})

	t.Run("token in courses and contents errors", func(t *testing.T) {
		var rec apiRecorder
		var notice strings.Builder
		opts := apiOptions(&rec, &notice)
		opts.Contents = func(_ context.Context, _, token string, _ int) ([]Section, error) {
			return nil, errors.New("GET /x?token=" + token + " falló")
		}
		opts.Courses = func(_ context.Context, _, token string) ([]Course, error) {
			if rec.courses == 0 {
				rec.courses++
				return nil, errors.New("GET /y?token=" + token + " falló")
			}
			return apiTestCourse, nil
		}
		scriptedInputs(t, apiAuthKeys, credKeys,
			[]string{keyEnter}, // courses failed: retry
			[]string{keySpc, keyEnter},
			[]string{keyCtrlC}, // contents failed -> no files -> back to the course list: abort
		)
		_, _ = RunInteractiveFormWithAuth(context.Background(), opts, false)
		assertNoSecrets(t, notice.String())
		if !strings.Contains(notice.String(), "***") {
			t.Errorf("notice = %q", notice.String())
		}
	})
}

func assertNoSecrets(t *testing.T, s string) {
	t.Helper()
	for _, secret := range []string{canaryPass, canaryToken} {
		if strings.Contains(s, secret) {
			t.Errorf("a secret leaked into %q", s)
		}
	}
}

func TestScrub(t *testing.T) {
	got := scrub(errors.New("a pw=hunter2 t=abc"), "hunter2", "", "abc")
	if got != "a pw=*** t=***" {
		t.Errorf("scrub = %q", got)
	}
}

func TestFileLabels(t *testing.T) {
	got := fileLabels([]FileItem{
		{Name: "  Clase   1 ", URL: "https://c/a", Size: 2516582},
		{Name: "Clase 1", URL: "https://c/b"},
		{Name: "Guía", SubDir: "/Ejercicios/Resueltos/", Size: 512},
		{Name: "", URL: "https://c/webservice/pluginfile.php/1/Gu%C3%ADa%201.pdf"},
		{Name: "", URL: "https://c/"},
	})
	want := []string{
		"Clase 1 (2.4 MB)",
		"Clase 1 (2)",
		"Guía — Ejercicios/Resueltos (512 B)",
		"Guía 1.pdf",
		"Archivo 5",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("labels = %q, want %q", got, want)
	}
	for _, l := range got {
		if strings.Contains(l, "http") {
			t.Errorf("label %q must not show a link", l)
		}
	}
}

func TestDownloadSubDir(t *testing.T) {
	tests := []struct {
		name string
		c    Course
		s    Section
		f    FileItem
		want string
	}{
		{"resource", Course{1, "Cálculo II"}, Section{Number: 3, Name: "Límites"}, FileItem{}, "Cálculo II/03 - Límites/"},
		{"folder segments", Course{1, "C"}, Section{Number: 0, Name: "General"}, FileItem{SubDir: "Guías/Año 1"}, "C/00 - General/Guías/Año 1/"},
		{"unsafe names", Course{1, "A/B: C?"}, Section{Number: 12, Name: "x*y"}, FileItem{SubDir: `..\../con/./a|b`}, "A_B_ C_/12 - x_y/_con/a_b/"},
		{"empty names", Course{7, ""}, Section{Number: 2}, FileItem{}, "Curso 7/02 - Sección 2/"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := downloadSubDir(tc.c, tc.s, tc.f)
			if got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
			if strings.Contains(got, "..") || strings.Contains(got, `\`) {
				t.Errorf("%q must not escape its folder", got)
			}
		})
	}
}

func TestSpinWhile(t *testing.T) {
	scriptedInputs(t)
	got, err := spinWhile(context.Background(), "x", func(context.Context) (int, error) { return 7, nil })
	if got != 7 || err != nil {
		t.Fatalf("got %d %v", got, err)
	}
	boom := errors.New("boom")
	if _, err := spinWhile(context.Background(), "x", func(context.Context) (int, error) { return 0, boom }); !errors.Is(err, boom) {
		t.Fatalf("err = %v", err)
	}
}

func TestBusyModel(t *testing.T) {
	m := &busy{title: "Cargando", cancel: func() {}}
	if m.View() == "" {
		t.Fatal("must render while working")
	}
	m.Update(busyDoneMsg{})
	if m.View() != "" {
		t.Error("a finished spinner must leave nothing on screen")
	}
	cancelled := false
	m2 := &busy{title: "Cargando", cancel: func() { cancelled = true }}
	m2.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	if !m2.aborted || !cancelled || m2.View() != "" {
		t.Errorf("ctrl+c must abort: aborted=%v cancelled=%v", m2.aborted, cancelled)
	}
	if m2.Init() == nil {
		t.Error("Init must start the spinner")
	}
	if _, cmd := m.Update(struct{}{}); cmd != nil {
		t.Error("unknown messages are ignored")
	}
}

func TestOutcomeOf(t *testing.T) {
	if outcomeOf(choiceManual) != apiManual || outcomeOf(choiceCookie) != apiCookie {
		t.Error("menu choices map to their outcome")
	}
}

func TestBuildAuthGroups_MethodScreenOnlyWithLogin(t *testing.T) {
	var choice, custom, method string
	consent := true
	opts := &AuthOptions{Universities: []University{{Name: "U", Domain: "u.cl"}}}
	if g := buildAuthGroups(opts, "u.cl", &choice, &custom, &method, &consent); len(g) != 1 {
		t.Errorf("without Login: consent only, got %d groups", len(g))
	}
	opts.Login = func(context.Context, string, string, string) (string, error) { return "", nil }
	if g := buildAuthGroups(opts, "u.cl", &choice, &custom, &method, &consent); len(g) != 2 {
		t.Errorf("with Login: method + consent, got %d groups", len(g))
	}
	if g := buildAuthGroups(opts, "", &choice, &custom, &method, &consent); len(g) != 4 {
		t.Errorf("with Login and no preset domain: 4 groups, got %d", len(g))
	}
}

func TestPickFromCourse_WarnsAboutActivitiesWithFiles(t *testing.T) {
	var notice strings.Builder
	got, err := pickFromCourse(Course{ID: 1, Name: "Cálculo"}, []Section{
		{Number: 1, OtherModules: 2},
		{Number: 2, OtherModules: 1},
	}, &notice, false)
	if err != nil || got != nil {
		t.Fatalf("got %v, %v", got, err)
	}
	out := notice.String()
	if !strings.Contains(out, "3 actividades") || !strings.Contains(out, "no tiene archivos descargables") {
		t.Errorf("notice = %q", out)
	}
}

package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/sebasinmas/MooFetch/internal/domain"
	"github.com/sebasinmas/MooFetch/internal/plugins/demo"
	"github.com/sebasinmas/MooFetch/internal/plugins/moodle"
	"github.com/sebasinmas/MooFetch/internal/tui"
)

const (
	testPass  = "S3cr3t-PW-canary"
	testToken = "TOKEN-canary-9f8e7d"
)

// defaultNewMobileAPI is the factory as shipped, captured before any test
// replaces it.
var defaultNewMobileAPI = newMobileAPI

// fakeAPI is a moodle.MobileAPI that records what it receives.
type fakeAPI struct {
	loginErr, siteErr, coursesErr, contentsErr error
	calls                                      []string
	courses                                    []moodle.APICourse
	sections                                   []moodle.APISection
}

func (f *fakeAPI) Login(_ context.Context, user, pass string) (string, error) {
	f.calls = append(f.calls, "login:"+user)
	if f.loginErr != nil {
		return "", f.loginErr
	}
	if pass != testPass {
		return "", moodle.ErrInvalidCredentials
	}
	return testToken, nil
}

func (f *fakeAPI) SiteInfo(_ context.Context, token string) (moodle.SiteInfo, error) {
	f.calls = append(f.calls, "siteinfo:"+token)
	return moodle.SiteInfo{UserID: 42}, f.siteErr
}

func (f *fakeAPI) Courses(_ context.Context, token string, userID int) ([]moodle.APICourse, error) {
	f.calls = append(f.calls, fmt.Sprintf("courses:%s:%d", token, userID))
	return f.courses, f.coursesErr
}

func (f *fakeAPI) Contents(_ context.Context, token string, courseID int) ([]moodle.APISection, error) {
	f.calls = append(f.calls, fmt.Sprintf("contents:%s:%d", token, courseID))
	return f.sections, f.contentsErr
}

// withFakeAPI installs a fake factory and returns the URLs it was asked for.
func withFakeAPI(t *testing.T, api *fakeAPI) *[]string {
	t.Helper()
	var sites []string
	old := newMobileAPI
	newMobileAPI = func(site string) (moodle.MobileAPI, error) {
		sites = append(sites, site)
		return api, nil
	}
	t.Cleanup(func() { newMobileAPI = old })
	return &sites
}

func TestAPIAdapter_LoginBuildsHTTPSSiteAndCachesTheClient(t *testing.T) {
	api := &fakeAPI{}
	sites := withFakeAPI(t, api)
	a := newAPIAdapter()

	token, err := a.Login(context.Background(), "campus.uni.cl", "alumno", testPass)
	if err != nil || token != testToken {
		t.Fatalf("Login = %q, %v", token, err)
	}
	if _, err := a.Login(context.Background(), "campus.uni.cl", "alumno", testPass); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Login(context.Background(), "otro.uni.cl", "alumno", testPass); err != nil {
		t.Fatal(err)
	}
	if want := []string{"https://campus.uni.cl", "https://otro.uni.cl"}; !reflect.DeepEqual(*sites, want) {
		t.Errorf("sites = %v, want one client per campus %v", *sites, want)
	}
}

func TestAPIAdapter_LoginErrors(t *testing.T) {
	a := newAPIAdapter()
	withFakeAPI(t, &fakeAPI{})
	if _, err := a.Login(context.Background(), "c.cl", "u", "mala"); !errors.Is(err, domain.ErrAuthenticationFailed) {
		t.Errorf("rejected credentials must keep wrapping ErrAuthenticationFailed, got %v", err)
	}

	withFakeAPI(t, &fakeAPI{loginErr: moodle.ErrAPIUnavailable})
	if _, err := newAPIAdapter().Login(context.Background(), "c.cl", "u", testPass); !errors.Is(err, tui.ErrAPIUnavailable) {
		t.Errorf("want tui.ErrAPIUnavailable, got %v", err)
	}

	// the default factory is not wired in this branch
	newMobileAPI = defaultNewMobileAPI
	if _, err := newAPIAdapter().Login(context.Background(), "c.cl", "u", testPass); err == nil || strings.Contains(err.Error(), testPass) {
		t.Errorf("default factory error = %v", err)
	}
	newMobileAPI = func(string) (moodle.MobileAPI, error) { return nil, errors.New("fábrica") }
	for name, call := range map[string]func() error{
		"Courses": func() error { _, err := newAPIAdapter().Courses(context.Background(), "c.cl", testToken); return err },
		"Contents": func() error {
			_, err := newAPIAdapter().Contents(context.Background(), "c.cl", testToken, 1)
			return err
		},
	} {
		if err := call(); err == nil || err.Error() != "fábrica" {
			t.Errorf("%s must surface the factory error, got %v", name, err)
		}
	}
}

func TestAPIAdapter_CoursesUsesSiteInfoUserID(t *testing.T) {
	api := &fakeAPI{courses: []moodle.APICourse{
		{ID: 1, FullName: " Cálculo II ", ShortName: "CAL2"},
		{ID: 2, FullName: "", ShortName: "FIS1"},
	}}
	withFakeAPI(t, api)
	got, err := newAPIAdapter().Courses(context.Background(), "c.cl", testToken)
	if err != nil {
		t.Fatal(err)
	}
	if want := []tui.Course{{ID: 1, Name: "Cálculo II"}, {ID: 2, Name: "FIS1"}}; !reflect.DeepEqual(got, want) {
		t.Errorf("courses = %+v, want %+v", got, want)
	}
	if want := []string{"siteinfo:" + testToken, "courses:" + testToken + ":42"}; !reflect.DeepEqual(api.calls, want) {
		t.Errorf("calls = %v, want %v", api.calls, want)
	}
}

func TestAPIAdapter_CoursesErrorMapping(t *testing.T) {
	tests := []struct {
		name string
		api  *fakeAPI
		want error
	}{
		{"site info expired", &fakeAPI{siteErr: moodle.ErrInvalidToken}, domain.ErrAuthenticationFailed},
		{"courses expired", &fakeAPI{coursesErr: moodle.ErrInvalidToken}, domain.ErrAuthenticationFailed},
		{"site info unavailable", &fakeAPI{siteErr: moodle.ErrAPIUnavailable}, tui.ErrAPIUnavailable},
		{"courses unavailable", &fakeAPI{coursesErr: moodle.ErrAPIUnavailable}, tui.ErrAPIUnavailable},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			withFakeAPI(t, tc.api)
			if _, err := newAPIAdapter().Courses(context.Background(), "c.cl", testToken); !errors.Is(err, tc.want) {
				t.Errorf("err = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestAPIAdapter_ContentsMapsSectionsAndFiles(t *testing.T) {
	api := &fakeAPI{sections: []moodle.APISection{
		{ID: 5, Number: 1, Name: "Tema 1", Files: []moodle.APIFile{
			{Name: "Clase", FileName: "clase.pdf", URL: "https://c.cl/webservice/pluginfile.php/1/clase.pdf", Size: 10, Module: "resource"},
			{Name: "  ", FileName: "guia.pdf", URL: "https://c.cl/webservice/pluginfile.php/2/guia.pdf", SubDir: "Guías", Size: 20, Module: "folder"},
		}},
		{ID: 6, Number: 2, Name: "Vacía"},
	}}
	withFakeAPI(t, api)
	got, err := newAPIAdapter().Contents(context.Background(), "c.cl", testToken, 7)
	if err != nil {
		t.Fatal(err)
	}
	want := []tui.Section{
		{ID: 5, Number: 1, Name: "Tema 1", Files: []tui.FileItem{
			{Name: "Clase", URL: "https://c.cl/webservice/pluginfile.php/1/clase.pdf", Size: 10},
			{Name: "guia.pdf", URL: "https://c.cl/webservice/pluginfile.php/2/guia.pdf", SubDir: "Guías", Size: 20},
		}},
		{ID: 6, Number: 2, Name: "Vacía", Files: []tui.FileItem{}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("sections = %+v, want %+v", got, want)
	}
	if want := []string{"contents:" + testToken + ":7"}; !reflect.DeepEqual(api.calls, want) {
		t.Errorf("calls = %v", api.calls)
	}

	for _, e := range []struct{ in, want error }{
		{moodle.ErrInvalidToken, domain.ErrAuthenticationFailed},
		{moodle.ErrAPIUnavailable, tui.ErrAPIUnavailable},
	} {
		withFakeAPI(t, &fakeAPI{contentsErr: e.in})
		if _, err := newAPIAdapter().Contents(context.Background(), "c.cl", testToken, 1); !errors.Is(err, e.want) {
			t.Errorf("Contents(%v) err = %v, want %v", e.in, err, e.want)
		}
	}
}

func TestAuthOptions_WiresTheAPIAndDefault(t *testing.T) {
	resetFlags(t)
	withAuthFlags(t, "ufro", "", fakeCookies{})
	opts, err := authOptions()
	if err != nil {
		t.Fatal(err)
	}
	if opts.Login == nil || opts.Courses == nil || opts.Contents == nil || opts.DefaultAuth != "api" {
		t.Errorf("API not wired: %+v", opts)
	}
	flagAuth = "cookie"
	opts, err = authOptions()
	if err != nil || opts.DefaultAuth != "cookie" {
		t.Errorf("DefaultAuth = %q, %v", opts.DefaultAuth, err)
	}

	api := &fakeAPI{}
	sites := withFakeAPI(t, api)
	if tok, err := opts.Login(context.Background(), "campusvirtual.ufro.cl", "a", testPass); err != nil || tok != testToken {
		t.Errorf("Login = %q, %v", tok, err)
	}
	if !reflect.DeepEqual(*sites, []string{"https://campusvirtual.ufro.cl"}) {
		t.Errorf("sites = %v", *sites)
	}
}

func TestAuthFlag_Validation(t *testing.T) {
	for _, v := range []string{"api", "cookie"} {
		resetFlags(t)
		if _, err := runCLI(t, "--auth", v, "version"); err != nil {
			t.Errorf("--auth %s must be accepted: %v", v, err)
		}
	}
	for _, args := range [][]string{{"--auth", "otra", "run", "-u", "https://a.cl/a.pdf"}, {"--auth=x", "run", "--demo", "-u", "https://a.cl/a.pdf"}} {
		_, err := runCLI(t, args...)
		if err == nil {
			t.Fatalf("%v must fail", args)
		}
		if code := DetermineExitCode(context.Background(), err); code != ExitUsageErr {
			t.Errorf("%v: exit code = %d, want %d (err: %v)", args, code, ExitUsageErr, err)
		}
	}
	resetFlags(t)
	flagAuth = "x"
	if _, err := authOptions(); err == nil {
		t.Error("authOptions must reject an invalid --auth")
	}
}

func TestAuthFlag_AppearsInHelp(t *testing.T) {
	out, err := runCLI(t, "--help")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "--auth MODO") || !strings.Contains(out, "usuario y contraseña") {
		t.Errorf("help must document --auth:\n%s", out)
	}
}

func TestCreateTasks_APIItems(t *testing.T) {
	out := t.TempDir()
	form := &tui.FormData{
		Token: testToken,
		URLs:  []string{"https://c.cl/webservice/pluginfile.php/1/a.pdf", "https://c.cl/webservice/pluginfile.php/2/b.pdf"},
		Items: []tui.Download{
			{URL: "https://c.cl/webservice/pluginfile.php/1/a.pdf", SubDir: "Cálculo II/01 - Límites/"},
			{URL: "https://c.cl/webservice/pluginfile.php/2/b.pdf", SubDir: "Cálculo II/01 - Límites/Guías/"},
		},
	}
	tasks, err := createTasks(form, out)
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 2 {
		t.Fatalf("tasks = %+v", tasks)
	}
	for i, task := range tasks {
		if task.ID != i+1 || task.Token != testToken || task.Cookie != "" || task.URL != form.Items[i].URL {
			t.Errorf("task %d = %+v", i, task)
		}
		if strings.Contains(task.URL, testToken) {
			t.Errorf("the token must not be part of the URL: %q", task.URL)
		}
	}
	if want := filepath.Join(out, "Cálculo II", "01 - Límites"); tasks[0].OutputDir != want {
		t.Errorf("OutputDir = %q, want %q", tasks[0].OutputDir, want)
	}
	if want := filepath.Join(out, "Cálculo II", "01 - Límites", "Guías"); tasks[1].OutputDir != want {
		t.Errorf("OutputDir = %q, want %q", tasks[1].OutputDir, want)
	}
}

func TestCreateTasks_RejectsTraversal(t *testing.T) {
	out := t.TempDir()
	for _, sub := range []string{"../fuera/", "a/../../fuera/", "..", `..\fuera`, "a/../../../x"} {
		form := &tui.FormData{Items: []tui.Download{{URL: "https://c.cl/a.pdf", SubDir: sub}}}
		if sub == `..\fuera` && filepath.Separator == '/' {
			continue // a backslash is an ordinary character on Unix
		}
		tasks, err := createTasks(form, out)
		if err == nil || tasks != nil {
			t.Errorf("SubDir %q must be rejected, got %+v, %v", sub, tasks, err)
		}
	}
	// a sub-folder that only looks like ".." is fine
	form := &tui.FormData{Items: []tui.Download{{URL: "https://c.cl/a.pdf", SubDir: "..oculta/x/"}}}
	if _, err := createTasks(form, out); err != nil {
		t.Errorf("..oculta is a normal name: %v", err)
	}
	// a relative output dir works too
	if _, err := createTasks(&tui.FormData{Items: []tui.Download{{URL: "u", SubDir: "a/b/"}}}, "."); err != nil {
		t.Errorf("relative output dir: %v", err)
	}
	if _, err := createTasks(&tui.FormData{Items: []tui.Download{{URL: "u", SubDir: "../x/"}}}, "."); err == nil {
		t.Error("relative output dir: traversal must be rejected")
	}
}

func TestCreateTasks_LegacyPathUnchanged(t *testing.T) {
	form := &tui.FormData{Cookie: "MoodleSession=zzz", URLs: []string{"https://c.cl/a.pdf", "https://c.cl/b.pdf"}}
	tasks, err := createTasks(form, "/salida")
	if err != nil {
		t.Fatal(err)
	}
	want := []domain.Task{
		{ID: 1, URL: "https://c.cl/a.pdf", Cookie: "MoodleSession=zzz", OutputDir: "/salida"},
		{ID: 2, URL: "https://c.cl/b.pdf", Cookie: "MoodleSession=zzz", OutputDir: "/salida"},
	}
	if !reflect.DeepEqual(tasks, want) {
		t.Errorf("tasks = %+v, want %+v", tasks, want)
	}
}

func TestExecute_RejectsTraversalBeforeDownloading(t *testing.T) {
	resetFlags(t)
	flagOutputDir = t.TempDir()
	flagDemo = true
	err := execute(context.Background(), &tui.FormData{Items: []tui.Download{{URL: "https://c.cl/a.pdf", SubDir: "../x/"}}})
	if err == nil || !strings.Contains(err.Error(), "fuera de la carpeta de salida") {
		t.Fatalf("err = %v", err)
	}
}

// The API route end to end with the demo downloader: files land in
// <salida>/<curso>/<NN - sección>/<subcarpeta>/ and the token never reaches
// the progress output.
func TestExecute_APIItemsLandInCourseSectionFolders(t *testing.T) {
	resetFlags(t)
	out := t.TempDir()
	flagOutputDir = out
	flagDemo = true
	flagHeadless = true
	flagConcurrency = 2

	var stderr bytes.Buffer
	oldErr := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stderr = w
	done := make(chan struct{})
	go func() { _, _ = stderr.ReadFrom(r); close(done) }()

	form := &tui.FormData{
		Token: testToken,
		URLs:  []string{demoFileBase + "1/mod_resource/content/1/Clase_01.pdf", demoFileBase + "1/mod_folder/content/0/Guias/Guia_01.pdf"},
		Items: []tui.Download{
			{URL: demoFileBase + "1/mod_resource/content/1/Clase_01.pdf", SubDir: "Estructuras de Datos/01 - Listas/"},
			{URL: demoFileBase + "1/mod_folder/content/0/Guias/Guia_01.pdf", SubDir: "Estructuras de Datos/01 - Listas/Guías/"},
		},
	}
	runErr := execute(context.Background(), form)
	_ = w.Close()
	<-done
	os.Stderr = oldErr
	if runErr != nil {
		t.Fatal(runErr)
	}
	for _, rel := range []string{
		filepath.Join("Estructuras de Datos", "01 - Listas", "Clase_01.pdf"),
		filepath.Join("Estructuras de Datos", "01 - Listas", "Guías", "Guia_01.pdf"),
	} {
		if _, err := os.Stat(filepath.Join(out, rel)); err != nil {
			t.Errorf("missing %s: %v", rel, err)
		}
	}
	if strings.Contains(stderr.String(), testToken) {
		t.Errorf("the token leaked into the progress output: %q", stderr.String())
	}
}

func TestWithDemo_SimulatesTheAPIFlow(t *testing.T) {
	opts := withDemo(&tui.AuthOptions{})
	ctx := context.Background()

	tok, err := opts.Login(ctx, "campusvirtual.ufro.cl", "alumno", "cualquiera")
	if err != nil || tok == "" {
		t.Fatalf("Login = %q, %v", tok, err)
	}
	if _, err := opts.Login(ctx, "campusvirtual.ufro.cl", "", "x"); !errors.Is(err, domain.ErrAuthenticationFailed) {
		t.Errorf("empty user must be rejected: %v", err)
	}
	if _, err := opts.Login(ctx, "campusvirtual.ufro.cl", "a", ""); !errors.Is(err, domain.ErrAuthenticationFailed) {
		t.Errorf("empty password must be rejected: %v", err)
	}

	courses, err := opts.Courses(ctx, "campusvirtual.ufro.cl", tok)
	if err != nil || len(courses) != 3 {
		t.Fatalf("Courses = %+v, %v", courses, err)
	}
	courses[0].Name = "mutado"
	if demoCourses[0].Name == "mutado" {
		t.Error("Courses must return a copy")
	}

	for _, c := range demoCourses {
		sections, err := opts.Contents(ctx, "campusvirtual.ufro.cl", tok, c.ID)
		if err != nil || len(sections) == 0 {
			t.Fatalf("Contents(%d) = %+v, %v", c.ID, sections, err)
		}
	}
	if got, err := opts.Contents(ctx, "x", tok, 999); err != nil || got != nil {
		t.Errorf("unknown course: %+v, %v", got, err)
	}
}

func TestDemoCourses_FixturesAreValidAndDistinct(t *testing.T) {
	urls, files := map[string]bool{}, map[string]bool{}
	n := 0
	for _, c := range demoCourses {
		if c.Name == "" {
			t.Fatalf("course %d has no name", c.ID)
		}
		for _, s := range demoContents(c.ID) {
			for _, f := range s.Files {
				n++
				u, err := url.Parse(f.URL)
				if err != nil || u.Host != "campusvirtual.ufro.cl" || !strings.Contains(u.Path, "/webservice/pluginfile.php/") {
					t.Errorf("bad URL %q", f.URL)
				}
				if strings.Contains(f.URL, "token") {
					t.Errorf("URL with a token: %q", f.URL)
				}
				name := demo.ExtractPDFResource(f.URL, n)
				if len(name) < 5 || !strings.HasSuffix(name, ".pdf") {
					t.Errorf("demo downloader would name %q as %q", f.URL, name)
				}
				if f.Name == "" || f.Size <= 0 || urls[f.URL] || files[name] {
					t.Errorf("incomplete or duplicated fixture: %+v", f)
				}
				urls[f.URL], files[name] = true, true
			}
		}
	}
	if n < 10 {
		t.Errorf("only %d demo files", n)
	}
}

func TestWithDemo_APIStepsRespectCancellation(t *testing.T) {
	opts := withDemo(&tui.AuthOptions{})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	start := time.Now()
	if _, err := opts.Login(ctx, "c", "u", "p"); !errors.Is(err, context.Canceled) {
		t.Errorf("Login err = %v", err)
	}
	if _, err := opts.Courses(ctx, "c", "t"); !errors.Is(err, context.Canceled) {
		t.Errorf("Courses err = %v", err)
	}
	if _, err := opts.Contents(ctx, "c", "t", 101); !errors.Is(err, context.Canceled) {
		t.Errorf("Contents err = %v", err)
	}
	if time.Since(start) > time.Second {
		t.Error("cancelled demo steps must return immediately")
	}
}

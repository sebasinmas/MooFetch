package moodle

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sebasinmas/MooFetch/internal/domain"
	"github.com/sebasinmas/MooFetch/internal/logger"
)

const (
	testToken    = "0123456789abcdef0123456789abcdef"
	testPassword = "pa55w0rd-S3cr3t!"
	testUser     = "alumno.ejemplo"
)

// seenRequest is what the fake campus recorded about one request.
type seenRequest struct {
	Method      string
	Path        string
	RawQuery    string
	ContentType string
	UserAgent   string
	Form        url.Values
}

type fakeCampus struct {
	mu       sync.Mutex
	requests []seenRequest
	handler  func(w http.ResponseWriter, r *http.Request, form url.Values)
}

func (f *fakeCampus) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	form := r.PostForm
	f.mu.Lock()
	f.requests = append(f.requests, seenRequest{
		Method: r.Method, Path: r.URL.Path, RawQuery: r.URL.RawQuery,
		ContentType: r.Header.Get("Content-Type"), UserAgent: r.UserAgent(), Form: form,
	})
	f.mu.Unlock()
	f.handler(w, r, form)
}

func (f *fakeCampus) seen() []seenRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]seenRequest(nil), f.requests...)
}

// sleeps records the injected pauses instead of waiting.
type sleeps struct {
	mu sync.Mutex
	d  []time.Duration
}

func (s *sleeps) sleep(_ context.Context, d time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.d = append(s.d, d)
	return nil
}

func (s *sleeps) got() []time.Duration {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]time.Duration(nil), s.d...)
}

// newCampus starts a TLS fake campus and returns a client for it.
func newCampus(t *testing.T, h func(w http.ResponseWriter, r *http.Request, form url.Values), opts ...APIOption) (*APIClient, *fakeCampus, *sleeps, *httptest.Server) {
	t.Helper()
	fc := &fakeCampus{handler: h}
	srv := httptest.NewTLSServer(fc)
	t.Cleanup(srv.Close)
	sl := &sleeps{}
	all := append([]APIOption{WithAPIHTTPClient(srv.Client()), WithAPISleep(sl.sleep)}, opts...)
	c, err := NewAPIClient(srv.URL, all...)
	if err != nil {
		t.Fatalf("NewAPIClient: %v", err)
	}
	return c, fc, sl, srv
}

func fixture(t *testing.T, name string, srv *httptest.Server) []byte {
	t.Helper()
	b, err := os.ReadFile("testdata/api/" + name)
	if err != nil {
		t.Fatal(err)
	}
	u, _ := url.Parse(srv.URL)
	r := strings.NewReplacer("{{HOST}}", srv.URL, "{{HOSTNAME}}", u.Host)
	return []byte(r.Replace(string(b)))
}

func writeJSON(w http.ResponseWriter, body string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_, _ = w.Write([]byte(body))
}

func assertNoSecrets(t *testing.T, what, text string) {
	t.Helper()
	for _, s := range []string{testToken, testPassword} {
		if strings.Contains(text, s) {
			t.Errorf("%s leaks a secret", what)
		}
	}
}

// ------------------------------------------------------------ constructor

func TestNewAPIClient_URLRules(t *testing.T) {
	tests := []struct {
		name     string
		in       string
		wantErr  error
		wantAny  bool
		wantBase string
	}{
		{name: "https ok", in: "https://campus.example.cl", wantBase: "https://campus.example.cl"},
		{name: "trailing slash", in: "https://campus.example.cl/", wantBase: "https://campus.example.cl"},
		{name: "path prefix kept", in: "https://campus.example.cl/moodle//", wantBase: "https://campus.example.cl/moodle"},
		{name: "query fragment userinfo dropped", in: "https://u:p@Campus.Example.cl/m/?a=b#frag", wantBase: "https://campus.example.cl/m"},
		{name: "bare host is https", in: "campus.example.cl", wantBase: "https://campus.example.cl"},
		{name: "http rejected", in: "http://campus.example.cl", wantErr: ErrInsecureURL},
		{name: "ftp rejected", in: "ftp://campus.example.cl", wantErr: ErrInsecureURL},
		{name: "empty", in: "  ", wantAny: true},
		{name: "no host", in: "https://", wantAny: true},
		{name: "garbage", in: "https://exa mple.cl", wantAny: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c, err := NewAPIClient(tc.in)
			switch {
			case tc.wantErr != nil:
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("err = %v, want %v", err, tc.wantErr)
				}
			case tc.wantAny:
				if err == nil {
					t.Fatal("expected an error")
				}
			default:
				if err != nil {
					t.Fatal(err)
				}
				if got := c.base.String(); got != tc.wantBase {
					t.Errorf("base = %q, want %q", got, tc.wantBase)
				}
				if got := c.endpoint(loginPath); got != tc.wantBase+loginPath {
					t.Errorf("endpoint = %q", got)
				}
			}
		})
	}
}

func TestNewAPIClient_DefaultsAndOptions(t *testing.T) {
	c, err := NewAPIClient("https://campus.example.cl")
	if err != nil {
		t.Fatal(err)
	}
	if c.delay != 1500*time.Millisecond || DefaultScanDelay != 1500*time.Millisecond {
		t.Errorf("default delay = %v", c.delay)
	}
	if !strings.HasPrefix(c.userAgent, "MooFetch/") || !strings.Contains(c.userAgent, repositoryAddress) {
		t.Errorf("user agent = %q", c.userAgent)
	}
	// nil / empty options keep the defaults
	c, err = NewAPIClient("https://campus.example.cl", WithAPIHTTPClient(nil), WithAPISleep(nil), WithAPIUserAgent("  "))
	if err != nil {
		t.Fatal(err)
	}
	if c.client == nil || c.sleep == nil || c.userAgent == "" {
		t.Error("nil options must keep defaults")
	}
	c, _ = NewAPIClient("https://campus.example.cl", WithAPIDelay(-time.Second))
	if c.delay != 0 {
		t.Errorf("negative delay = %v, want 0", c.delay)
	}
}

func TestAPIClient_DoesNotMutateCallerClientAndNeverFollowsRedirects(t *testing.T) {
	var hits atomic.Int32
	other := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { hits.Add(1) }))
	defer other.Close()
	c, _, _, srv := newCampus(t, func(w http.ResponseWriter, r *http.Request, _ url.Values) {
		http.Redirect(w, r, other.URL+"/login/token.php", http.StatusTemporaryRedirect)
	})
	if srv.Client().CheckRedirect != nil {
		t.Error("caller's client was modified")
	}
	_, err := c.Login(context.Background(), testUser, testPassword)
	if err == nil || !strings.Contains(err.Error(), "HTTP 307") {
		t.Fatalf("err = %v, want HTTP 307", err)
	}
	if hits.Load() != 0 {
		t.Error("a redirect was followed: password could have been resent")
	}
}

// ------------------------------------------------------------------ login

func TestLogin_Success(t *testing.T) {
	c, fc, sl, _ := newCampus(t, func(w http.ResponseWriter, _ *http.Request, _ url.Values) {
		writeJSON(w, `{"token":"`+testToken+`","privatetoken":"zzz"}`)
	}, WithAPIUserAgent("MooFetch/test"))
	tok, err := c.Login(context.Background(), testUser, testPassword)
	if err != nil {
		t.Fatal(err)
	}
	if tok != testToken {
		t.Error("wrong token returned")
	}
	reqs := fc.seen()
	if len(reqs) != 1 {
		t.Fatalf("%d requests", len(reqs))
	}
	r := reqs[0]
	if r.Method != http.MethodPost || r.Path != "/login/token.php" || r.RawQuery != "" {
		t.Errorf("request = %+v", r)
	}
	if r.ContentType != "application/x-www-form-urlencoded" || r.UserAgent != "MooFetch/test" {
		t.Errorf("headers = %q / %q", r.ContentType, r.UserAgent)
	}
	if r.Form.Get("username") != testUser || r.Form.Get("password") != testPassword || r.Form.Get("service") != "moodle_mobile_app" {
		t.Errorf("form = %v", r.Form)
	}
	if len(sl.got()) != 0 {
		t.Errorf("first request must not be delayed: %v", sl.got())
	}
}

func TestLogin_Errors(t *testing.T) {
	huge := `{"token":"` + strings.Repeat("a", maxLoginBody) + `"}`
	tests := []struct {
		name   string
		status int
		body   string
		check  func(t *testing.T, err error)
	}{
		{name: "invalidlogin", body: `{"error":"Usuario o contraseña incorrectos","errorcode":"invalidlogin","stacktrace":null,"debuginfo":null,"reproductionlink":null}`,
			check: func(t *testing.T, err error) {
				if !errors.Is(err, ErrInvalidCredentials) || !errors.Is(err, domain.ErrAuthenticationFailed) {
					t.Errorf("err = %v", err)
				}
			}},
		{name: "enablewsdescription", body: `{"error":"x","errorcode":"enablewsdescription"}`, check: wantIs(ErrAPIUnavailable)},
		{name: "webservicesnotenabled", body: `{"error":"x","errorcode":"webservicesnotenabled"}`, check: wantIs(ErrAPIUnavailable)},
		{name: "servicenotavailable", body: `{"error":"x","errorcode":"servicenotavailable"}`, check: wantIs(ErrAPIUnavailable)},
		{name: "missing service", body: `{"error":"x","errorcode":"servicenotexist"}`, check: wantIs(ErrAPIUnavailable)},
		{name: "unknown code is descriptive without body", body: `{"error":"Detalle ZETA-INTERNO","errorcode":"usernotconfirmed"}`,
			check: func(t *testing.T, err error) {
				if err == nil || !strings.Contains(err.Error(), "usernotconfirmed") || strings.Contains(err.Error(), "ZETA-INTERNO") {
					t.Errorf("err = %v", err)
				}
				if errors.Is(err, domain.ErrAuthenticationFailed) {
					t.Error("unknown codes must not be fatal auth errors")
				}
			}},
		{name: "hostile errorcode is not echoed", body: `{"error":"x","errorcode":"bad code <script>` + testPassword + `"}`,
			check: func(t *testing.T, err error) {
				if err == nil || strings.Contains(err.Error(), "script") || !strings.Contains(err.Error(), "desconocido") {
					t.Errorf("err = %v", err)
				}
			}},
		{name: "error without code", body: `{"error":"algo"}`, check: wantErrContains("desconocido")},
		{name: "empty token", body: `{"token":""}`, check: wantErrContains("token")},
		{name: "missing token", body: `{}`, check: wantErrContains("token")},
		{name: "not json", body: `<html>Login</html>`, check: wantErrContains("no es JSON")},
		{name: "oversize", body: huge, check: wantErrContains("límite")},
		{name: "http 500", status: 500, body: `{"token":"` + testToken + `"}`, check: wantErrContains("HTTP 500")},
		{name: "http 404", status: 404, body: `nope`, check: wantErrContains("HTTP 404")},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c, _, _, _ := newCampus(t, func(w http.ResponseWriter, _ *http.Request, _ url.Values) {
				if tc.status != 0 {
					w.WriteHeader(tc.status)
				}
				writeJSON(w, tc.body)
			})
			tok, err := c.Login(context.Background(), testUser, testPassword)
			if err == nil || tok != "" {
				t.Fatalf("tok=%q err=%v", tok, err)
			}
			tc.check(t, err)
			assertNoSecrets(t, "error", err.Error())
			if strings.Contains(err.Error(), testUser) {
				t.Error("error leaks the username")
			}
		})
	}
}

func wantIs(target error) func(*testing.T, error) {
	return func(t *testing.T, err error) {
		t.Helper()
		if !errors.Is(err, target) {
			t.Errorf("err = %v, want %v", err, target)
		}
	}
}

func wantErrContains(sub string) func(*testing.T, error) {
	return func(t *testing.T, err error) {
		t.Helper()
		if err == nil || !strings.Contains(err.Error(), sub) {
			t.Errorf("err = %v, want it to contain %q", err, sub)
		}
	}
}

func TestLogin_FixturesFromMoodle35(t *testing.T) {
	for name, want := range map[string]error{
		"login_invalid.json":          ErrInvalidCredentials,
		"login_ws_disabled.json":      ErrAPIUnavailable,
		"exception_invalidtoken.json": nil, // an exception object has no errorcode/error keys for login
	} {
		t.Run(name, func(t *testing.T) {
			var srvRef *httptest.Server
			c, _, _, srv := newCampus(t, func(w http.ResponseWriter, _ *http.Request, _ url.Values) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write(fixture(t, name, srvRef))
			})
			srvRef = srv
			_, err := c.Login(context.Background(), testUser, testPassword)
			if want != nil && !errors.Is(err, want) {
				t.Errorf("err = %v, want %v", err, want)
			}
			if want == nil && err == nil {
				t.Error("expected an error (no token)")
			}
		})
	}
}

func TestLogin_TransportErrorHasNoSecrets(t *testing.T) {
	c, _, _, srv := newCampus(t, func(http.ResponseWriter, *http.Request, url.Values) {})
	srv.Close()
	_, err := c.Login(context.Background(), testUser, testPassword)
	if err == nil {
		t.Fatal("expected an error")
	}
	assertNoSecrets(t, "error", err.Error())
	var ue *url.Error
	if errors.As(err, &ue) {
		t.Error("a *url.Error (which embeds the URL) must not leave the client")
	}
}

// -------------------------------------------------------- webservice calls

func TestWebserviceCalls_TokenInBodyNotInURL(t *testing.T) {
	c, fc, _, srvRef := newCampus(t, func(w http.ResponseWriter, _ *http.Request, form url.Values) {
		switch form.Get("wsfunction") {
		case "core_webservice_get_site_info":
			writeJSON(w, string(fixtureFor(t, "site_info.json")))
		case "core_enrol_get_users_courses":
			writeJSON(w, string(fixtureFor(t, "courses.json")))
		default:
			writeJSON(w, `[]`)
		}
	})
	_ = srvRef
	ctx := context.Background()
	info, err := c.SiteInfo(ctx, testToken)
	if err != nil {
		t.Fatal(err)
	}
	if info.UserID != 3456 || info.SiteName != "Campus Virtual UFRO" || !strings.HasPrefix(info.Release, "3.5.4") {
		t.Errorf("info = %+v", info)
	}
	if !info.Functions["core_course_get_contents"] || len(info.Functions) != 3 {
		t.Errorf("functions = %v", info.Functions)
	}
	if _, err := c.Courses(ctx, testToken, info.UserID); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Contents(ctx, testToken, 4001); err != nil {
		t.Fatal(err)
	}

	reqs := fc.seen()
	if len(reqs) != 3 {
		t.Fatalf("%d requests", len(reqs))
	}
	wantFn := []string{"core_webservice_get_site_info", "core_enrol_get_users_courses", "core_course_get_contents"}
	for i, r := range reqs {
		if r.Method != http.MethodPost || r.Path != "/webservice/rest/server.php" {
			t.Errorf("request %d = %s %s", i, r.Method, r.Path)
		}
		if r.RawQuery != "" {
			t.Errorf("request %d has a query string: the token must travel in the body", i)
		}
		if r.Form.Get("wstoken") != testToken || r.Form.Get("wsfunction") != wantFn[i] || r.Form.Get("moodlewsrestformat") != "json" {
			t.Errorf("request %d form = %v", i, r.Form)
		}
	}
	if reqs[1].Form.Get("userid") != "3456" || reqs[2].Form.Get("courseid") != "4001" {
		t.Error("function parameters missing")
	}
}

// fixtureFor loads a fixture whose placeholders do not matter.
func fixtureFor(t *testing.T, name string) []byte {
	t.Helper()
	return fixture(t, name, httptest.NewUnstartedServer(nil))
}

func TestSiteInfo_Errors(t *testing.T) {
	tests := []struct {
		name  string
		body  string
		check func(*testing.T, error)
	}{
		{name: "no user id", body: `{"sitename":"x","userid":0}`, check: wantErrContains("usuario")},
		{name: "array instead of object", body: `[]`, check: wantErrContains("inesperada")},
		{name: "empty body", body: ``, check: wantErrContains("vacía")},
		{name: "not json", body: `<html>`, check: wantErrContains("inesperada")},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c, _, _, _ := newCampus(t, func(w http.ResponseWriter, _ *http.Request, _ url.Values) { writeJSON(w, tc.body) })
			_, err := c.SiteInfo(context.Background(), testToken)
			tc.check(t, err)
		})
	}
}

func TestWebserviceExceptions(t *testing.T) {
	tests := []struct {
		name  string
		body  string
		check func(*testing.T, error)
	}{
		{name: "invalidtoken", body: `{"exception":"moodle_exception","errorcode":"invalidtoken","message":"Token inválido"}`,
			check: func(t *testing.T, err error) {
				if !errors.Is(err, ErrInvalidToken) || !domain.IsFatalAuth(err) {
					t.Errorf("err = %v", err)
				}
			}},
		{name: "accessexception", body: `{"exception":"webservice_access_exception","errorcode":"accessexception","message":"Excepción de control de acceso"}`, check: wantIs(ErrInvalidToken)},
		{name: "webservicesnotenabled", body: `{"exception":"moodle_exception","errorcode":"webservicesnotenabled","message":"x"}`, check: wantIs(ErrAPIUnavailable)},
		{name: "invalidfunction", body: `{"exception":"moodle_exception","errorcode":"invalidfunction","message":"x"}`, check: wantIs(ErrAPIUnavailable)},
		{name: "other exception shows errorcode only", body: `{"exception":"moodle_exception","errorcode":"invalidcourseid","message":"MENSAJE-INTERNO ` + testToken + `"}`,
			check: func(t *testing.T, err error) {
				if err == nil || !strings.Contains(err.Error(), "invalidcourseid") || strings.Contains(err.Error(), "MENSAJE-INTERNO") {
					t.Errorf("err = %v", err)
				}
			}},
		{name: "exception without code", body: `{"exception":"moodle_exception"}`, check: wantErrContains("desconocido")},
	}
	for _, tc := range tests {
		for _, call := range []string{"site", "courses", "contents"} {
			t.Run(tc.name+"/"+call, func(t *testing.T) {
				c, _, _, _ := newCampus(t, func(w http.ResponseWriter, _ *http.Request, _ url.Values) { writeJSON(w, tc.body) })
				var err error
				switch call {
				case "site":
					_, err = c.SiteInfo(context.Background(), testToken)
				case "courses":
					_, err = c.Courses(context.Background(), testToken, 1)
				default:
					_, err = c.Contents(context.Background(), testToken, 1)
				}
				tc.check(t, err)
				assertNoSecrets(t, "error", err.Error())
			})
		}
	}
}

func TestCourses_SortedAndDeduplicated(t *testing.T) {
	c, _, _, _ := newCampus(t, func(w http.ResponseWriter, _ *http.Request, _ url.Values) {
		writeJSON(w, string(fixtureFor(t, "courses.json")))
	})
	got, err := c.Courses(context.Background(), testToken, 3456)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, co := range got {
		names = append(names, co.FullName)
	}
	// Case- and accent-insensitive: "Álgebra" among the A's, lower-case "zoología" last.
	want := []string{"Álgebra Lineal", "Biología Celular", "Cálculo Diferencial", "zoología general"}
	if strings.Join(names, "|") != strings.Join(want, "|") {
		t.Fatalf("courses = %v, want %v", names, want)
	}
	for _, co := range got {
		if co.FullName == "Cálculo Diferencial (duplicado)" {
			t.Error("duplicate id kept")
		}
		if co.ID == 4001 && co.ShortName != "CAL-201" {
			t.Errorf("shortname = %q", co.ShortName)
		}
	}
}

func TestCourses_FallbacksAndInvalidEntries(t *testing.T) {
	c, _, _, _ := newCampus(t, func(w http.ResponseWriter, _ *http.Request, _ url.Values) {
		writeJSON(w, `[{"id":0,"fullname":"zero"},{"id":9,"shortname":" SHORT ","fullname":"  "},
		  {"id":8,"fullname":"Same","shortname":"B"},{"id":7,"fullname":"same","shortname":"A"}]`)
	})
	got, err := c.Courses(context.Background(), testToken, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("courses = %+v", got)
	}
	// "same" (id 7) and "Same" (id 8) tie case-insensitively and fall back to the id; "SHORT" sorts after them.
	if got[0].ID != 7 || got[1].ID != 8 || got[2].ID != 9 || got[2].FullName != "SHORT" {
		t.Errorf("courses = %+v", got)
	}
}

// --------------------------------------------------------------- contents

func TestContents_ParsesMoodle35Fixture(t *testing.T) {
	var srvRef *httptest.Server
	c, _, _, srv := newCampus(t, func(w http.ResponseWriter, _ *http.Request, _ url.Values) {
		writeJSON(w, string(fixture(t, "contents.json", srvRef)))
	})
	srvRef = srv
	secs, err := c.Contents(context.Background(), testToken, 4001)
	if err != nil {
		t.Fatal(err)
	}
	if len(secs) != 5 {
		t.Fatalf("%d sections, want 5 (empty and hidden ones are kept)", len(secs))
	}
	for i, s := range secs {
		if s.Number != i || s.Files == nil {
			t.Errorf("section %d = %+v", i, s)
		}
	}
	if len(secs[0].Files) != 0 { // forum + label
		t.Errorf("general section files = %+v", secs[0].Files)
	}
	if secs[3].Name != "" || len(secs[3].Files) != 0 {
		t.Errorf("hidden section = %+v", secs[3])
	}
	if len(secs[4].Files) != 0 {
		t.Errorf("hostile section kept files: %+v", secs[4].Files)
	}

	// Section 1: two resources; url and page modules ignored.
	files := secs[1].Files
	if len(files) != 2 {
		t.Fatalf("section 1 files = %+v", files)
	}
	g := files[0]
	if g.Name != "Guía de estudio 1" || g.FileName != "Guia 1.pdf" || g.SubDir != "" || g.Module != "resource" || g.Size != 245760 {
		t.Errorf("resource = %+v", g)
	}
	if !g.Modified.Equal(time.Unix(1551380000, 0)) {
		t.Errorf("modified = %v", g.Modified)
	}
	if g.URL != srv.URL+"/webservice/pluginfile.php/88001/mod_resource/content/1/Guia%201.pdf?forcedownload=1" {
		t.Errorf("url = %q", g.URL)
	}
	if strings.Contains(files[1].URL, "token") || strings.Contains(files[1].URL, "LEAKED") ||
		!strings.HasSuffix(files[1].URL, "/Clase1.pptx?forcedownload=1") {
		t.Errorf("token not stripped from fileurl: %q", files[1].URL)
	}

	// Section 2: folder with nested filepath.
	ff := secs[2].Files
	if len(ff) != 3 {
		t.Fatalf("folder files = %+v", ff)
	}
	wantDirs := []string{"Material de clase", "Material de clase/Guías/Parte 1", "Material de clase/etc"}
	for i, f := range ff {
		if f.Module != "folder" || f.SubDir != wantDirs[i] || f.Name != f.FileName {
			t.Errorf("folder file %d = %+v", i, f)
		}
	}
	if !ff[2].Modified.IsZero() {
		t.Errorf("timemodified 0 should give the zero time, got %v", ff[2].Modified)
	}
}

func TestContents_LogsSkippedCountWithoutURLs(t *testing.T) {
	var buf bytes.Buffer
	var srvRef *httptest.Server
	c, _, _, srv := newCampus(t, func(w http.ResponseWriter, _ *http.Request, _ url.Values) {
		writeJSON(w, string(fixture(t, "contents.json", srvRef)))
	}, WithAPILogger(logger.NewWriter(&buf)))
	srvRef = srv
	if _, err := c.Contents(context.Background(), testToken, 1); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, "skipped") || !strings.Contains(out, "count=2") {
		t.Errorf("log = %s", out)
	}
	if strings.Contains(out, "evil.example.net") || strings.Contains(out, testToken) {
		t.Errorf("log leaks: %s", out)
	}
}

func TestCleanFileURL(t *testing.T) {
	c, _ := NewAPIClient("https://campus.example.cl")
	tests := []struct {
		in   string
		want string
		ok   bool
	}{
		{"https://campus.example.cl/webservice/pluginfile.php/1/a.pdf", "https://campus.example.cl/webservice/pluginfile.php/1/a.pdf", true},
		{"https://CAMPUS.example.cl:443/webservice/pluginfile.php/1/a.pdf?token=abc", "https://CAMPUS.example.cl:443/webservice/pluginfile.php/1/a.pdf", true},
		{"https://campus.example.cl/p/a.pdf?WSToken=abc&x=1&token=d", "https://campus.example.cl/p/a.pdf?x=1", true},
		{"https://campus.example.cl/p/a.pdf?%74oken=abc&y=%20z", "https://campus.example.cl/p/a.pdf?y=%20z", true},
		{"https://u:p@campus.example.cl/p/a.pdf#frag", "https://campus.example.cl/p/a.pdf", true},
		{"https://evil.example.net/p/a.pdf", "", false},
		{"https://campus.example.cl.evil.net/p/a.pdf", "", false},
		{"https://campus.example.cl:8443/p/a.pdf", "", false},
		{"http://campus.example.cl/p/a.pdf", "", false},
		{"//campus.example.cl/p/a.pdf", "", false},
		{"/webservice/pluginfile.php/1/a.pdf", "", false},
		{"", "", false},
		{"https://campus.example.cl/%zz", "", false},
	}
	for _, tc := range tests {
		got, ok := c.cleanFileURL(tc.in)
		if ok != tc.ok || got != tc.want {
			t.Errorf("cleanFileURL(%q) = %q,%v want %q,%v", tc.in, got, ok, tc.want, tc.ok)
		}
	}
}

func TestFolderSubDir(t *testing.T) {
	tests := []struct{ name, path, want string }{
		{"Carpeta", "/", "Carpeta"},
		{"Carpeta", "", "Carpeta"},
		{"Carpeta", "/a/b/", "Carpeta/a/b"},
		{"Carpeta", "a//b", "Carpeta/a/b"},
		{"A/B", "/c/", "A-B/c"},
		{`A\B`, `\c\d\`, "A-B/c/d"},
		{"Carpeta", "/../x/./y/..", "Carpeta/x/y"},
		{"..", "/", ""},
		{"", "/z/", "z"},
	}
	for _, tc := range tests {
		if got := folderSubDir(tc.name, tc.path); got != tc.want {
			t.Errorf("folderSubDir(%q,%q) = %q, want %q", tc.name, tc.path, got, tc.want)
		}
	}
}

func TestStripSecretParams(t *testing.T) {
	tests := map[string]string{
		"":                            "",
		"token=a":                     "",
		"a=1&token=x&b=2":             "a=1&b=2",
		"WSTOKEN=x&a=1":               "a=1",
		"a=1&&b":                      "a=1&b",
		"tokenx=1&mytoken=2&token_=3": "tokenx=1&mytoken=2&token_=3",
		"forcedownload=1&token":       "forcedownload=1",
		"a=%zz&token=1":               "a=%zz",
	}
	for in, want := range tests {
		if got := stripSecretParams(in); got != want {
			t.Errorf("stripSecretParams(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSafeCode(t *testing.T) {
	for in, want := range map[string]string{
		"invalidtoken": "invalidtoken", "dml_missing.record-1": "dml_missing.record-1",
		"": "desconocido", "a b": "desconocido", "ñ": "desconocido", strings.Repeat("a", 65): "desconocido",
	} {
		if got := safeCode(in); got != want {
			t.Errorf("safeCode(%q) = %q, want %q", in, got, want)
		}
	}
}

// ----------------------------------------------------------------- pacing

func TestPacing_RecordedBetweenRequestsNotBeforeFirst(t *testing.T) {
	c, _, sl, _ := newCampus(t, func(w http.ResponseWriter, _ *http.Request, _ url.Values) { writeJSON(w, `[]`) },
		WithAPIDelay(1500*time.Millisecond))
	ctx := context.Background()
	if _, err := c.Courses(ctx, testToken, 1); err != nil {
		t.Fatal(err)
	}
	if len(sl.got()) != 0 {
		t.Fatalf("slept before the first request: %v", sl.got())
	}
	for i := 0; i < 3; i++ {
		if _, err := c.Contents(ctx, testToken, 1); err != nil {
			t.Fatal(err)
		}
	}
	got := sl.got()
	if len(got) != 3 {
		t.Fatalf("sleeps = %v, want 3", got)
	}
	for _, d := range got {
		if d != 1500*time.Millisecond {
			t.Errorf("pause = %v", d)
		}
	}
}

func TestPacing_ZeroDelayNeverSleeps(t *testing.T) {
	c, _, sl, _ := newCampus(t, func(w http.ResponseWriter, _ *http.Request, _ url.Values) { writeJSON(w, `[]`) }, WithAPIDelay(0))
	for i := 0; i < 3; i++ {
		_, _ = c.Courses(context.Background(), testToken, 1)
	}
	if len(sl.got()) != 0 {
		t.Errorf("sleeps = %v", sl.got())
	}
}

func TestPacing_SleepErrorAbortsAndFreesSlot(t *testing.T) {
	boom := errors.New("sleep interrupted")
	fail := true
	c, fc, _, _ := newCampus(t, func(w http.ResponseWriter, _ *http.Request, _ url.Values) { writeJSON(w, `[]`) },
		WithAPISleep(func(context.Context, time.Duration) error {
			if fail {
				return boom
			}
			return nil
		}))
	if _, err := c.Courses(context.Background(), testToken, 1); err != nil { // first: no sleep
		t.Fatal(err)
	}
	if _, err := c.Courses(context.Background(), testToken, 1); !errors.Is(err, boom) {
		t.Fatalf("err = %v, want %v", err, boom)
	}
	if n := len(fc.seen()); n != 1 {
		t.Errorf("%d requests reached the server, want 1", n)
	}
	fail = false
	if _, err := c.Courses(context.Background(), testToken, 1); err != nil {
		t.Fatalf("slot not released: %v", err)
	}
}

func TestSleepCtx(t *testing.T) {
	if err := sleepCtx(context.Background(), time.Millisecond); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := sleepCtx(ctx, time.Hour); !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v", err)
	}
}

func TestRequestsAreSequential(t *testing.T) {
	var cur, peak atomic.Int32
	c, _, _, _ := newCampus(t, func(w http.ResponseWriter, _ *http.Request, _ url.Values) {
		n := cur.Add(1)
		for {
			p := peak.Load()
			if n <= p || peak.CompareAndSwap(p, n) {
				break
			}
		}
		time.Sleep(5 * time.Millisecond)
		cur.Add(-1)
		writeJSON(w, `[]`)
	}, WithAPIDelay(0))
	var wg sync.WaitGroup
	for i := 0; i < 6; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = c.Courses(context.Background(), testToken, 1)
		}()
	}
	wg.Wait()
	if peak.Load() != 1 {
		t.Errorf("peak concurrency = %d, want 1", peak.Load())
	}
}

// ------------------------------------------------------------ cancellation

func TestContextCancellation(t *testing.T) {
	t.Run("already cancelled", func(t *testing.T) {
		c, fc, _, _ := newCampus(t, func(w http.ResponseWriter, _ *http.Request, _ url.Values) { writeJSON(w, `[]`) })
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		_, err := c.Courses(ctx, testToken, 1)
		if !errors.Is(err, context.Canceled) {
			t.Errorf("err = %v", err)
		}
		if len(fc.seen()) != 0 {
			t.Error("request sent with a cancelled context")
		}
	})
	t.Run("waiting for the slot", func(t *testing.T) {
		c, _, _, _ := newCampus(t, func(w http.ResponseWriter, _ *http.Request, _ url.Values) { writeJSON(w, `[]`) })
		c.sem <- struct{}{} // another request holds the slot
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
		defer cancel()
		if _, err := c.Contents(ctx, testToken, 1); !errors.Is(err, context.DeadlineExceeded) {
			t.Errorf("err = %v", err)
		}
		<-c.sem
	})
	t.Run("during the request", func(t *testing.T) {
		started := make(chan struct{})
		release := make(chan struct{})
		c, _, _, _ := newCampus(t, func(w http.ResponseWriter, _ *http.Request, _ url.Values) {
			close(started)
			<-release
			writeJSON(w, `[]`)
		})
		defer close(release)
		ctx, cancel := context.WithCancel(context.Background())
		errc := make(chan error, 1)
		go func() {
			_, err := c.Login(ctx, testUser, testPassword)
			errc <- err
		}()
		<-started
		cancel()
		err := <-errc
		if !errors.Is(err, context.Canceled) {
			t.Errorf("err = %v", err)
		}
		assertNoSecrets(t, "error", err.Error())
	})
}

func TestBodyReadErrorAndOversizeWebservice(t *testing.T) {
	t.Run("truncated body", func(t *testing.T) {
		c, _, _, _ := newCampus(t, func(w http.ResponseWriter, _ *http.Request, _ url.Values) {
			w.Header().Set("Content-Length", "100")
			_, _ = w.Write([]byte(`[{"id":1`))
		})
		_, err := c.Courses(context.Background(), testToken, 1)
		if err == nil {
			t.Fatal("expected an error")
		}
		assertNoSecrets(t, "error", err.Error())
	})
	t.Run("oversize", func(t *testing.T) {
		c, _, _, _ := newCampus(t, func(w http.ResponseWriter, _ *http.Request, _ url.Values) {
			_, _ = w.Write([]byte("[" + strings.Repeat(" ", maxAPIBody) + "]"))
		})
		if _, err := c.Contents(context.Background(), testToken, 1); err == nil || !strings.Contains(err.Error(), "límite") {
			t.Errorf("err = %v", err)
		}
	})
}

func TestRequestError_PlainError(t *testing.T) {
	c, _ := NewAPIClient("https://campus.example.cl")
	err := c.requestError(context.Background(), "/x", errors.New(`boom https://h/p?token=SECRETVALUE`))
	if strings.Contains(err.Error(), "SECRETVALUE") {
		t.Errorf("err = %v", err)
	}
}

func TestDebugLogsContainNoSecrets(t *testing.T) {
	var buf bytes.Buffer
	c, _, _, _ := newCampus(t, func(w http.ResponseWriter, _ *http.Request, form url.Values) {
		if form.Get("wsfunction") == "" {
			writeJSON(w, `{"token":"`+testToken+`"}`)
			return
		}
		writeJSON(w, `[]`)
	}, WithAPILogger(logger.NewWriter(&buf)))
	tok, err := c.Login(context.Background(), testUser, testPassword)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Courses(context.Background(), tok, 1); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, "core_enrol_get_users_courses") {
		t.Errorf("expected a debug line, got %q", out)
	}
	assertNoSecrets(t, "log", out)
	if strings.Contains(out, testUser) {
		t.Error("log leaks the username")
	}
}

package moodle

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sebasinmas/MooFetch/internal/domain"
	"github.com/sebasinmas/MooFetch/internal/logger"
)

var tokenTestPDF = []byte("%PDF-1.4\n1 0 obj\n<< /Type /Catalog >>\nendobj\n%%EOF")

const pluginfileTestPath = "/webservice/pluginfile.php/88001/mod_resource/content/1/Guia.pdf"

// tokenPlugin builds a Plugin that trusts srv's certificate and uses the
// production redirect policy.
func tokenPlugin(srv *httptest.Server, l *logger.Logger) *Plugin {
	client := srv.Client()
	client.CheckRedirect = redirectPolicy(l)
	return New(WithHTTPClient(client), WithLogger(l))
}

func servePDF(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/pdf")
	_, _ = w.Write(tokenTestPDF)
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// ------------------------------------------------------- tokenRequestURL

func TestTokenRequestURL(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string // "" means ErrTokenNotAttachable
	}{
		{"plain", "https://c.example.cl/webservice/pluginfile.php/1/a.pdf", "https://c.example.cl/webservice/pluginfile.php/1/a.pdf?token=TKN"},
		{"keeps query", "https://c.example.cl/webservice/pluginfile.php/1/a.pdf?forcedownload=1", "https://c.example.cl/webservice/pluginfile.php/1/a.pdf?forcedownload=1&token=TKN"},
		{"replaces old token", "https://c.example.cl/webservice/pluginfile.php/1/a.pdf?token=OLD&x=1&wstoken=OLD2", "https://c.example.cl/webservice/pluginfile.php/1/a.pdf?x=1&token=TKN"},
		{"drops fragment", "https://c.example.cl/webservice/pluginfile.php/1/a.pdf#f", "https://c.example.cl/webservice/pluginfile.php/1/a.pdf?token=TKN"},
		{"prefix path", "https://c.example.cl/moodle/webservice/pluginfile.php/1/a.pdf", "https://c.example.cl/moodle/webservice/pluginfile.php/1/a.pdf?token=TKN"},
		{"other host", "https://evil.example.com/webservice/pluginfile.php/1/a.pdf", ""},
		{"subdomain of campus", "https://x.c.example.cl/webservice/pluginfile.php/1/a.pdf", ""},
		{"http", "http://c.example.cl/webservice/pluginfile.php/1/a.pdf", ""},
		{"other path", "https://c.example.cl/pluginfile.php/1/a.pdf", ""},
		{"login page", "https://c.example.cl/login/index.php", ""},
		{"no trailing slash after pluginfile.php", "https://c.example.cl/webservice/pluginfile.php", ""},
		{"path only in query", "https://c.example.cl/x?u=/webservice/pluginfile.php/1", ""},
		{"credentials", "https://u:p@c.example.cl/webservice/pluginfile.php/1/a.pdf", ""},
		{"no host", "https:///webservice/pluginfile.php/1/a.pdf", ""},
		{"relative", "/webservice/pluginfile.php/1/a.pdf", ""},
		{"empty", "", ""},
		{"unparseable", "https://c.example.cl/%zz/webservice/pluginfile.php/", ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := tokenRequestURL(tc.in, "TKN", "c.example.cl")
			if tc.want == "" {
				if !errors.Is(err, ErrTokenNotAttachable) || got != "" {
					t.Errorf("got %q, %v; want ErrTokenNotAttachable", got, err)
				}
				return
			}
			if err != nil || got != tc.want {
				t.Errorf("got %q, %v; want %q", got, err, tc.want)
			}
		})
	}
}

func TestTokenRequestURL_EscapesToken(t *testing.T) {
	got, err := tokenRequestURL("https://c.example.cl/webservice/pluginfile.php/1/a.pdf", "a+b/c=d&e f", "c.example.cl")
	if err != nil {
		t.Fatal(err)
	}
	u, _ := url.Parse(got)
	if u.Query().Get("token") != "a+b/c=d&e f" || strings.Count(u.RawQuery, "&") != 0 {
		t.Errorf("query = %q", u.RawQuery)
	}
}

// -------------------------------------------------------------- Download

func TestDownload_TokenAttachedOnlyAsQueryAndURLsStayClean(t *testing.T) {
	var gotToken, gotCookie string
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotToken = r.URL.Query().Get("token")
		gotCookie = r.Header.Get("Cookie")
		servePDF(w)
	}))
	defer srv.Close()

	task := domain.Task{ID: 7, URL: srv.URL + pluginfileTestPath + "?forcedownload=1", Token: testToken, TokenHost: hostOf(srv.URL + pluginfileTestPath + "?forcedownload=1"), OutputDir: t.TempDir()}
	var progressURLs []string
	res, err := tokenPlugin(srv, nil).Download(context.Background(), task, func(u domain.ProgressUpdate) {
		progressURLs = append(progressURLs, u.URL)
	})
	if err != nil {
		t.Fatal(err)
	}
	if gotToken != testToken || gotCookie != "" {
		t.Errorf("server saw token=%q cookie=%q", gotToken, gotCookie)
	}
	if res.URL != task.URL || strings.Contains(res.URL, "token") {
		t.Errorf("Result.URL = %q", res.URL)
	}
	if len(progressURLs) == 0 {
		t.Fatal("no progress")
	}
	for _, u := range progressURLs {
		if u != task.URL {
			t.Errorf("progress URL = %q", u)
		}
	}
	if res.Filename != "Guia.pdf" || !fileExists(filepath.Join(task.OutputDir, "Guia.pdf")) {
		t.Errorf("result = %+v", res)
	}
	if strings.Contains(task.URL, testToken) {
		t.Error("Task.URL was modified")
	}
}

func TestDownload_ExistingTokenInURLIsReplaced(t *testing.T) {
	var rawQuery string
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rawQuery = r.URL.RawQuery
		servePDF(w)
	}))
	defer srv.Close()
	task := domain.Task{ID: 1, URL: srv.URL + pluginfileTestPath + "?token=OLDTOKEN&forcedownload=1", Token: testToken, TokenHost: hostOf(srv.URL + pluginfileTestPath + "?token=OLDTOKEN&forcedownload=1"), OutputDir: t.TempDir()}
	if _, err := tokenPlugin(srv, nil).Download(context.Background(), task, nil); err != nil {
		t.Fatal(err)
	}
	if rawQuery != "forcedownload=1&token="+testToken {
		t.Errorf("query = %q", rawQuery)
	}
}

func TestDownload_TokenNotAttachedToIneligibleURLs(t *testing.T) {
	var hits atomic.Int32
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { hits.Add(1); servePDF(w) })
	tlsSrv := httptest.NewTLSServer(handler)
	defer tlsSrv.Close()
	plain := httptest.NewServer(handler)
	defer plain.Close()

	for name, u := range map[string]string{
		"http":               plain.URL + pluginfileTestPath,
		"not a pluginfile":   tlsSrv.URL + "/pluginfile.php/1/a.pdf",
		"login":              tlsSrv.URL + "/login/index.php",
		"relative":           "/webservice/pluginfile.php/1/a.pdf",
		"credentials in url": strings.Replace(tlsSrv.URL, "https://", "https://u:p@", 1) + pluginfileTestPath,
	} {
		t.Run(name, func(t *testing.T) {
			_, err := tokenPlugin(tlsSrv, nil).Download(context.Background(),
				domain.Task{ID: 1, URL: u, Token: testToken, TokenHost: hostOf(u), OutputDir: t.TempDir()}, nil)
			if !errors.Is(err, ErrTokenNotAttachable) {
				t.Fatalf("err = %v", err)
			}
			if strings.Contains(err.Error(), testToken) {
				t.Error("error leaks the token")
			}
			if !strings.Contains(err.Error(), "https") {
				t.Errorf("error should explain the rule in Spanish: %v", err)
			}
		})
	}
	if hits.Load() != 0 {
		t.Errorf("%d requests were sent for ineligible URLs", hits.Load())
	}
}

func TestDownload_CookieFlowUnchangedWithoutToken(t *testing.T) {
	var rawQuery, cookie string
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rawQuery, cookie = r.URL.RawQuery, r.Header.Get("Cookie")
		servePDF(w)
	}))
	defer srv.Close()
	// no pluginfile path needed and a JSON-looking body is not inspected
	task := domain.Task{ID: 1, URL: srv.URL + "/pluginfile.php/1/a.pdf", Cookie: "MoodleSession=abc", OutputDir: t.TempDir()}
	if _, err := tokenPlugin(srv, nil).Download(context.Background(), task, nil); err != nil {
		t.Fatal(err)
	}
	if rawQuery != "" || cookie != "MoodleSession=abc" {
		t.Errorf("query=%q cookie=%q", rawQuery, cookie)
	}
}

// ------------------------------------------------- JSON error in the body

func TestDownload_JSONErrorBody(t *testing.T) {
	invalid := `{"error":"Token inválido","errorcode":"invalidtoken","stacktrace":null,"debuginfo":null,"reproductionlink":null}`
	tests := []struct {
		name     string
		file     string
		ctype    string
		body     string
		wantAuth bool
		wantOK   bool
		wantSub  string
	}{
		{name: "invalidtoken json content type", file: "Guia.pdf", ctype: "application/json", body: invalid, wantAuth: true},
		{name: "invalidtoken html content type", file: "Guia.pdf", ctype: "text/html", body: invalid, wantAuth: true},
		{name: "invalidtoken on a pptx", file: "Clase.pptx", ctype: "application/json", body: invalid, wantAuth: true},
		{name: "exception style", file: "Guia.pdf", ctype: "application/json", body: `{"exception":"webservice_access_exception","errorcode":"accessexception","message":"x"}`, wantAuth: true},
		{name: "padded with whitespace", file: "Guia.pdf", ctype: "application/json", body: "  \n" + invalid + "\n", wantAuth: true},
		{name: "other errorcode is not auth", file: "Guia.pdf", ctype: "application/json", body: `{"error":"x","errorcode":"filenotfound"}`, wantSub: "filenotfound"},
		{name: "hostile errorcode sanitised", file: "Guia.pdf", ctype: "application/json", body: `{"error":"x","errorcode":"a b<c>"}`, wantSub: "desconocido"},
		{name: "json file without errorcode is a normal download", file: "datos.json", ctype: "application/json", body: `{"a":1}`, wantOK: true},
		{name: "errorcode without message is a normal download", file: "datos.json", ctype: "application/json", body: `{"errorcode":"x"}`, wantOK: true},
		{name: "non string errorcode is a normal download", file: "datos.json", ctype: "application/json", body: `{"errorcode":5,"error":"x"}`, wantOK: true},
		{name: "empty body", file: "vacio.txt", ctype: "text/plain", body: "", wantOK: true},
		{name: "big json is a file", file: "grande.json", ctype: "application/json",
			body: `{"error":"x","errorcode":"invalidtoken","pad":"` + strings.Repeat("a", 2*maxJSONErrorBody) + `"}`, wantOK: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", tc.ctype)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer srv.Close()
			dir := t.TempDir()
			task := domain.Task{ID: 1, URL: srv.URL + "/webservice/pluginfile.php/1/mod_resource/content/1/" + tc.file, Token: testToken, TokenHost: hostOf(srv.URL + "/webservice/pluginfile.php/1/mod_resource/content/1/" + tc.file), OutputDir: dir}
			res, err := tokenPlugin(srv, nil).Download(context.Background(), task, nil)
			if tc.wantOK {
				if err != nil || res == nil || !fileExists(filepath.Join(dir, tc.file)) {
					t.Fatalf("res=%v err=%v", res, err)
				}
				return
			}
			if err == nil {
				t.Fatal("expected an error")
			}
			if errors.Is(err, ErrNotPDF) {
				t.Errorf("must not be reported as ErrNotPDF: %v", err)
			}
			if got := errors.Is(err, domain.ErrAuthenticationFailed); got != tc.wantAuth {
				t.Errorf("auth error = %v, want %v (%v)", got, tc.wantAuth, err)
			}
			if tc.wantAuth && !domain.IsFatalAuth(err) {
				t.Error("must trip the circuit breaker")
			}
			if tc.wantSub != "" && !strings.Contains(err.Error(), tc.wantSub) {
				t.Errorf("err = %v, want %q", err, tc.wantSub)
			}
			entries, _ := os.ReadDir(dir)
			if len(entries) != 0 {
				t.Errorf("files left behind: %v", entries)
			}
		})
	}
}

func TestDownload_PDFStillValidatedAfterJSONCheck(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("<html>login</html>"))
	}))
	defer srv.Close()
	_, err := tokenPlugin(srv, nil).Download(context.Background(),
		domain.Task{ID: 1, URL: srv.URL + pluginfileTestPath, Token: testToken, TokenHost: hostOf(srv.URL + pluginfileTestPath), OutputDir: t.TempDir()}, nil)
	if !errors.Is(err, ErrNotPDF) {
		t.Errorf("err = %v, want ErrNotPDF", err)
	}
}

// --------------------------------------------------------------- scrubbing

func TestDownload_ErrorsAreScrubbed(t *testing.T) {
	check := func(t *testing.T, err error) {
		t.Helper()
		if err == nil {
			t.Fatal("expected an error")
		}
		msg := err.Error()
		if strings.Contains(msg, testToken) || strings.Contains(msg, "token=") {
			t.Errorf("error leaks the token: %s", msg)
		}
		var ue *url.Error
		if errors.As(err, &ue) {
			t.Error("a *url.Error must not be reachable")
		}
	}
	task := func(srv *httptest.Server, dir string) domain.Task {
		return domain.Task{ID: 1, URL: srv.URL + pluginfileTestPath, Token: testToken, TokenHost: hostOf(srv.URL + pluginfileTestPath), OutputDir: dir}
	}

	t.Run("connection closed by server", func(t *testing.T) {
		srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			conn, _, _ := w.(http.Hijacker).Hijack()
			_ = conn.Close()
		}))
		defer srv.Close()
		_, err := tokenPlugin(srv, nil).Download(context.Background(), task(srv, t.TempDir()), nil)
		check(t, err)
		if !strings.Contains(err.Error(), srv.URL+pluginfileTestPath) {
			t.Errorf("the clean URL should remain readable: %v", err)
		}
	})
	t.Run("dial error", func(t *testing.T) {
		srv := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
		tk := task(srv, t.TempDir())
		p := tokenPlugin(srv, nil)
		srv.Close()
		_, err := p.Download(context.Background(), tk, nil)
		check(t, err)
	})
	t.Run("http status", func(t *testing.T) {
		srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "boom "+testToken, http.StatusInternalServerError)
		}))
		defer srv.Close()
		_, err := tokenPlugin(srv, nil).Download(context.Background(), task(srv, t.TempDir()), nil)
		check(t, err)
		if !errors.Is(err, ErrUnexpectedStatus) {
			t.Errorf("err = %v", err)
		}
	})
	t.Run("forbidden is fatal auth", func(t *testing.T) {
		srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusForbidden)
		}))
		defer srv.Close()
		_, err := tokenPlugin(srv, nil).Download(context.Background(), task(srv, t.TempDir()), nil)
		check(t, err)
		if !domain.IsFatalAuth(err) {
			t.Errorf("err = %v", err)
		}
	})
	t.Run("redirect to login", func(t *testing.T) {
		srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, "/login/index.php?token="+testToken, http.StatusSeeOther)
		}))
		defer srv.Close()
		_, err := tokenPlugin(srv, nil).Download(context.Background(), task(srv, t.TempDir()), nil)
		check(t, err)
		if !errors.Is(err, domain.ErrAuthenticationFailed) || !domain.IsFatalAuth(err) {
			t.Errorf("err = %v", err)
		}
	})
	t.Run("context cancelled keeps errors.Is", func(t *testing.T) {
		started := make(chan struct{})
		release := make(chan struct{})
		srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			close(started)
			<-release
		}))
		defer srv.Close()
		defer close(release)
		ctx, cancel := context.WithCancel(context.Background())
		errc := make(chan error, 1)
		go func() {
			_, err := tokenPlugin(srv, nil).Download(ctx, task(srv, t.TempDir()), nil)
			errc <- err
		}()
		<-started
		cancel()
		err := <-errc
		check(t, err)
		if !errors.Is(err, context.Canceled) {
			t.Errorf("errors.Is(context.Canceled) lost: %v", err)
		}
	})
	t.Run("stalled stream", func(t *testing.T) {
		release := make(chan struct{})
		srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Length", "5000")
			_, _ = w.Write([]byte("%PDF-"))
			w.(http.Flusher).Flush()
			<-release
		}))
		defer srv.Close()
		defer close(release)
		client := srv.Client()
		p := New(WithHTTPClient(client), WithInactivityTimeout(50*time.Millisecond))
		_, err := p.Download(context.Background(), task(srv, t.TempDir()), nil)
		check(t, err)
		if !errors.Is(err, ErrInactivityTimeout) {
			t.Errorf("err = %v", err)
		}
	})
}

func TestScrubTokenError(t *testing.T) {
	task := domain.Task{URL: "https://c.example.cl/webservice/pluginfile.php/1/a.pdf", Token: "a+b/c=d&e f", TokenHost: hostOf("https://c.example.cl/webservice/pluginfile.php/1/a.pdf")}
	reqURL, _ := tokenRequestURL(task.URL, task.Token, "c.example.cl")

	t.Run("unrelated error returned as is", func(t *testing.T) {
		e := errors.New("disk full")
		if got := scrubTokenError(e, task); got != e {
			t.Errorf("got %v", got)
		}
	})
	t.Run("url error rewritten", func(t *testing.T) {
		e := &url.Error{Op: "Get", URL: reqURL, Err: fmt.Errorf("tls: %w", os.ErrDeadlineExceeded)}
		got := scrubTokenError(e, task)
		if strings.Contains(got.Error(), "a+b") || strings.Contains(got.Error(), "a%2Bb") || strings.Contains(got.Error(), "token=") {
			t.Errorf("got %q", got.Error())
		}
		if !strings.Contains(got.Error(), task.URL) {
			t.Errorf("clean URL missing: %q", got.Error())
		}
		if !errors.Is(got, os.ErrDeadlineExceeded) {
			t.Error("errors.Is must still see the cause")
		}
		var ue *url.Error
		if errors.As(got, &ue) {
			t.Error("tainted *url.Error reachable through errors.As")
		}
		if domain.IsFatalAuth(got) {
			t.Error("not an auth error")
		}
	})
	t.Run("escaped forms", func(t *testing.T) {
		for _, form := range []string{task.Token, url.QueryEscape(task.Token), url.PathEscape(task.Token)} {
			got := scrubTokenError(errors.New("x "+form+" y"), task).Error()
			if strings.Contains(got, form) || got != "x REDACTED y" {
				t.Errorf("form %q -> %q", form, got)
			}
		}
	})
	t.Run("stray token param", func(t *testing.T) {
		got := scrubTokenError(errors.New(`Get "https://x/y?token=other&a=1"`), task).Error()
		if strings.Contains(got, "other") {
			t.Errorf("got %q", got)
		}
	})
	t.Run("fatal auth preserved", func(t *testing.T) {
		got := scrubTokenError(fmt.Errorf("%w: %s", ErrAuthenticationFailed, task.Token), task)
		if !domain.IsFatalAuth(got) || !errors.Is(got, domain.ErrAuthenticationFailed) {
			t.Errorf("got %v", got)
		}
	})
}

// ---------------------------------------------------------------- redirects

// twoHosts starts two TLS servers and a client that resolves any host name to
// them by port, so redirects can really change host ("127.0.0.1" -> "example.com").
func redirectClient(t *testing.T, base *httptest.Server, l *logger.Logger) *http.Client {
	t.Helper()
	tr := base.Client().Transport.(*http.Transport).Clone()
	tr.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		_, port, _ := net.SplitHostPort(addr)
		return (&net.Dialer{}).DialContext(ctx, network, "127.0.0.1:"+port)
	}
	return &http.Client{Transport: tr, CheckRedirect: redirectPolicy(l)}
}

func TestDownload_TokenNotSentToOtherHostOnRedirect(t *testing.T) {
	var otherReqPtr atomic.Pointer[http.Request]
	other := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		otherReqPtr.Store(r.Clone(context.Background()))
		servePDF(w)
	}))
	defer other.Close()
	_, otherPort, _ := net.SplitHostPort(other.Listener.Addr().String())

	var firstToken string
	origin := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		firstToken = r.URL.Query().Get("token")
		http.Redirect(w, r, "https://example.com:"+otherPort+"/webservice/pluginfile.php/9/x.pdf", http.StatusFound)
	}))
	defer origin.Close()

	var buf bytes.Buffer
	l := logger.NewWriter(&buf)
	p := New(WithHTTPClient(redirectClient(t, origin, l)), WithLogger(l))
	dir := t.TempDir()
	res, err := p.Download(context.Background(), domain.Task{ID: 3, URL: origin.URL + pluginfileTestPath, Token: testToken, TokenHost: hostOf(origin.URL + pluginfileTestPath), OutputDir: dir}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if firstToken != testToken {
		t.Fatal("the first hop must carry the token")
	}
	otherReq := otherReqPtr.Load()
	if otherReq == nil {
		t.Fatal("redirect was not followed")
	}
	if otherReq.Host == "" || strings.HasPrefix(otherReq.Host, "127.0.0.1") {
		t.Fatalf("redirect did not change host: %q", otherReq.Host)
	}
	if strings.Contains(otherReq.URL.String(), testToken) || strings.Contains(otherReq.URL.RawQuery, "token") {
		t.Errorf("token reached the other host in the URL: %s", otherReq.URL)
	}
	for k, v := range otherReq.Header {
		if strings.Contains(strings.Join(v, ","), testToken) {
			t.Errorf("token reached the other host in header %s", k)
		}
	}
	if res.URL != origin.URL+pluginfileTestPath {
		t.Errorf("Result.URL = %q", res.URL)
	}
	out := buf.String()
	if strings.Contains(out, testToken) || !strings.Contains(out, "task redirect") || !strings.Contains(out, "token=REDACTED") {
		t.Errorf("redirect log not redacted: %s", out)
	}
}

func TestDownload_TokenNotCopiedOnSameHostRedirect(t *testing.T) {
	var queries []string
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		queries = append(queries, r.URL.RawQuery)
		if strings.HasSuffix(r.URL.Path, "/first.pdf") {
			http.Redirect(w, r, "/webservice/pluginfile.php/2/second.pdf", http.StatusFound)
			return
		}
		servePDF(w)
	}))
	defer srv.Close()
	task := domain.Task{ID: 1, URL: srv.URL + "/webservice/pluginfile.php/1/first.pdf", Token: testToken, TokenHost: hostOf(srv.URL + "/webservice/pluginfile.php/1/first.pdf"), OutputDir: t.TempDir()}
	if _, err := tokenPlugin(srv, nil).Download(context.Background(), task, nil); err != nil {
		t.Fatal(err)
	}
	if len(queries) != 2 || queries[0] != "token="+testToken || queries[1] != "" {
		t.Errorf("queries = %q", queries)
	}
}

// -------------------------------------------------------------- the canary

const (
	canaryPassword = "CANARY-PASSWORD-Zq93xK!"
	canaryToken    = "CANARYTOKEN7f3a9c1d5e8b2046aa"
)

// newCanaryCampus is a small but complete fake campus: login, web service and
// pluginfile, with every error shape the real one produces.
func newCanaryCampus(t *testing.T) *httptest.Server {
	t.Helper()
	var srv *httptest.Server
	mux := http.NewServeMux()
	mux.HandleFunc("/login/token.php", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		if r.PostForm.Get("password") != canaryPassword {
			writeJSON(w, `{"error":"Usuario o contraseña incorrectos","errorcode":"invalidlogin","stacktrace":null,"debuginfo":null,"reproductionlink":null}`)
			return
		}
		writeJSON(w, `{"token":"`+canaryToken+`"}`)
	})
	mux.HandleFunc("/webservice/rest/server.php", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		if r.PostForm.Get("wstoken") != canaryToken || r.URL.RawQuery != "" {
			writeJSON(w, `{"exception":"moodle_exception","errorcode":"invalidtoken","message":"Token inválido"}`)
			return
		}
		switch r.PostForm.Get("wsfunction") {
		case "core_webservice_get_site_info":
			writeJSON(w, string(fixture(t, "site_info.json", srv)))
		case "core_enrol_get_users_courses":
			writeJSON(w, string(fixture(t, "courses.json", srv)))
		default:
			writeJSON(w, string(fixture(t, "contents.json", srv)))
		}
	})
	mux.HandleFunc("/webservice/pluginfile.php/", func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/redir/"):
			http.Redirect(w, r, "/login/index.php", http.StatusSeeOther)
		case r.URL.Query().Get("token") != canaryToken:
			writeJSON(w, `{"error":"Token inválido","errorcode":"invalidtoken","stacktrace":null,"debuginfo":null,"reproductionlink":null}`)
		default:
			servePDF(w)
		}
	})
	srv = httptest.NewTLSServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

// TestCanary_NoSecretEverLeaves runs login, API calls and downloads (happy and
// failing paths) with recognisable secrets and checks they appear neither in
// the log, nor in any error, nor in any Task/Result/progress URL.
func TestCanary_NoSecretEverLeaves(t *testing.T) {
	srv := newCanaryCampus(t)
	var logBuf bytes.Buffer
	l := logger.NewWriter(&logBuf)
	ctx := context.Background()

	var observed []string // every string that is visible outside the plugin
	note := func(what string, err error) {
		if err != nil {
			observed = append(observed, what+": "+err.Error())
		}
	}

	api, err := NewAPIClient(srv.URL, WithAPIHTTPClient(srv.Client()), WithAPILogger(l),
		WithAPISleep(func(context.Context, time.Duration) error { return nil }))
	if err != nil {
		t.Fatal(err)
	}

	// failing login
	_, err = api.Login(ctx, "alumno", "wrong-password")
	note("bad login", err)
	if !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("bad login err = %v", err)
	}

	// happy path
	token, err := api.Login(ctx, "alumno", canaryPassword)
	if err != nil || token != canaryToken {
		t.Fatalf("login: %v", err)
	}
	info, err := api.SiteInfo(ctx, token)
	note("site info", err)
	if err != nil {
		t.Fatal(err)
	}
	courses, err := api.Courses(ctx, token, info.UserID)
	note("courses", err)
	if err != nil || len(courses) == 0 {
		t.Fatalf("courses: %v", err)
	}
	sections, err := api.Contents(ctx, token, courses[0].ID)
	note("contents", err)
	if err != nil {
		t.Fatal(err)
	}

	// bad token on the API
	_, err = api.SiteInfo(ctx, "bad-token-value")
	note("bad token", err)
	if !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("bad token err = %v", err)
	}

	plugin := tokenPlugin(srv, l)
	dir := t.TempDir()
	downloaded := 0
	for _, s := range sections {
		for _, f := range s.Files {
			observed = append(observed, "api file url: "+f.URL, "api file name: "+f.Name+" "+f.SubDir)
			task := domain.Task{ID: downloaded + 1, URL: f.URL, Token: token, TokenHost: hostOf(f.URL), OutputDir: filepath.Join(dir, f.SubDir)}
			l.LogTaskStart(task.ID, task.URL)
			res, err := plugin.Download(ctx, task, func(u domain.ProgressUpdate) {
				observed = append(observed, "progress url: "+u.URL)
			})
			note("download", err)
			if err != nil {
				t.Errorf("download of %s failed: %v", f.FileName, err)
			}
			if res != nil {
				observed = append(observed, "result url: "+res.URL, "result file: "+res.Filename)
			}
			observed = append(observed, "task url: "+task.URL)
			downloaded++
		}
	}
	if downloaded == 0 {
		t.Fatal("no files to download")
	}

	// failing downloads: wrong token (JSON error body), redirect to login, dial error
	for name, tk := range map[string]domain.Task{
		"wrong token": {ID: 90, URL: srv.URL + pluginfileTestPath, Token: "WRONG-" + canaryToken, TokenHost: hostOf(srv.URL + pluginfileTestPath), OutputDir: dir},
		"login redir": {ID: 91, URL: srv.URL + "/webservice/pluginfile.php/redir/a.pdf", Token: canaryToken, TokenHost: hostOf(srv.URL + "/webservice/pluginfile.php/redir/a.pdf"), OutputDir: dir},
	} {
		res, err := plugin.Download(ctx, tk, nil)
		if err == nil || res != nil {
			t.Fatalf("%s: expected failure", name)
		}
		note(name, err)
		if !domain.IsFatalAuth(err) {
			t.Errorf("%s: want fatal auth, got %v", name, err)
		}
	}
	dead := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	deadPlugin := tokenPlugin(dead, l)
	deadURL := dead.URL + pluginfileTestPath
	dead.Close()
	_, err = deadPlugin.Download(ctx, domain.Task{ID: 92, URL: deadURL, Token: canaryToken, TokenHost: hostOf(deadURL), OutputDir: dir}, nil)
	note("dial error", err)
	if err == nil {
		t.Fatal("dial error expected")
	}
	// ineligible URL
	_, err = plugin.Download(ctx, domain.Task{ID: 93, URL: srv.URL + "/login/index.php", Token: canaryToken, TokenHost: hostOf(srv.URL + "/login/index.php"), OutputDir: dir}, nil)
	note("ineligible", err)

	// Accidental logging by any code path.
	tokenURL := srv.URL + pluginfileTestPath + "?token=" + canaryToken
	l.LogTaskStart(1, tokenURL)
	l.LogTaskRedirect(1, tokenURL, tokenURL+"&wstoken="+canaryToken, 302)
	l.LogTaskError(1, tokenURL, fmt.Errorf(`Get %q: boom`, tokenURL))
	l.LogTaskSuccess(1, "token="+canaryToken, 1)
	l.Info("manual", "url", tokenURL, "token", canaryToken, "password", canaryPassword,
		"err", fmt.Errorf("failed %s", tokenURL), slog.Any("typed", logger.Token(canaryToken)))
	l.Printf("printf %s password=%s", tokenURL, canaryPassword)

	logOut := logBuf.String()
	if !strings.Contains(logOut, "task response") || !strings.Contains(logOut, "moodle api call") {
		t.Fatalf("the log is unexpectedly empty (test would be vacuous):\n%s", logOut)
	}
	for _, secret := range []string{canaryToken, canaryPassword, "LEAKEDTOKENINFILEURL"} {
		if strings.Contains(logOut, secret) {
			t.Errorf("log leaks %q", secret[:6]+"…")
		}
		for _, o := range observed {
			if strings.Contains(o, secret) {
				t.Errorf("observable output leaks %q: %s", secret[:6]+"…", o)
			}
		}
	}
	for _, o := range observed {
		if strings.Contains(o, "token=") {
			t.Errorf("observable output carries a token parameter: %s", o)
		}
	}
}

// hostOf returns the host[:port] of rawURL, for pinning a test token to it.
func hostOf(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	return u.Host
}

func TestTokenRequestURLEmptyHostNeverAttaches(t *testing.T) {
	if got, err := tokenRequestURL("https://c.example.cl/webservice/pluginfile.php/1/a.pdf", "TKN", ""); !errors.Is(err, ErrTokenNotAttachable) || got != "" {
		t.Errorf("got %q, %v; want ErrTokenNotAttachable", got, err)
	}
}

func TestRefererFor(t *testing.T) {
	if got := refererFor("https://c.example.cl/webservice/pluginfile.php/1/a.pdf?x=1"); got != "https://c.example.cl/" {
		t.Errorf("got %q", got)
	}
	if got := refererFor("/relative"); got != "" {
		t.Errorf("got %q", got)
	}
}

func TestDownload_TaskFileNameOverridesTheServerName(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { servePDF(w) }))
	defer srv.Close()
	u := srv.URL + pluginfileTestPath
	task := domain.Task{ID: 1, URL: u, Token: testToken, TokenHost: hostOf(u), OutputDir: t.TempDir(), FileName: "Guia (2).pdf"}
	res, err := tokenPlugin(srv, nil).Download(context.Background(), task, nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.Filename != "Guia (2).pdf" || !fileExists(filepath.Join(task.OutputDir, "Guia (2).pdf")) {
		t.Errorf("result = %+v", res)
	}
	if fileExists(filepath.Join(task.OutputDir, "Guia.pdf")) {
		t.Error("the server name must not be used when FileName is set")
	}
}

func TestDownload_TaskFileNameIsSanitized(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { servePDF(w) }))
	defer srv.Close()
	u := srv.URL + pluginfileTestPath
	dir := t.TempDir()
	task := domain.Task{ID: 1, URL: u, Token: testToken, TokenHost: hostOf(u), OutputDir: dir, FileName: "../../evil.pdf"}
	res, err := tokenPlugin(srv, nil).Download(context.Background(), task, nil)
	if err != nil {
		t.Fatal(err)
	}
	if strings.ContainsAny(res.Filename, `/\`) || !fileExists(filepath.Join(dir, res.Filename)) {
		t.Errorf("result = %+v", res)
	}
}

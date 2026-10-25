package moodle_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sebasinmas/MooFetch/internal/domain"
	"github.com/sebasinmas/MooFetch/internal/plugins/moodle"
)

const scanCookie = "MoodleSessionabc=super_secret_scan_cookie"

func fixture(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("reading fixture %s: %v", name, err)
	}
	return string(b)
}

// recorder is a fake Moodle server that records every request and the sleeps
// the scanner asked for. Handlers run on distinct goroutines, hence the mutex.
type recorder struct {
	mu        sync.Mutex
	paths     []string
	cookies   []string
	agents    []string
	inFlight  atomic.Int32
	maxFlight atomic.Int32
	sleeps    []time.Duration
}

func (r *recorder) record(req *http.Request) {
	n := r.inFlight.Add(1)
	for {
		m := r.maxFlight.Load()
		if n <= m || r.maxFlight.CompareAndSwap(m, n) {
			break
		}
	}
	r.mu.Lock()
	r.paths = append(r.paths, req.URL.RequestURI())
	r.cookies = append(r.cookies, req.Header.Get("Cookie"))
	r.agents = append(r.agents, req.Header.Get("User-Agent"))
	r.mu.Unlock()
}

func (r *recorder) done() { r.inFlight.Add(-1) }

func (r *recorder) requests() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.paths)
}

func (r *recorder) sleep(_ context.Context, d time.Duration) error {
	r.mu.Lock()
	r.sleeps = append(r.sleeps, d)
	r.mu.Unlock()
	return nil
}

func newMoodleServer(t *testing.T, rec *recorder, h http.HandlerFunc) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec.record(r)
		defer rec.done()
		h(w, r)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func serveCourse(t *testing.T, host func() string) http.HandlerFunc {
	course := fixture(t, "course.html")
	f201 := fixture(t, "folder201.html")
	f202 := fixture(t, "folder202.html")
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		switch {
		case r.URL.Path == "/course/view.php":
			_, _ = fmt.Fprint(w, strings.ReplaceAll(course, "HOST", host()))
		case r.URL.Path == "/mod/folder/view.php" && r.URL.Query().Get("id") == "201":
			_, _ = fmt.Fprint(w, f201)
		case r.URL.Path == "/mod/folder/view.php" && r.URL.Query().Get("id") == "202":
			_, _ = fmt.Fprint(w, f202)
		default:
			http.NotFound(w, r)
		}
	}
}

func courseServer(t *testing.T, rec *recorder) *httptest.Server {
	var srv *httptest.Server
	srv = newMoodleServer(t, rec, serveCourse(t, func() string { return srv.Listener.Addr().String() }))
	return srv
}

func baseOpts(rec *recorder) moodle.ScanOptions {
	return moodle.ScanOptions{
		Delay:  2 * time.Second,
		Jitter: 500 * time.Millisecond,
		Sleep:  rec.sleep,
	}
}

func TestScanPage_CourseWithFoldersAndDedupe(t *testing.T) {
	rec := &recorder{}
	srv := courseServer(t, rec)

	links, err := moodle.New().ScanPage(context.Background(), srv.URL+"/course/view.php?id=5", scanCookie, baseOpts(rec))
	if err != nil {
		t.Fatalf("ScanPage: %v", err)
	}

	want := []moodle.Link{
		{URL: srv.URL + "/mod/resource/view.php?id=101", Title: "Apunte 1.1", Kind: moodle.KindResource},
		{URL: srv.URL + "/mod/resource/view.php?id=102", Title: "Guía 2", Kind: moodle.KindResource},
		{URL: srv.URL + "/mod/folder/view.php?id=201", Title: "Material extra", Kind: moodle.KindFolder},
		{URL: srv.URL + "/pluginfile.php/60/mod_folder/content/0/Lectura%20A.pdf?forcedownload=1", Title: "Lectura A.pdf", Kind: moodle.KindPluginfile},
		{URL: srv.URL + "/pluginfile.php/60/mod_folder/content/0/sub/Lectura%20B.pdf?forcedownload=1", Title: "Lectura B.pdf", Kind: moodle.KindPluginfile},
		{URL: srv.URL + "/pluginfile.php/55/mod_resource/content/1/Pauta%20Control.pdf?forcedownload=1", Title: "Pauta Control", Kind: moodle.KindPluginfile},
		{URL: srv.URL + "/pluginfile.php/55/mod_resource/content/1/Imagen.png", Title: "Imagen.png", Kind: moodle.KindPluginfile},
		{URL: srv.URL + "/mod/folder/view.php?id=202", Title: "Carpeta B", Kind: moodle.KindFolder},
		{URL: srv.URL + "/pluginfile.php/61/mod_folder/content/0/Datos.zip", Title: "Datos.zip", Kind: moodle.KindPluginfile},
	}
	if len(links) != len(want) {
		t.Fatalf("got %d links, want %d:\n%+v", len(links), len(want), links)
	}
	for i := range want {
		if links[i] != want[i] {
			t.Errorf("link %d:\n got  %+v\n want %+v", i, links[i], want[i])
		}
	}

	// Page + 2 folders; the subfolder inside folder 201 is not followed.
	if got := rec.requests(); got != 3 {
		t.Fatalf("requests = %d, want 3 (%v)", got, rec.paths)
	}
	for i, c := range rec.cookies {
		if c != scanCookie {
			t.Errorf("request %d Cookie = %q, want the session cookie", i, c)
		}
		if !strings.HasPrefix(rec.agents[i], "MooFetch/") {
			t.Errorf("request %d User-Agent = %q, want identifiable MooFetch UA", i, rec.agents[i])
		}
	}
	if rec.maxFlight.Load() != 1 {
		t.Errorf("max concurrent requests = %d, want strictly sequential", rec.maxFlight.Load())
	}

	// No sleep before the first request, one per following request.
	if len(rec.sleeps) != 2 {
		t.Fatalf("sleeps = %v, want 2 (between 3 requests)", rec.sleeps)
	}
	for _, d := range rec.sleeps {
		if d < 2*time.Second || d >= 2500*time.Millisecond {
			t.Errorf("sleep %v outside [Delay, Delay+Jitter)", d)
		}
	}
}

func TestScanPage_MaxPagesBudget(t *testing.T) {
	rec := &recorder{}
	srv := courseServer(t, rec)
	opts := baseOpts(rec)
	opts.MaxPages = 2 // initial page + only the first folder

	links, err := moodle.New().ScanPage(context.Background(), srv.URL+"/course/view.php", scanCookie, opts)
	if err != nil {
		t.Fatalf("ScanPage: %v", err)
	}
	if got := rec.requests(); got != 2 {
		t.Fatalf("requests = %d, want 2 (%v)", got, rec.paths)
	}
	for _, l := range links {
		if strings.Contains(l.URL, "Datos.zip") {
			t.Errorf("folder 202 must not be expanded once the budget is spent: %+v", l)
		}
	}
	// The unexpanded folder itself is still reported.
	var folders int
	for _, l := range links {
		if l.Kind == moodle.KindFolder {
			folders++
		}
	}
	if folders != 2 {
		t.Errorf("folders reported = %d, want 2", folders)
	}
}

func TestScanPage_DefaultsSendIdentifiableUserAgent(t *testing.T) {
	rec := &recorder{}
	srv := newMoodleServer(t, rec, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprint(w, `<a href="/mod/resource/view.php?id=1">Uno</a>`)
	})
	moodle.UserAgentVersion = "9.9.9"
	t.Cleanup(func() { moodle.UserAgentVersion = "dev" })

	links, err := moodle.New().ScanPage(context.Background(), srv.URL+"/c", "", moodle.ScanOptions{})
	if err != nil || len(links) != 1 {
		t.Fatalf("links=%v err=%v", links, err)
	}
	if want := "MooFetch/9.9.9 (+https://github.com/sebasinmas/MooFetch)"; rec.agents[0] != want {
		t.Errorf("User-Agent = %q, want %q", rec.agents[0], want)
	}
	if rec.cookies[0] != "" {
		t.Errorf("empty cookie must not be sent, got %q", rec.cookies[0])
	}
}

func TestScanPage_CustomUserAgent(t *testing.T) {
	rec := &recorder{}
	srv := newMoodleServer(t, rec, func(w http.ResponseWriter, _ *http.Request) { _, _ = fmt.Fprint(w, "<p>nada</p>") })
	opts := baseOpts(rec)
	opts.UserAgent = "Custom/1"
	links, err := moodle.New().ScanPage(context.Background(), srv.URL, scanCookie, opts)
	if err != nil || len(links) != 0 {
		t.Fatalf("links=%v err=%v", links, err)
	}
	if rec.agents[0] != "Custom/1" {
		t.Errorf("User-Agent = %q", rec.agents[0])
	}
}

func TestScanPage_AuthFailures(t *testing.T) {
	login := fixture(t, "login.html")
	tests := []struct {
		name    string
		handler http.HandlerFunc
	}{
		{"redirect to login", func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, "/login/index.php", http.StatusSeeOther)
		}},
		{"200 login page", func(w http.ResponseWriter, _ *http.Request) { _, _ = fmt.Fprint(w, login) }},
		{"login form only", func(w http.ResponseWriter, _ *http.Request) {
			_, _ = fmt.Fprint(w, `<form action="/login/index.php" method="post"></form>`)
		}},
		{"element id login", func(w http.ResponseWriter, _ *http.Request) { _, _ = fmt.Fprint(w, `<div id="login"></div>`) }},
		{"403", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusForbidden) }},
		{"401", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusUnauthorized) }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := &recorder{}
			srv := newMoodleServer(t, rec, tt.handler)
			links, err := moodle.New().ScanPage(context.Background(), srv.URL+"/course/view.php?id=1", scanCookie, baseOpts(rec))
			if !errors.Is(err, moodle.ErrAuthenticationFailed) || !errors.Is(err, domain.ErrAuthenticationFailed) {
				t.Fatalf("err = %v, want ErrAuthenticationFailed", err)
			}
			if links != nil {
				t.Errorf("links = %v, want nil", links)
			}
			if rec.requests() != 1 || len(rec.sleeps) != 0 {
				t.Errorf("requests=%d sleeps=%v; auth failure must not be retried", rec.requests(), rec.sleeps)
			}
			if strings.Contains(err.Error(), "super_secret_scan_cookie") {
				t.Errorf("error leaks cookie: %v", err)
			}
		})
	}
}

func TestScanPage_AuthFailureInFolderAbortsScan(t *testing.T) {
	rec := &recorder{}
	var srv *httptest.Server
	course := serveCourse(t, func() string { return srv.Listener.Addr().String() })
	srv = newMoodleServer(t, rec, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/mod/folder/view.php" {
			http.Redirect(w, r, "/login/index.php", http.StatusSeeOther)
			return
		}
		course(w, r)
	})
	_, err := moodle.New().ScanPage(context.Background(), srv.URL+"/course/view.php", scanCookie, baseOpts(rec))
	if !errors.Is(err, moodle.ErrAuthenticationFailed) {
		t.Fatalf("err = %v, want ErrAuthenticationFailed", err)
	}
	if rec.requests() != 2 {
		t.Errorf("requests = %d, want 2 (page + first folder, then stop): %v", rec.requests(), rec.paths)
	}
}

func TestScanPage_RetryAfterThenSuccess(t *testing.T) {
	tests := []struct {
		name       string
		status     int
		retryAfter func() string
		minWait    time.Duration
		maxWait    time.Duration
	}{
		{"seconds", http.StatusTooManyRequests, func() string { return "7" }, 7 * time.Second, 7 * time.Second},
		{"http-date", http.StatusServiceUnavailable, func() string {
			return time.Now().Add(31 * time.Second).UTC().Format(http.TimeFormat)
		}, 28 * time.Second, 31 * time.Second},
		{"capped at 60s", http.StatusTooManyRequests, func() string { return "3600" }, 60 * time.Second, 60 * time.Second},
		{"past date means no wait beyond zero", http.StatusTooManyRequests, func() string {
			return time.Now().Add(-time.Hour).UTC().Format(http.TimeFormat)
		}, 2 * time.Second, 2 * time.Second}, // falls back to exponential backoff
		{"garbage header uses backoff", http.StatusTooManyRequests, func() string { return "soon" }, 2 * time.Second, 2 * time.Second},
		{"absent header uses backoff", http.StatusServiceUnavailable, func() string { return "" }, 2 * time.Second, 2 * time.Second},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := &recorder{}
			var calls atomic.Int32
			srv := newMoodleServer(t, rec, func(w http.ResponseWriter, _ *http.Request) {
				if calls.Add(1) == 1 {
					if ra := tt.retryAfter(); ra != "" {
						w.Header().Set("Retry-After", ra)
					}
					w.WriteHeader(tt.status)
					return
				}
				_, _ = fmt.Fprint(w, `<a href="/mod/resource/view.php?id=1">Uno</a>`)
			})
			links, err := moodle.New().ScanPage(context.Background(), srv.URL+"/c", scanCookie, baseOpts(rec))
			if err != nil {
				t.Fatalf("ScanPage: %v", err)
			}
			if len(links) != 1 {
				t.Fatalf("links = %v", links)
			}
			if rec.requests() != 2 || len(rec.sleeps) != 1 {
				t.Fatalf("requests=%d sleeps=%v, want 2 requests and 1 wait", rec.requests(), rec.sleeps)
			}
			if d := rec.sleeps[0]; d < tt.minWait || d > tt.maxWait {
				t.Errorf("wait = %v, want within [%v, %v]", d, tt.minWait, tt.maxWait)
			}
			for i, c := range rec.cookies {
				if c != scanCookie {
					t.Errorf("retry %d lost the cookie: %q", i, c)
				}
			}
		})
	}
}

func TestScanPage_RateLimitedForeverStopsAfterMaxRetries(t *testing.T) {
	rec := &recorder{}
	srv := newMoodleServer(t, rec, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	})
	opts := baseOpts(rec)
	opts.MaxRetries = 3

	links, err := moodle.New().ScanPage(context.Background(), srv.URL+"/c", scanCookie, opts)
	if !errors.Is(err, moodle.ErrRateLimited) {
		t.Fatalf("err = %v, want ErrRateLimited", err)
	}
	if links != nil {
		t.Errorf("links = %v, want nil", links)
	}
	if got := rec.requests(); got != 4 {
		t.Errorf("requests = %d, want exactly 1+MaxRetries = 4", got)
	}
	// Exponential backoff without Retry-After: 2s, 4s, 8s; no wait after the last failure.
	want := []time.Duration{2 * time.Second, 4 * time.Second, 8 * time.Second}
	if len(rec.sleeps) != len(want) {
		t.Fatalf("sleeps = %v, want %v", rec.sleeps, want)
	}
	for i := range want {
		if rec.sleeps[i] != want[i] {
			t.Errorf("sleep %d = %v, want %v", i, rec.sleeps[i], want[i])
		}
	}
	if !strings.Contains(err.Error(), "429") {
		t.Errorf("error should mention the status: %v", err)
	}
}

func TestScanPage_BackoffIsCapped(t *testing.T) {
	rec := &recorder{}
	srv := newMoodleServer(t, rec, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	})
	opts := baseOpts(rec)
	opts.MaxRetries = 7
	_, err := moodle.New().ScanPage(context.Background(), srv.URL+"/c", scanCookie, opts)
	if !errors.Is(err, moodle.ErrRateLimited) {
		t.Fatalf("err = %v", err)
	}
	for _, d := range rec.sleeps {
		if d > 60*time.Second {
			t.Errorf("wait %v exceeds the 60s cap", d)
		}
	}
	if last := rec.sleeps[len(rec.sleeps)-1]; last != 60*time.Second {
		t.Errorf("last wait = %v, want the 60s cap", last)
	}
}

func TestScanPage_NegativeMaxRetriesMeansNoRetry(t *testing.T) {
	rec := &recorder{}
	srv := newMoodleServer(t, rec, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	})
	opts := baseOpts(rec)
	opts.MaxRetries = -1
	_, err := moodle.New().ScanPage(context.Background(), srv.URL+"/c", scanCookie, opts)
	if !errors.Is(err, moodle.ErrRateLimited) || rec.requests() != 1 || len(rec.sleeps) != 0 {
		t.Fatalf("err=%v requests=%d sleeps=%v", err, rec.requests(), rec.sleeps)
	}
}

func TestScanPage_RateLimitedInFolderAborts(t *testing.T) {
	rec := &recorder{}
	var srv *httptest.Server
	course := serveCourse(t, func() string { return srv.Listener.Addr().String() })
	srv = newMoodleServer(t, rec, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/mod/folder/view.php" {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		course(w, r)
	})
	opts := baseOpts(rec)
	opts.MaxRetries = 1
	_, err := moodle.New().ScanPage(context.Background(), srv.URL+"/course/view.php", scanCookie, opts)
	if !errors.Is(err, moodle.ErrRateLimited) {
		t.Fatalf("err = %v", err)
	}
	if rec.requests() != 3 { // page + folder 201 + one retry; folder 202 never requested
		t.Errorf("requests = %d (%v)", rec.requests(), rec.paths)
	}
}

func TestScanPage_FolderErrorIsSkipped(t *testing.T) {
	rec := &recorder{}
	var srv *httptest.Server
	course := serveCourse(t, func() string { return srv.Listener.Addr().String() })
	srv = newMoodleServer(t, rec, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("id") == "201" {
			http.Error(w, "boom", http.StatusInternalServerError)
			return
		}
		course(w, r)
	})
	links, err := moodle.New().ScanPage(context.Background(), srv.URL+"/course/view.php", scanCookie, baseOpts(rec))
	if err != nil {
		t.Fatalf("a failing folder must not fail the scan: %v", err)
	}
	var hasA, hasDatos bool
	for _, l := range links {
		hasA = hasA || strings.Contains(l.URL, "Lectura%20A")
		hasDatos = hasDatos || strings.Contains(l.URL, "Datos.zip")
	}
	if hasA || !hasDatos {
		t.Errorf("hasA=%v hasDatos=%v; folder 201 skipped, 202 expanded expected", hasA, hasDatos)
	}
}

func TestScanPage_InitialPageErrors(t *testing.T) {
	rec := &recorder{}
	srv := newMoodleServer(t, rec, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNotFound) })
	_, err := moodle.New().ScanPage(context.Background(), srv.URL+"/c", scanCookie, baseOpts(rec))
	if !errors.Is(err, moodle.ErrUnexpectedStatus) {
		t.Fatalf("err = %v, want ErrUnexpectedStatus", err)
	}

	for _, bad := range []string{"", "/relative/only", "http://[::1"} {
		if _, err := moodle.New().ScanPage(context.Background(), bad, scanCookie, baseOpts(rec)); err == nil {
			t.Errorf("ScanPage(%q) = nil error, want invalid URL error", bad)
		}
	}
}

func TestScanPage_ConnectionError(t *testing.T) {
	rec := &recorder{}
	srv := newMoodleServer(t, rec, func(http.ResponseWriter, *http.Request) {})
	url := srv.URL
	srv.Close()
	_, err := moodle.New().ScanPage(context.Background(), url+"/c", scanCookie, baseOpts(rec))
	if err == nil || !strings.Contains(err.Error(), "request failed") {
		t.Fatalf("err = %v, want request failed", err)
	}
	if strings.Contains(err.Error(), "super_secret_scan_cookie") {
		t.Errorf("error leaks cookie: %v", err)
	}
}

func TestScanPage_BodyTooLarge(t *testing.T) {
	rec := &recorder{}
	srv := newMoodleServer(t, rec, func(w http.ResponseWriter, _ *http.Request) {
		chunk := []byte(strings.Repeat("a", 1<<20))
		for i := 0; i < 9; i++ {
			_, _ = w.Write(chunk)
		}
	})
	_, err := moodle.New().ScanPage(context.Background(), srv.URL+"/c", scanCookie, baseOpts(rec))
	if err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("err = %v, want size cap error", err)
	}
}

func TestScanPage_ContextCanceled(t *testing.T) {
	t.Run("before first request", func(t *testing.T) {
		rec := &recorder{}
		srv := courseServer(t, rec)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		_, err := moodle.New().ScanPage(ctx, srv.URL+"/course/view.php", scanCookie, baseOpts(rec))
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("err = %v, want context.Canceled", err)
		}
		if rec.requests() != 0 {
			t.Errorf("requests = %d, want 0", rec.requests())
		}
	})

	t.Run("during pacing sleep", func(t *testing.T) {
		rec := &recorder{}
		srv := courseServer(t, rec)
		ctx, cancel := context.WithCancel(context.Background())
		opts := baseOpts(rec)
		opts.Sleep = func(ctx context.Context, d time.Duration) error {
			rec.sleeps = append(rec.sleeps, d)
			cancel()
			return ctx.Err()
		}
		_, err := moodle.New().ScanPage(ctx, srv.URL+"/course/view.php", scanCookie, opts)
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("err = %v, want context.Canceled", err)
		}
		if rec.requests() != 1 {
			t.Errorf("requests = %d, want 1 (cancelled before the first folder)", rec.requests())
		}
	})

	t.Run("during retry wait", func(t *testing.T) {
		rec := &recorder{}
		srv := newMoodleServer(t, rec, func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusTooManyRequests)
		})
		ctx, cancel := context.WithCancel(context.Background())
		opts := baseOpts(rec)
		opts.Sleep = func(ctx context.Context, _ time.Duration) error {
			cancel()
			return ctx.Err()
		}
		_, err := moodle.New().ScanPage(ctx, srv.URL+"/c", scanCookie, opts)
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("err = %v, want context.Canceled", err)
		}
		if rec.requests() != 1 {
			t.Errorf("requests = %d, want 1", rec.requests())
		}
	})

	t.Run("real sleep honors cancellation", func(t *testing.T) {
		rec := &recorder{}
		srv := courseServer(t, rec)
		ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
		defer cancel()
		started := time.Now()
		_, err := moodle.New().ScanPage(ctx, srv.URL+"/course/view.php", scanCookie,
			moodle.ScanOptions{Delay: time.Hour, Jitter: -1}) // default Sleep
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("err = %v, want DeadlineExceeded", err)
		}
		if time.Since(started) > 10*time.Second {
			t.Errorf("scan did not stop promptly after cancellation")
		}
	})
}

func TestScanPage_DefaultSleepZeroDelay(t *testing.T) {
	rec := &recorder{}
	srv := courseServer(t, rec)
	// Negative Delay/Jitter mean "none": the default sleeper returns immediately
	// and, after a real 1ms timer for completeness, the scan completes.
	links, err := moodle.New().ScanPage(context.Background(), srv.URL+"/course/view.php", scanCookie,
		moodle.ScanOptions{Delay: -1, Jitter: -1})
	if err != nil || len(links) == 0 {
		t.Fatalf("links=%d err=%v", len(links), err)
	}
	links, err = moodle.New().ScanPage(context.Background(), srv.URL+"/course/view.php", scanCookie,
		moodle.ScanOptions{Delay: time.Millisecond, Jitter: -1, MaxPages: 2})
	if err != nil || len(links) == 0 {
		t.Fatalf("links=%d err=%v", len(links), err)
	}
}

func TestScanPage_ZeroJitterIsExactDelay(t *testing.T) {
	rec := &recorder{}
	srv := courseServer(t, rec)
	opts := baseOpts(rec)
	opts.Jitter = -1
	if _, err := moodle.New().ScanPage(context.Background(), srv.URL+"/course/view.php", scanCookie, opts); err != nil {
		t.Fatal(err)
	}
	for _, d := range rec.sleeps {
		if d != 2*time.Second {
			t.Errorf("sleep = %v, want exactly Delay", d)
		}
	}
}

func TestScanPage_ResolvesRelativeToFinalURLAfterRedirect(t *testing.T) {
	rec := &recorder{}
	srv := newMoodleServer(t, rec, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/old" {
			http.Redirect(w, r, "/sub/course/view.php?id=1", http.StatusFound)
			return
		}
		_, _ = fmt.Fprint(w, `<a href="../mod/resource/view.php?id=9">Rel</a>`)
	})
	links, err := moodle.New().ScanPage(context.Background(), srv.URL+"/old", scanCookie, baseOpts(rec))
	if err != nil || len(links) != 1 {
		t.Fatalf("links=%v err=%v", links, err)
	}
	if want := srv.URL + "/sub/mod/resource/view.php?id=9"; links[0].URL != want {
		t.Errorf("URL = %q, want %q", links[0].URL, want)
	}
}

func TestScanPage_DuplicateWithEmptyTitleIsCompleted(t *testing.T) {
	rec := &recorder{}
	srv := newMoodleServer(t, rec, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprint(w, `<a href="/mod/resource/view.php?id=3"><img src="x"></a>
			<a href="/mod/resource/view.php?id=3"><span class="sr-only">oculto</span>Texto <span class="visually-hidden">x</span>real</a>`)
	})
	links, err := moodle.New().ScanPage(context.Background(), srv.URL+"/c", scanCookie, baseOpts(rec))
	if err != nil || len(links) != 1 {
		t.Fatalf("links=%v err=%v", links, err)
	}
	if links[0].Title != "Texto real" {
		t.Errorf("Title = %q, want %q", links[0].Title, "Texto real")
	}
}

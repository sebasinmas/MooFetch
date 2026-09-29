package moodle_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/sebasinmas/MooFetch/internal/domain"
	"github.com/sebasinmas/MooFetch/internal/plugins/moodle"
)

func TestMoodle_ExtractFilename_EdgeCases(t *testing.T) {
	tests := []struct {
		name   string
		rawURL string
		cd     string
		want   string
	}{
		{"path traversal in header", "https://x.cl/r", `attachment; filename="../../etc/passwd"`, "passwd"},
		{"windows separators in header", "https://x.cl/r", `attachment; filename="..\..\evil.pdf"`, ".._.._evil.pdf"},
		{"forbidden chars replaced", "https://x.cl/r", `attachment; filename="a:b*c?d|e.pdf"`, "a_b_c_d_e.pdf"},
		{"unquoted filename", "https://x.cl/r", `attachment; filename=Guia.pdf`, "Guia.pdf"},
		{"inline disposition", "https://x.cl/r", `inline; filename="Inline.pdf"`, "Inline.pdf"},
		{"rfc5987 filename*", "https://x.cl/r", `attachment; filename*=UTF-8''Gu%C3%ADa%20Final.pdf`, "Guía Final.pdf"},
		{"empty filename falls back to URL", "https://x.cl/files/Doc.pdf", `attachment; filename=""`, "Doc.pdf"},
		{"malformed header falls back to URL", "https://x.cl/files/Doc.pdf", `;;;===`, "Doc.pdf"},
		{"header without filename", "https://x.cl/files/Doc.pdf", `attachment`, "Doc.pdf"},
		{"dot filename", "https://x.cl/r", `attachment; filename="."`, "download.pdf"},
		{"script path uses query pdf", "https://x.cl/view.php?file=Clase%201.pdf", "", "Clase 1.pdf"},
		{"script path without pdf falls back", "https://x.cl/view.php?id=9", "", "download_7.pdf"},
		{"encoded path", "https://x.cl/a/Apunte%20Uno.pdf", "", "Apunte Uno.pdf"},
		{"invalid URL", "http://[::1", "", "download_7.pdf"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h := make(http.Header)
			if tc.cd != "" {
				h.Set("Content-Disposition", tc.cd)
			}
			if got := moodle.ExtractFilename(tc.rawURL, h, 7); got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
	if got := moodle.ExtractFilename("https://x.cl/f/A.pdf", nil, 1); got != "A.pdf" {
		t.Errorf("nil header: got %q", got)
	}
}

func TestMoodle_StatusMapping(t *testing.T) {
	tests := []struct {
		status   int
		wantAuth bool
	}{
		{http.StatusUnauthorized, true},
		{http.StatusForbidden, true},
		{http.StatusNotFound, false},
		{http.StatusBadGateway, false},
	}
	for _, tc := range tests {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(tc.status)
		}))
		dir := t.TempDir()
		_, err := moodle.New().Download(context.Background(), domain.Task{ID: 1, URL: srv.URL + "/f.pdf", OutputDir: dir}, nil)
		srv.Close()
		if err == nil {
			t.Fatalf("status %d: expected error", tc.status)
		}
		if got := errors.Is(err, domain.ErrAuthenticationFailed); got != tc.wantAuth {
			t.Errorf("status %d: auth=%v want %v (err=%v)", tc.status, got, tc.wantAuth, err)
		}
		if !tc.wantAuth && !errors.Is(err, moodle.ErrUnexpectedStatus) {
			t.Errorf("status %d: want ErrUnexpectedStatus, got %v", tc.status, err)
		}
		entries, _ := os.ReadDir(dir)
		if len(entries) != 0 {
			t.Errorf("status %d: output dir should be empty, has %d entries", tc.status, len(entries))
		}
	}
}

func TestMoodle_RedirectToLoginWithQuery(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/login/index.php?next=1", http.StatusFound)
	}))
	defer srv.Close()
	_, err := moodle.New().Download(context.Background(), domain.Task{ID: 1, URL: srv.URL + "/x.pdf", OutputDir: t.TempDir()}, nil)
	if !errors.Is(err, domain.ErrAuthenticationFailed) || !domain.IsFatalAuth(err) {
		t.Fatalf("expected fatal auth error, got %v", err)
	}
}

func TestMoodle_TruncatedBodyCleansPart(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Length", "1000")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("short"))
		// handler returns with fewer bytes than declared: connection is aborted
	}))
	defer srv.Close()

	dir := t.TempDir()
	_, err := moodle.New().Download(context.Background(), domain.Task{ID: 1, URL: srv.URL + "/trunc.pdf", OutputDir: dir}, nil)
	if err == nil {
		t.Fatal("expected error for truncated body")
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 0 {
		t.Errorf("expected no leftover files, got %v", entries)
	}
}

func TestMoodle_ContentLengthVariants(t *testing.T) {
	t.Run("zero length body yields empty file", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Length", "0")
			w.WriteHeader(http.StatusOK)
		}))
		defer srv.Close()
		dir := t.TempDir()
		res, err := moodle.New().Download(context.Background(), domain.Task{ID: 1, URL: srv.URL + "/empty.pdf", OutputDir: dir}, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if res.BytesRead != 0 {
			t.Errorf("BytesRead = %d", res.BytesRead)
		}
		st, err := os.Stat(filepath.Join(dir, "empty.pdf"))
		if err != nil || st.Size() != 0 {
			t.Fatalf("expected empty final file, err=%v", err)
		}
		if _, err := os.Stat(filepath.Join(dir, "empty.pdf.moofetch.part")); !os.IsNotExist(err) {
			t.Errorf("part file should not remain")
		}
	})

	t.Run("chunked unknown length succeeds", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("hello "))
			w.(http.Flusher).Flush()
			_, _ = w.Write([]byte("world"))
		}))
		defer srv.Close()
		dir := t.TempDir()
		var sawUnknownTotal bool
		res, err := moodle.New().Download(context.Background(), domain.Task{ID: 1, URL: srv.URL + "/chunked.pdf", OutputDir: dir}, func(u domain.ProgressUpdate) {
			if u.TotalBytes <= 0 {
				sawUnknownTotal = true
			}
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if res.BytesRead != 11 || !sawUnknownTotal {
			t.Errorf("BytesRead=%d unknownTotal=%v", res.BytesRead, sawUnknownTotal)
		}
		data, _ := os.ReadFile(filepath.Join(dir, "chunked.pdf"))
		if string(data) != "hello world" {
			t.Errorf("content = %q", data)
		}
	})
}

func TestMoodle_HeaderFilenameNeverEscapesOutputDir(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Disposition", `attachment; filename="../../escape.pdf"`)
		_, _ = w.Write(dummyPDF)
	}))
	defer srv.Close()

	root := t.TempDir()
	out := filepath.Join(root, "a", "b")
	if _, err := moodle.New().Download(context.Background(), domain.Task{ID: 1, URL: srv.URL + "/x", OutputDir: out}, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(out, "escape.pdf")); err != nil {
		t.Errorf("file should be inside output dir: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "escape.pdf")); !os.IsNotExist(err) {
		t.Errorf("file escaped output dir")
	}
}

func TestMoodle_CancelledContextCleansPart(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "100000")
		_, _ = w.Write([]byte("partial"))
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	defer srv.Close()

	dir := t.TempDir()
	_, err := moodle.New().Download(ctx, domain.Task{ID: 1, URL: srv.URL + "/c.pdf", OutputDir: dir}, func(domain.ProgressUpdate) { cancel() })
	if err == nil {
		t.Fatal("expected error after cancel")
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 0 {
		t.Errorf("leftover files: %v", entries)
	}
}

func TestMoodle_CanHandle_Schemes(t *testing.T) {
	p := moodle.New()
	for u, want := range map[string]bool{
		"http://a.cl/x": true, "https://a.cl/x": true, "HTTPS://A.CL/x": true,
		"ftp://a.cl/x": false, "file:///etc/passwd": false, "": false, "a.cl/x": false, "http://[::1": false,
	} {
		if got := p.CanHandle(u); got != want {
			t.Errorf("CanHandle(%q) = %v, want %v", u, got, want)
		}
	}
}

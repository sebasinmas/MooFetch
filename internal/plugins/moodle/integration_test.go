package moodle_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/sebasinmas/MooFetch/internal/domain"
	"github.com/sebasinmas/MooFetch/internal/plugins/moodle"
)

// This file exercises Plugin.Download against deliberately broken servers and
// checks the on-disk outcome: either exactly the final file, or nothing at all.

const partSuffix = ".moofetch.part"

// listDir returns the sorted names in dir.
func listDir(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	sort.Strings(names)
	return names
}

func assertDirEmpty(t *testing.T, dir string) {
	t.Helper()
	if names := listDir(t, dir); len(names) != 0 {
		t.Errorf("output dir must be empty, found %v", names)
	}
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// hijackRaw writes raw bytes straight onto the connection and closes it,
// optionally with a TCP RST (SO_LINGER 0) instead of a clean FIN.
func hijackRaw(t *testing.T, w http.ResponseWriter, raw string, reset bool) {
	t.Helper()
	hj, ok := w.(http.Hijacker)
	if !ok {
		t.Error("ResponseWriter is not a Hijacker")
		return
	}
	conn, buf, err := hj.Hijack()
	if err != nil {
		t.Errorf("hijack: %v", err)
		return
	}
	_, _ = buf.WriteString(raw)
	_ = buf.Flush()
	if tcp, ok := conn.(*net.TCPConn); ok && reset {
		_ = tcp.SetLinger(0)
	}
	_ = conn.Close()
}

func pdfHandler(body []byte) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/pdf")
		w.Header().Set("Content-Length", fmt.Sprint(len(body)))
		_, _ = w.Write(body)
	}
}

func download(t *testing.T, p *moodle.Plugin, ctx context.Context, rawURL, dir string) (*domain.Result, error) {
	t.Helper()
	return p.Download(ctx, domain.Task{ID: 1, URL: rawURL, OutputDir: dir}, nil)
}

func TestIntegration_ValidPDF_BytesIdenticalNoLeftovers(t *testing.T) {
	srv := httptest.NewServer(pdfHandler(dummyPDF))
	defer srv.Close()
	dir := t.TempDir()

	res, err := download(t, moodle.New(), context.Background(), srv.URL+"/Apunte.pdf", dir)
	if err != nil {
		t.Fatalf("download: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(dir, "Apunte.pdf"))
	if err != nil {
		t.Fatalf("read saved file: %v", err)
	}
	if !bytes.Equal(got, dummyPDF) {
		t.Errorf("saved bytes differ from served bytes")
	}
	if !bytes.HasPrefix(got, []byte("%PDF-")) {
		t.Errorf("saved file does not start with %%PDF-")
	}
	if res.BytesRead != int64(len(dummyPDF)) || res.TotalBytes != int64(len(dummyPDF)) {
		t.Errorf("result bytes = %d/%d, want %d", res.BytesRead, res.TotalBytes, len(dummyPDF))
	}
	if names := listDir(t, dir); len(names) != 1 || names[0] != "Apunte.pdf" {
		t.Errorf("dir = %v, want only Apunte.pdf", names)
	}
}

func TestIntegration_BrokenStreams_LeaveNothingOnDisk(t *testing.T) {
	half := dummyPDF[:len(dummyPDF)/2]

	tests := []struct {
		name    string
		timeout time.Duration
		handler func(t *testing.T) http.HandlerFunc
		wantErr error // optional sentinel
	}{
		{
			name: "body shorter than Content-Length, connection closed cleanly",
			handler: func(t *testing.T) http.HandlerFunc {
				return func(w http.ResponseWriter, _ *http.Request) {
					raw := fmt.Sprintf("HTTP/1.1 200 OK\r\nContent-Type: application/pdf\r\nContent-Length: %d\r\n\r\n%s",
						len(dummyPDF), half)
					hijackRaw(t, w, raw, false)
				}
			},
		},
		{
			name: "body shorter than Content-Length, connection reset",
			handler: func(t *testing.T) http.HandlerFunc {
				return func(w http.ResponseWriter, _ *http.Request) {
					raw := fmt.Sprintf("HTTP/1.1 200 OK\r\nContent-Type: application/pdf\r\nContent-Length: %d\r\n\r\n%s",
						len(dummyPDF), half)
					hijackRaw(t, w, raw, true)
				}
			},
		},
		{
			name: "chunked stream killed mid-chunk (no Content-Length)",
			handler: func(t *testing.T) http.HandlerFunc {
				return func(w http.ResponseWriter, _ *http.Request) {
					// Announce a 0x40-byte chunk, deliver only part of it, then close.
					raw := "HTTP/1.1 200 OK\r\nContent-Type: application/pdf\r\nTransfer-Encoding: chunked\r\n\r\n" +
						"40\r\n" + string(dummyPDF[:10])
					hijackRaw(t, w, raw, false)
				}
			},
		},
		{
			name: "chunked stream reset after complete chunks, before terminator",
			handler: func(t *testing.T) http.HandlerFunc {
				return func(w http.ResponseWriter, _ *http.Request) {
					raw := "HTTP/1.1 200 OK\r\nContent-Type: application/pdf\r\nTransfer-Encoding: chunked\r\n\r\n" +
						fmt.Sprintf("%x\r\n%s\r\n", len(half), half)
					hijackRaw(t, w, raw, true)
				}
			},
		},
		{
			name:    "stall after partial body triggers inactivity timeout",
			timeout: 100 * time.Millisecond,
			wantErr: moodle.ErrInactivityTimeout,
			handler: func(t *testing.T) http.HandlerFunc {
				return func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set("Content-Type", "application/pdf")
					w.Header().Set("Content-Length", fmt.Sprint(len(dummyPDF)))
					_, _ = w.Write(half)
					w.(http.Flusher).Flush()
					<-r.Context().Done()
				}
			},
		},
		{
			name:    "stall right after headers (no body bytes at all)",
			timeout: 100 * time.Millisecond,
			wantErr: moodle.ErrInactivityTimeout,
			handler: func(t *testing.T) http.HandlerFunc {
				return func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set("Content-Type", "application/pdf")
					w.Header().Set("Content-Length", fmt.Sprint(len(dummyPDF)))
					w.WriteHeader(http.StatusOK)
					w.(http.Flusher).Flush()
					<-r.Context().Done()
				}
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(tc.handler(t))
			defer srv.Close()
			dir := t.TempDir()

			opts := []moodle.Option{}
			if tc.timeout > 0 {
				opts = append(opts, moodle.WithInactivityTimeout(tc.timeout))
			} else {
				opts = append(opts, moodle.WithInactivityTimeout(5*time.Second))
			}
			res, err := download(t, moodle.New(opts...), context.Background(), srv.URL+"/broken.pdf", dir)
			if err == nil {
				t.Fatalf("expected error, got result %+v", res)
			}
			if res != nil {
				t.Errorf("result must be nil on error, got %+v", res)
			}
			if tc.wantErr != nil && !errors.Is(err, tc.wantErr) {
				t.Errorf("error = %v, want it to wrap %v", err, tc.wantErr)
			}
			if domain.IsFatalAuth(err) {
				t.Errorf("a broken stream must not be classified as an auth failure: %v", err)
			}
			assertDirEmpty(t, dir)
		})
	}
}

func TestIntegration_ContextCancelledMidDownload_LeavesNothing(t *testing.T) {
	half := dummyPDF[:len(dummyPDF)/2]
	started := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/pdf")
		w.Header().Set("Content-Length", fmt.Sprint(len(dummyPDF)))
		_, _ = w.Write(half)
		w.(http.Flusher).Flush()
		close(started)
		<-r.Context().Done()
	}))
	defer srv.Close()
	dir := t.TempDir()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	type outcome struct {
		res *domain.Result
		err error
	}
	done := make(chan outcome, 1)
	go func() {
		res, err := download(t, moodle.New(moodle.WithInactivityTimeout(10*time.Second)), ctx, srv.URL+"/c.pdf", dir)
		done <- outcome{res, err}
	}()

	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("server never started streaming")
	}
	cancel()

	select {
	case o := <-done:
		if o.err == nil {
			t.Fatalf("expected error after cancel, got %+v", o.res)
		}
		if !errors.Is(o.err, context.Canceled) {
			t.Errorf("error = %v, want it to wrap context.Canceled", o.err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Download did not return after cancel")
	}
	assertDirEmpty(t, dir)
}

// TestIntegration_AtomicRename_PartVisibleFinalAbsentUntilComplete holds the
// stream open and checks the temp file is the only artifact until the last
// byte arrives, then that the rename publishes the final file atomically.
func TestIntegration_AtomicRename_PartVisibleFinalAbsentUntilComplete(t *testing.T) {
	half := dummyPDF[:len(dummyPDF)/2]
	started := make(chan struct{})
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/pdf")
		w.Header().Set("Content-Length", fmt.Sprint(len(dummyPDF)))
		_, _ = w.Write(half)
		w.(http.Flusher).Flush()
		close(started)
		select {
		case <-release:
		case <-r.Context().Done():
			return
		}
		_, _ = w.Write(dummyPDF[len(half):])
	}))
	defer srv.Close()
	dir := t.TempDir()
	final := filepath.Join(dir, "Slow.pdf")
	part := final + partSuffix

	done := make(chan error, 1)
	go func() {
		_, err := download(t, moodle.New(moodle.WithInactivityTimeout(10*time.Second)), context.Background(), srv.URL+"/Slow.pdf", dir)
		done <- err
	}()

	select {
	case <-started:
	case <-time.After(5 * time.Second):
		close(release)
		t.Fatal("server never started streaming")
	}

	// Wait (bounded) until the first half has actually reached the .part file.
	deadline := time.Now().Add(5 * time.Second)
	for {
		if fi, err := os.Stat(part); err == nil && fi.Size() == int64(len(half)) {
			break
		}
		if time.Now().After(deadline) {
			close(release)
			t.Fatalf(".part never reached %d bytes; dir = %v", len(half), listDir(t, dir))
		}
		time.Sleep(2 * time.Millisecond)
	}
	if exists(final) {
		t.Errorf("final file must not exist while the stream is in flight")
	}
	if names := listDir(t, dir); len(names) != 1 || names[0] != "Slow.pdf"+partSuffix {
		t.Errorf("in-flight dir = %v, want only the .part file", names)
	}

	close(release)
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("download: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Download did not finish after release")
	}

	got, err := os.ReadFile(final)
	if err != nil {
		t.Fatalf("final file missing after completion: %v", err)
	}
	if !bytes.Equal(got, dummyPDF) {
		t.Errorf("final bytes differ from served bytes")
	}
	if exists(part) {
		t.Errorf(".part must be gone after the rename")
	}
}

func TestIntegration_StalePartFromPreviousCrash(t *testing.T) {
	stale := bytes.Repeat([]byte("STALE-GARBAGE"), 500) // deliberately longer than the PDF

	t.Run("successful download overwrites it cleanly", func(t *testing.T) {
		srv := httptest.NewServer(pdfHandler(dummyPDF))
		defer srv.Close()
		dir := t.TempDir()
		part := filepath.Join(dir, "Doc.pdf"+partSuffix)
		if err := os.WriteFile(part, stale, 0o600); err != nil {
			t.Fatal(err)
		}

		if _, err := download(t, moodle.New(), context.Background(), srv.URL+"/Doc.pdf", dir); err != nil {
			t.Fatalf("download: %v", err)
		}
		got, err := os.ReadFile(filepath.Join(dir, "Doc.pdf"))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, dummyPDF) {
			t.Errorf("stale bytes leaked into the final file (len %d, want %d)", len(got), len(dummyPDF))
		}
		if names := listDir(t, dir); len(names) != 1 {
			t.Errorf("dir = %v, want only Doc.pdf", names)
		}
	})

	t.Run("failed download removes the stale part too", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			hijackRaw(t, w, "HTTP/1.1 200 OK\r\nContent-Length: 1000\r\n\r\n%PDF-sh", false)
		}))
		defer srv.Close()
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "Doc.pdf"+partSuffix), stale, 0o600); err != nil {
			t.Fatal(err)
		}

		if _, err := download(t, moodle.New(), context.Background(), srv.URL+"/Doc.pdf", dir); err == nil {
			t.Fatal("expected error")
		}
		assertDirEmpty(t, dir)
	})
}

func TestIntegration_Redownload_SuccessReplacesPreviousFile(t *testing.T) {
	srv := httptest.NewServer(pdfHandler(dummyPDF))
	defer srv.Close()
	dir := t.TempDir()
	target := filepath.Join(dir, "Doc.pdf")
	if err := os.WriteFile(target, []byte("%PDF-1.0 old version, longer than nothing at all, longer than nothing at all, longer than nothing"), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := download(t, moodle.New(), context.Background(), srv.URL+"/Doc.pdf", dir); err != nil {
		t.Fatalf("download: %v", err)
	}
	got, _ := os.ReadFile(target)
	if !bytes.Equal(got, dummyPDF) {
		t.Errorf("file was not replaced by the new download")
	}
}

// Probe 1: does a failed re-download destroy a previously downloaded good file?
//
// moodle.Download calls os.Remove(targetPath) whenever writeStreamToFile fails.
// The partial data lives in "<target>.moofetch.part", never in <target>, so the
// only thing that Remove can delete is a PRE-EXISTING file with the same name.

func TestIntegration_FailedRedownload_PreservesPreviousGoodFile(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hijackRaw(t, w, "HTTP/1.1 200 OK\r\nContent-Length: 1000\r\n\r\n%PDF-sh", false)
	}))
	defer srv.Close()
	dir := t.TempDir()
	target := filepath.Join(dir, "Doc.pdf")
	if err := os.WriteFile(target, dummyPDF, 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := download(t, moodle.New(), context.Background(), srv.URL+"/Doc.pdf", dir); err == nil {
		t.Fatal("expected error from truncated body")
	}
	got, err := os.ReadFile(target)
	if err != nil || !bytes.Equal(got, dummyPDF) {
		t.Errorf("previous good file must survive a failed re-download (err=%v)", err)
	}
}

// Failures that happen BEFORE the body is written (status mapping, request
// errors) never reach the os.Remove, so an existing file is preserved there.
func TestIntegration_RedownloadRejectedByStatus_PreservesPreviousFile(t *testing.T) {
	for _, status := range []int{http.StatusNotFound, http.StatusInternalServerError, http.StatusForbidden} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				http.Error(w, "nope", status)
			}))
			defer srv.Close()
			dir := t.TempDir()
			target := filepath.Join(dir, "Doc.pdf")
			if err := os.WriteFile(target, dummyPDF, 0o600); err != nil {
				t.Fatal(err)
			}

			if _, err := download(t, moodle.New(), context.Background(), srv.URL+"/Doc.pdf", dir); err == nil {
				t.Fatal("expected error")
			}
			got, err := os.ReadFile(target)
			if err != nil || !bytes.Equal(got, dummyPDF) {
				t.Errorf("existing file must be untouched (err=%v)", err)
			}
			if names := listDir(t, dir); len(names) != 1 {
				t.Errorf("dir = %v, want only the original file", names)
			}
		})
	}
}

// Probe 2: no content sniffing. Whatever the server answers with 2xx is saved
// under the name derived from the URL, even when it is clearly not a PDF
// (typically the HTML login page served with 200 when a session expired).

var nonPDFBodies = []struct {
	name        string
	contentType string
	body        string
}{
	{"expired-session login page as text/html", "text/html; charset=utf-8",
		"<!DOCTYPE html><html><head><title>Acceder</title></head><body><form action=\"/login/index.php\"></form></body></html>"},
	{"html body labelled application/pdf", "application/pdf", "<html><body>Please log in</body></html>"},
	{"plain garbage without %PDF- magic", "application/octet-stream", "this is definitely not a pdf"},
}

func TestIntegration_NonPDFBodyWithHTTP200_Rejected(t *testing.T) {
	for _, tc := range nonPDFBodies {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", tc.contentType)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer srv.Close()
			dir := t.TempDir()

			_, err := download(t, moodle.New(), context.Background(), srv.URL+"/Apunte.pdf", dir)
			if !errors.Is(err, moodle.ErrNotPDF) {
				t.Errorf("err = %v, want ErrNotPDF", err)
			}
			if strings.HasSuffix(strings.Join(listDir(t, dir), ","), ".pdf") {
				t.Errorf("no .pdf file should be left behind, dir = %v", listDir(t, dir))
			}
		})
	}
}

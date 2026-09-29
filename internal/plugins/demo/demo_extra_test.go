package demo_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sebasinmas/MooFetch/internal/domain"
	"github.com/sebasinmas/MooFetch/internal/logger"
	"github.com/sebasinmas/MooFetch/internal/plugins/demo"
)

func TestDemoPlugin_NameAndLogger(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "demo.log")
	l, err := logger.New(logPath)
	if err != nil {
		t.Fatal(err)
	}
	p := demo.New(demo.WithLogger(l), demo.WithStepDelay(0), demo.WithWriteDummyFiles(false))
	if p.Name() != "demo" {
		t.Errorf("Name = %q", p.Name())
	}
	res, err := p.Download(context.Background(), domain.Task{ID: 1, URL: "https://x.cl/a.pdf", OutputDir: t.TempDir()}, nil)
	if err != nil || res.Filename != "a.pdf" {
		t.Fatalf("download: %+v %v", res, err)
	}
	_ = l.Close()
	data, _ := os.ReadFile(logPath)
	if !strings.Contains(string(data), "[DEMO] Iniciando") || !strings.Contains(string(data), "completada") {
		t.Errorf("start/finish not logged: %s", data)
	}
}

func TestDemoPlugin_DummyFileWrittenAtomically(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested", "out")
	p := demo.New(demo.WithStepDelay(0))
	if _, err := p.Download(context.Background(), domain.Task{ID: 2, URL: "https://x.cl/Apunte.pdf", OutputDir: dir}, nil); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "Apunte.pdf"))
	if err != nil || !strings.HasPrefix(string(data), "%PDF-1.4") || !strings.Contains(string(data), "Resource: Apunte.pdf") {
		t.Fatalf("dummy pdf missing/invalid: %q %v", data, err)
	}
	if m, _ := filepath.Glob(filepath.Join(dir, "*.part")); len(m) != 0 {
		t.Errorf("temp .part file left behind: %v", m)
	}
}

// The simulation is best-effort about the dummy file: an unwritable output
// directory must not turn a simulated download into a failure.
func TestDemoPlugin_UnwritableOutputStillSucceeds(t *testing.T) {
	blocker := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	p := demo.New(demo.WithStepDelay(0))
	res, err := p.Download(context.Background(), domain.Task{ID: 3, URL: "https://x.cl/a.pdf", OutputDir: blocker}, nil)
	if err != nil || res == nil || res.BytesRead != res.TotalBytes || res.TotalBytes == 0 {
		t.Fatalf("got %+v %v", res, err)
	}
}

func TestDemoPlugin_ProgressIsMonotonicAndCompletes(t *testing.T) {
	p := demo.New(demo.WithStepDelay(0), demo.WithWriteDummyFiles(false))
	var last int64
	var n int
	res, err := p.Download(context.Background(), domain.Task{ID: 4, URL: "https://x.cl/a.pdf"}, func(u domain.ProgressUpdate) {
		if u.BytesRead < last || u.BytesRead > u.TotalBytes {
			t.Errorf("non-monotonic progress: %d after %d (total %d)", u.BytesRead, last, u.TotalBytes)
		}
		last = u.BytesRead
		n++
	})
	if err != nil || n != 20 || last != res.TotalBytes {
		t.Fatalf("steps=%d last=%d res=%+v err=%v", n, last, res, err)
	}
}

func TestDemoPlugin_ExtractPDFResource_MoreCases(t *testing.T) {
	tests := []struct {
		in   string
		id   int
		want string
	}{
		{"https://c.cl/mod/resource/view.php?id=55/Tema.pdf", 1, "Tema.pdf"},
		{"https://c.cl/mod/resource/view.php?id=55/notpdf", 1, "notpdf.pdf"},
		{"https://c.cl/mod/resource/view.php?id=77", 1, "Recurso_77.pdf"},
		{"https://c.cl/view.php?file=Guia%20Uno.pdf", 1, "Guia Uno.pdf"},
		{"https://c.cl/files/Lectura", 1, "Lectura.pdf"},
		{"https://c.cl/index.php", 9, "Recurso_09.pdf"},
		{"https://c.cl/%zz.pdf", 2, "%zz.pdf"},
		{"  \"Notas de clase\"  ", 3, "Notas de clase.pdf"},
		{"", 4, "Recurso_04.pdf"},
		{"https://c.cl/a/..%2F..%2Fetc%2Fpasswd.pdf", 5, "passwd.pdf"},
	}
	for _, tc := range tests {
		got := demo.ExtractPDFResource(tc.in, tc.id)
		if got != tc.want {
			t.Errorf("ExtractPDFResource(%q) = %q, want %q", tc.in, got, tc.want)
		}
		if strings.ContainsAny(got, "/\\") {
			t.Errorf("result %q contains a path separator", got)
		}
	}
}

// Package demo provides a simulated downloader plugin for showcases and demonstrations.
package demo

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"godownloader/internal/kernel"
	"godownloader/internal/logger"
)

// Plugin simulates downloading PDF resources without performing real HTTP requests.
type Plugin struct {
	logger          *logger.Logger
	stepDelay       time.Duration
	writeDummyFiles bool
}

// Option configures a demo Plugin instance.
type Option func(*Plugin)

// WithLogger associates a debug logger with the demo Plugin.
func WithLogger(l *logger.Logger) Option {
	return func(p *Plugin) {
		p.logger = l
	}
}

// WithStepDelay sets the delay between progress updates during download simulation.
func WithStepDelay(d time.Duration) Option {
	return func(p *Plugin) {
		p.stepDelay = d
	}
}

// WithWriteDummyFiles toggles writing minimal valid PDF files to the output directory.
func WithWriteDummyFiles(write bool) Option {
	return func(p *Plugin) {
		p.writeDummyFiles = write
	}
}

// New creates a new demo Plugin with default settings.
func New(opts ...Option) *Plugin {
	p := &Plugin{
		stepDelay:       65 * time.Millisecond,
		writeDummyFiles: true,
	}
	for _, opt := range opts {
		opt(p)
	}
	return p
}

// Name returns the identifier of this plugin.
func (p *Plugin) Name() string {
	return "demo"
}

// CanHandle returns true for any URL when running in demo mode.
func (p *Plugin) CanHandle(_ string) bool {
	return true
}

// Download simulates a concurrent download with realistic progress ticks and optional dummy file creation.
func (p *Plugin) Download(ctx context.Context, task kernel.Task, progress kernel.ProgressFunc) (*kernel.Result, error) {
	filename := ExtractPDFResource(task.URL, task.ID)
	totalBytes := calculateSimulatedSize(task.ID, filename)

	if p.logger != nil {
		p.logger.Printf("[DEMO] Iniciando simulación de descarga para tarea %d: %s (tamaño: %d bytes)", task.ID, filename, totalBytes)
	}

	numSteps := 20
	stepDelay := p.stepDelay
	if stepDelay > 0 {
		// Slightly vary tick interval per task for natural concurrent visual progress
		stepDelay += time.Duration((task.ID%4)*15) * time.Millisecond
	}

	for step := 1; step <= numSteps; step++ {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		if stepDelay > 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(stepDelay):
			}
		}

		bytesRead := int64(float64(totalBytes) * (float64(step) / float64(numSteps)))
		if step == numSteps {
			bytesRead = totalBytes
		}

		if progress != nil {
			progress(kernel.ProgressUpdate{
				TaskID:     task.ID,
				URL:        task.URL,
				Filename:   filename,
				BytesRead:  bytesRead,
				TotalBytes: totalBytes,
			})
		}
	}

	if p.writeDummyFiles {
		outDir := task.OutputDir
		if outDir == "" {
			outDir = "."
		}
		targetPath := filepath.Join(outDir, filename)
		_ = writeDummyPDF(targetPath, filename)
	}

	if p.logger != nil {
		p.logger.Printf("[DEMO] Simulación completada con éxito para tarea %d: %s", task.ID, filename)
	}

	return &kernel.Result{
		TaskID:     task.ID,
		URL:        task.URL,
		Filename:   filename,
		BytesRead:  totalBytes,
		TotalBytes: totalBytes,
		Err:        nil,
	}, nil
}

// ExtractPDFResource parses raw URLs or text inputs into a clean PDF resource name.
func ExtractPDFResource(rawURL string, taskID int) string {
	trimmed := strings.TrimSpace(rawURL)
	trimmed = strings.Trim(trimmed, `"'<>`)

	if u, err := url.Parse(trimmed); err == nil {
		if name := filenameFromQuery(u); name != "" {
			return name
		}
		if name := filenameFromPath(u); name != "" {
			return name
		}
		if name := filenameFromMoodleID(u); name != "" {
			return name
		}
	}

	cleanRaw := filepath.Base(filepath.Clean(trimmed))
	if strings.HasSuffix(strings.ToLower(cleanRaw), ".pdf") {
		return sanitizeFilename(cleanRaw)
	}
	if isMeaningfulName(cleanRaw) {
		return sanitizeFilename(cleanRaw + ".pdf")
	}

	return fmt.Sprintf("Recurso_%02d.pdf", taskID)
}

func filenameFromQuery(u *url.URL) string {
	for _, values := range u.Query() {
		for _, val := range values {
			decoded, err := url.QueryUnescape(val)
			if err == nil {
				cleanVal := strings.TrimSpace(decoded)
				if strings.HasSuffix(strings.ToLower(cleanVal), ".pdf") {
					return sanitizeFilename(filepath.Base(filepath.Clean(cleanVal)))
				}
			}
		}
	}
	return ""
}

func filenameFromPath(u *url.URL) string {
	base := path.Base(u.Path)
	decoded, err := url.PathUnescape(base)
	if err != nil {
		return ""
	}
	cleanBase := strings.TrimSpace(decoded)
	if strings.HasSuffix(strings.ToLower(cleanBase), ".pdf") {
		return sanitizeFilename(cleanBase)
	}
	if isMeaningfulName(cleanBase) {
		return sanitizeFilename(cleanBase + ".pdf")
	}
	return ""
}

func filenameFromMoodleID(u *url.URL) string {
	idVal := u.Query().Get("id")
	if idVal == "" {
		return ""
	}
	cleanID := strings.TrimSpace(idVal)
	if strings.Contains(cleanID, "/") {
		subparts := strings.Split(cleanID, "/")
		last := subparts[len(subparts)-1]
		if strings.HasSuffix(strings.ToLower(last), ".pdf") {
			return sanitizeFilename(last)
		}
	}
	return sanitizeFilename(fmt.Sprintf("Recurso_%s", cleanID))
}

func isMeaningfulName(name string) bool {
	clean := strings.ToLower(strings.TrimSpace(name))
	if clean == "" || clean == "." || clean == "/" || clean == "view.php" {
		return false
	}
	scriptExtensions := []string{".php", ".aspx", ".asp", ".jsp", ".do", ".cgi", ".html", ".htm"}
	for _, ext := range scriptExtensions {
		if strings.HasSuffix(clean, ext) {
			return false
		}
	}
	return true
}

func sanitizeFilename(name string) string {
	cleaned := filepath.Base(filepath.Clean(name))
	cleaned = strings.Trim(cleaned, `"' `)
	replacer := strings.NewReplacer(
		"/", "_",
		"\\", "_",
		":", "_",
		"*", "_",
		"?", "_",
		"\"", "_",
		"<", "_",
		">", "_",
		"|", "_",
		"\x00", "",
	)
	cleaned = replacer.Replace(cleaned)
	if cleaned == "" || cleaned == "." {
		return "Recurso_Demo.pdf"
	}
	if !strings.HasSuffix(strings.ToLower(cleaned), ".pdf") {
		cleaned += ".pdf"
	}
	return cleaned
}

func calculateSimulatedSize(taskID int, filename string) int64 {
	baseSizes := []int64{
		2_450_000, // ~2.4 MB
		4_890_000, // ~4.8 MB
		1_780_000, // ~1.8 MB
		3_620_000, // ~3.6 MB
		5_150_000, // ~5.1 MB
	}
	idx := (taskID - 1) % len(baseSizes)
	if idx < 0 {
		idx = 0
	}
	// Add pseudo-random deterministic variation based on taskID and filename length
	jitter := int64((taskID*73123 + len(filename)*4519) % 350000)
	return baseSizes[idx] + jitter
}

func writeDummyPDF(targetPath, filename string) error {
	if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
		return err
	}
	partPath := targetPath + ".godownload.part"
	dummy := fmt.Sprintf("%%PDF-1.4\n%% GoDownloader Showcase Demo\n%% Resource: %s\n1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n2 0 obj\n<< /Type /Pages /Kids [3 0 R] /Count 1 >>\nendobj\n3 0 obj\n<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>\nendobj\nxref\n0 4\n0000000000 65535 f \n0000000010 00000 n \n0000000063 00000 n \n0000000122 00000 n \ntrailer\n<< /Size 4 /Root 1 0 R >>\nstartxref\n197\n%%%%EOF\n", filename)
	if err := os.WriteFile(partPath, []byte(dummy), 0o644); err != nil {
		_ = os.Remove(partPath)
		return err
	}
	if err := os.Rename(partPath, targetPath); err != nil {
		_ = os.Remove(partPath)
		return err
	}
	return nil
}

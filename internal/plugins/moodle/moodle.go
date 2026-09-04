// Package moodle provides an authenticated downloader plugin for Moodle LMS platforms and general HTTP endpoints.
package moodle

import (
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"godownloader/internal/domain"
	"godownloader/internal/logger"
)

var (
	// ErrAuthenticationFailed indicates invalid or expired session cookie.
	ErrAuthenticationFailed = fmt.Errorf("%w: invalid or expired session cookie", domain.ErrAuthenticationFailed)
	// ErrUnexpectedStatus indicates non-2xx HTTP status.
	ErrUnexpectedStatus = errors.New("unexpected HTTP response status")
	// ErrInactivityTimeout indicates stream stalled without receiving data within the timeout window.
	ErrInactivityTimeout = errors.New("download stream stalled: inactivity timeout exceeded")
)

type contextKey string

const (
	cookieContextKey contextKey = "moodle_cookie"
	taskIDContextKey contextKey = "moodle_task_id"
)

// Plugin handles resource downloads from Moodle platforms (e.g. UFRO Campus Virtual) and generic HTTP endpoints.
type Plugin struct {
	client            *http.Client
	logger            *logger.Logger
	inactivityTimeout time.Duration
}

// Option configures a Plugin instance.
type Option func(*Plugin)

// WithInactivityTimeout sets maximum allowed idle time between received bytes on the stream.
func WithInactivityTimeout(d time.Duration) Option {
	return func(p *Plugin) {
		p.inactivityTimeout = d
	}
}

// WithHTTPClient allows passing a customized http.Client (e.g. for testing).
func WithHTTPClient(client *http.Client) Option {
	return func(p *Plugin) {
		if client != nil {
			p.client = client
		}
	}
}

// WithLogger associates a debug logger with the Plugin.
func WithLogger(l *logger.Logger) Option {
	return func(p *Plugin) {
		p.logger = l
	}
}

// New creates a new Plugin with default settings.
func New(opts ...Option) *Plugin {
	p := &Plugin{
		inactivityTimeout: 30 * time.Second,
	}
	for _, opt := range opts {
		opt(p)
	}
	if p.client == nil {
		p.client = defaultHTTPClient(p.logger)
	}
	return p
}

func defaultHTTPClient(l *logger.Logger) *http.Client {
	jar, _ := cookiejar.New(nil)
	transport := &http.Transport{
		DialContext: (&net.Dialer{
			Timeout:   15 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 20 * time.Second,
		MaxIdleConns:          100,
		MaxIdleConnsPerHost:   10,
		IdleConnTimeout:       90 * time.Second,
	}
	return &http.Client{
		Jar:       jar,
		Transport: transport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if l != nil && len(via) > 0 {
				taskID, _ := req.Context().Value(taskIDContextKey).(int)
				l.LogTaskRedirect(taskID, via[len(via)-1].URL.String(), req.URL.String(), req.Response.StatusCode)
			}

			target := strings.ToLower(req.URL.String())
			if strings.Contains(target, "/login") || strings.Contains(target, "login.php") {
				return fmt.Errorf("%w: redirected to login page (%s)", ErrAuthenticationFailed, req.URL.String())
			}
			if len(via) >= 10 {
				return errors.New("stopped after 10 redirects")
			}

			// Reliably propagate cookie across redirects within the same domain
			if cookie, ok := req.Context().Value(cookieContextKey).(string); ok && cookie != "" {
				if len(via) > 0 && isSameDomain(req.URL.Hostname(), via[0].URL.Hostname()) {
					req.Header.Set("Cookie", cookie)
				}
			}
			return nil
		},
		Timeout: 0,
	}
}

func isSameDomain(h1, h2 string) bool {
	h1 = strings.ToLower(h1)
	h2 = strings.ToLower(h2)
	return h1 == h2 || strings.HasSuffix(h1, "."+h2) || strings.HasSuffix(h2, "."+h1)
}

type idleTimeoutReader struct {
	r       io.Reader
	closer  io.Closer
	timeout time.Duration
}

func newIdleTimeoutReader(r io.Reader, closer io.Closer, timeout time.Duration) io.Reader {
	if timeout <= 0 {
		return r
	}
	return &idleTimeoutReader{
		r:       r,
		closer:  closer,
		timeout: timeout,
	}
}

func (itr *idleTimeoutReader) Read(p []byte) (int, error) {
	type readResult struct {
		n   int
		err error
	}
	done := make(chan readResult, 1)
	go func() {
		n, err := itr.r.Read(p)
		done <- readResult{n: n, err: err}
	}()

	timer := time.NewTimer(itr.timeout)
	defer timer.Stop()

	select {
	case res := <-done:
		return res.n, res.err
	case <-timer.C:
		if itr.closer != nil {
			_ = itr.closer.Close()
		}
		return 0, ErrInactivityTimeout
	}
}

// Name returns the identifier of this plugin.
func (p *Plugin) Name() string {
	return "moodle"
}

// CanHandle determines if the given URL is supported.
// Matches HTTP/HTTPS URLs, serving as the Moodle and general HTTP handler.
func (p *Plugin) CanHandle(rawURL string) bool {
	u, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	return u.Scheme == "http" || u.Scheme == "https"
}

// Download downloads the resource specified by task.URL using the given cookie.
func (p *Plugin) Download(ctx context.Context, task domain.Task, progress domain.ProgressFunc) (*domain.Result, error) {
	ctx = context.WithValue(ctx, cookieContextKey, task.Cookie)
	ctx = context.WithValue(ctx, taskIDContextKey, task.ID)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, task.URL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	if task.Cookie != "" {
		req.Header.Set("Cookie", task.Cookie)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64; rv:130.0) Gecko/20100101 Firefox/130.0")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,application/pdf,*/*;q=0.8")
	req.Header.Set("Accept-Language", "es-CL,es;q=0.9,en;q=0.8")
	req.Header.Set("Referer", "https://campusvirtual.ufro.cl/")

	resp, err := p.client.Do(req)
	if err != nil {
		if errors.Is(err, ErrAuthenticationFailed) || strings.Contains(err.Error(), ErrAuthenticationFailed.Error()) {
			return nil, ErrAuthenticationFailed
		}
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if p.logger != nil {
		p.logger.LogTaskResponse(
			task.ID,
			resp.StatusCode,
			resp.Header.Get("Content-Type"),
			resp.ContentLength,
			resp.Header.Get("Content-Disposition"),
		)
	}

	if err := checkResponseStatus(resp); err != nil {
		return nil, err
	}

	filename := ExtractFilename(task.URL, resp.Header, task.ID)
	outDir := task.OutputDir
	if outDir == "" {
		outDir = "."
	}

	targetPath := filepath.Join(outDir, filename)
	timeout := p.inactivityTimeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	streamReader := newIdleTimeoutReader(resp.Body, resp.Body, timeout)
	bytesWritten, err := writeStreamToFile(streamReader, targetPath, resp.ContentLength, task, filename, progress)
	if err != nil {
		_ = os.Remove(targetPath)
		return nil, err
	}

	return &domain.Result{
		TaskID:     task.ID,
		URL:        task.URL,
		Filename:   filename,
		BytesRead:  bytesWritten,
		TotalBytes: resp.ContentLength,
		Err:        nil,
	}, nil
}

func checkResponseStatus(resp *http.Response) error {
	if resp.StatusCode == http.StatusSeeOther || resp.StatusCode == http.StatusFound {
		location := resp.Header.Get("Location")
		if strings.Contains(strings.ToLower(location), "login") {
			return fmt.Errorf("%w: redirect to %s", ErrAuthenticationFailed, location)
		}
	}

	if resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusUnauthorized {
		return fmt.Errorf("%w: server returned status %d", ErrAuthenticationFailed, resp.StatusCode)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("%w: status %d (%s)", ErrUnexpectedStatus, resp.StatusCode, resp.Status)
	}

	return nil
}

func writeStreamToFile(reader io.Reader, targetPath string, totalBytes int64, task domain.Task, filename string, progress domain.ProgressFunc) (int64, error) {
	if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
		return 0, fmt.Errorf("failed to create directory: %w", err)
	}

	partPath := targetPath + ".godownload.part"
	file, err := os.Create(partPath)
	if err != nil {
		return 0, fmt.Errorf("failed to create file: %w", err)
	}

	var writeErr error
	var downloaded int64
	defer func() {
		_ = file.Close()
		if writeErr != nil {
			_ = os.Remove(partPath)
		}
	}()

	downloaded, writeErr = copyStreamWithProgress(file, reader, totalBytes, task, filename, progress)
	if writeErr != nil {
		return downloaded, writeErr
	}

	if totalBytes > 0 && downloaded != totalBytes {
		writeErr = fmt.Errorf("incomplete download: expected %d bytes, got %d", totalBytes, downloaded)
		return downloaded, writeErr
	}

	if err := file.Close(); err != nil {
		writeErr = fmt.Errorf("failed to close file: %w", err)
		return downloaded, writeErr
	}

	if err := os.Rename(partPath, targetPath); err != nil {
		writeErr = fmt.Errorf("failed to rename part file: %w", err)
		return downloaded, writeErr
	}

	return downloaded, nil
}

func copyStreamWithProgress(
	dst io.Writer,
	src io.Reader,
	totalBytes int64,
	task domain.Task,
	filename string,
	progress domain.ProgressFunc,
) (int64, error) {
	buf := make([]byte, 32*1024)
	var downloaded int64

	for {
		n, readErr := src.Read(buf)
		if n > 0 {
			if _, wErr := dst.Write(buf[:n]); wErr != nil {
				return downloaded, fmt.Errorf("failed writing to file: %w", wErr)
			}
			downloaded += int64(n)
			if progress != nil {
				progress(domain.ProgressUpdate{
					TaskID:     task.ID,
					URL:        task.URL,
					Filename:   filename,
					BytesRead:  downloaded,
					TotalBytes: totalBytes,
				})
			}
		}
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				break
			}
			return downloaded, fmt.Errorf("stream read error: %w", readErr)
		}
	}

	return downloaded, nil
}

// ExtractFilename determines the best filename from the URL, Content-Disposition header, or fallback.
func ExtractFilename(rawURL string, header http.Header, taskID int) string {
	if name := filenameFromHeader(header); name != "" {
		return name
	}

	if u, err := url.Parse(rawURL); err == nil {
		if name := filenameFromPath(u); name != "" {
			return name
		}
		if name := filenameFromQuery(u); name != "" {
			return name
		}
	}

	return fmt.Sprintf("download_%d.pdf", taskID)
}

func filenameFromHeader(header http.Header) string {
	if header == nil {
		return ""
	}
	cd := header.Get("Content-Disposition")
	if cd == "" {
		return ""
	}
	_, params, err := mime.ParseMediaType(cd)
	if err != nil {
		return ""
	}
	fname := strings.TrimSpace(params["filename"])
	if fname == "" {
		return ""
	}
	return sanitizeFilename(fname)
}

func filenameFromPath(u *url.URL) string {
	base := path.Base(u.Path)
	decoded, err := url.PathUnescape(base)
	if err == nil && isMeaningfulFilename(decoded) {
		return sanitizeFilename(decoded)
	}
	return ""
}

func filenameFromQuery(u *url.URL) string {
	for _, values := range u.Query() {
		for _, val := range values {
			decoded, err := url.QueryUnescape(val)
			if err == nil && strings.HasSuffix(strings.ToLower(decoded), ".pdf") {
				return sanitizeFilename(decoded)
			}
		}
	}
	return ""
}

func isMeaningfulFilename(name string) bool {
	clean := strings.ToLower(strings.TrimSpace(name))
	if clean == "" || clean == "." || clean == "/" {
		return false
	}
	// Common web script handlers should not be treated as target media filenames
	scriptExtensions := []string{".php", ".aspx", ".asp", ".jsp", ".do", ".cgi", ".html", ".htm"}
	for _, ext := range scriptExtensions {
		if strings.HasSuffix(clean, ext) {
			return false
		}
	}
	return strings.Contains(clean, ".")
}

func sanitizeFilename(name string) string {
	cleaned := filepath.Base(filepath.Clean(name))
	cleaned = strings.Trim(cleaned, `"' `)
	// Replace problematic path characters
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
		return "download.pdf"
	}
	return cleaned
}

package moodle

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"time"

	"golang.org/x/net/html"

	"github.com/sebasinmas/MooFetch/internal/logger"
)

// Link kinds reported by ScanPage.
const (
	KindResource   = "resource"   // /mod/resource/view.php?id=N (HTML or redirect to a file)
	KindPluginfile = "pluginfile" // /pluginfile.php/... (direct file URL)
	KindFolder     = "folder"     // /mod/folder/view.php?id=N (expanded one level)
)

// Defaults applied by ScanPage to zero-valued ScanOptions fields.
const (
	DefaultScanDelay      = 1500 * time.Millisecond
	DefaultScanJitter     = 500 * time.Millisecond
	DefaultScanMaxPages   = 30
	DefaultScanMaxRetries = 2

	maxScanBody       = 8 << 20 // 8 MiB cap on every page body read
	maxRetryWait      = 60 * time.Second
	scanBackoffBase   = 2 * time.Second
	repositoryAddress = "https://github.com/sebasinmas/MooFetch"
)

// UserAgentVersion is the version advertised in the default scan User-Agent.
// The composition root may overwrite it (the plugin must not import main).
var UserAgentVersion = "dev"

// ErrRateLimited indicates the server kept answering 429/503 after all
// permitted retries; the scan stops instead of insisting.
var ErrRateLimited = errors.New("server is rate limiting requests (429/503)")

// Link is a downloadable resource discovered on a Moodle page.
type Link struct {
	URL   string // absolute URL, fragment removed
	Title string // visible link text, whitespace collapsed, hidden a11y text dropped
	Kind  string // KindResource, KindPluginfile or KindFolder
}

// ScanOptions tunes ScanPage. Zero values select the documented defaults; a
// negative Delay, Jitter or MaxRetries means 0.
type ScanOptions struct {
	// Delay is the minimum pause between two consecutive requests (default 1500ms).
	Delay time.Duration
	// Jitter is the upper bound of a random extra pause added to Delay (default 500ms).
	Jitter time.Duration
	// MaxPages bounds the total number of pages fetched, including the initial
	// one (default 30). Retries of a throttled page do not count against it but
	// are bounded by MaxRetries.
	MaxPages int
	// MaxRetries is how many times a 429/503 page is retried (default 2).
	MaxRetries int
	// UserAgent identifies the tool; default "MooFetch/<ver> (+repo URL)".
	UserAgent string
	// Sleep waits for d or until ctx is done. Injectable for tests; the default
	// uses a real timer.
	Sleep func(ctx context.Context, d time.Duration) error
	// Logger receives debug diagnostics (never cookies). Falls back to the
	// plugin logger when nil.
	Logger *logger.Logger
}

func (o ScanOptions) withDefaults(fallback *logger.Logger) ScanOptions {
	o.Delay = durationOrDefault(o.Delay, DefaultScanDelay)
	o.Jitter = durationOrDefault(o.Jitter, DefaultScanJitter)
	if o.MaxPages <= 0 {
		o.MaxPages = DefaultScanMaxPages
	}
	switch {
	case o.MaxRetries == 0:
		o.MaxRetries = DefaultScanMaxRetries
	case o.MaxRetries < 0:
		o.MaxRetries = 0
	}
	if o.UserAgent == "" {
		o.UserAgent = fmt.Sprintf("MooFetch/%s (+%s)", UserAgentVersion, repositoryAddress)
	}
	if o.Sleep == nil {
		o.Sleep = sleepCtx
	}
	if o.Logger == nil {
		o.Logger = fallback
	}
	return o
}

func durationOrDefault(d, def time.Duration) time.Duration {
	switch {
	case d == 0:
		return def
	case d < 0:
		return 0
	}
	return d
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// ScanPage fetches pageURL with the session cookie and returns the downloadable
// links found in it, in page order and deduplicated by URL. Folder links are
// fetched (one level, no recursion) and their pluginfile links are inserted
// right after the folder entry.
//
// Requests are strictly sequential and spaced by Delay plus random Jitter.
// Authentication failures abort immediately with ErrAuthenticationFailed.
func (p *Plugin) ScanPage(ctx context.Context, pageURL, cookie string, opts ScanOptions) ([]Link, error) {
	s := &scanner{p: p, cookie: cookie, opts: opts.withDefaults(p.logger)}

	base, err := url.Parse(pageURL)
	if err != nil || base.Host == "" {
		return nil, fmt.Errorf("invalid page URL %q", pageURL)
	}

	pageLinks, err := s.fetchLinks(ctx, pageURL)
	if err != nil {
		return nil, err
	}

	// Links of the page itself win over folder contents on duplicates.
	pageLinks = dedupe(pageLinks)
	seen := make(map[string]bool, len(pageLinks))
	for _, l := range pageLinks {
		seen[l.URL] = true
	}

	out := make([]Link, 0, len(pageLinks))
	for _, l := range pageLinks {
		out = append(out, l)
		if l.Kind != KindFolder {
			continue
		}
		children, err := s.expandFolder(ctx, l.URL)
		if err != nil {
			return nil, err
		}
		for _, c := range dedupe(children) {
			if !seen[c.URL] {
				seen[c.URL] = true
				out = append(out, c)
			}
		}
	}
	return out, nil
}

// dedupe removes repeated URLs keeping the first position and title (an empty
// first title is completed from a later duplicate).
func dedupe(in []Link) []Link {
	idx := make(map[string]int, len(in))
	out := make([]Link, 0, len(in))
	for _, l := range in {
		if i, dup := idx[l.URL]; dup {
			if out[i].Title == "" {
				out[i].Title = l.Title
			}
			continue
		}
		idx[l.URL] = len(out)
		out = append(out, l)
	}
	return out
}

// expandFolder fetches one folder page and returns its pluginfile links.
// Only fatal conditions (auth, rate limiting, cancellation) are returned as
// errors; any other failure skips this folder.
func (s *scanner) expandFolder(ctx context.Context, folderURL string) ([]Link, error) {
	if s.pages >= s.opts.MaxPages {
		s.debug("scan budget exhausted, folder not expanded", "url", folderURL, "max_pages", s.opts.MaxPages)
		return nil, nil
	}
	links, err := s.fetchLinks(ctx, folderURL)
	if err != nil {
		if isFatalScanError(ctx, err) {
			return nil, err
		}
		s.debug("folder skipped", "url", folderURL, "error", err.Error())
		return nil, nil
	}
	var files []Link
	for _, l := range links {
		if l.Kind == KindPluginfile {
			files = append(files, l)
		}
	}
	return files, nil
}

func isFatalScanError(ctx context.Context, err error) bool {
	return errors.Is(err, ErrAuthenticationFailed) ||
		errors.Is(err, ErrRateLimited) ||
		ctx.Err() != nil
}

type scanner struct {
	p      *Plugin
	cookie string
	opts   ScanOptions
	pages  int // pages fetched so far (retries excluded)
}

func (s *scanner) debug(msg string, args ...any) {
	s.opts.Logger.Debug(msg, args...)
}

// fetchLinks downloads a page (with pacing and retries) and extracts links.
func (s *scanner) fetchLinks(ctx context.Context, pageURL string) ([]Link, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if s.pages > 0 {
		if err := s.opts.Sleep(ctx, s.pause()); err != nil {
			return nil, err
		}
	}
	s.pages++

	body, finalURL, err := s.getWithRetries(ctx, pageURL)
	if err != nil {
		return nil, err
	}
	doc, err := html.Parse(bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("parsing HTML of %s: %w", pageURL, err)
	}
	links, isLogin := extractLinks(doc, finalURL)
	if isLogin {
		return nil, fmt.Errorf("%w: response is the login page", ErrAuthenticationFailed)
	}
	return links, nil
}

func (s *scanner) pause() time.Duration {
	d := s.opts.Delay
	if s.opts.Jitter > 0 {
		d += time.Duration(rand.Int64N(int64(s.opts.Jitter))) //nolint:gosec // jitter does not need crypto randomness
	}
	return d
}

// rateLimitedError carries the server-provided wait for a 429/503 answer.
type rateLimitedError struct {
	status     int
	retryAfter time.Duration // 0 when the header is absent or unparseable
}

func (e *rateLimitedError) Error() string {
	return fmt.Sprintf("HTTP %d", e.status)
}

func (s *scanner) getWithRetries(ctx context.Context, pageURL string) ([]byte, *url.URL, error) {
	for attempt := 0; ; attempt++ {
		body, finalURL, err := s.getOnce(ctx, pageURL, attempt)
		var rl *rateLimitedError
		if !errors.As(err, &rl) {
			return body, finalURL, err
		}
		if attempt >= s.opts.MaxRetries {
			return nil, nil, fmt.Errorf("%w: %s still answering %s after %d retries", ErrRateLimited, pageURL, rl, s.opts.MaxRetries)
		}
		wait := rl.retryAfter
		if wait <= 0 {
			wait = backoff(attempt)
		}
		s.debug("rate limited, waiting", "url", pageURL, "status", rl.status, "wait", wait.String(), "attempt", attempt+1)
		if err := s.opts.Sleep(ctx, wait); err != nil {
			return nil, nil, err
		}
	}
}

func backoff(attempt int) time.Duration {
	d := scanBackoffBase
	for i := 0; i < attempt && d < maxRetryWait; i++ {
		d *= 2
	}
	return min(d, maxRetryWait)
}

func (s *scanner) getOnce(ctx context.Context, pageURL string, attempt int) ([]byte, *url.URL, error) {
	ctx = context.WithValue(ctx, cookieContextKey, s.cookie)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, pageURL, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create request: %w", err)
	}
	if s.cookie != "" {
		req.Header.Set("Cookie", s.cookie)
	}
	req.Header.Set("User-Agent", s.opts.UserAgent)
	req.Header.Set("Accept", "text/html,application/xhtml+xml;q=0.9,*/*;q=0.5")
	req.Header.Set("Accept-Language", "es-CL,es;q=0.9,en;q=0.8")

	s.debug("scan request", "url", pageURL, "attempt", attempt+1)
	resp, err := s.p.client.Do(req)
	if err != nil {
		if errors.Is(err, ErrAuthenticationFailed) || strings.Contains(err.Error(), ErrAuthenticationFailed.Error()) {
			return nil, nil, ErrAuthenticationFailed
		}
		return nil, nil, fmt.Errorf("request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode == http.StatusServiceUnavailable {
		return nil, nil, &rateLimitedError{status: resp.StatusCode, retryAfter: parseRetryAfter(resp.Header.Get("Retry-After"), time.Now())}
	}
	if err := checkResponseStatus(resp); err != nil {
		return nil, nil, err
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxScanBody+1))
	if err != nil {
		return nil, nil, fmt.Errorf("reading body: %w", err)
	}
	if len(body) > maxScanBody {
		return nil, nil, fmt.Errorf("page %s exceeds %d bytes", pageURL, maxScanBody)
	}
	return body, resp.Request.URL, nil
}

// parseRetryAfter understands both delta-seconds and HTTP-date forms, capped
// at maxRetryWait. It returns 0 when the header is empty or invalid.
func parseRetryAfter(v string, now time.Time) time.Duration {
	v = strings.TrimSpace(v)
	if v == "" {
		return 0
	}
	var d time.Duration
	if secs, err := strconv.Atoi(v); err == nil {
		d = time.Duration(secs) * time.Second
	} else if t, err := http.ParseTime(v); err == nil {
		d = t.Sub(now)
	}
	return min(max(d, 0), maxRetryWait)
}

// extractLinks walks the DOM collecting resource links in document order. The
// boolean reports whether the document looks like the Moodle login page.
func extractLinks(doc *html.Node, pageURL *url.URL) ([]Link, bool) {
	var links []Link
	isLogin := false
	var walk func(n *html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode {
			if looksLikeLogin(n) {
				isLogin = true
			}
			if n.Data == "a" {
				if l, ok := linkFromAnchor(n, pageURL); ok {
					links = append(links, l)
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(doc)
	return links, isLogin
}

func looksLikeLogin(n *html.Node) bool {
	if attr(n, "id") == "login" {
		return true
	}
	switch n.Data {
	case "form":
		return strings.Contains(attr(n, "action"), "/login/index.php")
	case "body":
		return hasClass(n, "pagelayout-login")
	}
	return false
}

func attr(n *html.Node, key string) string {
	for _, a := range n.Attr {
		if a.Key == key {
			return a.Val
		}
	}
	return ""
}

func hasClass(n *html.Node, class string) bool {
	for _, c := range strings.Fields(attr(n, "class")) {
		if c == class {
			return true
		}
	}
	return false
}

func linkFromAnchor(n *html.Node, pageURL *url.URL) (Link, bool) {
	href := strings.TrimSpace(attr(n, "href"))
	if href == "" {
		return Link{}, false
	}
	ref, err := url.Parse(href)
	if err != nil {
		return Link{}, false
	}
	abs := pageURL.ResolveReference(ref)
	if !strings.EqualFold(abs.Host, pageURL.Host) || (abs.Scheme != "http" && abs.Scheme != "https") {
		return Link{}, false
	}
	abs.Fragment = ""
	kind := classifyPath(abs)
	if kind == "" {
		return Link{}, false
	}
	title := collapseSpaces(anchorText(n))
	if title == "" && kind == KindPluginfile {
		if name, err := url.PathUnescape(path.Base(abs.Path)); err == nil {
			title = name
		}
	}
	return Link{URL: abs.String(), Title: title, Kind: kind}, true
}

func classifyPath(u *url.URL) string {
	switch {
	case strings.Contains(u.Path, "/pluginfile.php/"):
		return KindPluginfile
	case strings.HasSuffix(u.Path, "/mod/resource/view.php") && u.Query().Get("id") != "":
		return KindResource
	case strings.HasSuffix(u.Path, "/mod/folder/view.php") && u.Query().Get("id") != "":
		return KindFolder
	}
	return ""
}

// anchorText concatenates visible text under n, skipping Moodle's hidden
// accessibility spans (e.g. <span class="accesshide"> Archivo</span>).
func anchorText(n *html.Node) string {
	var sb strings.Builder
	var walk func(*html.Node)
	walk = func(c *html.Node) {
		switch {
		case c.Type == html.TextNode:
			sb.WriteString(c.Data)
		case c.Type == html.ElementNode && isHiddenText(c):
			return
		}
		for ch := c.FirstChild; ch != nil; ch = ch.NextSibling {
			walk(ch)
		}
	}
	for ch := n.FirstChild; ch != nil; ch = ch.NextSibling {
		walk(ch)
	}
	return sb.String()
}

func isHiddenText(n *html.Node) bool {
	return hasClass(n, "accesshide") || hasClass(n, "sr-only") || hasClass(n, "visually-hidden")
}

func collapseSpaces(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

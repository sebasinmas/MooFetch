package auth

import (
	"context"
	"errors"
	"iter"
	"path/filepath"
	"strings"
	"time"

	"github.com/browserutils/kooky"
	_ "github.com/browserutils/kooky/browser/all" // registers Chrome/Chromium/Firefox/Edge/... finders
)

// storeTimeout bounds how long ONE browser store may take to be read (and how
// long we wait for the next store to be discovered). A store that exceeds it
// is abandoned and reported, so a hung browser never blocks the others.
// It is a variable so tests can shrink it.
var storeTimeout = 8 * time.Second

// scanBudget bounds the whole search over all browsers, so several slow
// stores cannot add up to a long wait. Once spent, the store being read and
// the remaining ones are reported and the search ends with what it has.
// It is a variable so tests can shrink it.
var scanBudget = 30 * time.Second

// kookyTraverse is the seam over kooky's store discovery; tests replace it.
var kookyTraverse = kooky.TraverseCookieStores

const unknownBrowser = "(navegador desconocido)"

var (
	errInternalRead = errors.New("error interno al leer el almacén de cookies")
	errScanBudget   = errors.New("tiempo total agotado leyendo los navegadores")
)

// safeClose closes s, containing any panic.
func safeClose(s storeReader) {
	defer func() { _ = recover() }()
	_ = s.Close()
}

// storeReader is one browser profile we can read cookies from. The kooky
// stores (Chromium family, Safari...) and our own Firefox profiles implement it.
type storeReader interface {
	Browser() string
	Profile() string
	// Read returns the cookies accepted by filter, how the file was read and,
	// if something failed, the error. It may return cookies AND an error.
	Read(ctx context.Context, filter kooky.Filter) ([]*kooky.Cookie, Strategy, error)
	Close() error
}

// listStores is the seam over the real browsers; tests replace it so no real
// profile is ever read.
var listStores = defaultStores

// kookyStore adapts a kooky.CookieStore. Files are opened by kooky itself
// (with FILE_SHARE_DELETE on Windows). We never copy these stores: decrypting
// Chromium cookies needs the "Local State" file next to the original.
type kookyStore struct{ kooky.CookieStore }

func (s kookyStore) Read(ctx context.Context, filter kooky.Filter) ([]*kooky.Cookie, Strategy, error) {
	var out []*kooky.Cookie
	var firstErr error
	for c, err := range s.TraverseCookies(filter) {
		if ctx.Err() != nil {
			break
		}
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		if c != nil {
			out = append(out, c)
		}
	}
	if firstErr != nil && len(out) == 0 {
		return nil, StrategyNone, firstErr
	}
	return out, StrategyShared, firstErr
}

// defaultStores yields our Firefox profiles first (they also cover the
// session backup kooky ignores) and then every kooky store, skipping the
// Firefox ones already covered by ours.
func defaultStores(ctx context.Context) iter.Seq2[storeReader, error] {
	return func(yield func(storeReader, error) bool) {
		covered, ok := yieldFirefoxProfiles(yield)
		if !ok {
			return
		}
		yieldKookyStores(ctx, covered, yield)
	}
}

// yieldFirefoxProfiles yields one reader per Firefox profile folder found and
// returns the set of folders it covers. ok is false if the consumer stopped.
func yieldFirefoxProfiles(yield func(storeReader, error) bool) (covered map[string]bool, ok bool) {
	covered = map[string]bool{}
	for _, dir := range firefoxProfileDirs() {
		covered[filepath.Clean(dir)] = true
		if !yield(firefoxProfile{dir: dir}, nil) {
			return covered, false
		}
	}
	return covered, true
}

// yieldKookyStores yields the stores kooky finds, except Firefox profiles
// that are in covered.
func yieldKookyStores(ctx context.Context, covered map[string]bool, yield func(storeReader, error) bool) {
	for s, err := range kookyTraverse(ctx) {
		if err != nil {
			if !yield(nil, err) {
				return
			}
			continue
		}
		if s == nil {
			continue
		}
		if strings.EqualFold(s.Browser(), "firefox") && covered[filepath.Clean(filepath.Dir(s.FilePath()))] {
			safeClose(kookyStore{s})
			continue
		}
		if !yield(kookyStore{s}, nil) {
			return
		}
	}
}

// storeItem is one discovered store or one discovery error.
type storeItem struct {
	store storeReader
	err   error
}

// discoverStores streams the stores from listStores on its own goroutine so
// a finder that hangs cannot block the caller (it waits with a timeout).
// Closing done stops the producer; stores it never delivered are closed.
func discoverStores(ctx context.Context) (<-chan storeItem, func()) {
	ctx, cancel := context.WithCancel(ctx)
	list := listStores
	items := make(chan storeItem)
	go func() {
		defer close(items)
		defer func() {
			if recover() != nil { // a panic in a finder must not kill the process; never echo its value
				select {
				case items <- storeItem{err: errInternalRead}:
				case <-ctx.Done():
				}
			}
		}()
		for s, err := range list(ctx) {
			select {
			case items <- storeItem{store: s, err: err}:
			case <-ctx.Done():
				if s != nil {
					safeClose(s)
				}
				return
			}
		}
	}()
	return items, cancel
}

// readOutcome is the result of reading one store.
type readOutcome struct {
	cookies  []*kooky.Cookie
	strategy Strategy
	err      error
}

// readStoreTimed reads one store on its own goroutine and gives up after
// storeTimeout. The goroutine closes the store when it finishes, so a store
// is always closed exactly once, even if it was abandoned.
func readStoreTimed(ctx context.Context, s storeReader, filter kooky.Filter) readOutcome {
	rctx, cancel := context.WithCancel(ctx)
	defer cancel()
	done := make(chan readOutcome, 1) // buffered: an abandoned reader never blocks on it
	go func() {
		done <- safeRead(rctx, s, filter)
		safeClose(s) // after the send: a slow Close must not delay the result
	}()
	timer := time.NewTimer(storeTimeout)
	defer timer.Stop()
	select {
	case o := <-done:
		return o
	case <-timer.C:
		return readOutcome{strategy: StrategyNone, err: errStoreTimeout}
	case <-ctx.Done():
		return readOutcome{strategy: StrategyNone, err: ctx.Err()}
	}
}

// safeRead calls s.Read containing any panic. The panic value is never
// echoed: it could hold cookie data.
func safeRead(ctx context.Context, s storeReader, filter kooky.Filter) (o readOutcome) {
	defer func() {
		if recover() != nil {
			o = readOutcome{strategy: StrategyNone, err: errInternalRead}
		}
	}()
	cookies, strategy, err := s.Read(ctx, filter)
	return readOutcome{cookies: cookies, strategy: strategy, err: err}
}

// sessionFilter keeps MoodleSession[suffix] cookies that apply to domain.
// Moodle lets each site add a suffix to the cookie name (for example
// "MoodleSessionufro"), so the match is by prefix, not by exact name.
func sessionFilter(domain string) kooky.Filter {
	return kooky.FilterFunc(func(c *kooky.Cookie) bool {
		return isSessionCookieName(c.Name) && domainMatches(c.Domain, domain)
	})
}

// usable reports whether c can be sent: non-empty and not expired.
func usable(c *kooky.Cookie, now time.Time) bool {
	if c == nil || c.Value == "" {
		return false
	}
	return c.Expires.IsZero() || !c.Expires.Before(now)
}

// newest returns the most recently created usable cookie.
func newest(cookies []*kooky.Cookie, now time.Time) *kooky.Cookie {
	var best *kooky.Cookie
	for _, c := range cookies {
		if usable(c, now) && (best == nil || c.Creation.After(best.Creation)) {
			best = c
		}
	}
	return best
}

// buildReport turns the result of one store into its StoreReport. It returns
// the best usable cookie of the store (or nil).
func buildReport(s storeReader, o readOutcome, now time.Time) (StoreReport, *kooky.Cookie) {
	r := StoreReport{Browser: displayBrowser(s.Browser()), Profile: s.Profile(), Strategy: o.strategy}
	if r.Strategy == "" {
		r.Strategy = StrategyNone
	}
	if best := newest(o.cookies, now); best != nil {
		// Err may still hold a secondary problem (for example a temporary
		// copy that could not be deleted); it is diagnostic only.
		r.Outcome, r.Err = OutcomeFound, o.err
		return r, best
	}
	if o.err != nil {
		r.Outcome, r.Err = classifyError(o.err), o.err
		return r, nil
	}
	r.Outcome = OutcomeNoCookie
	return r, nil
}

// discoveryReport reports an error raised while looking for stores, which
// carries no store identity. It returns false for errors that only mean "this
// browser type does not exist here" (a missing file of a browser that is not
// installed, an unsupported browser type): they say nothing useful.
func discoveryReport(err error) (StoreReport, bool) {
	if err == nil || ignorableStoreError(err) {
		return StoreReport{}, false
	}
	return StoreReport{Browser: unknownBrowser, Outcome: classifyError(err), Strategy: StrategyNone, Err: err}, true
}

// scan reads every store sequentially and returns one report per store plus
// the newest usable cookie found. Errors of one store never stop the others,
// and the whole search is bounded by scanBudget.
func scan(ctx context.Context, domain string) ([]StoreReport, *kooky.Cookie) {
	filter := sessionFilter(domain)
	bctx, cancel := context.WithTimeout(ctx, scanBudget)
	defer cancel()
	items, stop := discoverStores(bctx)
	defer stop()
	var (
		reports []StoreReport
		best    *kooky.Cookie
	)
	for bctx.Err() == nil {
		it, ok, timedOut := nextStore(bctx, items)
		if timedOut {
			reports = append(reports, failureReport("(búsqueda de navegadores)", errStoreTimeout))
		}
		if !ok {
			break
		}
		rep, c, keep := visit(bctx, it, filter)
		if keep {
			reports = append(reports, rep)
		}
		if c != nil && (best == nil || c.Creation.After(best.Creation)) {
			best = c
		}
	}
	if ctx.Err() == nil && bctx.Err() != nil { // our budget ran out, not the caller's context
		reports = append(reports, failureReport("(navegadores restantes)", errScanBudget))
	}
	return reports, best
}

// visit handles one discovered item. keep is false when the item is not worth
// a report. If the scan budget ends while reading, the store is reported with
// errScanBudget instead of a bare "deadline exceeded".
func visit(ctx context.Context, it storeItem, filter kooky.Filter) (rep StoreReport, c *kooky.Cookie, keep bool) {
	if it.store == nil {
		rep, keep = discoveryReport(it.err)
		return rep, nil, keep
	}
	o := readStoreTimed(ctx, it.store, filter)
	if errors.Is(o.err, context.DeadlineExceeded) {
		o.err = errScanBudget
	}
	rep, c = buildReport(it.store, o, time.Now())
	return rep, c, true
}

// failureReport is a report for a problem that has no store identity.
func failureReport(browser string, err error) StoreReport {
	return StoreReport{Browser: browser, Outcome: OutcomeUnreadable, Strategy: StrategyNone, Err: err}
}

// nextStore waits for the next discovered store. ok is false when discovery
// finished, was cancelled or timed out (timedOut).
func nextStore(ctx context.Context, items <-chan storeItem) (it storeItem, ok, timedOut bool) {
	timer := time.NewTimer(storeTimeout)
	defer timer.Stop()
	select {
	case it, ok = <-items:
		return it, ok, false
	case <-timer.C:
		return storeItem{}, false, true
	case <-ctx.Done():
		return storeItem{}, false, false
	}
}

// displayBrowser gives browsers a readable name ("chrome" -> "Chrome").
func displayBrowser(name string) string {
	if name == "" {
		return unknownBrowser
	}
	return strings.ToUpper(name[:1]) + name[1:]
}

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

const unknownBrowser = "(navegador desconocido)"

var errInternalRead = errors.New("error interno al leer el almacén de cookies")

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
	for s, err := range kooky.TraverseCookieStores(ctx) {
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
			_ = s.Close()
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
	seq := listStores(ctx)
	items := make(chan storeItem)
	go func() {
		defer close(items)
		for s, err := range seq {
			select {
			case items <- storeItem{store: s, err: err}:
			case <-ctx.Done():
				if s != nil {
					_ = s.Close()
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
	done := make(chan readOutcome, 1)
	go func() {
		defer func() { _ = s.Close() }()
		defer func() {
			if recover() != nil { // never echo the panic value: it could hold cookie data
				done <- readOutcome{strategy: StrategyNone, err: errInternalRead}
			}
		}()
		cookies, strategy, err := s.Read(rctx, filter)
		done <- readOutcome{cookies: cookies, strategy: strategy, err: err}
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
		r.Outcome = OutcomeFound
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
// the newest usable cookie found. Errors of one store never stop the others.
func scan(ctx context.Context, domain string) ([]StoreReport, *kooky.Cookie) {
	filter := sessionFilter(domain)
	items, stop := discoverStores(ctx)
	defer stop()
	var (
		reports []StoreReport
		best    *kooky.Cookie
	)
	for ctx.Err() == nil {
		it, ok, timedOut := nextStore(ctx, items)
		if timedOut {
			reports = append(reports, StoreReport{Browser: "(búsqueda de navegadores)", Outcome: OutcomeUnreadable, Strategy: StrategyNone, Err: errStoreTimeout})
		}
		if !ok {
			break
		}
		if it.store == nil {
			if r, keep := discoveryReport(it.err); keep {
				reports = append(reports, r)
			}
			continue
		}
		now := time.Now()
		rep, c := buildReport(it.store, readStoreTimed(ctx, it.store, filter), now)
		reports = append(reports, rep)
		if c != nil && (best == nil || c.Creation.After(best.Creation)) {
			best = c
		}
	}
	return reports, best
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

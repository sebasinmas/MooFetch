package auth

import (
	"context"
	"errors"
	"iter"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/browserutils/kooky"
)

// fakeStore is a storeReader that never touches a real browser.
type fakeStore struct {
	browser, profile string
	cookies          []*kooky.Cookie
	strategy         Strategy
	err              error
	hang             bool // blocks until the read context is cancelled
	boom             bool // panics inside Read
	boomClose        bool // panics inside Close
	release          chan struct{}
	closed           atomic.Int32
}

func (f *fakeStore) Browser() string { return f.browser }
func (f *fakeStore) Profile() string { return f.profile }

func (f *fakeStore) Close() error {
	f.closed.Add(1)
	if f.boomClose {
		panic("close exploded")
	}
	return nil
}

func (f *fakeStore) Read(ctx context.Context, _ kooky.Filter) ([]*kooky.Cookie, Strategy, error) {
	if f.release != nil { // ignores the context: only the test releases it
		<-f.release
		return nil, StrategyNone, errors.New("released")
	}
	if f.hang {
		<-ctx.Done()
		return nil, StrategyNone, ctx.Err()
	}
	if f.boom {
		panic("secret-cookie-value")
	}
	return f.cookies, f.strategy, f.err
}

// stubStores replaces the store discovery with the given items.
func stubStores(t *testing.T, items ...storeItem) {
	t.Helper()
	old := listStores
	listStores = func(context.Context) iter.Seq2[storeReader, error] {
		return func(yield func(storeReader, error) bool) {
			for _, it := range items {
				if !yield(it.store, it.err) {
					return
				}
			}
		}
	}
	t.Cleanup(func() { listStores = old })
}

// stubStoreList is stubStores for plain stores.
func stubStoreList(t *testing.T, stores ...*fakeStore) {
	t.Helper()
	items := make([]storeItem, len(stores))
	for i, s := range stores {
		items[i] = storeItem{store: s}
	}
	stubStores(t, items...)
}

func setStoreTimeout(t *testing.T, d time.Duration) {
	t.Helper()
	old := storeTimeout
	storeTimeout = d
	t.Cleanup(func() { storeTimeout = old })
}

// noSleep makes every pause instant and counts them.
func noSleep(t *testing.T) *atomic.Int32 {
	t.Helper()
	var n atomic.Int32
	old := sleepCtx
	sleepCtx = func(ctx context.Context, _ time.Duration) error {
		n.Add(1)
		return ctx.Err()
	}
	t.Cleanup(func() { sleepCtx = old })
	return &n
}

func ck(val string, created time.Time, expires time.Time) *kooky.Cookie {
	c := &kooky.Cookie{}
	c.Name, c.Value, c.Creation, c.Expires = CookieName, val, created, expires
	return c
}

// TestMain keeps the temporary-copy folders of the whole package in one
// private folder, so tests never touch (or sweep) the real temp directory.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "auth-test-")
	if err != nil {
		panic(err)
	}
	tempBase = func() string { return dir }
	code := m.Run()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}

package auth

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/browserutils/kooky"
	"github.com/browserutils/kooky/browser/firefox"
	"github.com/pierrec/lz4/v4"
)

// Why this file exists: Moodle's session cookie has no expiry date, and Firefox
// keeps such "session cookies" out of cookies.sqlite. They live in the session
// backup (sessionstore-backups/recovery.jsonlz4). kooky only reads
// cookies.sqlite and does not know newer profile folders such as
// ~/.config/mozilla/firefox, so we also look there ourselves.

const (
	mozLz4Magic   = "mozLz40\x00"
	maxSessionLen = 64 << 20 // 64 MiB safety limit for the decompressed session file
)

var sessionFiles = []string{"recovery.jsonlz4", "recovery.baklz4", "previous.jsonlz4"}

// firefoxRoots lists folders that can hold Firefox profiles. Tests replace it.
var firefoxRoots = defaultFirefoxRoots

func defaultFirefoxRoots() []string {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	switch runtime.GOOS {
	case "windows":
		if appData := os.Getenv("APPDATA"); appData != "" {
			return []string{filepath.Join(appData, "Mozilla", "Firefox", "Profiles")}
		}
		return nil
	case "darwin":
		return []string{filepath.Join(home, "Library", "Application Support", "Firefox", "Profiles")}
	}
	config := os.Getenv("XDG_CONFIG_HOME")
	if config == "" {
		config = filepath.Join(home, ".config")
	}
	return []string{
		filepath.Join(config, "mozilla", "firefox"),
		filepath.Join(home, ".mozilla", "firefox"),
		filepath.Join(home, "snap", "firefox", "common", ".mozilla", "firefox"),
		filepath.Join(home, ".var", "app", "org.mozilla.firefox", ".mozilla", "firefox"),
	}
}

// firefoxProfileDirs returns the profile folders found under the known roots.
func firefoxProfileDirs() []string {
	var dirs []string
	for _, root := range firefoxRoots() {
		entries, err := os.ReadDir(root)
		if err != nil {
			continue
		}
		for _, e := range entries {
			dir := filepath.Join(root, e.Name())
			if !e.IsDir() {
				continue
			}
			if fileExists(filepath.Join(dir, "cookies.sqlite")) || fileExists(filepath.Join(dir, "sessionstore-backups")) {
				dirs = append(dirs, dir)
			}
		}
	}
	return dirs
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// firefoxProfile is one Firefox profile folder read by MooFetch itself:
// cookies.sqlite and the session backup, each with the resilient file
// strategies (shared read, then temporary copy). It implements storeReader.
type firefoxProfile struct{ dir string }

func (firefoxProfile) Browser() string   { return "firefox" }
func (p firefoxProfile) Profile() string { return filepath.Base(p.dir) }
func (firefoxProfile) Close() error      { return nil }

// profileRead accumulates the partial results of one profile.
type profileRead struct {
	cookies  []*kooky.Cookie
	strategy Strategy
	err      error
}

// add merges one file's result. A missing file is normal (not every profile
// has every file) and is ignored.
func (p *profileRead) add(cookies []*kooky.Cookie, strategy Strategy, err error) {
	p.cookies = append(p.cookies, cookies...)
	if err != nil {
		if p.err == nil && !errors.Is(err, fs.ErrNotExist) {
			p.err = err
		}
		return
	}
	if strategy == StrategyCopy || p.strategy == StrategyNone {
		p.strategy = strategy
	}
}

// Read returns the matching cookies of the profile: cookies.sqlite first,
// then the session backups. Errors are reported, not swallowed; they only
// matter to the caller when no cookie was found.
func (p firefoxProfile) Read(ctx context.Context, filter kooky.Filter) ([]*kooky.Cookie, Strategy, error) {
	res := profileRead{strategy: StrategyNone}
	sqlite := filepath.Join(p.dir, "cookies.sqlite")
	if fileExists(sqlite) {
		res.add(readSQLiteResilient(ctx, sqlite, filter))
	}
	for _, name := range sessionFiles {
		path := filepath.Join(p.dir, "sessionstore-backups", name)
		if ctx.Err() != nil {
			return res.cookies, res.strategy, ctx.Err()
		}
		res.add(readSessionFile(ctx, path, filter))
	}
	return res.cookies, res.strategy, res.err
}

// readSessionFile reads one session backup. A missing file returns
// fs.ErrNotExist (the caller ignores it).
func readSessionFile(ctx context.Context, path string, filter kooky.Filter) ([]*kooky.Cookie, Strategy, error) {
	raw, strategy, err := readFileResilient(ctx, path)
	if err != nil {
		return nil, strategy, err
	}
	data, err := decodeMozLz4(raw)
	if err != nil {
		return nil, strategy, fmt.Errorf("sesión de Firefox ilegible (%s): %w", filepath.Base(path), err)
	}
	var out []*kooky.Cookie
	for _, c := range parseSessionCookies(data, fileModTime(path)) {
		if filter.Filter(c) {
			out = append(out, c)
		}
	}
	return out, strategy, nil
}

// readSQLite reads cookies.sqlite through kooky (which opens the file sharing
// it with the browser on Windows). Tests replace it.
var readSQLite = func(path string, filter kooky.Filter) ([]*kooky.Cookie, error) {
	var out []*kooky.Cookie
	var firstErr error
	for c, err := range firefox.TraverseCookies(path, filter) {
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
	if len(out) > 0 {
		return out, nil
	}
	return nil, firstErr
}

// readSQLiteResilient reads cookies.sqlite: shared read with lock retries
// first, then a temporary copy (with its -wal and -shm siblings).
func readSQLiteResilient(ctx context.Context, path string, filter kooky.Filter) ([]*kooky.Cookie, Strategy, error) {
	var cookies []*kooky.Cookie
	err := retryOnLock(ctx, func() error {
		var rerr error
		cookies, rerr = readSQLite(path, filter)
		return rerr
	})
	if err == nil {
		return cookies, StrategyShared, nil
	}
	if !copyWorthTrying(ctx, err) {
		return nil, StrategyNone, err
	}
	cerr := withTempCopy(ctx, path, []string{"-wal", "-shm"}, func(copyPath string) error {
		var rerr error
		cookies, rerr = readSQLite(copyPath, filter)
		return rerr
	})
	if cerr != nil {
		return nil, StrategyNone, fmt.Errorf("%w; copia temporal: %v", err, cerr)
	}
	return cookies, StrategyCopy, nil
}

func fileModTime(path string) time.Time {
	if info, err := os.Stat(path); err == nil {
		return info.ModTime()
	}
	return time.Time{}
}

// parseSessionCookies extracts the top-level "cookies" list of a session backup.
// created is used as the creation time so newer backups win over older ones.
func parseSessionCookies(data []byte, created time.Time) []*kooky.Cookie {
	var doc struct {
		Cookies []struct {
			Host  string `json:"host"`
			Name  string `json:"name"`
			Value string `json:"value"`
		} `json:"cookies"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil
	}
	var out []*kooky.Cookie
	for _, c := range doc.Cookies {
		if c.Name == "" || c.Value == "" {
			continue
		}
		ck := &kooky.Cookie{}
		ck.Name, ck.Value, ck.Domain, ck.Creation = c.Name, c.Value, c.Host, created
		out = append(out, ck)
	}
	return out
}

// decodeMozLz4 decompresses Mozilla's "mozlz4" format: an 8-byte magic, a
// 4-byte little-endian size, then one LZ4 block.
func decodeMozLz4(b []byte) ([]byte, error) {
	if len(b) < 12 || !bytes.HasPrefix(b, []byte(mozLz4Magic)) {
		return nil, errors.New("formato mozlz4 inválido")
	}
	size := binary.LittleEndian.Uint32(b[8:12])
	if size == 0 || size > maxSessionLen {
		return nil, errors.New("tamaño mozlz4 inválido")
	}
	dst := make([]byte, size)
	n, err := lz4.UncompressBlock(b[12:], dst)
	if err != nil {
		return nil, err
	}
	return dst[:n], nil
}

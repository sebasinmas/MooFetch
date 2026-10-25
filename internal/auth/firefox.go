package auth

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
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

// firefoxExtraCookies yields cookies from Firefox profiles that kooky does not
// cover: cookies.sqlite in newer folders, and session cookies from the session
// backup. Read errors are skipped; a missing file is not an error here.
func firefoxExtraCookies(ctx context.Context, filters ...kooky.Filter) kooky.CookieSeq {
	return func(yield func(*kooky.Cookie, error) bool) {
		for _, dir := range firefoxProfileDirs() {
			if ctx.Err() != nil {
				return
			}
			for _, c := range profileCookies(dir, filters) {
				if !yield(c, nil) {
					return
				}
			}
		}
	}
}

// profileCookies returns the matching cookies of one profile: cookies.sqlite
// first, then the session backup.
func profileCookies(dir string, filters []kooky.Filter) []*kooky.Cookie {
	var out []*kooky.Cookie
	sqlite := filepath.Join(dir, "cookies.sqlite")
	if fileExists(sqlite) {
		for c, err := range firefox.TraverseCookies(sqlite, filters...) {
			if err == nil && c != nil {
				out = append(out, c)
			}
		}
	}
	for _, c := range sessionStoreCookies(dir) {
		if matchesAll(c, filters) {
			out = append(out, c)
		}
	}
	return out
}

func matchesAll(c *kooky.Cookie, filters []kooky.Filter) bool {
	for _, f := range filters {
		if !f.Filter(c) {
			return false
		}
	}
	return true
}

// sessionStoreCookies reads the session cookies saved in a profile's session backup.
func sessionStoreCookies(profileDir string) []*kooky.Cookie {
	var out []*kooky.Cookie
	for _, name := range sessionFiles {
		raw, err := os.ReadFile(filepath.Join(profileDir, "sessionstore-backups", name))
		if err != nil {
			continue
		}
		data, err := decodeMozLz4(raw)
		if err != nil {
			continue
		}
		out = append(out, parseSessionCookies(data, fileModTime(filepath.Join(profileDir, "sessionstore-backups", name)))...)
	}
	return out
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

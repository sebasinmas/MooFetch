package auth

import (
	"context"
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/browserutils/kooky"
	"github.com/pierrec/lz4/v4"
)

func mozLz4(t *testing.T, plain []byte) []byte {
	t.Helper()
	dst := make([]byte, lz4.CompressBlockBound(len(plain)))
	n, err := lz4.CompressBlock(plain, dst, nil)
	if err != nil {
		t.Fatal(err)
	}
	if n == 0 { // incompressible: not expected for our JSON, but keep the helper honest
		t.Fatal("test data did not compress")
	}
	out := append([]byte(mozLz4Magic), 0, 0, 0, 0)
	binary.LittleEndian.PutUint32(out[8:12], uint32(len(plain)))
	return append(out, dst[:n]...)
}

const sessionJSON = `{"windows":[],"cookies":[
 {"host":"campusvirtual.ufro.cl","name":"MoodleSession","value":"abc123","path":"/"},
 {"host":"campusvirtual.ufro.cl","name":"Other","value":"zzz","path":"/"},
 {"host":"other.example.com","name":"MoodleSession","value":"nope","path":"/"},
 {"host":"campusvirtual.ufro.cl","name":"MoodleSession","value":"","path":"/"}
]}`

func fakeFirefoxProfile(t *testing.T, fileName string, content []byte) string {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, "abc.default-release")
	if err := os.MkdirAll(filepath.Join(dir, "sessionstore-backups"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "sessionstore-backups", fileName), content, 0o600); err != nil {
		t.Fatal(err)
	}
	old := firefoxRoots
	firefoxRoots = func() []string { return []string{root, filepath.Join(root, "missing")} }
	t.Cleanup(func() { firefoxRoots = old })
	return dir
}

func TestDecodeMozLz4(t *testing.T) {
	plain := []byte(sessionJSON)
	got, err := decodeMozLz4(mozLz4(t, plain))
	if err != nil || string(got) != sessionJSON {
		t.Fatalf("round trip failed: %v", err)
	}

	bad := map[string][]byte{
		"too short":   []byte("mozLz40"),
		"wrong magic": append([]byte("notmoz!!"), make([]byte, 8)...),
		"zero size":   append([]byte(mozLz4Magic), 0, 0, 0, 0, 1),
		"huge size":   append([]byte(mozLz4Magic), 0xff, 0xff, 0xff, 0xff, 1),
		"bad block":   append(append([]byte(mozLz4Magic), 10, 0, 0, 0), 0xf0, 0xff, 0xff),
	}
	for name, in := range bad {
		if _, err := decodeMozLz4(in); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
}

func TestParseSessionCookies(t *testing.T) {
	created := time.Now()
	got := parseSessionCookies([]byte(sessionJSON), created)
	if len(got) != 3 { // the empty value is skipped
		t.Fatalf("got %d cookies, want 3", len(got))
	}
	if got[0].Name != "MoodleSession" || got[0].Value != "abc123" || got[0].Domain != "campusvirtual.ufro.cl" || !got[0].Creation.Equal(created) {
		t.Fatalf("unexpected cookie: %+v", got[0])
	}
	if parseSessionCookies([]byte("not json"), created) != nil {
		t.Fatal("invalid JSON must give no cookies")
	}
}

func TestFirefoxExtraCookies_FindsSessionCookie(t *testing.T) {
	fakeFirefoxProfile(t, "recovery.jsonlz4", mozLz4(t, []byte(sessionJSON)))

	// Use the real filter through the real seam, with only the Firefox source.
	filter := kooky.FilterFunc(func(c *kooky.Cookie) bool {
		return isSessionCookieName(c.Name) && domainMatches(c.Domain, "campusvirtual.ufro.cl")
	})
	var values []string
	for c, err := range firefoxExtraCookies(context.Background(), filter) {
		if err != nil {
			t.Fatal(err)
		}
		values = append(values, c.Value)
	}
	if len(values) != 1 || values[0] != "abc123" {
		t.Fatalf("got %v, want [abc123]", values)
	}
}

func TestFirefoxExtraCookies_IgnoresBrokenFilesAndStopsOnCancel(t *testing.T) {
	fakeFirefoxProfile(t, "recovery.jsonlz4", []byte("garbage"))
	all := kooky.FilterFunc(func(*kooky.Cookie) bool { return true })
	for c, err := range firefoxExtraCookies(context.Background(), all) {
		t.Fatalf("unexpected cookie %v %v", c, err)
	}

	fakeFirefoxProfile(t, "recovery.jsonlz4", mozLz4(t, []byte(sessionJSON)))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for c := range firefoxExtraCookies(ctx, all) {
		t.Fatalf("cancelled context must yield nothing, got %v", c)
	}
}

func TestFirefoxProfileDirs_SkipsFoldersWithoutData(t *testing.T) {
	root := t.TempDir()
	for _, d := range []string{"empty", "withcookies", "Crash Reports"} {
		if err := os.MkdirAll(filepath.Join(root, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "withcookies", "cookies.sqlite"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "profiles.ini"), nil, 0o600); err != nil { // a file, not a folder
		t.Fatal(err)
	}
	old := firefoxRoots
	firefoxRoots = func() []string { return []string{root} }
	t.Cleanup(func() { firefoxRoots = old })

	dirs := firefoxProfileDirs()
	if len(dirs) != 1 || filepath.Base(dirs[0]) != "withcookies" {
		t.Fatalf("got %v", dirs)
	}
}

func TestDefaultFirefoxRoots_IncludesXDGFolder(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "/tmp/xdg-test")
	t.Setenv("APPDATA", "/tmp/appdata-test")
	roots := defaultFirefoxRoots()
	if len(roots) == 0 {
		t.Fatal("expected at least one root")
	}
	found := false
	for _, r := range roots {
		if r == filepath.Join("/tmp/xdg-test", "mozilla", "firefox") || r == filepath.Join("/tmp/appdata-test", "Mozilla", "Firefox", "Profiles") {
			found = true
		}
	}
	if !found && isLinuxLike() {
		t.Fatalf("XDG folder missing from %v", roots)
	}
}

func TestIgnorableStoreError(t *testing.T) {
	if !ignorableStoreError(errors.New("cookie store: not implemented")) {
		t.Fatal("'not implemented' should be ignorable")
	}
	if ignorableStoreError(errors.New("database is locked")) {
		t.Fatal("a real read error must not be ignored")
	}
}

func isLinuxLike() bool { return os.PathSeparator == '/' && runtime.GOOS != "darwin" }

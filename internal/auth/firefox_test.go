package auth

import (
	"context"
	"encoding/binary"
	"errors"
	"io/fs"
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

func allCookies() kooky.Filter { return kooky.FilterFunc(func(*kooky.Cookie) bool { return true }) }

func TestFirefoxProfile_ReadsSessionBackup(t *testing.T) {
	dir := fakeFirefoxProfile(t, "recovery.jsonlz4", mozLz4(t, []byte(sessionJSON)))
	filter := sessionFilter("campusvirtual.ufro.cl")
	cookies, strategy, err := firefoxProfile{dir: dir}.Read(context.Background(), filter)
	if err != nil || strategy != StrategyShared || len(cookies) != 1 || cookies[0].Value != "abc123" {
		t.Fatalf("cookies=%v strategy=%s err=%v", cookies, strategy, err)
	}
	p := firefoxProfile{dir: dir}
	if p.Browser() != "firefox" || p.Profile() != "abc.default-release" || p.Close() != nil {
		t.Fatalf("unexpected identity %s/%s", p.Browser(), p.Profile())
	}
}

func firefoxStoresOf(t *testing.T) []storeReader {
	t.Helper()
	var stores []storeReader
	if _, ok := yieldFirefoxProfiles(func(s storeReader, _ error) bool { stores = append(stores, s); return true }); !ok || len(stores) != 1 {
		t.Fatalf("stores=%v", stores)
	}
	return stores
}

func TestFirefoxProfile_BrokenSessionBackupIsReportedNotSilent(t *testing.T) {
	fakeFirefoxProfile(t, "recovery.jsonlz4", []byte("garbage"))
	stubStores(t, storeItem{store: firefoxStoresOf(t)[0]})
	reports := KookyProvider{}.Diagnose(context.Background(), "campusvirtual.ufro.cl")
	if len(reports) != 1 || reports[0].Browser != "Firefox" || reports[0].Outcome != OutcomeUnreadable || reports[0].Err == nil {
		t.Fatalf("a corrupt session backup must be reported: %+v", reports)
	}
}

func TestFirefoxProfile_FoundThroughScan(t *testing.T) {
	fakeFirefoxProfile(t, "recovery.jsonlz4", mozLz4(t, []byte(sessionJSON)))
	stubStores(t, storeItem{store: firefoxStoresOf(t)[0]})
	got, err := KookyProvider{}.Find(context.Background(), "campusvirtual.ufro.cl")
	if err != nil || got != "MoodleSession=abc123" {
		t.Fatalf("got %q %v", got, err)
	}
}

func TestFirefoxProfile_LockedFilesUseCopyAndErrorsAreKept(t *testing.T) {
	noSleep(t)
	dir := fakeFirefoxProfile(t, "recovery.jsonlz4", mozLz4(t, []byte(sessionJSON)))
	if err := os.WriteFile(filepath.Join(dir, "cookies.sqlite"), []byte("db"), 0o600); err != nil {
		t.Fatal(err)
	}
	oldRead := readSQLite
	readSQLite = func(string, kooky.Filter) ([]*kooky.Cookie, error) { return nil, errBusy }
	t.Cleanup(func() { readSQLite = oldRead })
	dirs := trackTempDirs(t)
	// The sqlite copy attempt consumes one failing open, then the session backup fails its shared read and retries.
	failOpener(t, lockRetries+2)

	// cookies.sqlite stays locked (reader stub) but the session cookie is found
	// through the temporary copy: the cookie wins and the strategy is the copy.
	cookies, strategy, err := firefoxProfile{dir: dir}.Read(context.Background(), sessionFilter("campusvirtual.ufro.cl"))
	if len(cookies) != 1 || strategy != StrategyCopy || err == nil || classifyError(err) != OutcomeLocked {
		t.Fatalf("cookies=%v strategy=%s err=%v", cookies, strategy, err)
	}
	for _, d := range *dirs {
		if _, err := os.Stat(d); !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("temporary folder %s left behind", d)
		}
	}
}

func TestFirefoxProfile_NothingFoundAndCancel(t *testing.T) {
	dir := fakeFirefoxProfile(t, "recovery.jsonlz4", mozLz4(t, []byte(sessionJSON)))
	none := kooky.FilterFunc(func(*kooky.Cookie) bool { return false })
	cookies, _, err := firefoxProfile{dir: dir}.Read(context.Background(), none)
	if err != nil || len(cookies) != 0 {
		t.Fatalf("cookies=%v err=%v", cookies, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := (firefoxProfile{dir: dir}).Read(ctx, allCookies()); !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v", err)
	}
}

func TestReadSQLite_OnInvalidFile(t *testing.T) {
	p := filepath.Join(t.TempDir(), "cookies.sqlite")
	if err := os.WriteFile(p, []byte("not a database"), 0o600); err != nil {
		t.Fatal(err)
	}
	if cs, err := readSQLite(p, allCookies()); err == nil || cs != nil {
		t.Fatalf("an invalid database must be an error, got %v %v", cs, err)
	}
	if _, err := readSQLite(filepath.Join(t.TempDir(), "missing.sqlite"), allCookies()); err == nil {
		t.Fatal("a missing database must be an error")
	}
}

func TestProfileRead_Add(t *testing.T) {
	p := profileRead{strategy: StrategyNone}
	p.add(nil, StrategyNone, &fs.PathError{Op: "open", Path: "x", Err: fs.ErrNotExist})
	if p.err != nil {
		t.Fatalf("a missing file is not an error: %v", p.err)
	}
	p.add(nil, StrategyShared, nil)
	p.add(nil, StrategyCopy, nil)
	p.add(nil, StrategyShared, nil)
	if p.strategy != StrategyCopy {
		t.Fatalf("copy must be remembered, got %s", p.strategy)
	}
	first, second := errors.New("first"), errors.New("second")
	p.add(nil, StrategyNone, first)
	p.add(nil, StrategyNone, second)
	if !errors.Is(p.err, first) {
		t.Fatalf("keeps the first error, got %v", p.err)
	}
}

func TestYieldFirefoxProfiles_StopsWhenConsumerStops(t *testing.T) {
	root := t.TempDir()
	for _, n := range []string{"a", "b"} {
		if err := os.MkdirAll(filepath.Join(root, n, "sessionstore-backups"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	old := firefoxRoots
	firefoxRoots = func() []string { return []string{root} }
	t.Cleanup(func() { firefoxRoots = old })
	n := 0
	covered, ok := yieldFirefoxProfiles(func(storeReader, error) bool { n++; return false })
	if ok || n != 1 || len(covered) != 1 {
		t.Fatalf("ok=%v n=%d covered=%v", ok, n, covered)
	}
}

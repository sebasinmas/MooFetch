package tui

import (
	"context"
	"errors"
	tea "github.com/charmbracelet/bubbletea"
	"reflect"
	"strings"
	"testing"

	"github.com/sebasinmas/MooFetch/internal/domain"
)

const (
	keyDown = "\x1b[B"
	keySpc  = " "
)

var sampleResources = []Resource{
	{Title: "Clase 1", URL: "https://campus.uni.cl/mod/resource/view.php?id=1"},
	{Title: "Clase 2", URL: "https://campus.uni.cl/mod/resource/view.php?id=2"},
	{Title: "Guía", URL: "https://campus.uni.cl/pluginfile.php/9/mod_folder/content/0/guia.pdf"},
}

type scanCall struct{ page, cookie string }

// scanAuthOptions detects a cookie and scans with the supplied function.
func scanAuthOptions(notice *strings.Builder, calls *[]scanCall, scan func(call int) ([]Resource, error)) *AuthOptions {
	opts := testAuthOptions(func(context.Context, string) (string, error) { return "MoodleSession=zzz", nil })
	opts.Notice = notice
	opts.Scan = func(_ context.Context, page, cookie string) ([]Resource, error) {
		*calls = append(*calls, scanCall{page, cookie})
		return scan(len(*calls))
	}
	return opts
}

var (
	authKeys = []string{keyEnter, keyEnter, keyEnter} // splash, campus, consent
	pageKeys = []string{"https://campus.uni.cl/course/view.php?id=5", keyEnter}
)

func TestScanFlow_AllSelectedByDefault(t *testing.T) {
	var notice strings.Builder
	var calls []scanCall
	opts := scanAuthOptions(&notice, &calls, func(int) ([]Resource, error) { return sampleResources, nil })
	scriptedInputs(t, authKeys, pageKeys, []string{keyEnter})

	data, err := RunInteractiveFormWithAuth(context.Background(), opts, false)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{sampleResources[0].URL, sampleResources[1].URL, sampleResources[2].URL}
	if !reflect.DeepEqual(data.URLs, want) {
		t.Errorf("URLs = %v, want all %v", data.URLs, want)
	}
	if data.Cookie != "MoodleSession=zzz" {
		t.Errorf("cookie = %q", data.Cookie)
	}
	if len(calls) != 1 || calls[0].page != "https://campus.uni.cl/course/view.php?id=5" || calls[0].cookie != "MoodleSession=zzz" {
		t.Errorf("scan calls = %+v", calls)
	}
}

func TestScanFlow_DeselectWithSpace(t *testing.T) {
	var notice strings.Builder
	var calls []scanCall
	opts := scanAuthOptions(&notice, &calls, func(int) ([]Resource, error) { return sampleResources, nil })
	// untick the first, move down, untick the second, confirm
	scriptedInputs(t, authKeys, pageKeys, []string{keySpc, keyDown, keySpc, keyEnter})

	data, err := RunInteractiveFormWithAuth(context.Background(), opts, false)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{sampleResources[2].URL}; !reflect.DeepEqual(data.URLs, want) {
		t.Errorf("URLs = %v, want %v", data.URLs, want)
	}
}

func TestScanFlow_EmptyPageFallsBackToManualLinks(t *testing.T) {
	var notice strings.Builder
	var calls []scanCall
	opts := scanAuthOptions(&notice, &calls, func(int) ([]Resource, error) { return sampleResources, nil })
	// empty page URL -> manual links form, which we abort
	scriptedInputs(t, authKeys, []string{keyEnter}, []string{keyCtrlC})

	if _, err := RunInteractiveFormWithAuth(context.Background(), opts, false); !errors.Is(err, ErrFormAborted) {
		t.Fatalf("want ErrFormAborted from the manual form, got %v", err)
	}
	if len(calls) != 0 {
		t.Errorf("Scan must not run for an empty page URL, got %+v", calls)
	}
}

func TestScanFlow_ScanErrorOffersManualLinks(t *testing.T) {
	var notice strings.Builder
	var calls []scanCall
	opts := scanAuthOptions(&notice, &calls, func(int) ([]Resource, error) { return nil, errors.New("boom") })
	// error -> menu: Down selects "pegar a mano" -> manual form aborted
	scriptedInputs(t, authKeys, pageKeys, []string{keyDown, keyEnter}, []string{keyCtrlC})

	if _, err := RunInteractiveFormWithAuth(context.Background(), opts, false); !errors.Is(err, ErrFormAborted) {
		t.Fatalf("want ErrFormAborted, got %v", err)
	}
	if !strings.Contains(notice.String(), "No se pudo leer la página: boom") {
		t.Errorf("notice = %q", notice.String())
	}
}

func TestScanFlow_NoResourcesThenRetry(t *testing.T) {
	var notice strings.Builder
	var calls []scanCall
	opts := scanAuthOptions(&notice, &calls, func(n int) ([]Resource, error) {
		if n == 1 {
			return nil, nil
		}
		return sampleResources[:1], nil
	})
	// no resources -> menu: Enter retries -> page URL again (prefilled) -> select
	scriptedInputs(t, authKeys, pageKeys, []string{keyEnter}, []string{keyEnter}, []string{keyEnter})

	data, err := RunInteractiveFormWithAuth(context.Background(), opts, false)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(notice.String(), "No se encontraron recursos") {
		t.Errorf("notice = %q", notice.String())
	}
	if len(calls) != 2 || calls[1].page != calls[0].page {
		t.Errorf("retry should reuse the previous page URL, calls = %+v", calls)
	}
	if want := []string{sampleResources[0].URL}; !reflect.DeepEqual(data.URLs, want) {
		t.Errorf("URLs = %v", data.URLs)
	}
}

func TestScanFlow_AuthFailureAsksForANewCookie(t *testing.T) {
	var notice strings.Builder
	var calls []scanCall
	opts := scanAuthOptions(&notice, &calls, func(n int) ([]Resource, error) {
		if n == 1 {
			return nil, domain.ErrAuthenticationFailed
		}
		return sampleResources[:1], nil
	})
	scriptedInputs(t, authKeys,
		pageKeys,
		[]string{keyEnter},                 // menu: retry
		[]string{"nueva-cookie", keyEnter}, // new cookie
		[]string{keyEnter},                 // page URL (prefilled)
		[]string{keyEnter},                 // pick everything
	)

	data, err := RunInteractiveFormWithAuth(context.Background(), opts, false)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(notice.String(), "no es válida o expiró") {
		t.Errorf("notice = %q", notice.String())
	}
	if len(calls) != 2 || calls[1].cookie != "MoodleSession=nueva-cookie" {
		t.Errorf("second scan must use the new, normalized cookie: %+v", calls)
	}
	if data.Cookie != "MoodleSession=nueva-cookie" {
		t.Errorf("cookie = %q", data.Cookie)
	}
}

func TestScanFlow_ManualCookieWhenDetectionRefused(t *testing.T) {
	var notice strings.Builder
	var calls []scanCall
	opts := scanAuthOptions(&notice, &calls, func(int) ([]Resource, error) { return sampleResources[:1], nil })
	opts.Detect = func(context.Context, string) (string, error) { return "", errors.New("sin navegador") }
	scriptedInputs(t, authKeys, []string{"abc123", keyEnter}, pageKeys, []string{keyEnter})

	data, err := RunInteractiveFormWithAuth(context.Background(), opts, false)
	if err != nil {
		t.Fatal(err)
	}
	if data.Cookie != "MoodleSession=abc123" || calls[0].cookie != "MoodleSession=abc123" {
		t.Errorf("cookie = %q, scan cookie = %+v", data.Cookie, calls)
	}
}

func TestScanFlow_ContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	var notice strings.Builder
	var calls []scanCall
	opts := scanAuthOptions(&notice, &calls, func(int) ([]Resource, error) {
		cancel()
		return nil, context.Canceled
	})
	scriptedInputs(t, authKeys, pageKeys)

	if _, err := RunInteractiveFormWithAuth(ctx, opts, false); !errors.Is(err, context.Canceled) {
		t.Fatalf("want context.Canceled, got %v", err)
	}
}

func TestScanFlow_AbortOnSelection(t *testing.T) {
	var notice strings.Builder
	var calls []scanCall
	opts := scanAuthOptions(&notice, &calls, func(int) ([]Resource, error) { return sampleResources, nil })
	scriptedInputs(t, authKeys, pageKeys, []string{keyCtrlC})

	if _, err := RunInteractiveFormWithAuth(context.Background(), opts, false); !errors.Is(err, ErrFormAborted) {
		t.Fatalf("want ErrFormAborted, got %v", err)
	}
}

func TestScanFlow_DemoAndNoScanKeepManualLinks(t *testing.T) {
	opts := testAuthOptions(func(context.Context, string) (string, error) { return "MoodleSession=zzz", nil })
	opts.Scan = func(context.Context, string, string) ([]Resource, error) {
		t.Error("Scan must not run in demo mode")
		return nil, nil
	}
	scriptedInputs(t, []string{keyCtrlC})
	if _, err := RunInteractiveFormWithAuth(context.Background(), opts, true); !errors.Is(err, ErrFormAborted) {
		t.Fatalf("want ErrFormAborted, got %v", err)
	}
}

func TestValidatePageURL(t *testing.T) {
	tests := []struct {
		raw, campus string
		wantErr     bool
	}{
		{"", "campus.uni.cl", false},
		{"  ", "campus.uni.cl", false},
		{"https://campus.uni.cl/course/view.php?id=1", "campus.uni.cl", false},
		{"https://CAMPUS.uni.cl/course/view.php?id=1", "campus.uni.cl", false},
		{"https://x.campus.uni.cl/a", "campus.uni.cl", false},
		{"https://evil.example/course", "campus.uni.cl", true},
		{"https://campus.uni.cl.evil.example/course", "campus.uni.cl", true},
		{"https://notcampus.uni.cl/a", "campus.uni.cl", true},
		{"campus.uni.cl/course", "campus.uni.cl", true},
		{"ftp://campus.uni.cl/a", "campus.uni.cl", true},
		{"https://anything.example/a", "", false},
	}
	for _, tc := range tests {
		if err := validatePageURL(tc.raw, tc.campus); (err != nil) != tc.wantErr {
			t.Errorf("validatePageURL(%q, %q) err = %v, wantErr %v", tc.raw, tc.campus, err, tc.wantErr)
		}
	}
}

func TestResourceLabels(t *testing.T) {
	got := resourceLabels([]Resource{
		{Title: "  Clase   1 ", URL: "https://c/a"},
		{Title: "Clase 1", URL: "https://c/b"},
		{Title: "Clase 1", URL: "https://c/c"},
		{Title: "", URL: "https://c/pluginfile.php/1/mod_resource/content/1/Gu%C3%ADa%201.pdf"},
		{Title: "", URL: "https://c/"},
	})
	want := []string{"Clase 1", "Clase 1 (2)", "Clase 1 (3)", "Guía 1.pdf", "Recurso 5"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("labels = %q, want %q", got, want)
	}
	for _, l := range got {
		if strings.Contains(l, "http") {
			t.Errorf("label %q must not show a link", l)
		}
	}
}

func TestScanFlow_DemoShowsTaggedScreensAndSimulatedScan(t *testing.T) {
	var notice strings.Builder
	var calls []scanCall
	opts := scanAuthOptions(&notice, &calls, func(int) ([]Resource, error) { return sampleResources, nil })
	opts.Demo = true
	scriptedInputs(t, authKeys, pageKeys, []string{keyDown, keySpc, keyEnter})

	// demo + Scan keeps the whole wizard (it no longer short-circuits to the plain form)
	data, err := RunInteractiveFormWithAuth(context.Background(), opts, true)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{sampleResources[0].URL, sampleResources[2].URL}; !reflect.DeepEqual(data.URLs, want) {
		t.Errorf("URLs = %v, want %v", data.URLs, want)
	}
	if len(calls) != 1 {
		t.Errorf("simulated Scan should run once, got %d", len(calls))
	}
}

func TestDemoTagsAndText(t *testing.T) {
	if got := tagged("Titulo", true); got != "Titulo [MODO DEMO]" {
		t.Errorf("tagged demo = %q", got)
	}
	if got := tagged("Titulo", false); got != "Titulo" {
		t.Errorf("tagged normal = %q", got)
	}
	if !strings.Contains(pageURLHelp(true), "modo demo") || strings.Contains(pageURLHelp(false), "demo") {
		t.Error("page URL help must mention demo only in demo mode")
	}
	var choice, custom, method string
	consent := true
	opts := &AuthOptions{Universities: []University{{Name: "U", Domain: "c.uni.cl"}}, Demo: true}
	if got := len(buildAuthGroups(opts, "c.uni.cl", &choice, &custom, &method, &consent)); got != 1 {
		t.Errorf("preset domain should leave only the consent group, got %d", got)
	}
}

func TestScanning_ViewIsEmptyOnceFinished(t *testing.T) {
	m := &scanning{title: "Leyendo...", cancel: func() {}}
	if m.View() == "" {
		t.Fatal("spinner must render while scanning")
	}
	m.Update(scanDoneMsg{})
	if got := m.View(); got != "" {
		t.Errorf("a finished scan must leave nothing on screen, got %q", got)
	}

	m2 := &scanning{title: "Leyendo...", cancel: func() {}}
	m2.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	if !m2.aborted || m2.View() != "" {
		t.Errorf("ctrl+c must abort and clear the view: aborted=%v view=%q", m2.aborted, m2.View())
	}
}

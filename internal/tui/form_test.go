package tui_test

import (
	"reflect"
	"testing"

	"moofetch/internal/tui"
)

func TestCleanURLs(t *testing.T) {
	raw := `
   https://campusvirtual.ufro.cl/mod/resource/Apunte%201.pdf   
"https://campusvirtual.ufro.cl/mod/resource/Guia%202.pdf"
'https://campusvirtual.ufro.cl/mod/resource/Calculo.pdf'
<https://campusvirtual.ufro.cl/mod/resource/Algebra.pdf>

   
not-a-valid-url
ftp://unsupported-scheme.com/file.pdf
https://campusvirtual.ufro.cl/mod/resource/Apunte%201.pdf
`

	expected := []string{
		"https://campusvirtual.ufro.cl/mod/resource/Apunte%201.pdf",
		"https://campusvirtual.ufro.cl/mod/resource/Guia%202.pdf",
		"https://campusvirtual.ufro.cl/mod/resource/Calculo.pdf",
		"https://campusvirtual.ufro.cl/mod/resource/Algebra.pdf",
	}

	actual := tui.CleanURLs(raw)
	if !reflect.DeepEqual(actual, expected) {
		t.Errorf("CleanURLs mismatch.\nExpected: %#v\nActual:   %#v", expected, actual)
	}
}

func TestCleanURLs_Empty(t *testing.T) {
	if len(tui.CleanURLs("")) != 0 {
		t.Errorf("expected empty slice for empty string")
	}
	if len(tui.CleanURLs("   \n\n  \t  ")) != 0 {
		t.Errorf("expected empty slice for whitespace string")
	}
}

func TestNormalizeCookie(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{
			input:    "MoodleSession=abc123xyz",
			expected: "MoodleSession=abc123xyz",
		},
		{
			input:    "  Cookie: MoodleSession=abc123xyz  ",
			expected: "MoodleSession=abc123xyz",
		},
		{
			input:    `"MoodleSession=abc123xyz"`,
			expected: "MoodleSession=abc123xyz",
		},
		{
			input:    "abc123xyz",
			expected: "MoodleSession=abc123xyz",
		},
		{
			input:    "custom_session=123; tracking=456",
			expected: "custom_session=123; tracking=456",
		},
		{
			input:    "MoodleSession=MoodleSession=abc123xyz",
			expected: "MoodleSession=abc123xyz",
		},
		{
			input:    "Cookie: MoodleSession=MoodleSession=abc123xyz",
			expected: "MoodleSession=abc123xyz",
		},
		{
			input:    `MoodleSession:"p07s7j0vkcf687k3a6m68nu5c9"`,
			expected: "MoodleSession=p07s7j0vkcf687k3a6m68nu5c9",
		},
		{
			input:    `"MoodleSession": "p07s7j0vkcf687k3a6m68nu5c9"`,
			expected: "MoodleSession=p07s7j0vkcf687k3a6m68nu5c9",
		},
		{
			input:    `MoodleSession="p07s7j0vkcf687k3a6m68nu5c9"`,
			expected: "MoodleSession=p07s7j0vkcf687k3a6m68nu5c9",
		},
		{
			input:    `"p07s7j0vkcf687k3a6m68nu5c9"`,
			expected: "MoodleSession=p07s7j0vkcf687k3a6m68nu5c9",
		},
	}

	for _, tc := range tests {
		actual := tui.NormalizeCookie(tc.input)
		if actual != tc.expected {
			t.Errorf("input '%s': expected '%s', got '%s'", tc.input, tc.expected, actual)
		}
	}
}

func TestCleanURLsDemo(t *testing.T) {
	raw := `
https://campusvirtual.ufro.cl/mod/resource/view.php?id=101/Calculo.pdf
campusvirtual.ufro.cl/mod/resource/view.php?id=102/Algebra.pdf
"Guia_03_Fisica.pdf"
<https://example.com/demo.pdf>
https://campusvirtual.ufro.cl/mod/resource/view.php?id=101/Calculo.pdf
`

	urls := tui.CleanURLsDemo(raw)
	if len(urls) != 4 {
		t.Fatalf("expected 4 deduplicated URLs, got %d: %#v", len(urls), urls)
	}

	if urls[0] != "https://campusvirtual.ufro.cl/mod/resource/view.php?id=101/Calculo.pdf" {
		t.Errorf("unexpected url[0]: %s", urls[0])
	}
	if urls[1] != "https://campusvirtual.ufro.cl/mod/resource/view.php?id=102/Algebra.pdf" {
		t.Errorf("unexpected url[1]: %s", urls[1])
	}
	if urls[2] != "https://Guia_03_Fisica.pdf" {
		t.Errorf("unexpected url[2]: %s", urls[2])
	}
	if urls[3] != "https://example.com/demo.pdf" {
		t.Errorf("unexpected url[3]: %s", urls[3])
	}
}

func TestCleanURLsDemo_Empty(t *testing.T) {
	urls := tui.CleanURLsDemo("")
	if len(urls) == 0 {
		t.Errorf("expected default sample URLs when input is empty in demo mode")
	}

	urlsWhitespace := tui.CleanURLsDemo("   \n\n  \t ")
	if len(urlsWhitespace) == 0 {
		t.Errorf("expected default sample URLs when input is whitespace in demo mode")
	}
}

func TestNewInteractiveForm_DemoDefaults(t *testing.T) {
	ctrl := tui.NewInteractiveForm(true)
	if ctrl == nil || ctrl.Form == nil {
		t.Fatal("expected non-nil form controller")
	}

	data := ctrl.GetData()
	if data == nil {
		t.Fatal("expected non-nil FormData")
	}

	if data.Cookie != "MoodleSession=demo_session_ufro_showcase" {
		t.Errorf("expected default demo cookie, got %q", data.Cookie)
	}

	if len(data.URLs) != 3 {
		t.Errorf("expected 3 default sample URLs, got %d", len(data.URLs))
	}
}

func TestNewInteractiveForm_PreDetectedCookieSkipsStep(t *testing.T) {
	ctrl := tui.NewInteractiveFormWithCookie(false, "MoodleSession=detected")
	if got := ctrl.GetData().Cookie; got != "MoodleSession=detected" {
		t.Fatalf("cookie = %q", got)
	}
}

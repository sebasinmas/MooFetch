package auth

import (
	"context"
	"regexp"
	"testing"
)

// wellFormedCookie matches exactly one "Name=value" pair: a token name, a
// non-empty value and no quotes, whitespace, semicolons or control characters.
var wellFormedCookie = regexp.MustCompile(`^[A-Za-z0-9_.-]+=[^\s"';\x00-\x1f]+$`)

func TestDetect_ProducesWellFormedCookie(t *testing.T) {
	tests := []struct {
		name     string
		provided string
		want     string
	}{
		{"bare value gets default name", "abc123", "MoodleSession=abc123"},
		{"bare value with surrounding whitespace", " \tabc123\n", "MoodleSession=abc123"},
		{"provider returns Name=value", "MoodleSession=abc123", "MoodleSession=abc123"},
		{"suffixed name keeps its real name", "MoodleSessionxyz=abc123", "MoodleSessionxyz=abc123"},
		{"Name=value with surrounding whitespace", "  MoodleSessionxyz=abc123 ", "MoodleSessionxyz=abc123"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p := &fakeProvider{val: tc.provided}
			got, err := Detect(context.Background(), p, "campus.example.cl")
			if err != nil {
				t.Fatalf("Detect: %v", err)
			}
			if got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
			if !wellFormedCookie.MatchString(got) {
				t.Errorf("%q is not a well-formed Name=value cookie", got)
			}
		})
	}
}

// TestDetect_QuotedProviderValue_CurrentBehaviour documents that Detect does
// not unquote what the provider returns: a quoted bare value yields a cookie
// whose value still carries the quotes. Browsers normally store Moodle session
// ids unquoted, so this is only a robustness gap, but the cmd layer does not
// re-normalize detected cookies either.
func TestDetect_QuotedProviderValue_CurrentBehaviour(t *testing.T) {
	got, err := Detect(context.Background(), &fakeProvider{val: `"abc123"`}, "campus.example.cl")
	if err != nil {
		t.Fatal(err)
	}
	if got != `MoodleSession="abc123"` {
		t.Errorf("current behaviour changed: got %q", got)
	}
}

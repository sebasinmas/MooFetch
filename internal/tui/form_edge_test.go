package tui_test

import (
	"reflect"
	"testing"

	"github.com/sebasinmas/MooFetch/internal/tui"
)

func TestCleanURLs_EdgeCases(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want []string
	}{
		{"CRLF line endings", "https://a.cl/1.pdf\r\nhttps://a.cl/2.pdf\r\n", []string{"https://a.cl/1.pdf", "https://a.cl/2.pdf"}},
		{"old Mac CR only is one line and stays invalid", "https://a.cl/1.pdf\rhttps://a.cl/2.pdf", nil},
		{"tabs and spaces around", "\t  https://a.cl/1.pdf \t\n", []string{"https://a.cl/1.pdf"}},
		{"duplicates across CRLF and whitespace", "https://a.cl/1.pdf\r\n  https://a.cl/1.pdf  \r\n\"https://a.cl/1.pdf\"", []string{"https://a.cl/1.pdf"}},
		{"http is accepted", "http://a.cl/x", []string{"http://a.cl/x"}},
		{"uppercase scheme is accepted", "HTTPS://a.cl/x", []string{"HTTPS://a.cl/x"}},
		{"non http schemes dropped", "ftp://a.cl/x\nfile:///etc/passwd\njavascript:alert(1)\nmailto:a@b.cl\ndata:text/plain,hi", nil},
		{"scheme without host dropped", "https://\nhttp:///path", nil},
		{"relative path dropped", "/mod/resource/view.php?id=1\nview.php", nil},
		{"only quotes and brackets", "\"\"\n<>\n''", nil},
		{"order preserved", "https://b.cl/2\nhttps://a.cl/1", []string{"https://b.cl/2", "https://a.cl/1"}},
		{"query strings kept", "https://a.cl/view.php?id=5&x=y", []string{"https://a.cl/view.php?id=5&x=y"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := tui.CleanURLs(tc.raw)
			if len(got) == 0 && len(tc.want) == 0 {
				return
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("got %#v, want %#v", got, tc.want)
			}
		})
	}
}

func TestNormalizeCookie_EdgeCases(t *testing.T) {
	tests := []struct{ in, want string }{
		{"", ""},
		{"   ", ""},
		{`""`, ""},
		{"token", "MoodleSession=token"},
		{"MoodleSession=token", "MoodleSession=token"},
		{"moodlesession=token", "moodlesession=token"},
		{"MOODLESESSION=MoodleSession=token", "MOODLESESSION=token"},
		{"  MoodleSession = token  ", "MoodleSession=token"},
		{"COOKIE: MoodleSession=token", "MoodleSession=token"},
		{"Cookie: Cookie: token", "MoodleSession=token"},
		{"Cookie:MoodleSession=token", "MoodleSession=token"},
		{"'MoodleSession=token'", "MoodleSession=token"},
		{"MoodleSession:token", "MoodleSession=token"},
		{"a=b; MoodleSession=token", "a=b; MoodleSession=token"},
		{"MoodleSession=", "MoodleSession="},
	}
	for _, tc := range tests {
		if got := tui.NormalizeCookie(tc.in); got != tc.want {
			t.Errorf("NormalizeCookie(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestNormalizeCookie_Idempotent(t *testing.T) {
	for _, in := range []string{"token", "Cookie: MoodleSession=t", `MoodleSession:"t"`, "MoodleSession=MoodleSession=t", "a=b; c=d"} {
		once := tui.NormalizeCookie(in)
		if twice := tui.NormalizeCookie(once); twice != once {
			t.Errorf("not idempotent for %q: %q -> %q", in, once, twice)
		}
	}
}

func TestCleanURLsDemo_EdgeCases(t *testing.T) {
	if got := tui.CleanURLsDemo("  \r\n\t"); len(got) != 3 {
		t.Errorf("whitespace-only input should yield 3 sample URLs, got %v", got)
	}
	got := tui.CleanURLsDemo("http://a.cl/x\r\nhttp://a.cl/x\r\nplainname")
	want := []string{"http://a.cl/x", "https://campusvirtual.ufro.cl/mod/resource/view.php?file=plainname"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %#v, want %#v", got, want)
	}
}

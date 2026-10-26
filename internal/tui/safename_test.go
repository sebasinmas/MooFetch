package tui

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestSafeDirName(t *testing.T) {
	tests := []struct {
		name, in, want string
	}{
		{"plain", "Cálculo II", "Cálculo II"},
		{"invalid chars", `a\b/c:d*e?f"g<h>i|j`, "a_b_c_d_e_f_g_h_i_j"},
		{"control chars", "a\x00b\x1fc\x7fd", "a_b_c_d"},
		{"whitespace collapses", "  uno \t\n  dos   tres  ", "uno dos tres"},
		{"nbsp collapses", "uno  dos", "uno dos"},
		{"leading and trailing dots", "..  .nombre. . ", "nombre"},
		{"only dots", "...", "Sin nombre"},
		{"dot", ".", "Sin nombre"},
		{"dotdot", "..", "Sin nombre"},
		{"empty", "", "Sin nombre"},
		{"blank", " \t ", "Sin nombre"},
		{"traversal", "../../etc/passwd", "_.._etc_passwd"},
		{"windows traversal", `..\..\Windows`, "_.._Windows"},
		{"reserved CON", "CON", "_CON"},
		{"reserved lowercase", "nul", "_nul"},
		{"reserved with extension", "aux.txt", "_aux.txt"},
		{"reserved COM1", "COM1", "_COM1"},
		{"reserved LPT9.pdf", "LPT9.pdf", "_LPT9.pdf"},
		{"reserved with trailing dot", "PRN.", "_PRN"},
		{"COM0 is fine", "COM0", "COM0"},
		{"COM10 is fine", "COM10", "COM10"},
		{"CONSOLA is fine", "CONSOLA", "CONSOLA"},
		{"colon is replaced first", "CON:", "CON_"},
		{"emoji kept", "Física 🧪", "Física 🧪"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := safeDirName(tc.in); got != tc.want {
				t.Errorf("safeDirName(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestSafeDirName_LongNamesAreCutOnARuneBoundary(t *testing.T) {
	for _, r := range []string{"a", "é", "日", "🧪"} {
		in := strings.Repeat(r, 200)
		got := safeDirName(in)
		if len(got) > maxNameBytes {
			t.Errorf("%q x200: %d bytes, want <= %d", r, len(got), maxNameBytes)
		}
		if !utf8.ValidString(got) || got == "" {
			t.Errorf("%q x200: invalid result %q", r, got)
		}
	}
	// the cut must not leave a trailing space or dot
	in := strings.Repeat("a", maxNameBytes-1) + " . b"
	if got := safeDirName(in); strings.HasSuffix(got, " ") || strings.HasSuffix(got, ".") {
		t.Errorf("trailing space/dot kept: %q", got)
	}
}

func TestSafeDirName_NeverUnsafe(t *testing.T) {
	for _, in := range []string{"", ".", "..", "...", "/", `\`, "CON", "con.", " nul ", "a/../b"} {
		got := safeDirName(in)
		if got == "" || got == "." || got == ".." || strings.ContainsAny(got, `\/:*?"<>|`) {
			t.Errorf("safeDirName(%q) = %q is unsafe", in, got)
		}
	}
}

package moodle

import (
	"math/rand"
	"strings"
	"testing"
	"testing/quick"
	"unicode"
	"unicode/utf8"
)

func TestSanitizeFilename_Table(t *testing.T) {
	long := strings.Repeat("a", 300)
	longMB := strings.Repeat("ñ", 200) // 400 bytes
	longEmoji := strings.Repeat("😀", 100)

	tests := []struct {
		name string
		in   string
		want string
	}{
		// normal names
		{"plain", "Guia.pdf", "Guia.pdf"},
		{"spaces and dash", "Capitulo 1 - Calculo.pdf", "Capitulo 1 - Calculo.pdf"},
		{"accents", "Guía de ejercicios 2.pdf", "Guía de ejercicios 2.pdf"},
		{"emoji", "Clase 😀 1.pdf", "Clase 😀 1.pdf"},
		{"cjk", "课程.pdf", "课程.pdf"},
		{"leading dot kept", ".hidden.pdf", ".hidden.pdf"},
		{"quoted", `"Quoted.pdf"`, "Quoted.pdf"},
		{"single quoted", `'Quoted.pdf'`, "Quoted.pdf"},

		// each invalid char
		{"backslash", `a\b.pdf`, "a_b.pdf"},
		{"colon", "a:b.pdf", "a_b.pdf"},
		{"star", "a*b.pdf", "a_b.pdf"},
		{"question", "a?b.pdf", "a_b.pdf"},
		{"inner double quote", `a"b.pdf`, "a_b.pdf"},
		{"lt", "a<b.pdf", "a_b.pdf"},
		{"gt", "a>b.pdf", "a_b.pdf"},
		{"pipe", "a|b.pdf", "a_b.pdf"},
		{"all together", `a:b*c?d|e.pdf`, "a_b_c_d_e.pdf"},

		// control chars
		{"nul", "a\x00b.pdf", "a_b.pdf"},
		{"tab inside", "a\tb.pdf", "a_b.pdf"},
		{"newline inside", "a\nb.pdf", "a_b.pdf"},
		{"0x01", "a\x01b.pdf", "a_b.pdf"},
		{"0x1f", "a\x1fb.pdf", "a_b.pdf"},
		{"del", "a\x7fb.pdf", "a_b.pdf"},
		{"c1 control", "a\u0085b.pdf", "a_b.pdf"},
		{"invalid utf8", "a\xffb.pdf", "a_b.pdf"},
		{"trailing newline trimmed", "a.pdf\n", "a.pdf"},

		// reserved device names
		{"CON ext", "CON.pdf", "_CON.pdf"},
		{"con lower", "con.pdf", "_con.pdf"},
		{"Con mixed", "CoN.pdf", "_CoN.pdf"},
		{"nul no ext", "nul", "_nul"},
		{"PRN", "PRN", "_PRN"},
		{"AUX ext", "aux.txt", "_aux.txt"},
		{"COM1", "COM1.pdf", "_COM1.pdf"},
		{"COM9", "com9", "_com9"},
		{"LPT1", "LPT1.pdf", "_LPT1.pdf"},
		{"LPT9 lower", "lpt9.pdf", "_lpt9.pdf"},
		{"first dot only", "com1.txt.pdf", "_com1.txt.pdf"},
		{"superscript COM", "COM¹.pdf", "_COM¹.pdf"},
		{"superscript LPT", "lpt³.pdf", "_lpt³.pdf"},
		{"space before dot", "CON .pdf", "_CON .pdf"},
		{"CONIN$", "CONIN$.pdf", "_CONIN$.pdf"},
		{"CONSOLE untouched", "CONSOLE.pdf", "CONSOLE.pdf"},
		{"COM10 untouched", "COM10.pdf", "COM10.pdf"},
		{"COM0 untouched", "COM0.pdf", "COM0.pdf"},
		{"con inside untouched", "my_con.pdf", "my_con.pdf"},
		{"nul suffix untouched", "nullable.pdf", "nullable.pdf"},
		{"reserved via path", "dir/CON.pdf", "_CON.pdf"},

		// trailing dots/spaces
		{"trailing dot", "a.pdf.", "a.pdf"},
		{"trailing dots", "a.pdf...", "a.pdf"},
		{"trailing space", "a.pdf ", "a.pdf"},
		{"trailing mixed", "a.pdf . .", "a.pdf"},
		{"leading space", "  a.pdf", "a.pdf"},
		{"inner spaces kept", "a  b.pdf", "a  b.pdf"},
		{"no extension trailing dot", "report.", "report"},
		{"dot exposed after quote trim", `"a."`, "a"},

		// only dots / empty
		{"empty", "", "download.pdf"},
		{"dot", ".", "download.pdf"},
		{"dotdot", "..", "download.pdf"},
		{"dots", "....", "download.pdf"},
		{"dots and spaces", " . . ", "download.pdf"},
		{"spaces", "   ", "download.pdf"},
		{"quotes only", `""`, "download.pdf"},
		{"slash only", "/", "download.pdf"},
		{"slashes", "///", "download.pdf"},
		{"dir dot", "a/..", "download.pdf"},

		// traversal
		{"unix traversal", "../../etc/passwd", "passwd"},
		{"unix traversal pdf", "../../x.pdf", "x.pdf"},
		{"absolute unix", "/etc/passwd", "passwd"},
		{"trailing slash", "dir/file.pdf/", "file.pdf"},
		{"windows traversal", `..\..\evil.pdf`, ".._.._evil.pdf"},
		{"windows drive path", `C:\Windows\x.pdf`, "C__Windows_x.pdf"},
		{"unc path", `\\host\share\x.pdf`, "__host_share_x.pdf"},
		{"mixed separators", `../..\x.pdf`, ".._x.pdf"},

		// length cap
		{"long ascii keeps ext", long + ".pdf", strings.Repeat("a", 176) + ".pdf"},
		{"long ascii no ext", long, strings.Repeat("a", 180)},
		{"exactly at cap", strings.Repeat("b", 176) + ".pdf", strings.Repeat("b", 176) + ".pdf"},
		{"multibyte keeps ext", longMB + ".pdf", strings.Repeat("ñ", 88) + ".pdf"},
		{"emoji keeps ext", longEmoji + ".pdf", strings.Repeat("😀", 44) + ".pdf"},
		{"long ext dropped as ext", "a" + strings.Repeat("b", 300) + ".averyveryveryverylongextension", "a" + strings.Repeat("b", 179)},
		{"reserved exposed by cut", "CON" + strings.Repeat(" ", 200) + "x", "_CON"},
		{"reserved prefix stays in cap", "con." + strings.Repeat("a", 300), "_con." + strings.Repeat("a", 175)},
		{"cut leaves trailing space", strings.Repeat("a", 175) + " " + strings.Repeat("z", 50) + ".pdf", strings.Repeat("a", 175) + ".pdf"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := sanitizeFilename(tc.in)
			if got != tc.want {
				t.Errorf("sanitizeFilename(%q) = %q, want %q", tc.in, got, tc.want)
			}
			assertSafeFilename(t, tc.in, got)
		})
	}
}

func TestSanitizeFilename_LongNamesStayValid(t *testing.T) {
	for _, r := range []string{"a", "ñ", "€", "😀", "课"} {
		for n := 1; n < 300; n += 7 {
			in := strings.Repeat(r, n) + ".pdf"
			got := sanitizeFilename(in)
			assertSafeFilename(t, in, got)
			if len(in) > maxFilenameBytes && !strings.HasSuffix(got, ".pdf") {
				t.Fatalf("extension lost for %d x %q: %q", n, r, got)
			}
		}
	}
}

func TestTruncateFilename_StemEmptyFallsBack(t *testing.T) {
	// A name that is nothing but spaces/dots in the cut region.
	in := strings.Repeat(" ", 250) + ".pdf"
	if got := truncateFilename(in, 180); got != "download.pdf" {
		t.Errorf("got %q", got)
	}
}

func TestCutOnRuneBoundary(t *testing.T) {
	if got := cutOnRuneBoundary("ñññ", 3); got != "ñ" {
		t.Errorf("got %q", got)
	}
	if got := cutOnRuneBoundary("ñ", 1); got != "" {
		t.Errorf("got %q", got)
	}
	if got := cutOnRuneBoundary("abc", 10); got != "abc" {
		t.Errorf("got %q", got)
	}
}

// TestSanitizeFilename_Invariants is a property test over random and
// adversarial strings.
func TestSanitizeFilename_Invariants(t *testing.T) {
	prop := func(s string) bool {
		return checkSafeFilename(s, sanitizeFilename(s)) == ""
	}
	if err := quick.Check(prop, &quick.Config{MaxCount: 5000}); err != nil {
		t.Fatal(err)
	}

	pieces := []string{
		"a", "Z", "ñ", "😀", ".", "..", " ", "/", `\`, ":", "*", "?", `"`, "<", ">", "|",
		"\x00", "\x1f", "\x7f", "\xff", "con", "NUL", "com1", "LPT9", "COM¹", ".pdf", "'", "\t",
		strings.Repeat("x", 90),
	}
	rng := rand.New(rand.NewSource(1))
	for i := 0; i < 20000; i++ {
		var b strings.Builder
		for j, n := 0, 1+rng.Intn(8); j < n; j++ {
			b.WriteString(pieces[rng.Intn(len(pieces))])
		}
		in := b.String()
		if msg := checkSafeFilename(in, sanitizeFilename(in)); msg != "" {
			t.Fatalf("%s", msg)
		}
	}
}

func assertSafeFilename(t *testing.T, in, got string) {
	t.Helper()
	if msg := checkSafeFilename(in, got); msg != "" {
		t.Error(msg)
	}
}

// checkSafeFilename returns a description of the first violated invariant or "".
func checkSafeFilename(in, got string) string {
	fail := func(why string) string {
		return why + ": sanitizeFilename(" + quoteForMsg(in) + ") = " + quoteForMsg(got)
	}
	switch {
	case got == "":
		return fail("empty")
	case got == "." || got == "..":
		return fail("dot name")
	case !utf8.ValidString(got):
		return fail("invalid utf-8")
	case len(got) > maxFilenameBytes:
		return fail("too long")
	case strings.ContainsAny(got, `/\:*?"<>|`):
		return fail("forbidden char")
	case strings.IndexFunc(got, unicode.IsControl) >= 0:
		return fail("control char")
	case strings.HasSuffix(got, ".") || strings.HasSuffix(got, " "):
		return fail("trailing dot/space")
	case strings.HasPrefix(got, " "):
		return fail("leading space")
	case refIsWindowsReserved(got):
		return fail("reserved name")
	}
	return ""
}

func quoteForMsg(s string) string {
	if len(s) > 80 {
		return strings.ToValidUTF8(s[:80], "?") + "..."
	}
	return strings.ToValidUTF8(s, "?")
}

// refIsWindowsReserved is an independent restatement of the Windows rule.
func refIsWindowsReserved(name string) bool {
	stem := strings.ToUpper(strings.TrimRight(strings.SplitN(name, ".", 2)[0], " "))
	switch stem {
	case "CON", "PRN", "AUX", "NUL", "COM¹", "COM²", "COM³", "LPT¹", "LPT²", "LPT³":
		return true
	}
	if len(stem) == 4 && (stem[:3] == "COM" || stem[:3] == "LPT") && stem[3] >= '1' && stem[3] <= '9' {
		return true
	}
	return false
}

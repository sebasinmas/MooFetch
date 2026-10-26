package tui

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// maxNameBytes caps a directory/file name component (well under the 255-byte
// limit of common file systems, leaving room for extensions and suffixes).
const maxNameBytes = 100

// fallbackName replaces names that end up empty after cleaning.
const fallbackName = "Sin nombre"

// safeDirName turns an arbitrary title (course, section, folder) into a
// single path component that is valid on Windows, macOS and Linux:
//   - \ / : * ? " < > | and control characters become "_";
//   - whitespace runs collapse into one space;
//   - leading/trailing dots and spaces are dropped (Windows ignores them);
//   - reserved device names (CON, NUL, COM1..9, LPT1..9, with or without an
//     extension) get a "_" prefix;
//   - it is cut to about maxNameBytes on a rune boundary;
//   - the result is never "", "." or "..".
func safeDirName(name string) string {
	var b strings.Builder
	for _, r := range name {
		switch {
		case unicode.IsSpace(r):
			b.WriteRune(' ')
		case unicode.IsControl(r) || strings.ContainsRune(`\/:*?"<>|`, r):
			b.WriteRune('_')
		default:
			b.WriteRune(r)
		}
	}
	s := trimNameEnds(strings.Join(strings.Fields(b.String()), " "))
	s = trimNameEnds(cutOnRune(s, maxNameBytes))
	if s == "" {
		return fallbackName
	}
	if isReservedName(s) {
		return "_" + s
	}
	return s
}

func trimNameEnds(s string) string { return strings.Trim(s, ". ") }

// cutOnRune shortens s to at most limit bytes without splitting a rune.
func cutOnRune(s string, limit int) string {
	for len(s) > limit {
		_, size := utf8.DecodeLastRuneInString(s)
		s = s[:len(s)-size]
	}
	return s
}

// isReservedName reports whether s (ignoring any extension) is a Windows
// device name.
func isReservedName(s string) bool {
	base := strings.ToUpper(strings.TrimRight(strings.SplitN(s, ".", 2)[0], " "))
	switch base {
	case "CON", "PRN", "AUX", "NUL":
		return true
	}
	if len(base) == 4 && (strings.HasPrefix(base, "COM") || strings.HasPrefix(base, "LPT")) {
		return base[3] >= '1' && base[3] <= '9'
	}
	return false
}

package moodle

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	// fallbackFilename is returned when nothing usable is left of a name.
	fallbackFilename = "download.pdf"
	// maxFilenameBytes caps the file NAME (not the full path). The in-flight
	// "<name>.moofetch.part" adds 14 bytes, so the longest component written
	// to disk is 194 bytes, under the 255 limit of NTFS/ext4/APFS.
	maxFilenameBytes = 180
	// maxExtBytes is the longest extension preserved when truncating.
	maxExtBytes = 16
)

// invalidFilenameChars are replaced by "_" on every OS so a download lands
// under the same name everywhere. "/" is handled earlier (path split).
const invalidFilenameChars = `\:*?"<>|`

// windowsReservedNames are device names Windows refuses (or redirects) as file
// names, regardless of case or extension. Superscript digits are included
// because Windows also treats COM¹, LPT² ... as devices.
var windowsReservedNames = map[string]struct{}{
	"CON": {}, "PRN": {}, "AUX": {}, "NUL": {}, "CONIN$": {}, "CONOUT$": {},
	"COM1": {}, "COM2": {}, "COM3": {}, "COM4": {}, "COM5": {},
	"COM6": {}, "COM7": {}, "COM8": {}, "COM9": {},
	"COM¹": {}, "COM²": {}, "COM³": {},
	"LPT1": {}, "LPT2": {}, "LPT3": {}, "LPT4": {}, "LPT5": {},
	"LPT6": {}, "LPT7": {}, "LPT8": {}, "LPT9": {},
	"LPT¹": {}, "LPT²": {}, "LPT³": {},
}

// sanitizeFilename turns an untrusted name (Content-Disposition, URL path or
// query) into a bare, portable file name. The result is never empty, never
// contains a path separator, never is "." or "..", and is safe on Windows,
// Linux and macOS alike:
//
//   - Only the segment after the last "/" is kept ("../../x.pdf" -> "x.pdf").
//   - "\" and the other Windows-invalid characters become "_", so a Windows
//     style traversal stays inert ("..\..\x.pdf" -> ".._.._x.pdf").
//   - Control characters (including NUL) become "_".
//   - Surrounding quotes/whitespace and trailing dots/spaces are stripped.
//   - Windows device names get a "_" prefix ("CON.pdf" -> "_CON.pdf").
//   - The name is capped at maxFilenameBytes keeping the extension and
//     never splitting a UTF-8 rune.
func sanitizeFilename(name string) string {
	name = lastPathSegment(name)
	name = strings.TrimFunc(name, func(r rune) bool { return r == '"' || r == '\'' || unicode.IsSpace(r) })
	name = strings.Map(replaceUnsafeRune, name)
	name = trimDotsAndSpaces(name)
	if name == "" {
		return fallbackFilename
	}
	name = truncateFilename(name, maxFilenameBytes)
	if isWindowsReserved(name) {
		// Re-cut one byte shorter so the "_" prefix keeps us within the cap.
		return "_" + truncateFilename(name, maxFilenameBytes-1)
	}
	return name
}

// lastPathSegment returns what follows the last "/" ignoring trailing slashes.
func lastPathSegment(name string) string {
	name = strings.TrimRight(name, "/")
	if i := strings.LastIndex(name, "/"); i >= 0 {
		return name[i+1:]
	}
	return name
}

func replaceUnsafeRune(r rune) rune {
	if r == utf8.RuneError || unicode.IsControl(r) || strings.ContainsRune(invalidFilenameChars, r) {
		return '_'
	}
	return r
}

// trimDotsAndSpaces removes leading whitespace and trailing dots/whitespace,
// which Windows silently drops (causing collisions) and which can leave "..".
func trimDotsAndSpaces(s string) string {
	s = strings.TrimLeftFunc(s, unicode.IsSpace)
	return strings.TrimRightFunc(s, func(r rune) bool { return r == '.' || unicode.IsSpace(r) })
}

// truncateFilename shortens name to at most limit bytes, keeping a short
// extension and cutting only on rune boundaries.
func truncateFilename(name string, limit int) string {
	if len(name) <= limit {
		return name
	}
	ext := ""
	if i := strings.LastIndexByte(name, '.'); i > 0 && len(name)-i <= maxExtBytes {
		ext = name[i:]
	}
	stem := cutOnRuneBoundary(name[:len(name)-len(ext)], limit-len(ext))
	stem = trimDotsAndSpaces(stem)
	if stem == "" {
		stem = "download"
	}
	return stem + ext
}

func cutOnRuneBoundary(s string, limit int) string {
	if len(s) <= limit {
		return s
	}
	for limit > 0 && !utf8.RuneStart(s[limit]) {
		limit--
	}
	return s[:limit]
}

// isWindowsReserved reports whether the part before the first dot is a
// Windows device name ("com1.txt.pdf" is reserved, "COM10.pdf" is not).
func isWindowsReserved(name string) bool {
	stem, _, _ := strings.Cut(name, ".")
	stem = strings.TrimRightFunc(stem, unicode.IsSpace)
	_, reserved := windowsReservedNames[strings.ToUpper(stem)]
	return reserved
}

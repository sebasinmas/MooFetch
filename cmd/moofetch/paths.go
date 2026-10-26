package main

import (
	"net/url"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"unicode/utf8"
)

// windowsPathBudget is the longest full path MooFetch writes on Windows. The
// classic limit is 260 characters (MAX_PATH); the rest is left for the
// ".moofetch.part" temporary suffix and for tools that count a trailing NUL.
const windowsPathBudget = 240

// partSuffix is appended to the file while it downloads (see plugins/moodle).
const partSuffix = ".moofetch.part"

// minDirRunes is the shortest a folder name is cut to before the file name
// itself is shortened.
const minDirRunes = 8

// pathGOOS is the OS the path rules follow; tests change it.
var pathGOOS = runtime.GOOS

// fileNameOfURL is the file name a pluginfile URL ends with.
func fileNameOfURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	base := path.Base(u.Path)
	if base == "." || base == "/" || base == "" {
		return ""
	}
	if name, uerr := url.PathUnescape(base); uerr == nil {
		return name
	}
	return base
}

// fitPath shortens subDir (slash separated) and, if still needed, fileName so
// that outputDir/subDir/fileName stays within the Windows path budget. On other
// systems, and for paths that already fit, nothing changes. The second result
// is the (possibly shortened) file name; it equals fileName when untouched.
func fitPath(goos, outputDir, subDir, fileName string) (string, string) {
	if goos != "windows" {
		return subDir, fileName
	}
	base := outputDir
	if abs, err := filepath.Abs(outputDir); err == nil {
		base = abs
	}
	segs := splitSegments(subDir)
	total := func(file string) int {
		n := utf8.RuneCountInString(base) + 1 + utf8.RuneCountInString(file) + len(partSuffix)
		for _, s := range segs {
			n += utf8.RuneCountInString(s) + 1
		}
		return n
	}
	for total(fileName) > windowsPathBudget {
		i := longest(segs)
		if i < 0 || utf8.RuneCountInString(segs[i]) <= minDirRunes {
			break
		}
		segs[i] = trimEnds(cutRunes(segs[i], utf8.RuneCountInString(segs[i])-1))
	}
	for total(fileName) > windowsPathBudget && utf8.RuneCountInString(stem(fileName)) > 1 {
		fileName = shortenStem(fileName)
	}
	if len(segs) == 0 {
		return "", fileName
	}
	return strings.Join(segs, "/") + "/", fileName
}

func splitSegments(subDir string) []string {
	return strings.FieldsFunc(subDir, func(r rune) bool { return r == '/' || r == '\\' })
}

// longest returns the index of the longest segment, or -1.
func longest(segs []string) int {
	best, idx := 0, -1
	for i, s := range segs {
		if n := utf8.RuneCountInString(s); n > best {
			best, idx = n, i
		}
	}
	return idx
}

func cutRunes(s string, n int) string {
	r := []rune(s)
	if n >= len(r) {
		return s
	}
	return string(r[:max(n, 1)])
}

func trimEnds(s string) string {
	if s = strings.Trim(s, ". "); s == "" {
		return "_"
	}
	return s
}

func stem(name string) string { return strings.TrimSuffix(name, filepath.Ext(name)) }

// shortenStem cuts one rune off the file name before its extension.
func shortenStem(name string) string {
	ext := filepath.Ext(name)
	return trimEnds(cutRunes(stem(name), utf8.RuneCountInString(stem(name))-1)) + ext
}

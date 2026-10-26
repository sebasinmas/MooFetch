package main

import (
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/sebasinmas/MooFetch/internal/tui"
)

func TestFileNameOfURL(t *testing.T) {
	tests := map[string]string{
		"https://c.cl/webservice/pluginfile.php/1/mod_resource/content/1/Gu%C3%ADa%201.pdf?forcedownload=1": "Guía 1.pdf",
		"https://c.cl/": "",
		"::bad":         "",
	}
	for in, want := range tests {
		if got := fileNameOfURL(in); got != want {
			t.Errorf("fileNameOfURL(%q) = %q, want %q", in, got, want)
		}
	}
}

func fullLen(out, sub, file string) int {
	return utf8.RuneCountInString(out) + 1 + utf8.RuneCountInString(strings.ReplaceAll(sub, "/", "\\")) +
		utf8.RuneCountInString(file) + len(partSuffix)
}

func TestFitPath_NotWindowsIsUntouched(t *testing.T) {
	long := strings.Repeat("a", 90) + "/" + strings.Repeat("b", 90) + "/" + strings.Repeat("c", 90) + "/"
	for _, goos := range []string{"linux", "darwin"} {
		sub, name := fitPath(goos, "/out", long, strings.Repeat("n", 100)+".pdf")
		if sub != long || len(name) != 104 {
			t.Errorf("%s: path must not change", goos)
		}
	}
}

func TestFitPath_WindowsKeepsShortPaths(t *testing.T) {
	sub, name := fitPath("windows", `C:\Users\ana\Descargas`, "Cálculo/01 - Límites/", "Guía 1.pdf")
	if sub != "Cálculo/01 - Límites/" || name != "Guía 1.pdf" {
		t.Errorf("got %q %q", sub, name)
	}
}

func TestFitPath_WindowsShrinksLongPaths(t *testing.T) {
	out := `C:\Users\ana\Descargas\MooFetch`
	tests := []struct {
		name string
		sub  string
		file string
	}{
		{"deep nesting", strings.Join([]string{strings.Repeat("Curso ", 15), "03 - " + strings.Repeat("Unidad ", 12), strings.Repeat("Carpeta ", 10), strings.Repeat("Sub ", 20), strings.Repeat("Otra ", 20)}, "/") + "/", "Guía de ejercicios.pdf"},
		{"huge file name", "Curso/01 - Intro/", strings.Repeat("nombre muy largo ", 20) + ".pdf"},
		{"both", strings.Repeat("x", 100) + "/" + strings.Repeat("y", 100) + "/" + strings.Repeat("z", 100) + "/", strings.Repeat("f", 120) + ".pptx"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			sub, name := fitPath("windows", out, tc.sub, tc.file)
			if got := fullLen(out, sub, name); got > windowsPathBudget {
				t.Errorf("path is %d characters, budget %d (%q, %q)", got, windowsPathBudget, sub, name)
			}
			if filepath.Ext(name) != filepath.Ext(tc.file) {
				t.Errorf("extension lost: %q", name)
			}
			if strings.HasPrefix(sub, "/") || strings.Contains(sub, "..") || strings.Contains(sub, "//") {
				t.Errorf("bad sub dir %q", sub)
			}
			for _, seg := range splitSegments(sub) {
				if strings.HasSuffix(seg, ".") || strings.HasSuffix(seg, " ") || seg == "" {
					t.Errorf("segment %q is not valid on Windows", seg)
				}
			}
		})
	}
}

func TestFitPath_KeepsNumberedPrefix(t *testing.T) {
	sub, _ := fitPath("windows", `C:\out`, strings.Repeat("Curso ", 30)+"/03 - "+strings.Repeat("Sección ", 25)+"/", "a.pdf")
	segs := splitSegments(sub)
	if len(segs) != 2 || !strings.HasPrefix(segs[1], "03 - ") {
		t.Errorf("the section number must survive shortening: %q", segs)
	}
}

func TestCreateTasks_SameNameInSameFolderGetsSuffix(t *testing.T) {
	out := t.TempDir()
	form := &tui.FormData{
		Token:  testToken,
		Campus: "c.cl",
		Items: []tui.Download{
			{URL: "https://c.cl/webservice/pluginfile.php/1/mod_resource/content/1/Clase.pdf", SubDir: "Curso/01 - A/"},
			{URL: "https://c.cl/webservice/pluginfile.php/2/mod_resource/content/1/Clase.pdf", SubDir: "Curso/01 - A/"},
			{URL: "https://c.cl/webservice/pluginfile.php/3/mod_resource/content/1/clase.PDF", SubDir: "Curso/01 - A/"},
			{URL: "https://c.cl/webservice/pluginfile.php/4/mod_resource/content/1/Clase.pdf", SubDir: "Curso/02 - B/"},
		},
	}
	tasks, err := createTasks(form, out)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"", "Clase (2).pdf", "clase (3).PDF", ""}
	for i, task := range tasks {
		if task.FileName != want[i] {
			t.Errorf("task %d FileName = %q, want %q", i, task.FileName, want[i])
		}
	}
}

func TestCreateTasks_WindowsShortensLongNestedFolders(t *testing.T) {
	old := pathGOOS
	pathGOOS = "windows"
	t.Cleanup(func() { pathGOOS = old })
	out := t.TempDir()
	deep := "Curso " + strings.Repeat("largo ", 15) + "/03 - " + strings.Repeat("Unidad ", 12) + "/" + strings.Repeat("Carpeta ", 10) + "/" + strings.Repeat("Sub ", 20) + "/"
	form := &tui.FormData{Token: testToken, Campus: "c.cl", Items: []tui.Download{
		{URL: "https://c.cl/webservice/pluginfile.php/1/mod_folder/content/0/Guia.pdf", SubDir: deep},
	}}
	tasks, err := createTasks(form, out)
	if err != nil {
		t.Fatal(err)
	}
	abs, _ := filepath.Abs(out)
	rel, _ := filepath.Rel(abs, tasks[0].OutputDir)
	if n := fullLen(abs, filepath.ToSlash(rel)+"/", "Guia.pdf"); n > windowsPathBudget {
		t.Errorf("task path has %d characters", n)
	}
}

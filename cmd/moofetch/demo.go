package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/sebasinmas/MooFetch/internal/domain"
	"github.com/sebasinmas/MooFetch/internal/tui"
)

// demoCookie is the fake session cookie the --demo wizard "detects".
const demoCookie = "MoodleSession=demo_session_ufro_showcase"

// demoResources are the course resources the --demo wizard "finds". The URLs
// end in a file name so the demo downloader shows realistic names.
var demoResources = []tui.Resource{
	{Title: "Programa del curso", URL: "https://campusvirtual.ufro.cl/pluginfile.php/101/mod_resource/content/1/Programa_del_Curso.pdf"},
	{Title: "Clase 01 · Introducción a las estructuras de datos", URL: "https://campusvirtual.ufro.cl/pluginfile.php/102/mod_resource/content/1/Clase_01_Introduccion.pdf"},
	{Title: "Clase 02 · Listas enlazadas", URL: "https://campusvirtual.ufro.cl/pluginfile.php/103/mod_resource/content/1/Clase_02_Listas_Enlazadas.pdf"},
	{Title: "Clase 03 · Pilas y colas", URL: "https://campusvirtual.ufro.cl/pluginfile.php/104/mod_resource/content/1/Clase_03_Pilas_y_Colas.pdf"},
	{Title: "Guía de ejercicios 1", URL: "https://campusvirtual.ufro.cl/pluginfile.php/105/mod_resource/content/1/Guia_Ejercicios_01.pdf"},
	{Title: "Guía de ejercicios 2", URL: "https://campusvirtual.ufro.cl/pluginfile.php/106/mod_resource/content/1/Guia_Ejercicios_02.pdf"},
	{Title: "Lectura complementaria · Capítulo 10", URL: "https://campusvirtual.ufro.cl/pluginfile.php/107/mod_resource/content/1/Lectura_Complementaria.pdf"},
	{Title: "Pauta del Control 1", URL: "https://campusvirtual.ufro.cl/pluginfile.php/108/mod_resource/content/1/Pauta_Control_1.pdf"},
}

// Pauses that make the simulated steps visible (spinner, "searching" notice).
const (
	demoDetectDelay = 800 * time.Millisecond
	demoScanDelay   = 1800 * time.Millisecond

	demoLoginDelay    = 1200 * time.Millisecond
	demoCoursesDelay  = 900 * time.Millisecond
	demoContentsDelay = 1100 * time.Millisecond
)

// demoToken is the fake token the --demo login "receives". It is not a real
// credential and never leaves the process.
const demoToken = "demo-token-ufro-showcase"

// demoCourses are the courses the --demo API flow "lists".
var demoCourses = []tui.Course{
	{ID: 101, Name: "Estructuras de Datos"},
	{ID: 102, Name: "Cálculo Diferencial e Integral"},
	{ID: 103, Name: "Física General I"},
}

const demoFileBase = "https://campusvirtual.ufro.cl/webservice/pluginfile.php/"

// demoFile builds a fixture file whose URL ends in a distinct .pdf name, as
// the demo downloader expects.
func demoFile(ctx int, title, file, folder string, size int64) tui.FileItem {
	kind := "mod_resource/content/1/"
	subDir := ""
	if folder != "" {
		kind = "mod_folder/content/0/" + folder + "/"
		subDir = folder
	}
	return tui.FileItem{Name: title, URL: fmt.Sprintf("%s%d/%s%s", demoFileBase, ctx, kind, file), SubDir: subDir, Size: size}
}

// demoContents returns the sections of a demo course (nil for unknown ids).
func demoContents(courseID int) []tui.Section {
	switch courseID {
	case 101:
		return []tui.Section{
			{ID: 1, Number: 0, Name: "General", Files: []tui.FileItem{
				demoFile(1101, "Programa del curso", "Programa_Estructuras_de_Datos.pdf", "", 412_000),
				demoFile(1102, "Calendario de evaluaciones", "Calendario_Evaluaciones_ED.pdf", "", 188_000),
			}},
			{ID: 2, Number: 1, Name: "Listas enlazadas", Files: []tui.FileItem{
				demoFile(1103, "Clase 01 · Introducción", "Clase_01_Introduccion_ED.pdf", "", 2_516_582),
				demoFile(1104, "Clase 02 · Listas simples", "Clase_02_Listas_Simples.pdf", "", 3_145_728),
				demoFile(1105, "Guía de ejercicios", "Guia_Listas_01.pdf", "Guías", 640_000),
				demoFile(1106, "Guía de ejercicios (resueltos)", "Guia_Listas_01_Resuelta.pdf", "Guías", 702_000),
			}},
			{ID: 3, Number: 2, Name: "Pilas y colas", Files: []tui.FileItem{
				demoFile(1107, "Clase 03 · Pilas", "Clase_03_Pilas.pdf", "", 1_990_000),
				demoFile(1108, "Clase 04 · Colas", "Clase_04_Colas.pdf", "", 2_204_000),
			}},
			{ID: 4, Number: 3, Name: "Evaluaciones"},
		}
	case 102:
		return []tui.Section{
			{ID: 5, Number: 0, Name: "General", Files: []tui.FileItem{
				demoFile(1201, "Programa del curso", "Programa_Calculo.pdf", "", 350_000),
			}},
			{ID: 6, Number: 1, Name: "Límites y continuidad", Files: []tui.FileItem{
				demoFile(1202, "Apunte de límites", "Apunte_Limites.pdf", "", 1_480_000),
				demoFile(1203, "Guía 1 · Límites", "Guia_Calculo_01.pdf", "", 520_000),
			}},
			{ID: 7, Number: 2, Name: "Derivadas", Files: []tui.FileItem{
				demoFile(1204, "Apunte de derivadas", "Apunte_Derivadas.pdf", "", 1_760_000),
				demoFile(1205, "Pauta del Control 1", "Pauta_Control_Calculo_1.pdf", "", 310_000),
			}},
		}
	case 103:
		return []tui.Section{
			{ID: 8, Number: 0, Name: "General", Files: []tui.FileItem{
				demoFile(1301, "Programa del curso", "Programa_Fisica_I.pdf", "", 298_000),
			}},
			{ID: 9, Number: 1, Name: "Cinemática", Files: []tui.FileItem{
				demoFile(1302, "Clase 01 · Movimiento en una dimensión", "Clase_01_Cinematica_1D.pdf", "", 2_900_000),
				demoFile(1303, "Problemas propuestos", "Problemas_Cinematica.pdf", "Problemas", 880_000),
			}},
		}
	}
	return nil
}

// withDemo turns the interactive wizard options into a guided simulation: no
// browser is read and no request is made, but every screen of the real flow is
// shown, so people can see how the app works.
func withDemo(opts *tui.AuthOptions) *tui.AuthOptions {
	opts.Demo = true
	opts.Login = func(ctx context.Context, _, user, pass string) (string, error) {
		if err := sleepCtx(ctx, demoLoginDelay); err != nil {
			return "", err
		}
		if strings.TrimSpace(user) == "" || pass == "" {
			return "", domain.ErrAuthenticationFailed
		}
		return demoToken, nil
	}
	opts.Courses = func(ctx context.Context, _, _ string) ([]tui.Course, error) {
		if err := sleepCtx(ctx, demoCoursesDelay); err != nil {
			return nil, err
		}
		return append([]tui.Course(nil), demoCourses...), nil
	}
	opts.Contents = func(ctx context.Context, _, _ string, courseID int) ([]tui.Section, error) {
		if err := sleepCtx(ctx, demoContentsDelay); err != nil {
			return nil, err
		}
		return demoContents(courseID), nil
	}
	opts.Detect = func(ctx context.Context, _ string) (string, error) {
		return demoCookie, sleepCtx(ctx, demoDetectDelay)
	}
	opts.Scan = func(ctx context.Context, _, _ string) ([]tui.Resource, error) {
		if err := sleepCtx(ctx, demoScanDelay); err != nil {
			return nil, err
		}
		return append([]tui.Resource(nil), demoResources...), nil
	}
	return opts
}

// sleepCtx waits for d or until ctx is cancelled.
func sleepCtx(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

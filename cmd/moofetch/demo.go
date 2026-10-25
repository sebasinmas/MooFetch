package main

import (
	"context"
	"time"

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
)

// withDemo turns the interactive wizard options into a guided simulation: no
// browser is read and no request is made, but every screen of the real flow is
// shown, so people can see how the app works.
func withDemo(opts *tui.AuthOptions) *tui.AuthOptions {
	opts.Demo = true
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

//go:build ignore

// gen-traceability regenera la tabla de docs/traceability.md a partir de
// docs/traceability.json (fuente de verdad).
//
// Uso (desde la raíz del repositorio):
//
//	go run scripts/gen-traceability.go                 # reescribe la tabla
//	go run scripts/gen-traceability.go -check          # falla si la tabla está desactualizada
//	go run scripts/gen-traceability.go -notes v1.1.0   # imprime patchnotes de una versión
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"
)

const (
	beginMarker = "<!-- BEGIN TABLE -->"
	endMarker   = "<!-- END TABLE -->"
)

// Item es una entrada de docs/traceability.json.
type Item struct {
	ID          string   `json:"id"`
	Titulo      string   `json:"titulo"`
	Descripcion string   `json:"descripcion"`
	Fecha       *string  `json:"fecha"`
	Estado      string   `json:"estado"`
	Rama        *string  `json:"rama"`
	Commit      *string  `json:"commit"`
	Version     *string  `json:"version"`
	Evidencia   []string `json:"evidencia"`
}

var (
	validEstados = map[string]bool{"implementado": true, "en proceso": true, "por implementar": true}
	idRe         = regexp.MustCompile(`^TR-\d{3}$`)
	dateRe       = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)
)

func main() {
	jsonPath := flag.String("json", "docs/traceability.json", "ruta al JSON de trazabilidad")
	mdPath := flag.String("md", "docs/traceability.md", "ruta al Markdown a actualizar")
	check := flag.Bool("check", false, "solo verifica que la tabla esté al día")
	notes := flag.String("notes", "", "imprime patchnotes de la versión indicada (ej. v1.1.0)")
	flag.Parse()

	items, err := load(*jsonPath)
	if err != nil {
		fatal(err)
	}

	if *notes != "" {
		fmt.Print(patchnotes(items, *notes))
		return
	}

	md, err := os.ReadFile(*mdPath)
	if err != nil {
		fatal(err)
	}
	out, err := replaceTable(string(md), table(items))
	if err != nil {
		fatal(err)
	}
	if *check {
		if out != string(md) {
			fatal(fmt.Errorf("%s está desactualizado: ejecuta go run scripts/gen-traceability.go", *mdPath))
		}
		return
	}
	if out == string(md) {
		return
	}
	if err := os.WriteFile(*mdPath, []byte(out), 0o644); err != nil {
		fatal(err)
	}
	fmt.Printf("%s actualizado (%d items)\n", *mdPath, len(items))
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "error:", err)
	os.Exit(1)
}

func load(path string) ([]Item, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var items []Item
	if err := json.Unmarshal(data, &items); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	seen := map[string]bool{}
	for _, it := range items {
		if err := validate(it); err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		if seen[it.ID] {
			return nil, fmt.Errorf("%s: id duplicado %s", path, it.ID)
		}
		seen[it.ID] = true
	}
	sort.SliceStable(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	return items, nil
}

func validate(it Item) error {
	if !idRe.MatchString(it.ID) {
		return fmt.Errorf("id inválido %q (esperado TR-NNN)", it.ID)
	}
	if strings.TrimSpace(it.Titulo) == "" {
		return fmt.Errorf("%s: titulo vacío", it.ID)
	}
	if !validEstados[it.Estado] {
		return fmt.Errorf("%s: estado %q no válido (implementado | en proceso | por implementar)", it.ID, it.Estado)
	}
	if it.Fecha != nil && !dateRe.MatchString(*it.Fecha) {
		return fmt.Errorf("%s: fecha %q no es YYYY-MM-DD", it.ID, *it.Fecha)
	}
	if it.Estado == "por implementar" && it.Fecha != nil {
		return fmt.Errorf("%s: un item por implementar debe tener fecha null", it.ID)
	}
	return nil
}

func orDash(s *string) string {
	if s == nil || *s == "" {
		return "—"
	}
	return *s
}

func cell(s string) string {
	return strings.ReplaceAll(s, "|", "\\|")
}

func table(items []Item) string {
	var b strings.Builder
	b.WriteString("| ID | Funcionalidad | Fecha | Estado | Rama/commit | Versión |\n")
	b.WriteString("| :--- | :--- | :---: | :---: | :--- | :---: |\n")
	for _, it := range items {
		rama := "—"
		if it.Rama != nil && it.Commit != nil {
			rama = fmt.Sprintf("`%s` / `%s`", *it.Rama, *it.Commit)
		} else if it.Rama != nil {
			rama = fmt.Sprintf("`%s`", *it.Rama)
		} else if it.Commit != nil {
			rama = fmt.Sprintf("`%s`", *it.Commit)
		}
		fmt.Fprintf(&b, "| %s | %s | %s | %s | %s | %s |\n",
			it.ID, cell(it.Titulo), orDash(it.Fecha), it.Estado, rama, orDash(it.Version))
	}
	return b.String()
}

func replaceTable(md, tbl string) (string, error) {
	start := strings.Index(md, beginMarker)
	end := strings.Index(md, endMarker)
	if start < 0 || end < 0 || end < start {
		return "", fmt.Errorf("faltan los marcadores %s / %s", beginMarker, endMarker)
	}
	return md[:start+len(beginMarker)] + "\n" + tbl + md[end:], nil
}

func patchnotes(items []Item, version string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "## %s\n\n", version)
	n := 0
	for _, it := range items {
		if it.Version != nil && *it.Version == version {
			fmt.Fprintf(&b, "- %s: %s (%s, %s)\n", it.ID, it.Titulo, it.Estado, orDash(it.Fecha))
			n++
		}
	}
	if n == 0 {
		b.WriteString("_Sin items con esta versión._\n")
	}
	return b.String()
}

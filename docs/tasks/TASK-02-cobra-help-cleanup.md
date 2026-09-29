# TASK-02: Limpieza de la salida de ayuda de Cobra
- **Prioridad:** 🟡 Media · **Depende de:** TASK-01
- **Archivos:** `cmd/moofetch/root.go`, `run.go`, `version.go`, `cobra_test.go`

## Pasos
1. `rootCmd.CompletionOptions.DisableDefaultCmd = true` (elimina el comando `completion` autogenerado).
2. Revisar `Use/Short/Long/Example` de root, `run` y `version`: idioma consistente y marca MooFetch; ejemplos reales de pipes.
3. Plantilla de uso/ayuda ordenada si hace falta; los alias ocultos (`--logger`, `--plain`) siguen ocultos.
4. Tests en `cobra_test.go`: la ayuda no contiene `completion`, incluye los ejemplos y las flags esperadas.

## DoD
`moofetch --help` y `moofetch run --help` limpios; tests y lint pasan.

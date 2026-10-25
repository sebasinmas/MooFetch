# ADR 004: Rebranding a MooFetch

- **Estado:** Aceptado (Accepted)
- **Área:** Producto / Build / Documentación

## 1. Decisión
- Nombre del producto: **MooFetch** (identidad con vaca 🐄 en README y TUI). Binario: `moofetch`.
- Módulo Go: `github.com/sebasinmas/MooFetch` (ver `go.mod`; inicialmente se propuso `moofetch`, luego se pasó a la ruta completa de GitHub); entrypoint `cmd/godownloader` → `cmd/moofetch`. Cobra `Use: "moofetch"`.
- Sufijo de archivos temporales: `.godownload.part` → `.moofetch.part` (actualizar plugin moodle, tests y docs).
- GoReleaser, workflows de CI, variables `-X main.Version/Commit/BuildDate` y enlaces del README apuntan al nuevo repositorio. La URL del repositorio, `https://github.com/sebasinmas/MooFetch`, está confirmada y es la usada en `go.mod`, el README y GoReleaser.
- Variable de entorno `MOODLE_SESSION` y códigos de salida POSIX no cambian.

## 2. Consecuencias
Cambio transversal que toca todos los imports; debe ejecutarse primero (TASK-01) y fusionarse antes de las demás tareas.

# TASK-01: Rebranding a MooFetch 🐄
- **Prioridad:** 🔴 Bloqueante · **Depende de:** ninguna
- **Referencia:** [ADR 004](../adr/004-rebranding-moofetch.md)

## Pasos
1. `go.mod`: módulo `moofetch`; actualizar todos los imports (`godownloader/internal/...`).
2. `git mv cmd/godownloader cmd/moofetch`; `Use: "moofetch"` en Cobra; textos de versión.
3. `.goreleaser.yaml` (`project_name`, build `id`, `binary`, ldflags, archivos), `.github/workflows/*`, `.gitignore` si referencia el binario.
4. Sufijo temporal `.godownload.part` → `.moofetch.part` en el plugin moodle, sus tests y docs.
5. TUI (`internal/tui`): nuevo banner «MooFetch» y vaca (ASCII/emoji 🐄) en `splash.go` (`defaultBanner`, `defaultStages`), títulos del formulario y tarjeta de progreso; mantener soporte `NO_COLOR`; ajustar tests de splash/form/progress.
6. README: título, badges, enlaces, comandos de ejemplo (`moofetch`), diagrama. Enlaces al repositorio: usar `https://github.com/sebasinmas/MooFetch` **solo tras confirmar la URL** con el usuario.
7. `CLAUDE.md` y `docs/` sin referencias residuales a GoDownloader (salvo historial de ADRs).

## DoD
- `go build -o bin/moofetch ./cmd/moofetch` y `go test -race ./...` pasan.
- `grep -ri godownloader . --exclude-dir=.git` solo devuelve menciones históricas intencionales.
- `./bin/moofetch --version` y `./bin/moofetch run --demo -o /tmp/out` funcionan.

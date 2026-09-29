# TASK-04: Autodetección del token de sesión
- **Prioridad:** 🔴 Alta · **Depende de:** TASK-01
- **Referencia:** [ADR 003](../adr/003-deteccion-automatica-de-token.md) y sección 3.5 de [architecture.md](../architecture.md)

## Pasos
1. Crear `internal/auth`: interfaz `CookieProvider`, catálogo de universidades (`ufro` → `campusvirtual.ufro.cl`, extensible) y validación de dominio personalizado.
2. Adaptador `kooky` (`github.com/browserutils/kooky` + `browser/all`) que devuelve la cookie `MoodleSession` del dominio. Añadir la dependencia con `go get`.
3. `internal/tui/form.go`: paso «¿Cuál es tu universidad?» (huh Select + «Otra…»), mensaje de consentimiento, y fallback a pegado manual si falla o el usuario declina.
4. CLI: flags `--uni` y `--domain` (no usar `-u`, ya lo usa `run --urls`); headless: flag → `MOODLE_SESSION`.
5. Cableado solo en `cmd/moofetch`; `internal/auth` no importa `kernel` ni `tui`.
6. Tests con proveedor falso; el adaptador kooky queda delgado.
7. Verificar `CGO_ENABLED=0 go build ./...` en linux/darwin/windows; si kooky exige CGO, aislar con build tags y documentarlo.

## DoD
Con sesión iniciada en un navegador local, `moofetch` obtiene la cookie sin pegarla; sin navegador/cookie cae al pegado manual; la cookie no aparece en logs.

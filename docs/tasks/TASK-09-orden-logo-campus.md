# TASK-09: Mostrar el logo antes de la selección de campus
- **Estado:** En proceso (decisión tomada: logo primero; código pendiente)
- **Prioridad:** 🟢 Baja · **Depende de:** TASK-04
- **Referencia:** TR-018 en [traceability.md](../traceability.md)

## Contexto
El orden decidido por el dueño es: logo con la vaca → selección de campus → extracción automática de la cookie → pegado de enlaces. En el código actual el orden real es distinto: el paso de campus y consentimiento (`internal/tui/authstep.go`, `RunInteractiveFormWithAuth` → `askAuthStep`) se ejecuta **antes** del splash con el logo (`internal/tui/form.go`, `runForm`), que aparece recién al abrir el formulario de cookie/URLs.

## Pasos
1. Mostrar el splash (`NewSplash`, `internal/tui/splash.go`) antes de `askAuthStep`, ya sea como pantalla previa o integrando el formulario de campus dentro del programa Bubble Tea del splash. Elegir la opción que evite parpadeos y dos programas `tea` consecutivos si es posible.
2. Mantener las restricciones de arquitectura: `tui` no importa `auth` (recibe `AuthOptions` inyectado), el modo headless no muestra splash y respeta `NO_COLOR`.
3. Tests sin TTY (`testInput` / `tea.WithOutput(io.Discard)`) que verifiquen el orden splash → campus → formulario de URLs; `go test -race -count=2 ./internal/tui/` en verde.
4. Actualizar README (flujo del asistente, hoy descrito como campus → splash → formulario), `docs/architecture.md` §3.5 y TR-018 en `docs/traceability.json` (estado `implementado`, commit y fecha), y regenerar la tabla con `go run scripts/gen-traceability.go`.

## DoD
- [x] Decisión registrada: logo primero (2026-10-19, dueño del proyecto).
- [ ] Splash visible antes del paso de campus en el modo interactivo.
- [ ] Tests del orden en `internal/tui`, deterministas y sin TTY.
- [ ] Docs y TR-018 alineados con el comportamiento real; `gofmt -l .` vacío y `golangci-lint run` sin issues.

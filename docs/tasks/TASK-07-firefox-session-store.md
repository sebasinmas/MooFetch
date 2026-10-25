# TASK-07: Cookie de sesión de Firefox (session store)
- **Prioridad:** 🟡 Media · **Depende de:** TASK-04
- **Referencia:** [ADR 003, sección 7](../adr/003-deteccion-automatica-de-token.md), TR-006 en [traceability.md](../traceability.md)
- **Estado:** Fusionada en `develop`; falta la verificación manual en Firefox
- **Rama:** `fix/cookie-autodetect` (commit `7c20081`), fusionada en `develop`

## Contexto
`MoodleSession` es una cookie de sesión: Firefox no la guarda en `cookies.sqlite` (lo único que lee kooky), sino en `sessionstore-backups/recovery.jsonlz4` (también `recovery.baklz4` y `previous.jsonlz4`). Además, Moodle puede añadir un sufijo al nombre (`MoodleSession<sufijo>`).

## Pasos
1. En la rama `fix/cookie-autodetect`: lectura del session store de Firefox (`internal/auth/firefox.go`), búsqueda en carpetas de perfil nuevas y coincidencia de la cookie por prefijo `MoodleSession` (`internal/auth/kooky.go`). Ya implementado y con tests en la rama.
2. Revisar y fusionar la rama en `develop` mediante PR (corre CI).
3. Al fusionar: actualizar TR-006 en `docs/traceability.json` a `implementado` (con versión y fecha), regenerar la tabla y adaptar el README (la rama trae su propio texto de autodetección, `--uni` y `--domain`) y el ADR 003.
4. Verificar manualmente en Firefox con una sesión real del campus (TR-019).

## DoD
- [x] Rama `fix/cookie-autodetect` fusionada en `develop` con `go test -race ./...` y `gofmt -l .` limpios.
- [ ] Verificación manual en Firefox (Linux al menos) con el campus real.
- [x] TR-006 actualizado y README/ADR 003 sin contradicciones.

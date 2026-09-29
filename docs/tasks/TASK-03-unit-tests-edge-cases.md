# TASK-03: Tests unitarios de casos borde y mejoras de calidad de vida
- **Prioridad:** 🟡 Media · **Depende de:** TASK-02, TASK-04 (para cubrir sus cambios)

## Cobertura a agregar
- **moodle:** sanitización de nombres, variantes de `Content-Disposition`, redirección a login (303), 401/403, cuerpo truncado, timeout de inactividad, limpieza de `.moofetch.part`, `Content-Length` vacío/cero.
- **kernel:** concurrencia 0/negativa, cancelación a mitad de lote, circuit breaker con resultados mixtos, handler nil, URLs duplicadas.
- **tui:** `CleanURLs` con CRLF, espacios, duplicados y esquemas no http; normalización de cookie `token` vs `MoodleSession=token`.
- **cmd:** mapeo de códigos de salida, errores de uso.
- **logger:** ofuscación de cookies cortas/vacías.
- **auth:** proveedor falso, dominio inexistente, cookie ausente, contexto cancelado.

## QoL
Aceptar cookie con o sin prefijo `MoodleSession=`, deduplicar URLs y mensajes de error más claros (con tests).

## DoD
`go test -race ./...` y `golangci-lint run` pasan; la cobertura no baja respecto de `coverage.out` actual.

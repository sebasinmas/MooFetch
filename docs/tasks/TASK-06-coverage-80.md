# TASK-06: Cobertura de tests superior al 80%
- **Prioridad:** 🟡 Media · **Depende de:** TASK-01 a TASK-05
- **Agente:** `test-coverage-engineer` (`.claude/agents/test-coverage-engineer.md`)

## Estado inicial (`go test -coverprofile`)
| Paquete | Cobertura |
| :--- | :---: |
| cmd/moofetch | 67.9% |
| internal/auth | 75.0% |
| internal/tui | 78.0% |
| internal/plugins/demo | 78.9% |
| internal/plugins/moodle | 83.4% |
| internal/kernel | 93.2% |
| internal/logger | 97.9% |
| internal/domain | 100% |
| **Total** | **82.9%** |

## Resultado (antes / después)
| Paquete | Antes | Después |
| :--- | :---: | :---: |
| cmd/moofetch | 67.9% | 93.0% |
| internal/auth | 75.0% | 96.9% |
| internal/tui | 78.0% | 93.8% |
| internal/plugins/demo | 78.9% | 91.4% |
| internal/plugins/moodle | 83.4% | 83.4% |
| internal/kernel | 93.2% | 93.2% |
| internal/logger | 97.9% | 97.9% |
| internal/domain | 100% | 100% |
| **Total** | **82.9%** | **93.1%** |

Verificado: `go test -race -count=3 ./...` verde, `gofmt -l .` vacío, `golangci-lint run` 0 issues.
Notas: el estado global de flags de Cobra se resetea con `resetFlags` (`cmd/moofetch/helpers_test.go`); `auth` usa la costura `traverseCookies`; `tui` usa la costura `testInput` (sin TTY); `main` se extrajo a `realMain`. Bug corregido: el error de cookie ausente por stdin devolvía exit 1 en vez de 2.

## Condición de éxito
1. **Cada paquete** con cobertura **> 80%** y el **total > 80%** (`go tool cover -func`).
2. Sin tests frágiles: pasan con `go test -race -count=2 ./...` sin TTY, red ni navegadores reales.
3. `gofmt -l .` vacío y `golangci-lint run` con 0 issues.
4. Sin exclusiones de cobertura ni tests sin aserciones.

## Pasos
1. Medir y listar funciones sin cubrir por paquete (empezar por `cmd/moofetch`, `internal/auth` y `internal/tui`).
2. Cubrir rutas de error, cancelación y casos borde; en `internal/auth`, el adaptador kooky detrás de una costura inyectable (sin leer navegadores reales).
3. Arreglar el estado global de flags de Cobra que rompe `-count=2` (`TestCobra_RunMissingURLs`, `TestExitCode_AuthFailureEndToEnd`).
4. Documentar la tabla antes/después en este archivo y marcar la tarea en `docs/tasks/README.md`.
5. Opcional: umbral de cobertura en `.github/workflows/ci.yml`.

# Trazabilidad de funcionalidades de MooFetch

Este documento responde a la pregunta «¿qué tiene el proyecto, desde cuándo y en qué estado está?». Sirve como base para generar notas de versión (*patchnotes*).

> **Fuente de verdad:** [`traceability.json`](traceability.json). La tabla de abajo **se genera** desde ese JSON con `go run scripts/gen-traceability.go`; no la edites a mano (los cambios se pierden). Edita el JSON y vuelve a generar.

## Vocabulario fijo de estados

| Estado | Significado |
| :--- | :--- |
| `implementado` | Está en la rama `develop` (código y tests). Si la verificación manual está pendiente, se indica en la descripción del item y tiene su propio item. |
| `en proceso` | Existe trabajo en una rama que **aún no se fusiona** (hoy: TR-015, TR-018, TR-021, TR-023 y TR-024, en la rama `feature/tui-cli-scan-releases`). |
| `por implementar` | No existe código aún; la fecha del item es `null`. |

La **fecha** es la del commit principal (`git log`). La **versión** es el tag más antiguo que contiene ese commit (`v1.0.0`, `v1.1.0`); `null` si aún no se publica.

## Tabla

<!-- BEGIN TABLE -->
| ID | Funcionalidad | Fecha | Estado | Rama/commit | Versión |
| :--- | :--- | :---: | :---: | :--- | :---: |
| TR-001 | Stack base: Go + Cobra + Charm + Bubble Tea | 2026-09-03 | implementado | `develop` / `563c378` | v1.0.0 |
| TR-002 | Microkernel estático con inyección de dependencias | 2026-09-04 | implementado | `develop` / `45c4083` | v1.1.0 |
| TR-003 | Logo / splash con vaca (marca MooFetch) | 2026-09-03 | implementado | `develop` / `e2c6510` | v1.1.0 |
| TR-004 | Selección de campus (UFRO: campusvirtual.ufro.cl) | 2026-09-29 | implementado | `develop` / `68ef9f3` | v1.1.0 |
| TR-005 | Extracción automática de MoodleSession (Chrome/Chromium/Edge/Opera/Brave vía kooky) | 2026-09-29 | implementado | `develop` / `68ef9f3` | v1.1.0 |
| TR-006 | Firefox: cookie de sesión desde sessionstore-backups | 2026-09-29 | implementado | `develop` / `7c20081` | — |
| TR-007 | Verificación manual de autodetección en Chrome | — | por implementar | — | — |
| TR-008 | Verificación manual de autodetección en Edge | — | por implementar | — | — |
| TR-009 | Verificación manual de autodetección en Opera | — | por implementar | — | — |
| TR-010 | Verificación manual de autodetección en Safari (solo macOS) | — | por implementar | — | — |
| TR-011 | Pegado de enlaces Moodle: TUI, CleanURLs, -f/-u y stdin | 2026-09-04 | implementado | `develop` / `6605513` | v1.1.0 |
| TR-012 | Modo headless y códigos de salida POSIX | 2026-09-04 | implementado | `develop` / `0120fa8` | v1.1.0 |
| TR-013 | Cobertura de tests superior al 80% por paquete | 2026-09-29 | implementado | `develop` / `0bd7ed9` | v1.1.0 |
| TR-014 | CI de PR y release con GoReleaser | 2026-09-03 | implementado | `develop` / `fbfe103` | v1.0.0 |
| TR-015 | golangci-lint en CI | 2026-10-24 | en proceso | `feature/tui-cli-scan-releases` / `e821d2f` | — |
| TR-016 | Extensión de navegador | — | por implementar | — | — |
| TR-017 | Aplicación de escritorio (GUI) | — | por implementar | — | — |
| TR-018 | Orden del flujo interactivo: logo, campus y consentimiento en pantallas separadas | 2026-10-24 | en proceso | `feature/tui-cli-scan-releases` / `52df60b` | — |
| TR-019 | Verificación manual de autodetección en Firefox (session store) | — | por implementar | — | — |
| TR-020 | Tests de integración de cookie y descargas rotas; endurecimiento de descargas | 2026-10-19 | implementado | `develop` / `1b006e8` | — |
| TR-021 | Comando scan: listar recursos de una página del campus (experimental) | 2026-10-24 | en proceso | `feature/tui-cli-scan-releases` / `480c4a1` | — |
| TR-022 | Verificación manual de scan contra el campus real | — | por implementar | — | — |
| TR-023 | Ayuda de la CLI legible: flags agrupadas, códigos de salida y pipeline compartido | 2026-10-24 | en proceso | `feature/tui-cli-scan-releases` / `75410f2` | — |
| TR-024 | Patchnotes legibles en GitHub: changelog agrupado y validación de títulos de PR | 2026-10-24 | en proceso | `feature/tui-cli-scan-releases` / `e821d2f` | — |
<!-- END TABLE -->

## Notas

- **Orden del flujo interactivo (TR-018):** el splash con el logo se muestra primero y luego cada decisión (campus, consentimiento) tiene su pantalla. Está implementado en la rama `feature/tui-cli-scan-releases` y falta verlo en un terminal real; detalle en [TASK-09](tasks/TASK-09-orden-logo-campus.md).
- **Firefox (TR-006):** el arreglo se originó en la rama `fix/cookie-autodetect` (commit `7c20081`) y está fusionado en `develop`; la verificación manual es TR-019. Detalle en [ADR 003](adr/003-deteccion-automatica-de-token.md).
- **Verificación manual (TR-007 a TR-010):** ningún navegador se ha verificado manualmente contra el campus real; los tests usan un proveedor de cookies falso. Ver [TASK-08](tasks/TASK-08-verificacion-manual-navegadores.md).
- **`scan` (TR-021, TR-022):** el comando se probó solo con servidores falsos; la prueba contra el campus real decide si se queda o se adelanta la extensión de navegador ([ADR 005](adr/005-scan-de-recursos.md)).

## Cómo mantenerlo

Regla: **todo cambio que se fusione debe añadir o actualizar su item `TR-` en `traceability.json`** (estado, rama, commit, versión, evidencia) y regenerar la tabla.

```bash
# Regenerar la tabla de este documento
go run scripts/gen-traceability.go

# Verificar que está al día (útil antes de abrir la PR)
go run scripts/gen-traceability.go -check
```

## Cómo generar patchnotes

Las notas de una versión salen de los items cuyo campo `version` coincide con el tag:

```bash
# Con el script (Markdown listo para pegar en la Release)
go run scripts/gen-traceability.go -notes v1.1.0

# Alternativa con jq
jq -r '.[] | select(.version == "v1.1.0") | "- \(.id) \(.titulo) (\(.estado))"' docs/traceability.json
```

Al preparar una versión nueva: pasar los items fusionados de `en proceso` a `implementado`, asignarles `version` y `fecha`, y generar las notas con el comando anterior. Los commits convencionales (`feat:`, `fix:`) siguen alimentando el changelog automático de GoReleaser (ver [workflow.md](workflow.md)).

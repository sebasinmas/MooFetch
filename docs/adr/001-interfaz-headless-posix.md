# ADR 001: Interfaz Headless, Tuberías Unix y Códigos de Salida POSIX para Integración Externa

- **Estado:** Aceptado (Accepted)
- **Fecha:** 2026-09-04
- **Autores:** Equipo de Arquitectura e Ingeniería de GoDownloader
- **Versión:** 1.1.0
- **Área:** CLI / Kernel / Integraciones (GUI & Extensiones)

---

## 1. Contexto y Problemática

En sus versiones iniciales (v1.0.x), `GoDownloader` (CampusFetch) fue concebido primordialmente como un script interactivo de terminal impulsado por el framework TUI Charmbracelet (`bubbletea`, `huh`, `lipgloss`). Si bien proporcionaba una experiencia atractiva para usuarios individuales en emuladores de terminal con soporte TTY interactivo, presentaba severas limitaciones arquitectónicas:

1. **Acoplamiento Fuerte con la TUI:** El flujo de ejecución dependía intrínsecamente del asistente interactivo de entrada de datos y de la vista de progreso en pantalla completa. Al intentar ejecutar el binario en entornos no interactivos (CI/CD, scripts bash, cron jobs o subprocesos), la herramienta fallaba o se bloqueaba esperando eventos de teclado en un descriptor de archivo TTY inexistente.
2. **Incompatibilidad con Tuberías Unix (Pipes):** No existía capacidad de ingerir flujos de datos (`stdin`) ni de separar el canal de salida de datos (`stdout`) del canal de diagnóstico y progreso (`stderr`).
3. **Falta de Determinismo en Errores (Exit Codes):** La aplicación utilizaba salidas binarias arbitrarias (`os.Exit(0)` o `os.Exit(1)`). Un fallo por parámetros erróneos, un error de red transitorio, una interrupción deliberada (`Ctrl+C`) o una sesión académica expirada (HTTP 401/403/303) eran indistinguibles a nivel de sistema operativo.
4. **Barrera de Integración para el Roadmap v2.0:** El plan estratégico del proyecto contempla dos componentes satélite:
   - Una **Extensión de Navegador** (Manifest V3) para interceptar enlaces y sesiones de Moodle.
   - Una **Interfaz Gráfica de Escritorio** moderna construida sobre Tauri o Wails.
   Ambos clientes requieren invocar al binario de Go como un proceso secundario (sidecar/subprocess), orquestarlo mediante señales del sistema operativo y reaccionar a su estado de salida de manera no interactiva.

---

## 2. Decisión Arquitectónica

Se decidió desacoplar completamente la capa de presentación de la capa de orquestación y transporte (Microkernel), e implementar una interfaz CLI de grado industrial conforme a los estándares POSIX:

### 2.1 Migración a Cobra y Subcomandos Idiomáticos
Se migró el motor de flags estándar al framework `github.com/spf13/cobra`, estructurando la interfaz en:
- Comando raíz (`godownloader`): Asistente interactivo predeterminado cuando se detecta un TTY interactivo en `stdin`/`stdout`.
- Subcomando `run` (`godownloader run`): Modo de ejecución directa para entornos programáticos, recibiendo URLs por flag (`--urls`, `--file`) o canalizadas por `stdin`.
- Subcomando `version` (`godownloader version`): Diagnóstico y metadata inyectada en tiempo de compilación (`ldflags`).

### 2.2 Detección Automática de Terminal y Modo Headless
Se incorporó la biblioteca `github.com/mattn/go-isatty` para inspeccionar los descriptores de archivo `os.Stdin.Fd()` y `os.Stdout.Fd()`:
- **Detección Dinámica:** Si `stdin` o `stdout` no corresponden a una terminal de caracteres interactiva (ej. `cat urls.txt | godownloader run ...`), la CLI activa inmediatamente el modo Headless.
- **Bandera Explícita:** Se habilitó el flag global `--headless` (con alias `--plain`) para forzar este comportamiento incluso en terminales TTY.
- **Canalización Estricta de I/O:** En modo headless, el motor omite cualquier inicialización de Bubble Tea o secuencias ANSI de pantalla completa. Las notificaciones y barras de progreso en texto plano se dirigen estrictamente a `os.Stderr`, reservando `os.Stdout` para posibles salidas estructuradas o redirecciones sin contaminación.

### 2.3 Estandarización de Códigos de Salida POSIX
Se adoptó un esquema formal de códigos de salida alineado con `sysexits.h` y las convenciones del estándar POSIX:

| Código de Salida | Constante Interna | Significado Arquitectónico | Criterio de Activación |
| :---: | :--- | :--- | :--- |
| `0` | `ExitSuccess` | Éxito total | Lote de descargas completado sin errores. |
| `1` | `ExitGeneralErr` | Error general de ejecución | Fallos de conexión, caídas de socket o fallas de I/O en disco durante el lote. |
| `2` | `ExitUsageErr` | Error de sintaxis / Parámetros | Flags no reconocidos, argumentos obligatorios ausentes o listas vacías de URLs. |
| `77` | `ExitAuthErr` | Fallo de Autenticación (`EX_NOPERM`) | Cookie de sesión inválida, expirada o redirección a login detectada por el Circuit Breaker. |
| `130` | `ExitInterrupted` | Interrupción de Proceso (`128 + SIGINT`) | Cancelación limpia por señales `SIGINT` (`Ctrl+C`), `SIGTERM` o anulación de formulario. |

### 2.4 Control del Ciclo de Vida y Propagación de Contextos
El punto de entrada `main()` enlaza el contexto raíz del sistema operativo mediante `signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)`. Este contexto cancelable se propaga a través de:
- El dispatcher del microkernel (`kernel.Dispatch`).
- El pool acotado de workers concurrentes.
- El transporte HTTP con lectores de timeout de inactividad (`idleTimeoutReader`).
- El sistema de archivos con escrituras atómicas en archivos temporales `.godownload.part`.

Ante cualquier señal de corte o disparo del Circuit Breaker de autenticación, el contexto se cancela de forma determinista, drenando las goroutines activas y eliminando residuos parciales en disco antes de salir con el código correspondiente.

---

## 3. Consecuencias y Beneficios

### Positivas (Enablers)

1. **Habilitación Directa de GUI (Tauri / Wails):**
   - El proceso host de la GUI puede invocar `godownloader run --headless -k "<COOKIE>" --urls "..."` como un subproceso hijo (`child_process` / `Command`).
   - Puede monitorear el progreso consumiendo la salida línea por línea desde el flujo de `stderr`.
   - Puede interpretar el `Exit Code` de manera inmediata: si el proceso termina con código `77`, la interfaz gráfica sabe con certeza matemática que debe presentar al usuario un modal de renovación de sesión, sin requerir análisis frágil de cadenas de texto (*regex scraping*).
   - El envío de un `SIGINT` o terminación del proceso desde la interfaz gráfica garantiza una salida limpia con código `130` sin dejar archivos corruptos en el disco del usuario.
2. **Integración con Extensión de Navegador:**
   - Permite la interacción mediante mecanismos de *Native Messaging* o scripts de automatización locales, transfiriendo lotes masivos de URLs capturadas desde la intranet universitaria directamente a través de tuberías estándar (`stdin`).
3. **Automatización y Scripting en el Ecosistema Unix:**
   - La herramienta ahora es un ciudadano de primera clase en entornos POSIX: interoperable con `xargs`, `cat`, `grep`, `nohup` y flujos de automatización por lotes.
4. **Privacidad Garantizada en Integraciones:**
   - Dado que el motor desacoplado utiliza `log/slog` con ofuscación en runtime (`slog.LogValuer`), ninguna integración de subproceso corre el riesgo de filtrar la sesión en los buffers de diagnóstico compartidos.

### Negativas / Compromisos (Trade-offs)

1. **Mayor Rigor en Pruebas:**
   - Requiere mantener suites de tests dedicadas para validar escenarios con y sin TTY (`cobra_test.go`, `headless_pipe_test.go`, `exitcode_test.go`), así como emulación de tuberías en memoria (`io.Pipe`).
2. **Duplicación de la Lógica de Progreso:**
   - Se mantiene un renderizador reactivo rico para TUI (`tui/progress.go` en Bubble Tea) y un emisor secuencial simplificado para Headless (`tui/headless.go`). Sin embargo, ambos son observadores puros que consumen los mismos eventos atómicos emitidos por el `kernel.Kernel` (`EventTaskStarted`, `EventTaskProgress`, etc.), mitigando la dispersión lógica.

---

## 4. Estado de Implementación

- [x] Subcomandos Cobra implementados en `cmd/downloader/root.go` y `cmd/downloader/run.go`.
- [x] Detección de terminal e interfaz Headless en `internal/tui/headless.go`.
- [x] Mapeo de errores y códigos POSIX en `cmd/downloader/exitcode.go`.
- [x] Circuit Breaker de autenticación integrado con `ExitAuthErr` (77) en `internal/kernel/kernel.go`.
- [x] Cobertura de pruebas unitarias y de concurrencia libres de carreras (`go test -race ./...`).

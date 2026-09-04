# ADR 002: Desacoplamiento de Dominio, Microkernel Estático y Patrón Consumer-Driven para Plugins

- **Estado:** Aceptado (Accepted)
- **Fecha:** 2026-09-04
- **Autores:** Equipo de Arquitectura e Ingeniería de GoDownloader
- **Versión:** 1.1.0
- **Área:** Arquitectura / Kernel / Dominio / Extensiones

---

## 1. Contexto y Problemática

Durante el proceso de evolución arquitectónica de `GoDownloader` (v1.1.x), surgió la necesidad de asegurar que el motor de descargas fuera 100% agnóstico de la interfaz de usuario y de las implementaciones específicas de los protocolos de descarga (Moodle, Blackboard, Canvas, endpoints directos).

En este proceso se evaluaron dos inquietudes críticas:
1. **Prevención de Importaciones Cíclicas:** Se propuso preliminarmente crear un paquete denominado `internal/core` para extraer interfaces (`DownloaderPlugin`) y estructuras de datos (`Task`, `Result`), bajo la premisa de evitar dependencias cruzadas entre el orquestador (`kernel`) y los adaptadores (`plugins`).
2. **Coherencia con las Buenas Prácticas del Ecosistema Go:** En lenguajes orientados a objetos tradicionales (Java, C#), es común agrupar interfaces en paquetes técnicos abstractos (`com.app.core.interfaces`). Sin embargo, en Go:
   - Paquetes con nombres genéricos como `core`, `common`, `util` o `types` se consideran antipatrones (*code smells*) debido a su falta de semántica de dominio (Effective Go).
   - El principio fundamental de diseño en Go dicta: *"Accept interfaces, return structs"* y *"Interfaces belong to the consumer, not the package that implements them"*.
3. **Dispersión de la Capa Visual:** La interfaz de terminal se encontraba fragmentada entre `internal/ui` (pantalla de bienvenida / splash) e `internal/tui` (asistente de formulario con Huh y monitor de progreso reactivo con Bubble Tea).
4. **Nomenclatura del Entrypoint:** El directorio de ejecución se denominaba `cmd/downloader`, discrepando del nombre canónico del binario y del proyecto (`godownloader`).

---

## 2. Decisión Arquitectónica

Tras una auditoría técnica profunda, se adoptaron las siguientes decisiones de diseño:

### 2.1 Descarte del Antipatrón `internal/core`
Se determinó que la preocupación por importaciones cíclicas entre `kernel` y `plugins` era un falso problema: el orquestador (`kernel`) jamás debe importar implementaciones concretas de plugins; son los plugins y el orquestador quienes son ensamblados en el punto de entrada (`cmd/godownloader`) mediante Inyección de Dependencias.

Se rechazó la creación de un paquete genérico `internal/core` por violar las convenciones idiomáticas del lenguaje Go.

### 2.2 Creación de `internal/domain` para Primitivas Puras
Para los casos donde se requiere desacoplar el modelo de datos de la lógica del worker pool y del dispatcher concurrente, se estableció el paquete semántico **`internal/domain`** (en lugar de `core`). Este paquete contiene exclusivamente entidades y tipos de valor inmutables:
- Estructuras de datos: `Task`, `Result`, `Event`, `EventType`, `ProgressUpdate`.
- Errores de dominio: `ErrAuthenticationFailed` e interfaz `FatalAuthError`.
- Firmas funcionales: `ProgressFunc`, `EventHandler`.

`internal/domain` no posee dependencias externas ni de otros paquetes internos del proyecto.

### 2.3 Patrón Consumer-Driven Interface para Plugins
En estricto cumplimiento de la filosofía de Go:
- La interfaz **`DownloaderPlugin`** (o `Plugin`) se declara en el paquete que la consume: **`internal/kernel`**.
- Los plugins (`internal/plugins/moodle`, `internal/plugins/demo`) **no importan una interfaz abstracta para implementarla**. Retornan una estructura concreta (`*Plugin`).
- Gracias al tipado estructural implícito de Go (*duck typing*), cualquier estructura que implemente los métodos `Name() string`, `CanHandle(string) bool` y `Download(context.Context, domain.Task, domain.ProgressFunc) (*domain.Result, error)` satisface automáticamente el contrato del `kernel` en tiempo de compilación.

```go
// internal/kernel/kernel.go
package kernel

import (
    "context"
    "godownloader/internal/domain"
)

// DownloaderPlugin define el contrato exigido por el Kernel a cualquier adaptador de descarga.
type DownloaderPlugin interface {
    Name() string
    CanHandle(rawURL string) bool
    Download(ctx context.Context, task domain.Task, progress domain.ProgressFunc) (*domain.Result, error)
}
```

### 2.4 Consolidación de la Capa Visual en `internal/tui`
Se traslada `internal/ui/splash.go` y sus pruebas unitarias a `internal/tui/`, eliminando por completo el directorio `internal/ui`. Toda la interacción visual (asistente de formulario interactivo `huh`, pantalla splash `lipgloss`, vista de progreso `bubbletea` y salida sin formato para pipelines `headless`) queda agrupada bajo un único paquete cohesivo.

### 2.5 Normalización del Entrypoint a `cmd/godownloader`
Se renombra el directorio `cmd/downloader` a **`cmd/godownloader`** para alinearlo con el estándar de la comunidad (*Standard Go Project Layout*), asegurando que `go install ./cmd/godownloader` genere un binario con el nombre oficial del producto.

---

## 3. Consecuencias y Beneficios

### Positivas (Enablers)
1. **Grafo de Dependencias Limpio (DAG):** Flujo unidireccional y sin ciclos verificable en compilación: `cmd -> tui / kernel / plugins -> domain`.
2. **Alineación con el Ecosistema Go:** El diseño sigue los mismos patrones de diseño utilizados en herramientas de referencia como las de Charmbracelet (`bubbletea`) y HashiCorp (`packer`, `terraform`).
3. **Desacoplamiento Absoluto para Extensiones y GUIs:** El motor `kernel` y los `plugins` pueden ser importados directamente por aplicaciones Wails (`cmd/godownloader-gui`) o demonios de comunicación IPC para extensiones web (`cmd/godownloader-daemon`) sin arrastrar dependencias de terminal ni de emuladores ANSI.
4. **Alta Cohesión Visual:** El equipo de desarrollo tiene un único punto de referencia (`internal/tui`) para ajustar estilos, temas claros/oscuros, compatibilidad con `NO_COLOR` y maquetación de terminal.

### Negativas / Compromisos (Trade-offs)
1. **Actualización de Rutas de Importación:** Requiere actualizar las sentencias `import` en todos los archivos de `cmd/`, `internal/tui/`, `internal/plugins/` y sus respectivos archivos de prueba `_test.go`.
2. **Disciplina en el Dominio:** Debe evitarse la tentación de incorporar lógica de red, concurrencia o formateo de strings dentro de `internal/domain`.

---

## 4. Estado de Implementación / Tareas Derivadas

- [x] Creación de `internal/domain` con las entidades base (`Task`, `Result`, `Event`, `ProgressUpdate`, `ErrAuthenticationFailed`).
- [x] Refactorización de `internal/kernel` para importar `domain` y declarar la interfaz consumidora `DownloaderPlugin`.
- [x] Actualización de adaptadores en `internal/plugins/moodle` e `internal/plugins/demo` para consumir `domain`.
- [x] Fusión de `internal/ui/splash.go` en `internal/tui/` y eliminación del directorio `internal/ui`.
- [x] Renombrado de `cmd/downloader` a `cmd/godownloader`.
- [x] Validación de compilación y pruebas de concurrencia libres de condiciones de carrera (`go test -race ./...`).

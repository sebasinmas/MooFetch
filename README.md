<div align="center">

# ⚡ GoDownloader (CampusFetch)

**Descargas masivas, concurrentes y resilientes para Campus Virtual UFRO y plataformas Moodle.**  
*De un script interactivo a un motor CLI de grado empresarial para pipelines Unix, extensiones y GUIs.*

[![Go Version](https://img.shields.io/badge/Go-1.21+-00ADD8?style=for-the-badge&logo=go)](https://golang.org)
[![Version](https://img.shields.io/badge/Release-v1.1.0-blueviolet?style=for-the-badge)](https://github.com/sebasinmas/GoDownloader/releases)
[![License: MIT](https://img.shields.io/badge/License-MIT-green.svg?style=for-the-badge)](https://opensource.org/licenses/MIT)
[![POSIX Compliant](https://img.shields.io/badge/POSIX-Sysexits_Compliant-orange?style=for-the-badge&logo=linux)](https://pubs.opengroup.org/)

[Características](#-características-principales) • [Instalación](#-instalación) • [Inicio Rápido](#-inicio-rápido) • [Uso en Pipelines](#-modo-headless-y-pipelines-unix) • [Códigos de Salida](#-códigos-de-salida-posix) • [Arquitectura](#-arquitectura-del-sistema) • [Privacidad](#-privacidad-y-seguridad-garantizada) • [Roadmap](#-roadmap)

</div>

---

## 💡 El Problema

Al terminar el semestre en el **Campus Virtual de la Universidad de La Frontera (UFRO)** o cualquier intranet universitaria impulsada por **Moodle**, los estudiantes y docentes se enfrentan al tedio de descargar decenas de presentaciones, guías y documentos PDF uno por uno. 

Las plataformas LMS implementan capas de autenticación con cookies temporales y redirecciones de seguridad (`HTTP 303 See Other` hacia `/login.php`). Estas protecciones rompen de inmediato a los gestores tradicionales como `wget` o `curl`, provocando descargas de archivos HTML corruptos de 2 KB en vez de los documentos deseados.

## 🚀 La Solución: GoDownloader v1.1.0

**GoDownloader** es un motor de descargas concurrente y de alto rendimiento escrito en Go. 

En su versión **v1.1.0**, el proyecto evoluciona de un simple asistente interactivo a una **herramienta CLI de grado industrial construida sobre Cobra**, diseñada para operar indistintamente en consolas interactivas o como núcleo de automatización (*headless engine*). Inyecta credenciales en memoria, sortea redirecciones HTTP 303, aísla fallos de sesión con un *Circuit Breaker* inteligente y garantiza la integridad de tu disco mediante escrituras atómicas.

---

## ✨ Características Principales

- 🏎️ **Worker Pool Acotado y Concurrente:** Despacho paralelo mediante goroutines y canales acotados. Previene saturar tanto tu ancho de banda como los servidores institucionales.
- 🛡️ **Escrituras Atómicas (`.part`)**: Cada archivo se descarga temporalmente con la extensión `.godownload.part`. Si la descarga se cancela o falla a mitad de camino, el residuo se elimina automáticamente, garantizando que nunca queden PDFs incompletos o corruptos en disco.
- ⚡ **Circuit Breaker ante Expiración de Sesión:** Si tu cookie caduca o es rechazada (HTTP 401, 403 o redirección a login), el motor aborta el lote completo instantáneamente (*fail-fast*), evitando cientos de peticiones infructuosas.
- 🤖 **Modo Dual: TUI Rica y Headless POSIX:**
  - **Interactivo:** Asistente visual y barras de progreso reactivas desarrolladas con **Charmbracelet** (`bubbletea`, `huh`, `lipgloss`).
  - **Headless:** Detección automática de tuberías (`cat urls.txt | godownloader run`) con salida de progreso dirigida a `stderr` para no contaminar tus flujos Unix.
- 🔒 **Privacidad sin Fugas (Zero-Leak):** Las cookies solo existen en la memoria volátil del proceso. Los registros de depuración utilizan `log/slog` con ofuscación nativa (`slog.LogValuer`), ocultando cualquier token antes de tocar el disco.
- ⏱️ **Transporte HTTP Resiliente:** Soporta descargas continuas de archivos pesados sin timeouts ciegos, incorporando un detector de inactividad de 30 segundos si el servidor se congela.
- 🎨 **Compatibilidad con `NO_COLOR`:** Degrada elegantemente sus estilos ANSI en fondos claros o entornos donde se especifique la variable de estándar `NO_COLOR=1`.

---

## 📦 Instalación

### Opción 1: Binarios Precompilados (Recomendado)
Descarga el ejecutable para tu plataforma (Linux, macOS o Windows) directamente desde la sección de [Releases](https://github.com/sebasinmas/GoDownloader/releases).

```bash
# Ejemplo en Linux / macOS:
tar -xzf godownloader_v1.1.0_linux_amd64.tar.gz
sudo mv godownloader /usr/local/bin/
```

### Opción 2: Compilación con Inyección de Versión (`ldflags`)
Si dispones del toolchain de Go (1.21 o superior):

```bash
# Clonar repositorio
git clone https://github.com/sebasinmas/GoDownloader.git
cd GoDownloader

# Compilar binario optimizado inyectando versión y commit
go build -ldflags "-s -w \
  -X main.Version=1.2.0 \
  -X main.Commit=$(git rev-parse --short HEAD 2>/dev/null || echo 'release') \
  -X main.BuildDate=$(date +%Y-%m-%d)" \
  -o bin/godownloader ./cmd/godownloader

# Verificar instalación
./bin/godownloader --version
```

### Opción 3: Vía `go install`
```bash
go install github.com/sebasinmas/GoDownloader/cmd/godownloader@latest
```

---

## 📖 Inicio Rápido

### 1. Obtener la Cookie de Sesión (10 Segundos)

1. Abre tu navegador e inicia sesión en el **Campus Virtual UFRO** (o tu intranet Moodle).
2. Presiona `F12` para desplegar las **Herramientas de Desarrollador** y ve a la pestaña **Red** (*Network*).
3. Recarga (`F5`), pulsa en cualquier recurso de la lista y localiza la cabecera `Cookie` en **Cabeceras de Solicitud** (*Request Headers*).
4. Copia el valor de tu sesión, por ejemplo:
   ```text
   MoodleSession=tu_token_aqui_123456
   ```
   *(También puedes extraerla desde la pestaña **Almacenamiento / Application** -> **Cookies**)*.

---

### 2. Uso Interactivo (Clásico TUI)

Ideal para el uso diario en terminales interactivas:

```bash
godownloader
```

Un asistente visual te guiará en dos pasos:
1. Pega tu cookie de sesión (`MoodleSession=...`).
2. Pega la lista de URLs de tus documentos (puedes pegar múltiples líneas a la vez).
3. Presiona `Esc` + `Enter` y visualiza la descarga concurrente con barras de avance por archivo y porcentaje global.

---

## ⚙️ Modo Headless y Pipelines Unix

GoDownloader v1.1.0 es un ciudadano de primera clase en el ecosistema POSIX. Detecta automáticamente si la entrada estándar (`stdin`) o la salida (`stdout`) están conectadas a una tubería, conmutando a modo headless de forma transparente.

### Ejemplos en Tuberías (Pipes)

```bash
# Ingerir URLs directamente desde un archivo de texto con cookie por parámetro
cat urls.txt | godownloader run -k "MoodleSession=abc123xyz" --headless

# Usar variable de entorno para la cookie y definir salida personalizada
export MOODLE_SESSION="MoodleSession=abc123xyz"
cat urls.txt | godownloader run -o ./apuntes_semestre -c 8

# Filtrar URLs con grep y descargar en paralelo con bandera --file
grep "pdf" historial_campus.txt > urls_pdf.txt
godownloader run -k "$MOODLE_SESSION" -f urls_pdf.txt --concurrency 6
```

> **Nota para Desarrolladores:** En modo headless, el registro de avance se transmite a **`stderr`** (`[1/10] ▶ Descargando...`), garantizando que **`stdout`** permanezca limpio para redirecciones de flujos o pipes hacia herramientas como `jq` o `awk`.

---

## 🚦 Códigos de Salida POSIX

GoDownloader implementa códigos de retorno deterministas alineados con la especificación `sysexits.h` y las convenciones POSIX. Esto permite que scripts de bash, CI/CD, extensiones de navegador y aplicaciones de escritorio (Tauri/Wails) manejen el ciclo de vida de la ejecución con precisión:

| Código | Constante Interna | Categoría | Descripción Técnica |
| :---: | :--- | :--- | :--- |
| **`0`** | `ExitSuccess` | Éxito | Todas las tareas de descarga finalizaron satisfactoriamente. |
| **`1`** | `ExitGeneralErr` | Error General / Red | Fallo de conexión, timeout de socket no recuperable o error de I/O en disco durante el lote. |
| **`2`** | `ExitUsageErr` | Error de Parámetros | Flags no reconocidos, argumentos requeridos faltantes o lista de URLs vacía. |
| **`77`** | `ExitAuthErr` | Sesión Inválida (`EX_NOPERM`) | Cookie expirada, credenciales inválidas o redirección a login abortada por el Circuit Breaker. |
| **`130`** | `ExitInterrupted` | Interrupción (`128 + SIGINT`) | Cancelación limpia provocada por el usuario (`Ctrl+C`), `SIGTERM` o anulación del formulario. |

### Ejemplo de Integración en Scripts Shell

```bash
godownloader run -k "$MOODLE_SESSION" -f urls.txt --headless
EXIT_CODE=$?

case $EXIT_CODE in
  0)
    echo "✅ Todas las descargas concluyeron con éxito."
    ;;
  77)
    echo "🔒 La sesión de Moodle expiró. Por favor renueva tu cookie."
    exit 1
    ;;
  130)
    echo "⚠️ Descarga abortada por el usuario. No quedaron archivos temporales."
    ;;
  *)
    echo "❌ Error en la ejecución (Código $EXIT_CODE)."
    ;;
esac
```

---

## 🎛️ Referencia de Comandos y Banderas

```text
Uso:
  godownloader [flags]
  godownloader [command]

Comandos Disponibles:
  run         Ejecuta descargas en modo directo sin asistente interactivo
  version     Muestra la versión instalada, commit y fecha de compilación
  help        Ayuda sobre cualquier comando
```

### Banderas Globales

| Bandera | Shorthand | Valor por Defecto | Descripción |
| :--- | :---: | :---: | :--- |
| `--cookie` | `-k` | `""` | Cookie de sesión Moodle (o variable de entorno `MOODLE_SESSION`). |
| `--concurrency` | `-c` | `5` | Límite máximo de descargas simultáneas en el worker pool. |
| `--output` | `-o` | `"."` | Carpeta de destino para los archivos descargados. |
| `--headless` | | `false` | Fuerza la ejecución en modo headless (progreso plano a `stderr`). |
| `--log` | `-l` | `""` | Genera un archivo de diagnóstico estructurado con `log/slog`. |
| `--demo` | `-d` | `false` | Modo simulación local para presentaciones y pruebas de carga. |
| `--version` | `-v` | | Imprime la versión del binario. |

### Banderas Específicas de `run`

| Bandera | Shorthand | Descripción |
| :--- | :---: | :--- |
| `--urls` | `-u` | Lista de URLs separadas por coma. |
| `--file` | `-f` | Ruta a un archivo de texto con URLs (una por línea). |

### Variables de Entorno

- `MOODLE_SESSION`: Inyecta la cookie de sesión por defecto si no se especifica `--cookie`.
- `NO_COLOR`: Si está definida (cualquier valor), deshabilita completamente las secuencias de escape ANSI de color en TUI y logs.

---

## 🛡️ Privacidad y Seguridad Garantizada

GoDownloader fue desarrollado siguiendo el principio de **Mínimo Privilegio y Cero Persistencia de Secretos**:

- **Aislamiento en Memoria:** Tus credenciales de sesión (`MoodleSession`) residen exclusivamente en la memoria volátil del proceso durante el tiempo de ejecución.
- **Sin Guardado en Disco:** El binario jamás guarda ni almacena tus cookies en archivos de configuración, base de datos local ni historial.
- **Redacción Automática en Logs:** Al usar el flag `--log debug.txt`, el registrador estructurado (`log/slog`) aplica un formateador criptográfico (`SessionCookie`) que enmascara las cookies en tiempo real:
  ```text
  # Ejemplo de registro seguro generado:
  time=2026-09-04T00:00:00 level=INFO msg="GoDownloader inicializado" cookie="MoodleSession=a1b***f9z (len: 32)"
  ```
- **Protección contra Path Traversal:** Los nombres de archivo recibidos mediante cabeceras HTTP `Content-Disposition` se sanean estrictamente, bloqueando caracteres ilegales o secuencias maliciosas (`../`).

---

## 🏗️ Arquitectura del Sistema

GoDownloader está construido bajo el patrón **Microkernel Estático**:

```text
┌────────────────────────────────────────────────────────┐
│               Cobra CLI / Entrypoint                   │
│          (Detección TTY / Subcomandos / POSIX)         │
└───────────┬────────────────────────────────┬───────────┘
            │                                │
            ▼ (TTY Interactivo)              ▼ (Headless / Pipe)
   ┌───────────────────┐            ┌────────────────────┐
   │  Charm TUI Engine │            │ Stream Observador  │
   │  (Huh & Bubbletea)│            │    (a os.Stderr)   │
   └────────┬──────────┘            └────────┬───────────┘
            │                                │
            └───────────────┬────────────────┘
                            ▼
 ┌───────────────────────────────────────────────────────┐
 │               Kernel Orquestador                      │
 │    - Pool Acotado de Goroutines (Semáforo Dinámico)   │
 │    - Circuit Breaker de Autenticación                 │
 │    - Manejador de Contextos y Señales (SIGINT/SIGTERM) │
 └───────────┬───────────────────────────────┬───────────┘
             │                               │
             ▼                               ▼
    ┌─────────────────┐             ┌─────────────────┐
    │  Moodle Plugin  │             │  [Futuro] LMS   │
    │ (Auth/HTTP 303) │             │ (Canvas/B-Board)│
    └────────┬────────┘             └─────────────────┘
             ▼
    ┌─────────────────────────────────────────────────┐
    │ Capa de Transporte Seguro & E/S Atómica         │
    │  - Timeouts de Inactividad (Stream Watcher)     │
    │  - Archivos Temporales (.godownload.part)       │
    │  - Registro Estructurado con log/slog Ofuscado  │
    └─────────────────────────────────────────────────┘
```

1. **Microkernel Desacoplado:** El registro de plugins se realiza por inyección de dependencias (`kernel.WithPlugins(...)`), eliminando funciones `init()` con efectos secundarios globales.
2. **Worker Pool Bounded:** La concurrencia está estrictamente limitada por un semáforo de canales; no importa si procesas 5 o 5.000 URLs, nunca se crearán goroutines descontroladas.
3. **Escrituras Atómicas:** Se garantiza la consistencia del sistema de archivos local frente a caídas de red o cortes de energía.

---

## 🗺️ Roadmap

- [x] **v1.1.0:** Migración a Cobra, soporte Headless, escrituras atómicas, Circuit Breaker y códigos POSIX.
- [ ] **Extensión de Navegador (Chrome MV3):** Captura de enlaces y sesión con un solo clic desde el Campus Virtual.
- [ ] **Aplicación de Escritorio Multiplataforma (Tauri / Wails):** GUI visual que interactúa con este binario como subproceso POSIX.
- [ ] **Soporte para Carpetas Moodle (`mod/folder`):** Extracción recursiva de archivos comprimidos o directorios completos.
- [ ] **Plugins para Plataformas Adicionales:** Módulos para Canvas LMS y Blackboard Learn.
- [ ] **Distribución en Gestores de Paquetes:** Soporte oficial para `brew`, `scoop`, `winget` y AUR.

---

## 🤝 Contribuciones

¡Las contribuciones son bienvenidas! Si deseas reportar un fallo, proponer mejoras o adaptar GoDownloader a la plataforma de tu universidad:

1. Haz un **Fork** del proyecto.
2. Crea una rama para tu función (`git checkout -b feature/nueva-mejora`).
3. Comprueba que todos los tests unitarios pasen y no existan condiciones de carrera:
   ```bash
   go test -v -race ./...
   ```
4. Envía un **Pull Request** detallando tus cambios.

---

## 📄 Licencia

Distribuido bajo la Licencia **MIT**. Consulta el archivo `LICENSE` para más información.

> **Aviso de Uso Responsable:** Esta herramienta fue creada para facilitar el respaldo de material de estudio que el usuario ya tiene permiso legítimo de acceder. Respeta las políticas de uso y normativas de los servicios tecnológicos de tu institución académica.

<div align="center">

# 🐄 MooFetch

**Descargas masivas, concurrentes y resilientes para Campus Virtual UFRO y plataformas Moodle.**  
*De un script interactivo a un motor CLI de grado empresarial para pipelines Unix, extensiones y GUIs.*

[![Go Version](https://img.shields.io/badge/Go-1.24+-00ADD8?style=for-the-badge&logo=go)](https://golang.org)
[![Version](https://img.shields.io/badge/Release-v2.0.0-blueviolet?style=for-the-badge)](https://github.com/sebasinmas/MooFetch/releases)
[![License: MIT](https://img.shields.io/badge/License-MIT-green.svg?style=for-the-badge)](https://opensource.org/licenses/MIT)
[![POSIX Compliant](https://img.shields.io/badge/POSIX-Sysexits_Compliant-orange?style=for-the-badge&logo=linux)](https://pubs.opengroup.org/)

[Características](#-características-principales) • [Instalación](#-instalación) • [Inicio Rápido](#-inicio-rápido) • [Uso en Pipelines](#-modo-headless-y-pipelines-unix) • [Códigos de Salida](#-códigos-de-salida-posix) • [Arquitectura](#-arquitectura-del-sistema) • [Privacidad](#-privacidad-y-seguridad-garantizada) • [Roadmap](#-roadmap)

</div>

---

## 💡 El Problema

Al terminar el semestre en el **Campus Virtual de la Universidad de La Frontera (UFRO)** o cualquier intranet universitaria impulsada por **Moodle**, los estudiantes y docentes se enfrentan al tedio de descargar decenas de presentaciones, guías y documentos PDF uno por uno. 

Las plataformas LMS implementan capas de autenticación con cookies temporales y redirecciones de seguridad (`HTTP 303 See Other` hacia `/login.php`). Estas protecciones rompen de inmediato a los gestores tradicionales como `wget` o `curl`, provocando descargas de archivos HTML corruptos de 2 KB en vez de los documentos deseados.

## 🚀 La Solución: MooFetch v2.0.0

**MooFetch** es un motor de descargas concurrente y de alto rendimiento escrito en Go. 

En su versión **v2.0.0**, el asistente deja de pedirte enlaces: **lee la página de tu curso y te deja elegir por nombre qué descargar**. Sigue siendo una **herramienta CLI de grado industrial construida sobre Cobra**, diseñada para operar indistintamente en consolas interactivas o como núcleo de automatización (*headless engine*). Inyecta credenciales en memoria, sortea redirecciones HTTP 303, aísla fallos de sesión con un *Circuit Breaker* inteligente y garantiza la integridad de tu disco mediante escrituras atómicas.

### 🎬 Míralo en acción

<div align="center">
  <img src="docs/assets/demo.gif" alt="MooFetch: logo, elección de campus, lectura del curso, selección de recursos por nombre y descargas concurrentes" width="880">
</div>

El video es una simulación (`moofetch --demo`): no usa la red, no lee tu navegador ni necesita una cookie real, pero muestra exactamente las mismas pantallas. Pruébalo tú mismo:

```bash
moofetch --demo -o /tmp/moofetch-demo
```

---

## ✨ Características Principales

- 🏎️ **Worker Pool Acotado y Concurrente:** Despacho paralelo mediante goroutines y canales acotados. Previene saturar tanto tu ancho de banda como los servidores institucionales.
- 🛡️ **Escrituras Atómicas (`.part`)**: Cada archivo se descarga temporalmente con la extensión `.moofetch.part`. Si la descarga se cancela o falla a mitad de camino, el residuo se elimina automáticamente, garantizando que nunca queden PDFs incompletos o corruptos en disco.
- ⚡ **Circuit Breaker ante Expiración de Sesión:** Si tu cookie caduca o es rechazada (HTTP 401, 403 o redirección a login), el motor aborta el lote completo instantáneamente (*fail-fast*), evitando cientos de peticiones infructuosas.
- 🔎 **Escáner de cursos (`moofetch scan`):** Lee una página del curso con tu sesión y lista sus recursos (PDF, carpetas, archivos). Es amable con el servidor: peticiones de a una, con pausa, respetando `Retry-After`. En el asistente eliges por nombre qué descargar; en la terminal lo canalizas a `run`.
- 🤖 **Modo Dual: TUI Rica y Headless POSIX:**
  - **Interactivo:** Asistente visual y barras de progreso reactivas desarrolladas con **Charmbracelet** (`bubbletea`, `huh`, `lipgloss`).
  - **Headless:** Detección automática de tuberías (`cat urls.txt | moofetch run`) con salida de progreso dirigida a `stderr` para no contaminar tus flujos Unix.
- 🔒 **Privacidad sin Fugas (Zero-Leak):** Las cookies solo existen en la memoria volátil del proceso. Los registros de depuración utilizan `log/slog` con ofuscación nativa (`slog.LogValuer`), ocultando cualquier token antes de tocar el disco.
- ⏱️ **Transporte HTTP Resiliente:** Soporta descargas continuas de archivos pesados sin timeouts ciegos, incorporando un detector de inactividad de 30 segundos si el servidor se congela.
- 🎨 **Compatibilidad con `NO_COLOR`:** Degrada elegantemente sus estilos ANSI en fondos claros o entornos donde se especifique la variable de estándar `NO_COLOR=1`.

---

## 📦 Instalación

### Opción 1: Binarios Precompilados (Recomendado)
Descarga el ejecutable para tu plataforma (Linux, macOS o Windows) directamente desde la sección de [Releases](https://github.com/sebasinmas/MooFetch/releases).

```bash
# Ejemplo en Linux / macOS:
tar -xzf moofetch_2.0.0_linux_amd64.tar.gz
sudo mv moofetch /usr/local/bin/
```

### Opción 2: Compilación con Inyección de Versión (`ldflags`)
Si dispones del toolchain de Go (1.24 o superior):

```bash
# Clonar repositorio
git clone https://github.com/sebasinmas/MooFetch.git
cd MooFetch

# Compilar binario optimizado inyectando versión y commit
go build -ldflags "-s -w \
  -X main.Version=2.0.0 \
  -X main.Commit=$(git rev-parse --short HEAD 2>/dev/null || echo 'release') \
  -X main.BuildDate=$(date +%Y-%m-%d)" \
  -o bin/moofetch ./cmd/moofetch

# Verificar instalación
./bin/moofetch --version
```

### Opción 3: Vía `go install`
```bash
go install github.com/sebasinmas/MooFetch/cmd/moofetch@latest
```

---

## 📖 Inicio Rápido

### 1. Obtener la Cookie de Sesión

#### Opción A: Automática (recomendada)

MooFetch puede buscar la cookie por ti en los navegadores de tu computador (Chrome, Chromium, Edge y Firefox).

1. Inicia sesión en tu campus virtual con tu navegador de siempre.
2. Ejecuta `moofetch`. Te preguntará **"¿Cuál es tu universidad?"**. Elige la tuya (por ejemplo, UFRO usa `campusvirtual.ufro.cl`) o escribe otro dominio con **"Otra…"**.
3. Lee el aviso y acepta. MooFetch busca la cookie de ese sitio y sigue solo.
4. Si no la encuentra, te deja pegarla a mano (Opción B).

Sin el asistente, usa las banderas `--uni` o `--domain`:

```bash
moofetch run --uni ufro -f urls.txt
moofetch run --domain campus.miuniversidad.cl -f urls.txt
```

Notas:
- Cierra y vuelve a abrir el navegador si la búsqueda falla: a veces bloquea sus archivos mientras está abierto.
- Firefox: MoodleSession es una cookie de sesión, así que Firefox la guarda en su copia de la sesión y no en `cookies.sqlite`. MooFetch también lee esa copia, incluso en la carpeta nueva `~/.config/mozilla/firefox`. Mantén Firefox abierto con tu sesión iniciada.
- En Linux, el navegador puede pedir acceso a tu llavero de contraseñas para leer la cookie.
- MooFetch solo lee la cookie de **ese** dominio. No la guarda en disco ni la muestra en los registros.
- Orden de búsqueda sin asistente: `--cookie`, luego el navegador (`--uni`/`--domain`), luego `MOODLE_SESSION`.

#### Opción B: A mano (10 segundos)

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
moofetch
```

Flujo del asistente (cada decisión tiene su propia pantalla):

1. **Splash:** se muestra el logo de MooFetch con la vaca 🐄.
2. **Campus:** eliges tu universidad (por ejemplo `campusvirtual.ufro.cl`) o «Otra…» e ingresas el dominio. Con `--uni` o `--domain` esta pantalla se omite.
3. **Consentimiento:** aceptas o rechazas la detección automática de la cookie en tu navegador. Si no encuentra la cookie (por ejemplo, porque aún no iniciaste sesión en el campus), te lo dice y pasas a pegarla.
4. **Cookie** (`MoodleSession=...`): solo si la detección falló o la rechazaste.
5. **Página del curso:** pegas la URL de un curso o sección (debe ser de tu campus). MooFetch la lee con tu sesión y lista los recursos que encuentra.
6. **Selección:** ves los **nombres** de los recursos (no los enlaces), todos marcados. Con `Espacio` desmarcas los que no quieras, `Ctrl+A` marca o desmarca todos y `/` filtra.
   - Si dejas la URL vacía, o la lectura falla, puedes **pegar los enlaces a mano** como antes (una URL por línea).
7. Presiona `Esc` + `Enter` y visualiza la descarga concurrente con barras de avance por archivo y porcentaje global.

> **Nota:** la detección automática de la cookie está descrita en el [ADR 003](docs/adr/003-deteccion-automatica-de-token.md); el soporte de Firefox (cookie de sesión desde la copia de la sesión) ya está fusionado, y la verificación manual en cada navegador sigue pendiente.

---

## ⚙️ Modo Headless y Pipelines Unix

MooFetch es un ciudadano de primera clase en el ecosistema POSIX. Detecta automáticamente si la entrada estándar (`stdin`) o la salida (`stdout`) están conectadas a una tubería, conmutando a modo headless de forma transparente.

### Ejemplos en Tuberías (Pipes)

```bash
# Ingerir URLs directamente desde un archivo de texto con cookie por parámetro
cat urls.txt | moofetch run -k "MoodleSession=abc123xyz" --headless

# Usar variable de entorno para la cookie y definir salida personalizada
export MOODLE_SESSION="MoodleSession=abc123xyz"
cat urls.txt | moofetch run -o ./apuntes_semestre -c 8

# Filtrar URLs con grep y descargar en paralelo con bandera --file
grep "pdf" historial_campus.txt > urls_pdf.txt
moofetch run -k "$MOODLE_SESSION" -f urls_pdf.txt --concurrency 6

# Listar los recursos de un curso (un enlace por línea en stdout) y descargarlos
moofetch scan "https://campusvirtual.ufro.cl/course/view.php?id=123" --uni ufro > urls.txt
moofetch run -f urls.txt --uni ufro -o ./apuntes
```

`scan` pide las páginas de a una, con una pausa entre ellas (`--delay`, 1,5 s por defecto), respeta `Retry-After` y se detiene si tu sesión expiró. Los títulos van a `stderr`, así que `stdout` queda limpio para tuberías.

> **Nota para Desarrolladores:** En modo headless, el registro de avance se transmite a **`stderr`** (`[1/10] ▶ Descargando...`), garantizando que **`stdout`** permanezca limpio para redirecciones de flujos o pipes hacia herramientas como `jq` o `awk`.

---

## 🚦 Códigos de Salida POSIX

MooFetch implementa códigos de retorno deterministas alineados con la especificación `sysexits.h` y las convenciones POSIX. Esto permite que scripts de bash, CI/CD, extensiones de navegador y aplicaciones de escritorio (Tauri/Wails) manejen el ciclo de vida de la ejecución con precisión:

| Código | Constante Interna | Categoría | Descripción Técnica |
| :---: | :--- | :--- | :--- |
| **`0`** | `ExitSuccess` | Éxito | Todas las tareas de descarga finalizaron satisfactoriamente. |
| **`1`** | `ExitGeneralErr` | Error General / Red | Fallo de conexión, timeout de socket no recuperable o error de I/O en disco durante el lote. |
| **`2`** | `ExitUsageErr` | Error de Parámetros | Flags no reconocidos, argumentos requeridos faltantes o lista de URLs vacía. |
| **`77`** | `ExitAuthErr` | Sesión Inválida (`EX_NOPERM`) | Cookie expirada, credenciales inválidas o redirección a login abortada por el Circuit Breaker. |
| **`130`** | `ExitInterrupted` | Interrupción (`128 + SIGINT`) | Cancelación limpia provocada por el usuario (`Ctrl+C`), `SIGTERM` o anulación del formulario. |

### Ejemplo de Integración en Scripts Shell

```bash
moofetch run -k "$MOODLE_SESSION" -f urls.txt --headless
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

`moofetch --help` agrupa las banderas por tema y muestra al final los códigos de salida:

```text
Uso:
  moofetch [flags]
  moofetch <comando> [flags]

Comandos:
  help        Muestra la ayuda de un comando
  run         Descarga un lote de URLs sin asistente interactivo
  version     Muestra la versión, el commit y la fecha de compilación

Descarga:
  -o, --output DIR     carpeta donde se guardan los archivos (por defecto ".")
  -c, --concurrency N  descargas simultáneas (por defecto 5)

Sesión:
  -k, --cookie COOKIE  cookie de sesión (ej. MoodleSession=abc123)
      --uni CLAVE      universidad del catálogo, para leer la cookie del navegador (ej. ufro)
      --domain HOST    dominio del campus, para leer la cookie del navegador (ej. campus.ejemplo.cl)

Salida:
      --headless       sin interfaz, con el progreso en stderr
  -l, --log ARCHIVO    archivo de registro de depuración (ej. debug.txt)

Otros:
  -d, --demo           simula el asistente y las descargas, sin usar la red
  -h, --help           muestra esta ayuda
  -v, --version        muestra la versión
```

La cookie de sesión se busca en este orden: `-k/--cookie`, detección en el navegador (`--uni`/`--domain`) y la variable `MOODLE_SESSION`. `moofetch run --help` muestra además el grupo **Entrada** (`-u/--urls`, `-f/--file`).

### Banderas por grupo

| Grupo | Bandera | Shorthand | Valor por Defecto | Descripción |
| :--- | :--- | :---: | :---: | :--- |
| Descarga | `--output` | `-o` | `"."` | Carpeta donde se guardan los archivos. |
| Descarga | `--concurrency` | `-c` | `5` | Descargas simultáneas en el worker pool. |
| Sesión | `--cookie` | `-k` | `""` | Cookie de sesión Moodle (o variable de entorno `MOODLE_SESSION`). |
| Sesión | `--uni` | | `""` | Universidad de la lista (ej. `ufro`). Busca la cookie en tu navegador. |
| Sesión | `--domain` | | `""` | Dominio de tu Moodle (ej. `campus.miuniversidad.cl`). Gana sobre `--uni`. |
| Salida | `--headless` | | `false` | Sin interfaz: progreso plano a `stderr`. |
| Salida | `--log` | `-l` | `""` | Archivo de diagnóstico estructurado con `log/slog`. |
| Otros | `--demo` | `-d` | `false` | Recorre el asistente completo con una cookie y un curso simulados (sin red ni navegador) y descarga archivos de prueba. |
| Otros | `--version` | `-v` | | Imprime la versión del binario. |

Los alias ocultos `--plain` (= `--headless`) y `--logger` (= `--log`) siguen funcionando para no romper scripts existentes.

### Banderas Específicas de `run` (grupo Entrada)

| Bandera | Shorthand | Descripción |
| :--- | :---: | :--- |
| `--urls` | `-u` | URLs separadas por coma. |
| `--file` | `-f` | Archivo de texto con una URL por línea. |

Si no das `--urls` ni `--file` y hay datos por tubería, `run` lee las URLs de `stdin`.

### Variables de Entorno

- `MOODLE_SESSION`: Inyecta la cookie de sesión por defecto si no se especifica `--cookie`.
- `NO_COLOR`: Si está definida (cualquier valor), deshabilita completamente las secuencias de escape ANSI de color en TUI y logs.

---

## 🛡️ Privacidad y Seguridad Garantizada

MooFetch fue desarrollado siguiendo el principio de **Mínimo Privilegio y Cero Persistencia de Secretos**:

- **Aislamiento en Memoria:** Tus credenciales de sesión (`MoodleSession`) residen exclusivamente en la memoria volátil del proceso durante el tiempo de ejecución.
- **Sin Guardado en Disco:** El binario jamás guarda ni almacena tus cookies en archivos de configuración, base de datos local ni historial.
- **Redacción Automática en Logs:** Al usar el flag `--log debug.txt`, el registrador estructurado (`log/slog`) aplica un formateador criptográfico (`SessionCookie`) que enmascara las cookies en tiempo real:
  ```text
  # Ejemplo de registro seguro generado:
  time=2026-09-04T00:00:00 level=INFO msg="MooFetch inicializado" cookie="MoodleSession=a1b***f9z (len: 32)"
  ```
- **Protección contra Path Traversal:** Los nombres de archivo recibidos mediante cabeceras HTTP `Content-Disposition` se sanean estrictamente, bloqueando caracteres ilegales o secuencias maliciosas (`../`).

---

## 🏗️ Arquitectura del Sistema

MooFetch está construido bajo el patrón **Microkernel Estático**:

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
    │  - Archivos Temporales (.moofetch.part)         │
    │  - Registro Estructurado con log/slog Ofuscado  │
    └─────────────────────────────────────────────────┘
```

1. **Microkernel Desacoplado:** El registro de plugins se realiza por inyección de dependencias (`kernel.WithPlugins(...)`), eliminando funciones `init()` con efectos secundarios globales.
2. **Worker Pool Bounded:** La concurrencia está estrictamente limitada por un semáforo de canales; no importa si procesas 5 o 5.000 URLs, nunca se crearán goroutines descontroladas.
3. **Escrituras Atómicas:** Se garantiza la consistencia del sistema de archivos local frente a caídas de red o cortes de energía.
4. **Extensible:** añadir un plugin solo requiere implementar `DownloaderPlugin` e inyectarlo con `kernel.WithPlugins` (ver [docs/architecture.md](docs/architecture.md), sección Extensibilidad).

---

## 🗺️ Roadmap

El estado detallado de cada funcionalidad (fecha, rama, commit y versión) está en [docs/traceability.md](docs/traceability.md).

- [x] **v1.1.0:** Migración a Cobra, soporte Headless, escrituras atómicas, Circuit Breaker y códigos POSIX.
- [x] **v2.0.0:** `moofetch scan` y selección de recursos por nombre en el asistente, una decisión por pantalla con el logo primero, ayuda de la CLI con flags agrupadas, teclas en español, patchnotes automáticos por GitHub Releases.
- [x] **Selección de campus y autodetección de la cookie** (Chrome/Chromium/Edge/Opera/Brave vía kooky; verificación manual pendiente).
- [x] **Autodetección en Firefox** (cookie de sesión desde la copia de la sesión): fusionada; verificación manual pendiente.
- [ ] **Verificación manual por navegador:** Chrome, Edge, Opera, Firefox y Safari (solo macOS).
- [ ] **Extensión de Navegador (Chrome MV3):** Captura de enlaces y sesión con un solo clic desde el Campus Virtual.
- [ ] **Aplicación de Escritorio Multiplataforma (Tauri / Wails):** GUI visual que reutiliza el núcleo (kernel y dominio) de este proyecto.
- [ ] **Carpetas Moodle (`mod/folder`) anidadas:** `scan` ya expande un nivel; falta la extracción recursiva y agrupar por carpeta en la selección.
- [ ] **Plugins para Plataformas Adicionales:** Módulos para Canvas LMS y Blackboard Learn.
- [ ] **Distribución en Gestores de Paquetes:** Soporte oficial para `brew`, `scoop`, `winget` y AUR.

---

## 🤝 Contribuciones

¡Las contribuciones son bienvenidas! Si deseas reportar un fallo, proponer mejoras o adaptar MooFetch a la plataforma de tu universidad:

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

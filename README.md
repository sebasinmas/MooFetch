<div align="center">

# 🐄 MooFetch

**Descarga todo el material de tu curso de Moodle en un par de clics.**  
*Lee la página de tu curso, eliges por nombre qué quieres y MooFetch lo baja todo en paralelo.*

[![CI](https://img.shields.io/github/actions/workflow/status/sebasinmas/MooFetch/ci.yml?branch=main&style=for-the-badge&label=CI&logo=githubactions&logoColor=white)](https://github.com/sebasinmas/MooFetch/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/sebasinmas/MooFetch?style=for-the-badge&color=blueviolet)](https://github.com/sebasinmas/MooFetch/releases/latest)
[![Descargas](https://img.shields.io/github/downloads/sebasinmas/MooFetch/total?style=for-the-badge&color=success)](https://github.com/sebasinmas/MooFetch/releases)
[![Go](https://img.shields.io/github/go-mod/go-version/sebasinmas/MooFetch?style=for-the-badge&logo=go&logoColor=white&color=00ADD8)](https://golang.org)
[![Licencia MIT](https://img.shields.io/github/license/sebasinmas/MooFetch?style=for-the-badge&color=green)](LICENSE)

<img src="docs/assets/demo.gif" alt="MooFetch en acción: elección de campus, lectura del curso, selección de recursos por nombre y descargas concurrentes" width="880">

*Demostración simulada (`moofetch --demo`): muestra las mismas pantallas que la app real, sin red ni cookie.*

[¿Por qué?](#-por-qué-moofetch) • [Instalación](#-instalación) • [Cómo usarlo](#-cómo-usarlo) • [Scripts y pipelines](#-scripts-y-pipelines) • [Privacidad](#-privacidad) • [Roadmap](#-roadmap) • [Para desarrolladores](#-para-desarrolladores)

</div>

---

## 🎯 ¿Por qué MooFetch?

Al terminar el semestre tienes decenas de PDFs repartidos por el campus virtual (UFRO o cualquier Moodle) y bajarlos uno por uno es tedioso. Y `wget` o `curl` no sirven: el campus pide sesión y redirige al login, así que terminas con archivos HTML corruptos de 2 KB en vez de tus documentos.

MooFetch resuelve eso:

- 🔎 **Lee tu curso por ti.** Le das la URL de un curso o sección y te muestra los recursos por **nombre** (no por enlace), todos marcados. Desmarcas lo que no quieras y listo.
- 🧠 **Todo tu material en un solo lugar, listo para tu IA de estudio.** Baja el curso completo a una carpeta (`moofetch -o ./mi-curso`) y súbela de una vez a [NotebookLM](https://notebooklm.google.com) —o a ChatGPT, Claude o la herramienta que uses— para hacer resúmenes, preguntas y guías de estudio sobre *todo* el curso, sin cargar archivo por archivo.
- 🍃 **Amable con el campus.** Descargas en paralelo pero acotadas, y el escáner pide las páginas de a una, con pausa, para no sobrecargar el servidor.
- 🔒 **Tu sesión no sale de tu computador.** Usa tu cookie solo en memoria; no se guarda ni se registra.
- 🤖 **También sirve para scripts.** Modo headless, tuberías Unix y códigos de salida POSIX para automatizar.

---

## 📦 Instalación

No necesitas instalar Go: descarga el archivo de tu sistema, descomprímelo y listo. Los enlaces apuntan siempre a la última versión:

| Tu computador | Descarga |
| :--- | :--- |
| 🪟 **Windows** | [`MooFetch-Windows.zip`](https://github.com/sebasinmas/MooFetch/releases/latest/download/MooFetch-Windows.zip) |
| 🐧 **Linux** | [`MooFetch-Linux.tar.gz`](https://github.com/sebasinmas/MooFetch/releases/latest/download/MooFetch-Linux.tar.gz) |
| 🍎 **Mac** (Intel o chip Apple) | [`MooFetch-Mac.zip`](https://github.com/sebasinmas/MooFetch/releases/latest/download/MooFetch-Mac.zip) |

Todas las versiones y sus notas están en [**Releases**](https://github.com/sebasinmas/MooFetch/releases).

### Windows

1. Descarga `MooFetch-Windows.zip` y descomprímelo (clic derecho → *Extraer todo*).
2. Abre PowerShell en esa carpeta y ejecuta `.\moofetch.exe --version`. Para usarlo desde cualquier lugar, mueve `moofetch.exe` a una carpeta de tu `PATH` (o agrega su carpeta a la variable `Path` en *Configuración → Sistema → Acerca de → Configuración avanzada del sistema → Variables de entorno*).

Si SmartScreen avisa, elige *Más información → Ejecutar de todas formas* (el binario no está firmado).

### Linux

```bash
curl -L -o MooFetch-Linux.tar.gz \
  https://github.com/sebasinmas/MooFetch/releases/latest/download/MooFetch-Linux.tar.gz
tar -xzf MooFetch-Linux.tar.gz moofetch
sudo mv moofetch /usr/local/bin/

moofetch --version
```

### Mac

Un solo archivo sirve para Mac Intel y para chip Apple (M1, M2, M3, M4…).

```bash
curl -L -o MooFetch-Mac.zip \
  https://github.com/sebasinmas/MooFetch/releases/latest/download/MooFetch-Mac.zip
unzip MooFetch-Mac.zip moofetch
chmod +x moofetch
sudo mv moofetch /usr/local/bin/

moofetch --version
```

> Si descargaste el archivo con el navegador y el sistema bloquea el binario, ejecuta `xattr -d com.apple.quarantine moofetch`. Los binarios no están firmados.

<details>
<summary><b>Verificar la descarga (opcional)</b></summary>

Cada release incluye `checksums.txt` con el SHA-256 de los tres archivos (`MooFetch-Windows.zip`, `MooFetch-Linux.tar.gz` y `MooFetch-Mac.zip`). Descárgalo junto al tuyo, en la misma carpeta:

```bash
sha256sum -c checksums.txt --ignore-missing     # Linux
shasum -a 256 -c checksums.txt --ignore-missing # macOS
```

</details>

¿Prefieres compilarlo tú? Ver [Para desarrolladores](#-para-desarrolladores).

---

## 🚀 Cómo usarlo

1. **Inicia sesión** en tu campus virtual con tu navegador de siempre (Chrome, Edge, Firefox…).
2. **Ejecuta** `moofetch`:

   ```bash
   moofetch -o ./mi-curso
   ```

3. **Sigue el asistente**, una decisión por pantalla:
   1. **Campus:** eliges tu universidad (por ejemplo UFRO) o «Otra…» y escribes tu dominio. Con `--uni ufro` o `--domain campus.miuniversidad.cl` esta pantalla se omite.
   2. **Cookie:** aceptas que MooFetch la busque en tu navegador. Si no la encuentra (por ejemplo, porque aún no iniciaste sesión), te lo dice y puedes pegarla a mano.
   3. **Curso:** pegas la URL de tu curso o de una sección (debe ser de tu campus).
   4. **Selección:** ves los **nombres** de los recursos, todos marcados. `Espacio` desmarca, `Ctrl+A` alterna todos, `/` filtra y `Enter` confirma.
   5. **Descarga:** barras de avance por archivo, en paralelo, y un resumen al final.

Todo queda en la carpeta que indicaste con `-o`, lista para arrastrarla a NotebookLM. Si dejas la URL del curso vacía, o la lectura falla, también puedes **pegar los enlaces a mano** (uno por línea).

<details>
<summary><b>Cookie automática: navegadores y notas</b></summary>

MooFetch busca la cookie de **tu campus** (solo ese dominio) en Chrome, Chromium, Edge, Opera, Brave y Firefox (y Safari en macOS). Sin el asistente puedes indicar el campus con banderas:

```bash
moofetch run --uni ufro -f urls.txt
moofetch run --domain campus.miuniversidad.cl -f urls.txt
```

- Si la búsqueda falla, cierra y vuelve a abrir el navegador: a veces bloquea sus archivos mientras está abierto.
- **Firefox:** `MoodleSession` es una cookie de sesión, así que Firefox la guarda en su copia de la sesión y no en `cookies.sqlite`. MooFetch también lee esa copia, incluso en la carpeta nueva `~/.config/mozilla/firefox`. Mantén Firefox abierto con tu sesión iniciada.
- En Linux, el navegador puede pedir acceso a tu llavero de contraseñas para leer la cookie.
- Orden de búsqueda sin asistente: `--cookie`, luego el navegador (`--uni`/`--domain`), luego `MOODLE_SESSION`.
- La verificación manual en cada navegador sigue en curso (ver [Roadmap](#-roadmap)); detalles técnicos en el [ADR 003](docs/adr/003-deteccion-automatica-de-token.md).

</details>

<details>
<summary><b>Cookie a mano (10 segundos)</b></summary>

1. Inicia sesión en tu campus con el navegador.
2. Presiona `F12`, ve a la pestaña **Red** (*Network*) y recarga con `F5`.
3. Pulsa cualquier recurso y localiza la cabecera `Cookie` en **Cabeceras de Solicitud**.
4. Copia el valor de tu sesión, por ejemplo:
   ```text
   MoodleSession=tu_token_aqui_123456
   ```
   *(También en **Almacenamiento / Application** → **Cookies**.)*

</details>

<details>
<summary><b>Probarlo sin cuenta ni red (<code>--demo</code>)</b></summary>

```bash
moofetch --demo -o /tmp/moofetch-demo
```

Recorre el asistente completo con una cookie y un curso simulados, y descarga archivos de prueba. Es lo que muestra el video de arriba.

</details>

---

## 🔧 Scripts y pipelines

MooFetch es un ciudadano de primera clase en el ecosistema POSIX: detecta si la entrada o la salida son una tubería y cambia a modo headless solo, con el progreso en **`stderr`** para dejar **`stdout`** limpio.

```bash
# Listar los recursos de un curso (un enlace por línea) y descargarlos
moofetch scan "https://campusvirtual.ufro.cl/course/view.php?id=123" --uni ufro > urls.txt
moofetch run -f urls.txt --uni ufro -o ./apuntes

# URLs desde un archivo o tubería, con la cookie por parámetro o variable de entorno
cat urls.txt | moofetch run -k "MoodleSession=abc123xyz" --headless
export MOODLE_SESSION="MoodleSession=abc123xyz"
cat urls.txt | moofetch run -o ./apuntes_semestre -c 8

# Filtrar con grep y descargar en paralelo
grep "pdf" historial_campus.txt > urls_pdf.txt
moofetch run -k "$MOODLE_SESSION" -f urls_pdf.txt --concurrency 6
```

`scan` pide las páginas de a una, con una pausa entre ellas (`--delay`, 1,5 s por defecto), respeta `Retry-After`, no pide más de `--max-pages` (30) y se detiene si tu sesión expiró. Los títulos van a `stderr`.

### Códigos de salida

Alineados con `sysexits.h`, para que scripts y CI/CD reaccionen con precisión:

| Código | Constante | Significado |
| :---: | :--- | :--- |
| **`0`** | `ExitSuccess` | Todas las descargas terminaron bien. |
| **`1`** | `ExitGeneralErr` | Error general: red, disco u otro fallo en el lote. |
| **`2`** | `ExitUsageErr` | Uso incorrecto: bandera desconocida, falta la cookie o no hay URLs. |
| **`77`** | `ExitAuthErr` | Sesión inválida o expirada (`EX_NOPERM`): renueva la cookie. |
| **`130`** | `ExitInterrupted` | Interrumpido por el usuario (`Ctrl+C`), `SIGTERM` o formulario cancelado. |

```bash
moofetch run -k "$MOODLE_SESSION" -f urls.txt --headless
case $? in
  0)   echo "✅ Todas las descargas concluyeron con éxito." ;;
  77)  echo "🔒 La sesión de Moodle expiró. Renueva tu cookie."; exit 1 ;;
  130) echo "⚠️ Descarga abortada por el usuario. No quedaron archivos temporales." ;;
  *)   echo "❌ Error en la ejecución (código $?)." ;;
esac
```

<details>
<summary><b>Referencia de comandos y banderas</b></summary>

`moofetch --help` agrupa las banderas por tema y muestra al final los códigos de salida:

```text
Uso:
  moofetch [flags]
  moofetch <comando> [flags]

Comandos:
  help        Muestra la ayuda de un comando
  run         Descarga un lote de URLs sin asistente interactivo
  scan        Lista los recursos descargables de una página del campus
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

| Grupo | Bandera | Atajo | Por defecto | Descripción |
| :--- | :--- | :---: | :---: | :--- |
| Descarga | `--output` | `-o` | `"."` | Carpeta donde se guardan los archivos. |
| Descarga | `--concurrency` | `-c` | `5` | Descargas simultáneas en el worker pool. |
| Sesión | `--cookie` | `-k` | `""` | Cookie de sesión Moodle (o variable `MOODLE_SESSION`). |
| Sesión | `--uni` | | `""` | Universidad de la lista (ej. `ufro`). Busca la cookie en tu navegador. |
| Sesión | `--domain` | | `""` | Dominio de tu Moodle (ej. `campus.miuniversidad.cl`). Gana sobre `--uni`. |
| Salida | `--headless` | | `false` | Sin interfaz: progreso plano a `stderr`. |
| Salida | `--log` | `-l` | `""` | Archivo de diagnóstico estructurado con `log/slog`. |
| Otros | `--demo` | `-d` | `false` | Asistente y descargas simulados (sin red ni navegador). |
| Otros | `--version` | `-v` | | Imprime la versión del binario. |

**`run`** añade el grupo *Entrada*: `-u/--urls` (URLs separadas por coma) y `-f/--file` (archivo con una URL por línea). Sin ellas, lee las URLs de `stdin` si hay datos por tubería.
**`scan`** añade `--delay` (pausa entre peticiones, 1,5 s) y `--max-pages` (30).

Los alias ocultos `--plain` (= `--headless`) y `--logger` (= `--log`) siguen funcionando para no romper scripts existentes.

**Variables de entorno:** `MOODLE_SESSION` (cookie por defecto si no hay `--cookie`) y `NO_COLOR` (desactiva los colores ANSI).

</details>

---

## 🔒 Privacidad

MooFetch sigue el principio de **mínimo privilegio y cero persistencia de secretos**:

- **Solo en memoria:** tu cookie de sesión (`MoodleSession`) vive en la memoria del proceso mientras se ejecuta. Nunca se guarda en archivos de configuración, base de datos ni historial.
- **Solo tu campus:** al detectar la cookie solo lee la de **ese** dominio, y el escáner solo acepta URLs de tu campus, para que la cookie nunca se envíe a otro sitio.
- **Logs sin secretos:** con `--log debug.txt`, el registrador (`log/slog`) enmascara las cookies:
  ```text
  time=2026-09-04T00:00:00 level=INFO msg="MooFetch inicializado" cookie="MoodleSession=a1b***f9z (len: 32)"
  ```
- **Nombres de archivo seguros:** los nombres que llegan por `Content-Disposition` se sanean contra *path traversal* (`../`).

> **Uso responsable:** esta herramienta sirve para respaldar material de estudio al que ya tienes acceso legítimo. Respeta las políticas de uso de tu institución.

---

## 🧭 Roadmap

El estado detallado de cada funcionalidad (fecha, rama, commit y versión) está en [docs/traceability.md](docs/traceability.md).

- [x] **v2.0.0:** asistente que lee el curso y deja elegir por nombre, `moofetch scan`, una decisión por pantalla con el logo primero, ayuda de la CLI con banderas agrupadas, teclas en español y patchnotes automáticos.
- [x] **v1.1.0:** migración a Cobra, modo headless, escrituras atómicas, Circuit Breaker y códigos POSIX.
- [x] Selección de campus y autodetección de la cookie (Chrome, Chromium, Edge, Opera, Brave y Firefox).
- [ ] **Verificación manual por navegador:** Chrome, Edge, Opera, Firefox y Safari (solo macOS).
- [ ] **Carpetas Moodle anidadas:** `scan` ya expande un nivel; falta la recursión y agrupar por carpeta en la selección.
- [ ] **Extensión de navegador (Chrome MV3):** capturar enlaces y sesión con un clic desde el campus.
- [ ] **Aplicación de escritorio (Wails):** GUI que reutiliza el mismo núcleo.
- [ ] **Más plataformas:** módulos para Canvas LMS y Blackboard Learn.
- [ ] **Gestores de paquetes:** `brew`, `scoop`, `winget` y AUR.

---

## 🧱 Para desarrolladores

### Compilar desde el código

Necesitas Go 1.24 o superior.

```bash
git clone https://github.com/sebasinmas/MooFetch.git
cd MooFetch

# Compilar inyectando versión, commit y fecha (igual que en las releases)
go build -ldflags "-s -w \
  -X main.Version=2.0.0 \
  -X main.Commit=$(git rev-parse --short HEAD 2>/dev/null || echo 'release') \
  -X main.BuildDate=$(date +%Y-%m-%d)" \
  -o bin/moofetch ./cmd/moofetch

./bin/moofetch --version
```

O directamente con `go install github.com/sebasinmas/MooFetch/cmd/moofetch@latest`.

Antes de proponer cambios: `gofmt -l .` (debe salir vacío), `golangci-lint run` y `go test -race ./...`. El flujo de ramas, CI y releases está en [docs/workflow.md](docs/workflow.md).

### Arquitectura

MooFetch es un **microkernel estático**: el kernel orquesta, los plugins descargan y la interfaz solo consume eventos.

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

- 🧩 **Microkernel desacoplado:** los plugins se registran por inyección (`kernel.WithPlugins(...)`), sin funciones `init()` con efectos globales. Añadir una plataforma es implementar `DownloaderPlugin` (ver [docs/architecture.md](docs/architecture.md)).
- 🏎️ **Worker pool acotado:** la concurrencia la limita un semáforo de canales; da igual procesar 5 o 5.000 URLs, nunca habrá goroutines descontroladas.
- 🛡️ **Escrituras atómicas:** cada archivo se baja a `*.moofetch.part` y se renombra al terminar; si se cancela o falla, el residuo se elimina y un archivo previo nunca se borra. Un `.pdf` cuyo cuerpo no empiece con `%PDF-` (por ejemplo, una página de login con HTTP 200) se rechaza.
- ⚡ **Circuit Breaker de sesión:** ante un 401, 403 o redirección a login, el lote completo se aborta de inmediato (*fail-fast*), evitando cientos de peticiones inútiles.
- ⏱️ **Transporte resiliente:** sin timeouts ciegos para archivos pesados, con detector de inactividad de 30 s si el servidor se congela.
- 🎨 **TUI con Charm:** `bubbletea`, `huh` y `lipgloss`; respeta `NO_COLOR`.

Más detalle en [docs/architecture.md](docs/architecture.md) y en las decisiones de [docs/adr/](docs/adr/).

---

## 🤝 Contribuir

¡Las contribuciones son bienvenidas! Si quieres reportar un fallo, proponer mejoras o adaptar MooFetch al campus de tu universidad:

1. Haz un **fork** y crea una rama (`git checkout -b feature/nueva-mejora`).
2. Verifica que pasen los tests, sin condiciones de carrera: `go test -v -race ./...`.
3. Abre un **Pull Request** con un título convencional (`feat: …`, `fix: …`). Las reglas están en [CONTRIBUTING.md](CONTRIBUTING.md): de ese título salen las notas de cada versión.

## 📄 Licencia

Distribuido bajo la licencia **MIT**; consulta el archivo [`LICENSE`](LICENSE).

---

<div align="center">

¿Te ahorró horas de clics? 🌭 [Invítale un completo a SebaSinMas](https://ko-fi.com/sebasinmas)

</div>

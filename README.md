<div align="center">

# ⚡ [NOMBRE_POR_DEFINIR]

**Descargas masivas, concurrentes y ultrarrápidas para Campus Virtual UFRO y cualquier intranet basada en Moodle.**

[![CI](https://github.com/sebasinmas/GoDownloader/actions/workflows/ci.yml/badge.svg)](https://github.com/sebasinmas/GoDownloader/actions)
[![Go Version](https://img.shields.io/badge/Go-1.21+-00ADD8?style=flat&logo=go)](https://golang.org)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)

[Características](#-características) • [Instalación](#-instalación) • [Guía de Uso](#-guía-de-uso) • [Arquitectura](#-arquitectura-para-devs) • [Roadmap](#-roadmap)

</div>

---

## 💡 El Problema

¿Alguna vez has tenido que bajar 30 diapositivas, guías y PDFs uno a uno al final del semestre desde el **Campus Virtual de la UFRO**? Las plataformas Moodle suelen implementar redirecciones de seguridad (`HTTP 303 See Other`) y protecciones de sesión que rompen aceleradores de descarga tradicionales como `wget` o `curl`.

## 🚀 La Solución

**`[NOMBRE_POR_DEFINIR]`** es una herramienta de consola (CLI) moderna y de alto rendimiento escrita en Go.

Aunque nació diseñada y optimizada para la comunidad estudiantil de la **Universidad de La Frontera (UFRO)**, bajo el capó es un cliente agnóstico capaz de operar con **cualquier intranet universitaria que utilice Moodle**. Su motor inyecta directamente tu cookie activa (`MoodleSession`) en las cabeceras HTTP de peticiones concurrentes, resolviendo las redirecciones de seguridad y descargando todo el material de un semestre en segundos.

---

## ✨ Características

- 🏎️ **Concurrencia Extrema en Go**: Descargas paralelas respaldadas por _goroutines_ y controladas por un semáforo de concurrencia que previene sobrecargas en los servidores de tu universidad.
- 🔑 **Bypass de Sesión & Manejo de HTTP 303**: Inyección directa de cookies de sesión para resolver sin fricción las redirecciones de autenticación de Moodle.
- 🎨 **Interfaz de Terminal Interactiva (TUI)**: Experiencia visual moderna y pulida con el ecosistema **Charmbracelet** (`huh`, `bubbletea`, `lipgloss`) con indicadores de progreso en tiempo real y métricas por archivo.
- 🛡️ **Privacidad Absoluta**: Tus credenciales o cookies nunca se guardan en disco, no se registran en logs ni salen de tu máquina; residen estrictamente en la memoria volátil del proceso durante la ejecución.
- 🧩 **Arquitectura Modular (Microkernel Estático)**: Núcleo extensible y de bajo acoplamiento pensado para integrar soporte a otros LMS (como Canvas o Blackboard) sin tocar el motor principal.
- 🏷️ **Saneamiento Inteligente de Archivos**: Decodifica caracteres especiales (`%20` a espacios), respeta cabeceras `Content-Disposition` y protege contra ataques de _path-traversal_.

---

## 📦 Instalación

### Opción 1: Binarios Precompilados (Recomendado)

Descarga la versión más reciente para tu sistema operativo (Windows, Linux, macOS) desde la sección de [Releases](https://github.com/tu-usuario/nombre-repo/releases):

1. Descomprime el archivo descargado.
2. Ejecuta `[NOMBRE_POR_DEFINIR]` directamente desde tu terminal favorita.

### Opción 2: Vía `go install`

Si tienes el entorno de Go instalado (1.21 o superior):

```bash
go install github.com/tu-usuario/nombre-repo/cmd/downloader@latest
```

### Opción 3: Compilar desde el código fuente

```bash
# Clonar el repositorio
git clone https://github.com/tu-usuario/nombre-repo.git
cd nombre-repo

# Compilar binario (con inyección de versión opcional)
go build -ldflags "-X main.Version=1.1.0 -X main.Commit=$(git rev-parse --short HEAD 2>/dev/null || echo dev) -X main.BuildDate=$(date +%Y-%m-%d)" -o bin/godownloader ./cmd/downloader

# Consultar versión
./bin/godownloader --version

# Ejecutar
./bin/godownloader
```

---

## 📖 Guía de Uso

`[NOMBRE_POR_DEFINIR]` utiliza un asistente interactivo en terminal de 2 simples pasos:

```text
  PASO 1: Ingresa tu cookie de sesión
  PASO 2: Pega la lista de enlaces
  LISTO:  Descarga concurrente con barra de progreso
```

### Paso 1: Obtener tu cookie `MoodleSession` (¡Solo toma 10 segundos!)

1. Abre tu navegador e inicia sesión en el **Campus Virtual UFRO** (o el Moodle de tu universidad).
2. Presiona `F12` para abrir las **Herramientas de Desarrollador** y haz clic en la pestaña **Red** (_Network_).
3. Recarga la página (`F5`), selecciona cualquier petición que aparezca en la lista y busca en la sección **Cabeceras de Solicitud** (_Request Headers_) la cabecera `Cookie`.
4. Copia el valor que comienza por:
   ```text
   MoodleSession=tu_token_aqui_123456
   ```
   _(También puedes encontrarlo directamente en la pestaña **Almacenamiento/Application** -> **Cookies**)._

### Paso 2: Ejecutar y Descargar

1. Inicia la aplicación en tu consola:
   ```bash
   [NOMBRE_POR_DEFINIR]
   ```
2. **Pega tu cookie** cuando el asistente interactivo te lo solicite.
3. **Pega los enlaces** de los archivos/PDFs que necesitas (uno por línea; puedes copiar varios de una sola vez).
4. Presiona `Esc` + `Enter` para confirmar y observa cómo se descargan en paralelo.

### Flags y Parámetros Opcionales

Puedes personalizar el comportamiento del programa mediante banderas de línea de comandos:

```bash
# Definir una carpeta de destino personalizada
[NOMBRE_POR_DEFINIR] -output ./mis_apuntes

# Ajustar el número de descargas simultáneas (por defecto: 5)
[NOMBRE_POR_DEFINIR] -concurrency 8

# Activar modo de registro/depuración en archivo
[NOMBRE_POR_DEFINIR] -logger debug.txt
```

---

## 🏗️ Arquitectura para Devs

El proyecto está diseñado bajo el patrón **Modular Monolith con Microkernel Estático**.

```text
[NOMBRE_POR_DEFINIR] (Kernel)
       ├── Registry (Static init() registration)
       ├── Concurrency Dispatcher (Goroutines + Semaphore)
       └── Plugins Interface (DownloaderPlugin)
                 ├── Moodle / UFRO Plugin (Incluido)
                 └── [Futuro] Canvas / Blackboard Plugins
```

- **Zero-Runtime Overhead**: Los plugins se autoregistran en memoria durante el tiempo de inicio (`init()`) implementando la interfaz `kernel.DownloaderPlugin`, sin requerir CGO ni librerías dinámicas (`.so`).
- **Seguridad en Concurrencia**: Cobertura de pruebas unitarias al 100% evaluadas con detección de condiciones de carrera (`go test -race`).

---

## 🗺️ Roadmap

- [ ] **Desarrollo de GUI (Interfaz Gráfica de Usuario) multiplataforma** _(Hito prioritario en desarrollo: soporte nativo para Windows, Linux y macOS)_.
- [ ] Auto-detección opcional de sesión mediante integración con el navegador local.
- [ ] Plugin oficial para plataformas **Canvas LMS** y **Blackboard**.
- [ ] Detección y descarga automática de carpetas completas de cursos Moodle (`mod/folder`).
- [ ] Empaquetado oficial en gestores de dependencias comunitarios (`brew`, `scoop`, `winget`, `aur`).

---

## 🤝 Contribución

¡Las contribuciones son bienvenidas! Ya sea reportando un bug, sugiriendo una mejora o añadiendo soporte para el sistema de tu universidad:

1. Haz un Fork del proyecto.
2. Crea tu rama de características (`git checkout -b feature/mi-nueva-funcionalidad`).
3. Asegúrate de pasar los tests y linters (`go test -race ./... && golangci-lint run`).
4. Haz Commit de tus cambios (`git commit -m 'feat: soporte para nueva funcionalidad'`).
5. Haz Push a la rama (`git push origin feature/mi-nueva-funcionalidad`).
6. Abre un **Pull Request**.

---

## 📄 Licencia

Este proyecto está distribuido bajo la licencia **MIT**. Consulta el archivo `LICENSE` para más información.

> **Aviso de Uso Responsable:** Esta herramienta está pensada para fines educativos y personales, facilitando el respaldo de material de estudio que el usuario ya tiene derecho legítimo a acceder. Por favor, respeta las políticas de uso y términos de servicio de tu institución académica.

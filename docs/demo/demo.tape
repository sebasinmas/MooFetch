# ==============================================================================
# MooFetch · demo del asistente interactivo (VHS)
#
# Muestra el flujo completo en modo simulado (--demo): sin red, sin navegador y
# sin cookie real. Todo lo que se ve es lo que hace la app de verdad.
#
# Regenerar el GIF del README (desde la raíz del repositorio):
#   go build -o bin/moofetch ./cmd/moofetch
#   vhs docs/demo/demo.tape
#
# Requiere: vhs, ttyd y ffmpeg. Los tiempos (Sleep) esperan a los pasos
# simulados de la app (logo, inicio de sesión, lectura de cursos, descargas); si cambias esas
# pausas en cmd/moofetch/demo.go, ajusta también esta cinta.
# ==============================================================================

Output docs/assets/demo.gif
# Para un video aparte, descomenta:
# Output docs/assets/demo.mp4

Require bash

Set Shell "bash"
Set FontSize 16
Set Width 1180
Set Height 760
Set Padding 24
Set Framerate 24
Set TypingSpeed 55ms
Set Theme "Catppuccin Mocha"
Set WindowBar Colorful
Set WindowBarSize 36
Set BorderRadius 8
Set Margin 12
Set MarginFill "#1e1e2e"

# --- Preparación (no se graba) ------------------------------------------------
Hide
Type "PS1='$ '" Enter
Type "export PATH=$PWD/bin:$PATH NO_COLOR=" Enter
Type "rm -rf /tmp/moofetch-demo" Enter
Type "clear" Enter
Show

# --- 1. Lanzar la app en modo demo --------------------------------------------
Sleep 600ms
Type "moofetch --demo -o /tmp/moofetch-demo"
Sleep 700ms
Enter

# --- 2. Logo y elección de campus (una decisión por pantalla) -----------------
# El logo dura ~1,2 s; el resto es tiempo para leer la pantalla de campus.
Sleep 3800ms
Enter

# --- 3. ¿Cómo quieres entrar? Usuario y contraseña viene preseleccionado ------
Sleep 3s
Enter

# --- 4. Usuario y contraseña del campus (la contraseña va enmascarada) --------
Sleep 2s
Type "estudiante.demo"
Sleep 500ms
Enter
Sleep 600ms
Type "contraseña-demo"
Sleep 1s
Enter

# --- 5. Inicio de sesión y lista de cursos (ninguno viene marcado) ------------
Sleep 4s
Space
Sleep 500ms
Down 1
Sleep 300ms
Space
Sleep 2s
Enter

# --- 6. Secciones del primer curso: todo marcado -------------------------------
Sleep 3500ms
Down 2
Sleep 300ms
Space
Sleep 2s
Enter

# --- 7. Archivos del primer curso: todo marcado, desmarcamos uno --------------
Sleep 3s
Down 3
Sleep 300ms
Space
Sleep 2s
Enter

# --- 8. Segundo curso: secciones y archivos tal cual --------------------------
Sleep 3s
Enter
Sleep 3s
Enter

# --- 9. Descargas concurrentes y resumen --------------------------------------
Sleep 6s

Enter
Sleep 1500ms

# --- 10. Los archivos quedaron ordenados por curso y sección ------------------
Type "clear"
Enter
Type "cd /tmp/moofetch-demo && find . -type f | sort"
Enter
Sleep 5s

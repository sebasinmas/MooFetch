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
# simulados de la app (logo, lectura de la página, descargas); si cambias esas
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

# --- 3. Consentimiento para detectar la cookie ---------------------------------
Sleep 3s
Enter

# --- 4. Cookie "detectada" y URL del curso ------------------------------------
Sleep 2s
Type "https://campusvirtual.ufro.cl/course/view.php?id=4821"
Sleep 1s
Enter

# --- 5. Lectura de la página (spinner ~1,8 s) y lista de recursos -------------
Sleep 3500ms

# --- 6. Elegir qué descargar: todo viene marcado ------------------------------
Down 3
Sleep 400ms
Space
Sleep 600ms
Down 4
Sleep 400ms
Space
Sleep 2s
Enter

# --- 7. Descargas concurrentes y resumen --------------------------------------
Sleep 6s

Enter
Sleep 1500ms

# --- 8. Los archivos quedaron en disco -----------------------------------------
Type "ls /tmp/moofetch-demo"
Enter
Sleep 3s

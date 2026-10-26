#!/usr/bin/env bash
# build-windows-test.sh — genera en dist-test/ todo lo necesario para probar
# MooFetch en un PC con Windows SIN tener Go instalado:
#   - moofetch.exe (windows/amd64)
#   - un <paquete>.test.exe por cada paquete con tests (más su testdata/)
#   - LEEME.txt con las instrucciones de PowerShell
#
# Uso:  bash scripts/build-windows-test.sh        (desde cualquier directorio)
#       bash scripts/build-windows-test.sh --help
set -euo pipefail

usage() {
  echo "Uso: bash scripts/build-windows-test.sh   # compila dist-test/ para probar en Windows (ver docs/workflow.md)"
}

case "${1:-}" in
  -h | --help)
    usage
    exit 0
    ;;
  "") ;;
  *)
    usage >&2
    exit 2
    ;;
esac

# Raíz del repo: git si está disponible; si no, el directorio padre del script.
if ! ROOT="$(git rev-parse --show-toplevel 2>/dev/null)"; then
  ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
fi
cd "$ROOT"

OUT="dist-test"
MODULE="$(go list -m)"

SHA="$(git rev-parse --short HEAD 2>/dev/null || true)"
FULL_SHA="$(git rev-parse HEAD 2>/dev/null || true)"
SHA="${SHA:-local}"
FULL_SHA="${FULL_SHA:-local}"
DATE="$(date -u +%Y-%m-%d)"

export GOOS=windows GOARCH=amd64 CGO_ENABLED=0

rm -rf "$OUT"
mkdir -p "$OUT"

echo ">> Compilando $OUT/moofetch.exe (windows/amd64, versión windows-test-$SHA)"
go build -trimpath \
  -ldflags "-s -w -X main.Version=windows-test-$SHA -X main.Commit=$FULL_SHA -X main.BuildDate=$DATE" \
  -o "$OUT/moofetch.exe" ./cmd/moofetch

# Con GOOS=windows exportado, `go list` ya considera los archivos *_windows_test.go
# y los que llevan //go:build windows (que en Linux no aparecerían).
mapfile -t PKGS < <(go list -f '{{if or .TestGoFiles .XTestGoFiles}}{{.ImportPath}}{{end}}' ./...)

if [ "${#PKGS[@]}" -eq 0 ]; then
  echo "ERROR: no se encontró ningún paquete con tests" >&2
  exit 1
fi

for pkg in "${PKGS[@]}"; do
  rel="${pkg#"$MODULE"}"
  rel="${rel#/}"
  if [ -z "$rel" ]; then
    rel="."
    name="$(basename "$MODULE")"
  else
    name="$(basename "$rel")"
  fi
  dest="$OUT/$rel"
  mkdir -p "$dest"

  echo ">> Compilando tests de $pkg -> $dest/$name.test.exe"
  go test -c -trimpath -o "$dest/$name.test.exe" "$pkg"

  # Los tests se ejecutan con el directorio del paquete como cwd: llevar testdata/ al lado.
  if [ -d "$rel/testdata" ]; then
    cp -R "$rel/testdata" "$dest/testdata"
  fi
done

cat > "$OUT/LEEME.txt" <<LEEME
MooFetch - prueba en Windows (build windows-test-$SHA, $DATE)
================================================================

No necesitas Go instalado: todo está precompilado.

1. Copia la carpeta dist-test completa a un directorio del PC con Windows
   (por ejemplo C:\\moofetch-test). Mantén la estructura de subcarpetas: los
   tests usan sus carpetas testdata\\ de forma relativa.

2. Abre PowerShell y entra a esa carpeta:

     cd C:\\moofetch-test

   Si Windows bloquea los .exe copiados (SmartScreen / "Mark of the Web"):

     Get-ChildItem -Recurse | Unblock-File

3. Comprueba la versión del binario:

     .\\moofetch.exe --version

4. Diagnóstico de la detección de cookie (prueba con el navegador abierto y
   también cerrado; ver docs/workflow.md):

     .\\moofetch.exe doctor --uni ufro

   "doctor" imprime una tabla con el estado de cada navegador y NO muestra
   valores de cookies. Pegar su salida completa es seguro.

5. Suite de tests completa (cada .test.exe se ejecuta desde su propia carpeta):

     Get-ChildItem -Recurse -Filter *.test.exe | ForEach-Object { Push-Location \$_.DirectoryName; & \$_.FullName -test.v; Pop-Location }

   Para un solo paquete:

     cd internal\\auth
     .\\auth.test.exe -test.v

   Cómo leer el resultado:
   - Cada test imprime "--- PASS: Nombre" o "--- FAIL: Nombre"; al final de
     cada paquete aparece "PASS" o "FAIL".
   - "--- SKIP" es normal (tests que no aplican en este equipo).
   - Para ver solo los fallos de un paquete:
       .\\auth.test.exe -test.v | Select-String -Pattern '^--- FAIL|^FAIL|panic'
   - Para repetir un test concreto:
       .\\auth.test.exe -test.v -test.run NombreDelTest

6. Qué reportar de vuelta:
   - La salida de ".\\moofetch.exe --version".
   - La tabla completa de ".\\moofetch.exe doctor --uni ufro".
   - Los nombres de los tests que fallen (líneas "--- FAIL: ...") con su
     mensaje de error y el paquete al que pertenecen.
   - Versión de Windows, navegadores instalados (Chrome / Edge / Firefox) y
     si estaban abiertos durante la prueba.

No pegues cookies, tokens ni contraseñas en el reporte.
LEEME

echo
echo "== Resumen de $OUT/ =="
(
  cd "$OUT"
  find . -type f | LC_ALL=C sort | while IFS= read -r f; do
    printf '%12s bytes  %s\n' "$(wc -c < "$f" | tr -d ' ')" "$f"
  done
)
echo
echo "Listo. Copia la carpeta $OUT/ al PC con Windows y sigue $OUT/LEEME.txt"

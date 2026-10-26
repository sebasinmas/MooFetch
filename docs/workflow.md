# Flujo de trabajo de MooFetch

## 1. Compilar y probar en local

```bash
# Binario de desarrollo (versión "dev")
go build -o bin/moofetch ./cmd/moofetch
./bin/moofetch run --demo -o /tmp/out        # sin red, usa el plugin demo

# Binario con metadatos de versión (igual que en release)
go build -ldflags "-s -w \
  -X main.Version=0.0.0-local \
  -X main.Commit=$(git rev-parse --short HEAD) \
  -X main.BuildDate=$(date +%Y-%m-%d)" \
  -o bin/moofetch ./cmd/moofetch
./bin/moofetch --version

# Otra plataforma (los releases usan CGO_ENABLED=0)
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -o bin/moofetch.exe ./cmd/moofetch
```

Demo para el README (`docs/assets/demo.gif`), grabado con [VHS](https://github.com/charmbracelet/vhs) (requiere `vhs`, `ttyd` y `ffmpeg`):

```bash
go build -o bin/moofetch ./cmd/moofetch
vhs docs/demo/demo.tape      # regenera docs/assets/demo.gif (~35 s, ~600 KB)
```

La cinta usa `moofetch --demo` (cookie y escaneo simulados, ver `cmd/moofetch/demo.go`); sus `Sleep` esperan a esos pasos, así que si cambias las pausas simuladas o el orden de las pantallas, ajusta también `docs/demo/demo.tape`.

Calidad antes de subir (lo mismo que valida CI, más el linter):

```bash
gofmt -l .                     # debe imprimir nada
golangci-lint run              # config en .golangci.yml
go test -race ./...
```

## 2. CI (`.github/workflows/`)
Se dispara en **pull requests** hacia `main` y `develop`, y en los **push a `main`** (así el badge de CI del README refleja el estado real de `main`). Jobs de `ci.yml`:

- `lint`: formato con `gofmt`.
- `golangci-lint`: análisis estático con la config de `.golangci.yml` (formato v2; la acción es `golangci-lint-action@v7`). Ya corre en CI; cierra el pendiente TR-015 de [traceability.md](traceability.md) una vez fusionado.
- `traceability`: `go run scripts/gen-traceability.go -check` (la tabla de `docs/traceability.md` debe estar al día).
- `goreleaser-check`: `goreleaser check` sobre `.goreleaser.yaml` (no publica nada).
- `test`: tests con `-race` + cobertura.
- `build-dry-run`: compilación cruzada de prueba (linux/amd64, windows/amd64, darwin/amd64 y darwin/arm64) más `go vet` para Windows; espera a `lint`, `golangci-lint` y `test`.

Además `pr-title.yml` valida que el **título de la PR** sea un Conventional Commit (ver sección 5): con squash-merge ese título se convierte en la línea que aparece en las notas de la Release. Usa `pull_request` con permisos de solo lectura y un chequeo en bash, sin acciones de terceros.

## 3. Release con GoReleaser
`.github/workflows/release.yml` se dispara al empujar un tag `v*`. Hace checkout con `fetch-depth: 0` (GoReleaser necesita los tags anteriores para generar el changelog), toma la versión de Go de `go.mod` y ejecuta `goreleaser release --clean` (pin `~> v2`, igual que `version: 2` de `.goreleaser.yaml`). `.goreleaser.yaml` está dividido en secciones comentadas: proyecto, hooks, builds, archives, checksum, snapshot, changelog y release.

- Compila `cmd/moofetch` con `CGO_ENABLED=0` para linux/amd64, windows/amd64 y macOS (darwin amd64+arm64 fusionados en **un binario universal** con `universal_binaries` y `replace: true`). No se publica arm64 para Linux ni Windows.
- Inyecta versión, commit y fecha con `-X main.Version/Commit/BuildDate` (`{{ .Version }}` sale del tag).
- Empaqueta con nombres fijos, sin versión (para el enlace estable `releases/latest/download/<archivo>`): `MooFetch-Windows.zip`, `MooFetch-Linux.tar.gz` y `MooFetch-Mac.zip`. Cada uno contiene el binario, `README.md` y `LICENSE`. Genera `checksums.txt` (sha256). El README no hay que editarlo en cada release.
- Crea la Release en GitHub (`prerelease: auto`: `-rc1`/`-beta1` quedan como prerelease). El cuerpo lleva un encabezado con cómo verificar los checksums (`sha256sum -c checksums.txt --ignore-missing`), el changelog agrupado, y un pie con el enlace de comparación entre tags y a la trazabilidad.
- Changelog (`use: github`, agrega PR y autor): grupos ⚠️ cambios incompatibles (`!`), `feat`, `fix`, `security`, `perf`, `refactor`, `deps`/`build` y «Otros cambios». Se ocultan `docs`, `test`, `chore`, `ci`, `style` y los merges. La tabla tipo → sección está en [CONTRIBUTING.md](../CONTRIBUTING.md).
- `before.hooks` ejecuta `go mod tidy`.

Publicar una versión:

```bash
git checkout main && git pull
git tag -a v1.2.0 -m "MooFetch v1.2.0"
git push origin v1.2.0        # dispara el release
```

Probar el release sin publicar (requiere instalar goreleaser: `go install github.com/goreleaser/goreleaser/v2@latest`):

```bash
goreleaser check                            # valida .goreleaser.yaml (también corre en CI)
goreleaser release --snapshot --clean --skip=publish   # construye todo en ./dist sin tag ni publicación (NO genera changelog)
ls dist                                     # MooFetch-Linux.tar.gz  MooFetch-Mac.zip  MooFetch-Windows.zip  checksums.txt (+ carpetas intermedias)
./dist/moofetch_linux_amd64_v1/moofetch --version
file dist/moofetch_darwin_all/moofetch      # Mach-O universal binary with 2 architectures
```

Para previsualizar las **notas** hace falta un tag local (`goreleaser release --clean --skip=publish,validate`, luego `dist/CHANGELOG.md`); detalle en [CONTRIBUTING.md](../CONTRIBUTING.md). El patchnote por item de trazabilidad sale de `go run scripts/gen-traceability.go -notes vX.Y.Z`.

## 4. Flujo de ramas y trazabilidad
Flujo: `feature/*` (o `fix/*`) → `develop` → `main`. Cada paso entra por PR (así corre CI); los tags de versión se crean desde `main`. Se recomienda **squash-merge**: un PR = un commit = una línea en las notas.

- Una rama sin fusionar se documenta como **en proceso** en su item `TR-` hasta que entra a `develop`.
- **Regla:** todo cambio que se fusione debe añadir o actualizar su item `TR-` en [`traceability.json`](traceability.json) y regenerar la tabla con `go run scripts/gen-traceability.go` (ver [traceability.md](traceability.md), que también explica cómo generar patchnotes). CI lo verifica con `-check`.
- La plantilla de PR (`.github/pull_request_template.md`) pide el tipo de cambio, el item `TR-` afectado y una nota de versión de una línea.

### Probar en Windows antes de un tag
**Por qué:** por decisión del proyecto **no hay jobs de Windows ni macOS en GitHub Actions** (para no consumir los límites de minutos). Desarrollamos en Linux, así que la verificación en Windows real es manual y se hace **antes de cada tag de versión**. En CI solo se comprueba que todo *compila* para Windows: el job `build-dry-run` compila `cmd/moofetch` para linux/amd64, windows/amd64, darwin/amd64 y darwin/arm64 y ejecuta `GOOS=windows GOARCH=amd64 go vet ./...` (`go vet` también compila los tests, incluidos `*_windows_test.go` y los `//go:build windows`), por lo que los errores de compilación o de build tags en Windows se detectan sin runner de Windows. Lo que CI **no** puede detectar es el comportamiento en ejecución (archivos bloqueados por el navegador, cifrado de cookies, rutas, etc.): para eso sirve esta prueba.

**Pasos** (en Linux, desde cualquier directorio del repo, en el commit que se va a taguear):

```bash
bash scripts/build-windows-test.sh     # genera dist-test/ (ignorada por git)
```

1. El script compila `dist-test/moofetch.exe` (windows/amd64, versión `windows-test-<sha>`) y, por cada paquete con tests (también los que solo existen en Windows), un `<paquete>.test.exe` en su subcarpeta (p. ej. `dist-test/internal/auth/auth.test.exe`) junto con su `testdata/`. Detecta solos los `*_windows_test.go` nuevos. No hace falta Go en el PC con Windows.
2. Copia **toda** la carpeta `dist-test/` al PC con Windows (pendrive, red, etc.) conservando la estructura, y sigue `dist-test/LEEME.txt`. En PowerShell:

   ```powershell
   .\moofetch.exe --version
   .\moofetch.exe doctor --uni ufro
   Get-ChildItem -Recurse -Filter *.test.exe | ForEach-Object { Push-Location $_.DirectoryName; & $_.FullName -test.v; Pop-Location }
   ```

3. **Pantallas (TR-040):** ejecuta `.\moofetch.exe` (login por API) en **cmd**, **PowerShell** y **Windows Terminal**; sube y baja con las flechas por las listas de cursos, secciones y archivos y comprueba que no queden restos de texto. Repite con `$env:MOOFETCH_ALTSCREEN="0"` y `$env:MOOFETCH_ASCII="1"` para saber qué combinación lo arregla (la línea `Terminal:` de `doctor` dice el modo detectado). Con un curso de carpetas anidadas y nombres largos (TR-041) comprueba que todo se descargue.
4. **Qué reportar:** la tabla completa de `doctor` (no muestra valores de cookies, es seguro pegarla), los nombres de los tests que fallen (`--- FAIL: ...`) con su paquete y mensaje, y la versión de Windows y navegadores (si estaban abiertos). Nunca pegues cookies ni tokens.

**Regla:** **no se crea ningún tag de versión sin esta verificación manual en Windows** (trazabilidad TR-035). Si algo falla, se corrige y se repite la prueba con un `dist-test/` regenerado antes de taguear.

## 5. Buenas prácticas
- **Título de PR y commits con Conventional Commits**: `<tipo>(<scope>)<!>: <descripción>`, con tipos `feat`, `fix`, `perf`, `refactor`, `docs`, `test`, `chore`, `ci`, `build`, `deps`, `security`, `style`; `!` marca un cambio incompatible. El título alimenta las notas de la Release y lo valida CI (`pr-title.yml`). Ver [CONTRIBUTING.md](../CONTRIBUTING.md).
- Tags semánticos `vMAJOR.MINOR.PATCH`; `-rc1`/`-beta1` se publican como prerelease (`prerelease: auto`).
- Trabajar en ramas, abrir PR y solo taguear desde `main` con CI en verde. El toolchain requerido es Go 1.24+ (ver `go.mod`).
- **Ningún tag de versión sin probar en Windows**: antes de taguear, ejecutar `bash scripts/build-windows-test.sh`, probar `dist-test/` en un PC con Windows y reportar el resultado (ver «Probar en Windows antes de un tag», TR-035). CI no tiene runner de Windows; solo verifica que compile (`GOOS=windows go vet ./...`).
- Nunca commitear cookies ni logs de depuración (`moofetch_debug.txt`); los binarios y `dist/` están en `.gitignore`.
- Ejecutar `gofmt -l .`, `golangci-lint run` y `go test -race ./...` antes de la PR; los tres corren también en CI.

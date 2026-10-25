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

Calidad antes de subir (lo mismo que valida CI, más el linter):

```bash
gofmt -l .                     # debe imprimir nada
golangci-lint run              # config en .golangci.yml
go test -race ./...
```

## 2. CI (`.github/workflows/`)
Se dispara en **pull requests** hacia `main` y `develop` (no en push directo). Jobs de `ci.yml`:

- `lint`: formato con `gofmt`.
- `golangci-lint`: análisis estático con la config de `.golangci.yml` (formato v2; la acción es `golangci-lint-action@v7`). Ya corre en CI; cierra el pendiente TR-015 de [traceability.md](traceability.md) una vez fusionado.
- `traceability`: `go run scripts/gen-traceability.go -check` (la tabla de `docs/traceability.md` debe estar al día).
- `goreleaser-check`: `goreleaser check` sobre `.goreleaser.yaml` (no publica nada).
- `test`: tests con `-race` + cobertura.
- `build-dry-run`: compilación cruzada de prueba (linux/amd64, darwin/arm64, windows/amd64); espera a `lint`, `golangci-lint` y `test`.

Además `pr-title.yml` valida que el **título de la PR** sea un Conventional Commit (ver sección 5): con squash-merge ese título se convierte en la línea que aparece en las notas de la Release. Usa `pull_request` con permisos de solo lectura y un chequeo en bash, sin acciones de terceros.

## 3. Release con GoReleaser
`.github/workflows/release.yml` se dispara al empujar un tag `v*`. Hace checkout con `fetch-depth: 0` (GoReleaser necesita los tags anteriores para generar el changelog), toma la versión de Go de `go.mod` y ejecuta `goreleaser release --clean` (pin `~> v2`, igual que `version: 2` de `.goreleaser.yaml`). `.goreleaser.yaml` está dividido en secciones comentadas: proyecto, hooks, builds, archives, checksum, snapshot, changelog y release.

- Compila `cmd/moofetch` para linux/darwin/windows × amd64/arm64 con `CGO_ENABLED=0`.
- Inyecta versión, commit y fecha con `-X main.Version/Commit/BuildDate` (`{{ .Version }}` sale del tag).
- Empaqueta `tar.gz` (zip en Windows) con el README, genera `checksums.txt` (sha256).
- Crea la Release en GitHub (`prerelease: auto`: `-rc1`/`-beta1` quedan como prerelease). El cuerpo lleva un encabezado con cómo verificar los checksums (`sha256sum -c checksums.txt`), el changelog agrupado, y un pie con el enlace de comparación entre tags y a la trazabilidad.
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
goreleaser release --snapshot --clean       # construye todo en ./dist sin tag ni publicación (NO genera changelog)
./dist/moofetch_linux_amd64_v1/moofetch --version
```

Para previsualizar las **notas** hace falta un tag local (`goreleaser release --clean --skip=publish,validate`, luego `dist/CHANGELOG.md`); detalle en [CONTRIBUTING.md](../CONTRIBUTING.md). El patchnote por item de trazabilidad sale de `go run scripts/gen-traceability.go -notes vX.Y.Z`.

## 4. Flujo de ramas y trazabilidad
Flujo: `feature/*` (o `fix/*`) → `develop` → `main`. Cada paso entra por PR (así corre CI); los tags de versión se crean desde `main`. Se recomienda **squash-merge**: un PR = un commit = una línea en las notas.

- Una rama sin fusionar se documenta como **en proceso** en su item `TR-` hasta que entra a `develop`.
- **Regla:** todo cambio que se fusione debe añadir o actualizar su item `TR-` en [`traceability.json`](traceability.json) y regenerar la tabla con `go run scripts/gen-traceability.go` (ver [traceability.md](traceability.md), que también explica cómo generar patchnotes). CI lo verifica con `-check`.
- La plantilla de PR (`.github/pull_request_template.md`) pide el tipo de cambio, el item `TR-` afectado y una nota de versión de una línea.

## 5. Buenas prácticas
- **Título de PR y commits con Conventional Commits**: `<tipo>(<scope>)<!>: <descripción>`, con tipos `feat`, `fix`, `perf`, `refactor`, `docs`, `test`, `chore`, `ci`, `build`, `deps`, `security`, `style`; `!` marca un cambio incompatible. El título alimenta las notas de la Release y lo valida CI (`pr-title.yml`). Ver [CONTRIBUTING.md](../CONTRIBUTING.md).
- Tags semánticos `vMAJOR.MINOR.PATCH`; `-rc1`/`-beta1` se publican como prerelease (`prerelease: auto`).
- Trabajar en ramas, abrir PR y solo taguear desde `main` con CI en verde. El toolchain requerido es Go 1.24+ (ver `go.mod`).
- Nunca commitear cookies ni logs de depuración (`moofetch_debug.txt`); los binarios y `dist/` están en `.gitignore`.
- Ejecutar `gofmt -l .`, `golangci-lint run` y `go test -race ./...` antes de la PR; los tres corren también en CI.

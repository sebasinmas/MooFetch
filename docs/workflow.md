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

## 2. CI (`.github/workflows/ci.yml`)
Se dispara en **pull requests** hacia `main` y `develop` (no en push directo): `gofmt`, tests con `-race` + cobertura, y compilación cruzada de prueba (linux/amd64, darwin/arm64, windows/amd64). Ojo: `golangci-lint` **no** corre en CI todavía; solo local (pendiente: TR-015 en [traceability.md](traceability.md)).

## 3. Release con GoReleaser
`.github/workflows/release.yml` se dispara al empujar un tag `v*`. Ejecuta `goreleaser release --clean` con `.goreleaser.yaml`:

> **Divergencia temporal del pin de GoReleaser:** en `main`, `release.yml` fija `version: "~> v2"` (para coincidir con `version: 2` de `.goreleaser.yaml`); en `develop` sigue en `version: latest`. Divergen hasta que `main` se fusione en `develop`. El pin de `main` (`~> v2`) es el intencionado.

- Compila `cmd/moofetch` para linux/darwin/windows × amd64/arm64 con `CGO_ENABLED=0`.
- Inyecta versión, commit y fecha con `-X main.Version/Commit/BuildDate` (`{{ .Version }}` sale del tag).
- Empaqueta `tar.gz` (zip en Windows) con el README, genera `checksums.txt` (sha256).
- Crea la Release en GitHub con changelog agrupado por prefijo de commit: `feat:`, `fix:`, `perf:`, `refactor:`; se excluyen `docs:`, `test:`, `chore:`.
- `before.hooks` ejecuta `go mod tidy`.

Publicar una versión:

```bash
git checkout main && git pull
git tag -a v1.2.0 -m "MooFetch v1.2.0"
git push origin v1.2.0        # dispara el release
```

Probar el release sin publicar (requiere instalar goreleaser: `go install github.com/goreleaser/goreleaser/v2@latest`):

```bash
goreleaser check                            # valida .goreleaser.yaml
goreleaser release --snapshot --clean       # construye todo en ./dist sin tag ni publicación
./dist/moofetch_linux_amd64_v1/moofetch --version
```

## 4. Flujo de ramas y trazabilidad
Flujo: `fix/*` (o `feature/*`) → `develop` → `main`. Cada paso entra por PR (así corre CI); los tags de versión se crean desde `main`.

- Una rama sin fusionar se documenta como **en proceso** (hoy: `fix/cookie-autodetect`, Firefox; ver [ADR 003](adr/003-deteccion-automatica-de-token.md)).
- **Regla:** todo cambio que se fusione debe añadir o actualizar su item `TR-` en [`traceability.json`](traceability.json) y regenerar la tabla con `go run scripts/gen-traceability.go` (ver [traceability.md](traceability.md), que también explica cómo generar patchnotes).

## 5. Buenas prácticas
- Commits con prefijo convencional (`feat:`, `fix:`, `refactor:`…): alimentan el changelog.
- Tags semánticos `vMAJOR.MINOR.PATCH`; `-rc1`/`-beta1` se publican como prerelease (`prerelease: auto`).
- Trabajar en ramas, abrir PR (así corre CI) y solo taguear desde `main` con CI en verde. El toolchain requerido es Go 1.24+ (ver `go.mod`).
- Nunca commitear cookies ni logs de depuración (`moofetch_debug.txt`); los binarios y `dist/` están en `.gitignore`.
- Ejecutar `golangci-lint run` antes de la PR; añadirlo a `ci.yml` está pendiente (TR-015).

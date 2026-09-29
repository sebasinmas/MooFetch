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
go test -race ./...            # un test de internal/tui necesita /dev/tty (falla sin terminal real)
```

## 2. CI (`.github/workflows/ci.yml`)
Se dispara en **pull requests** hacia `main` y `develop` (no en push directo): `gofmt`, tests con `-race` + cobertura, y compilación cruzada de prueba (linux/amd64, darwin/arm64, windows/amd64). Ojo: `golangci-lint` **no** corre en CI todavía; solo local.

## 3. Release con GoReleaser
`.github/workflows/release.yml` se dispara al empujar un tag `v*`. Ejecuta `goreleaser release --clean` con `.goreleaser.yaml`:

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

## 4. Buenas prácticas
- Commits con prefijo convencional (`feat:`, `fix:`, `refactor:`…): alimentan el changelog.
- Tags semánticos `vMAJOR.MINOR.PATCH`; `-rc1`/`-beta1` se publican como prerelease (`prerelease: auto`).
- Trabajar en ramas, abrir PR (así corre CI) y solo taguear desde `main` con CI en verde.
- Nunca commitear cookies ni logs de depuración (`moofetch_debug.txt`); los binarios y `dist/` están en `.gitignore`.
- Ejecutar `golangci-lint run` antes de la PR; considerar añadirlo a `ci.yml`.

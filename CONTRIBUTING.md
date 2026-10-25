# Contribuir a MooFetch

Guía corta. Detalle de build, CI y release en [docs/workflow.md](docs/workflow.md); arquitectura en [docs/architecture.md](docs/architecture.md).

## Flujo de ramas

```
feature/* | fix/*  ──PR──▶  develop  ──PR──▶  main  ──tag vX.Y.Z──▶  Release
```

- Todo entra por **Pull Request** (así corre CI). No se empuja directo a `develop` ni a `main`.
- Los tags de versión se crean solo desde `main`, con CI en verde.
- Recomendamos **squash-merge**: cada PR queda como un único commit en `develop`/`main`, cuyo mensaje es el **título de la PR**. Así cada PR es exactamente una línea en las notas de versión.

## Título de PR y commits (Conventional Commits)

Formato: `<tipo>(<scope opcional>)<!>: <descripción en minúscula y en imperativo>`

Ejemplos: `feat(scan): listar recursos del curso` · `fix: rechazar HTML servido como PDF` · `feat!: renombrar --out a --output`

CI (`pr-title.yml`) rechaza la PR si el título no cumple el formato. El tipo decide en qué sección de las notas de la Release cae:

| Tipo | Sección en la Release |
| :--- | :--- |
| `tipo!:` (cualquiera con `!`) | ⚠️ Cambios incompatibles (siempre primero) |
| `feat` | 🚀 Nuevas características |
| `fix` | 🐛 Corrección de errores |
| `security` | 🔒 Seguridad |
| `perf` | ⚡ Rendimiento |
| `refactor` | 🔄 Refactorización y arquitectura |
| `deps`, `build` (incl. `build(deps)`) | 📦 Dependencias y build |
| *(sin prefijo válido)* | Otros cambios |
| `docs`, `test`, `chore`, `ci`, `style` | **Ocultos** (no aparecen) |

Los `merge` de GitHub/locales también se ocultan. Un cambio incompatible oculto por tipo (`chore!:`) sí se muestra, en el primer grupo. La regla vive en `changelog` de [`.goreleaser.yaml`](.goreleaser.yaml). Escribe el título pensando en quien lee las notas: describe el efecto para el usuario, no el detalle interno.

## Calidad antes de abrir la PR

```bash
gofmt -l .                                   # no debe imprimir nada
golangci-lint run                            # config en .golangci.yml (también corre en CI)
go test -race ./...
go run scripts/gen-traceability.go -check    # tabla de trazabilidad al día (también corre en CI)
```

## Trazabilidad

Todo cambio que se fusione debe añadir o actualizar su item `TR-` en [`docs/traceability.json`](docs/traceability.json) y regenerar la tabla con `go run scripts/gen-traceability.go` (ver [docs/traceability.md](docs/traceability.md)). La PR indica el item afectado y una «nota de versión» de una línea.

## Previsualizar las notas de versión

Las notas de la Release las genera GoReleaser a partir de los títulos/commits (sección `changelog` de `.goreleaser.yaml`). `goreleaser release --snapshot` **no** genera changelog; para verlo sin publicar, estando sobre un commit con tag local (por ejemplo uno de prueba que luego borras) y sin publicar nada:

```bash
git tag v0.0.0-notes-test                          # tag local de prueba
goreleaser release --clean --skip=publish,validate # construye en ./dist sin publicar
cat dist/CHANGELOG.md
git tag -d v0.0.0-notes-test                       # limpieza
```

Con `use: github` GoReleaser consulta la API para el autor de cada commit; para ello define `GITHUB_TOKEN` (solo lectura basta) o, para una vista rápida, prueba con una copia de la config usando `use: git`.

Aparte, el patchnote **funcional** (por item de trazabilidad, no por commit) sale del JSON:

```bash
go run scripts/gen-traceability.go -notes v1.2.0
```

Validar solo la configuración: `goreleaser check`. Para probar el empaquetado sin publicar: `goreleaser release --snapshot --clean --skip=publish`; `ls dist` debe mostrar exactamente `MooFetch-Windows.zip`, `MooFetch-Linux.tar.gz`, `MooFetch-Mac.zip` y `checksums.txt` (más carpetas intermedias). Esos nombres son fijos (sin versión): no los cambies, porque el README enlaza a `releases/latest/download/<archivo>`.

## Seguridad

Nunca commitees cookies de sesión ni logs de depuración (`moofetch_debug.txt`). Las cookies no se persisten ni se registran sin redactar.

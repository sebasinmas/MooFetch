<!--
TÍTULO DE LA PR (obligatorio, lo valida CI): <tipo>(<scope opcional>)<!>: <descripción>
Ej.: `feat(scan): listar recursos del curso` · `fix: rechazar HTML servido como PDF` · `feat!: renombrar --out`
Con squash-merge este título es la línea que aparece en las notas de versión. Ver CONTRIBUTING.md.
-->

## 📌 Resumen del cambio

<!-- ¿Qué problema resuelve o qué funcionalidad introduce? Breve. -->

## 🎯 Tipo de cambio

<!-- Debe coincidir con el tipo del título de la PR. -->

- [ ] `feat` nueva funcionalidad
- [ ] `fix` corrección de bug
- [ ] `perf` rendimiento
- [ ] `refactor` refactorización
- [ ] `security` seguridad
- [ ] `deps` / `build` dependencias o build
- [ ] `docs` / `test` / `chore` / `ci` / `style` (no aparecen en las notas de versión)
- [ ] **Cambio incompatible** (`!` en el título; describir la migración abajo)

## 🗒️ Nota de versión (una línea para el usuario)

<!-- Qué cambia para quien usa MooFetch, en lenguaje llano. Escribe "n/a" si es interno (docs, test, ci...). -->

>

## 🔗 Trazabilidad

- Item `TR-` afectado (`docs/traceability.json`): <!-- ej. TR-021, o "nuevo" -->
- Issue / ADR relacionado: <!-- ej. Closes #12, ADR 003 -->

## ✅ Checklist

- [ ] `gofmt -l .` no imprime nada
- [ ] `go test -race ./...` pasa
- [ ] `golangci-lint run` sin hallazgos
- [ ] `go run scripts/gen-traceability.go -check` pasa (tabla regenerada si cambió el JSON)
- [ ] Sin versiones fijas en el código (la versión sale del tag y de LDFLAGS)
- [ ] Respeta la arquitectura (`internal/domain` sin dependencias hacia afuera; plugins no importan `kernel` ni `tui`)
- [ ] Docs actualizadas si cambia el uso o el diseño

## 🧪 Cómo probarlo

<!-- Pasos para verificar localmente. Ej.: `go run ./cmd/moofetch run --demo -o /tmp/out`. Adjunta capturas si cambia la TUI. -->

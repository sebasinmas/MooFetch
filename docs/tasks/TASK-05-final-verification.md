# TASK-05: Verificación final
- **Depende de:** TASK-01 a TASK-04

## Checklist
- `gofmt -l .` vacío; `golangci-lint run`; `go test -race ./...`
- Cross-compile con `CGO_ENABLED=0` para linux/darwin/windows (amd64/arm64)
- `moofetch --help` sin `completion`, con marca de vaca; `moofetch run --demo -o /tmp/out`
- Prueba manual de autodetección y de fallback
- Actualizar `CLAUDE.md` y marcar todos los checkboxes en `docs/tasks/README.md`

## 📌 Resumen del Cambio

<!-- Describe de forma concisa el propósito de esta Pull Request. ¿Qué problema resuelve o qué nueva funcionalidad introduce? -->

---

## 🎯 Tipo de Cambio

- [ ] 🚀 Nueva funcionalidad (`feat`)
- [ ] 🐛 Corrección de bug (`fix`)
- [ ] 🔄 Refactorización arquitectónica (`refactor`)
- [ ] ⚡ Optimización de rendimiento (`perf`)
- [ ] 📝 Actualización de documentación (`docs`)
- [ ] 🧪 Pruebas unitarias o de integración (`test`)
- [ ] 🛠️ Tareas de mantenimiento o dependencias (`chore` / `ci`)

---

## 🔗 Issues o ADRs Relacionados

<!-- Enlaza los issues o ADRs correspondientes (ej. Closes #12, Cumple con ADR 002) -->
- Issue / ADR: 

---

## 📋 Checklist de Calidad y Definition of Done (DoD)

Antes de solicitar revisión, marca las casillas que certifiquen el cumplimiento de estándares:

- [ ] **Formato:** Se ejecutó `gofmt -l .` y no existen inconsistencias de espaciado o formato.
- [ ] **Concurrencia:** Los tests se ejecutaron con `-race` (`go test -v -race ./...`) y pasaron con 0 condiciones de carrera.
- [ ] **Compilación:** Se validó la compilación limpia en entornos locales (`go build ./...`).
- [ ] **Sin Hardcoding:** No se agregaron versiones fijas en código fuente; se delega a Git Tags y LDFLAGS.
- [ ] **Arquitectura Limpia:**
  - [ ] No se introdujeron paquetes genéricos prohibidos (`core`, `utils`, `types`, `common`).
  - [ ] Las dependencias internas apuntan al modelo de dominio (`internal/domain`) sin ciclos.
  - [ ] Las interfaces son consumidas donde se definen (*Consumer-Driven Interfaces*).
- [ ] **Documentación:** Se actualizaron los archivos en `docs/` o README si el cambio impacta el uso o diseño.

---

## 🧪 Instrucciones de Prueba y Verificación

<!-- Detalla los pasos para reproducir o verificar los cambios introducidos localmente -->

1. Descargar dependencias: `go mod download`
2. Ejecutar suite de pruebas: `go test -v -race ./...`
3. Probar ejecución CLI:
   ```bash
   go run ./cmd/godownloader --demo
   ```

---

## 📷 Capturas o Evidencias (Opcional)

<!-- Si aplica (ej. mejoras en la TUI Bubble Tea o salidas CLI), adjunta capturas o logs -->

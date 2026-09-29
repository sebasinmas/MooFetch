# ADR 003: Detección Automática del Token de Sesión Moodle

- **Estado:** Aceptado (Accepted)
- **Área:** Autenticación / CLI / TUI

## 1. Contexto
Hoy el usuario debe copiar manualmente la cookie `MoodleSession` desde las DevTools. Es el paso más engorroso del flujo.

## 2. Decisión
- Nuevo paquete `internal/auth` con el puerto `CookieProvider{ Find(ctx, domain) (string, error) }` (definido por el consumidor, ver ADR 002) y un catálogo de universidades (`ufro` → `campusvirtual.ufro.cl`) con opción de dominio personalizado.
- Adaptador concreto con `github.com/browserutils/kooky` que lee la cookie `MoodleSession` del dominio desde Chrome/Chromium/Firefox/Edge locales.
- El TUI pregunta «¿Cuál es tu universidad?» antes del paso de cookie; en modo headless se usan `--uni`/`--domain`.
- Se muestra un mensaje de consentimiento antes de leer los almacenes del navegador.
- El pegado manual y `MOODLE_SESSION` siguen siendo el fallback y no se eliminan.

## 3. Restricciones
- `internal/auth` no importa `kernel` ni `tui`; el cableado vive solo en `cmd/moofetch`.
- La cookie nunca se persiste ni se registra sin ofuscar (`logger.SessionCookie`).
- Las builds de release usan `CGO_ENABLED=0`. Si kooky requiere CGO en alguna plataforma, se aísla con build tags y la detección se degrada al pegado manual.

## 4. Consecuencias
- (+) Elimina el paso manual más propenso a errores.
- (−) Nueva dependencia externa y acceso a datos sensibles del navegador; mitigado con consentimiento explícito y fallback.

# ADR 003: Detección Automática del Token de Sesión Moodle

- **Estado:** Aceptado (Accepted)
- **Área:** Autenticación / CLI / TUI

## 1. Contexto
Hoy el usuario debe copiar manualmente la cookie `MoodleSession` desde las DevTools. Es el paso más engorroso del flujo.

## 2. Decisión
- Nuevo paquete `internal/auth` con el puerto `CookieProvider{ Find(ctx, domain) (string, error) }` (definido por el consumidor, ver ADR 002) y un catálogo de universidades (`ufro` → `campusvirtual.ufro.cl`) con opción de dominio personalizado.
- Adaptador concreto con `github.com/browserutils/kooky` que lee la cookie `MoodleSession` del dominio desde los navegadores locales que kooky soporta (ver matriz en la sección 5). El texto de consentimiento del TUI lista solo Chrome, Chromium, Firefox y Edge (ver sección 6).
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

## 5. Matriz de soporte y verificación por navegador
«Soporte en kooky» significa que `browser/all` registra un lector para ese navegador (`kooky v0.2.10`); **no** significa que MooFetch lo haya probado. Los tests de `internal/auth` usan un proveedor de cookies falso, por lo que **ninguna fila está verificada manualmente** contra el campus real. Seguimiento: [TASK-08](../tasks/TASK-08-verificacion-manual-navegadores.md) y [traceability.md](../traceability.md).

| Navegador | Soporte en kooky | Verificado manualmente | Notas |
| :--- | :---: | :---: | :--- |
| Chrome | Sí | Por implementar (TR-007) | Sin verificar. |
| Edge | Sí | Por implementar (TR-008) | Sin verificar. |
| Opera | Sí | Por implementar (TR-009) | Sin verificar. |
| Safari | Sí, solo macOS | Por implementar (TR-010) | Solo existe en macOS; se necesita un equipo macOS para verificar. |
| Firefox | Sí: `cookies.sqlite` y session store (sección 7) | Por implementar (TR-019) | Fusionado en `develop`; sin verificar manualmente. |
| Brave | Sí | Por implementar | Sin verificar. |

Chromium también está registrado, sin verificar. Como `MoodleSession` es una cookie de sesión (sección 7), queda por comprobar si cada navegador basado en Chromium la deja en su almacén en disco; esa es justamente una de las razones de la verificación manual. Las builds de release usan `CGO_ENABLED=0`; el efecto real en cada plataforma tampoco está verificado.

## 6. Texto de consentimiento
El mensaje de consentimiento (`consentText` en `internal/tui/authstep.go`) lista ahora todos los navegadores que busca kooky: Chrome, Chromium, Edge, Firefox, Opera, Brave y Safari (macOS). Si se limita o amplía la búsqueda, el texto debe mantenerse alineado.

## 7. Limitación Firefox (resuelta, pendiente de verificación manual)
- Moodle emite `MoodleSession` como **cookie de sesión** (sin fecha de expiración). Firefox **no** las guarda en `cookies.sqlite`, que es lo único que lee kooky.
- Mientras el navegador está abierto, esas cookies viven en el respaldo de sesión: `sessionstore-backups/recovery.jsonlz4` (y, como alternativas, `recovery.baklz4` y `previous.jsonlz4`), comprimido en formato mozLz4.
- Moodle permite que cada sitio añada un sufijo al nombre de la cookie (`MoodleSession<sufijo>`), por lo que el filtro acepta el prefijo `MoodleSession` y no solo el nombre exacto. `Detect` devuelve el nombre real de la cookie (`Nombre=valor`).
- El arreglo (lectura del session store de Firefox en `internal/auth/firefox.go`, búsqueda en carpetas de perfil nuevas como `~/.config/mozilla/firefox`, y coincidencia por prefijo) se originó en la rama `fix/cookie-autodetect` (commit `7c20081`) y está fusionado en `develop` (TR-006, [TASK-07](../tasks/TASK-07-firefox-session-store.md)). Falta la verificación manual con una sesión real (TR-019).

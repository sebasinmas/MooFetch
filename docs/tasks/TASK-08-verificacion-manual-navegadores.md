# TASK-08: Verificación manual de la autodetección en Edge, Opera, Safari y Chrome
- **Prioridad:** 🟡 Media · **Depende de:** TASK-04 (y TASK-07 para Firefox)
- **Referencia:** [ADR 003, sección 5](../adr/003-deteccion-automatica-de-token.md), TR-007 a TR-010 en [traceability.md](../traceability.md)

## Contexto
Los tests de `internal/auth` usan un proveedor de cookies falso; ningún navegador se ha verificado contra el campus real. kooky registra Chrome, Edge, Opera y Safari (solo macOS), pero «registrado» no es «probado».

## Pasos (repetir por navegador)
1. Iniciar sesión en `campusvirtual.ufro.cl` en el navegador a probar y dejarlo abierto.
2. Ejecutar `moofetch`, elegir UFRO y aceptar la detección automática.
3. Comprobar que la cookie se detecta (el formulario omite el paso de cookie) y que una descarga real funciona. Probar también con el navegador cerrado.
4. Registrar sistema operativo, versión del navegador y resultado; si falla, anotar el error mostrado.

## Matriz
| Navegador | Item | Estado |
| :--- | :---: | :---: |
| Chrome | TR-007 | [ ] |
| Edge | TR-008 | [ ] |
| Opera | TR-009 | [ ] |
| Safari (solo macOS) | TR-010 | [ ] |

## DoD
- [ ] Cada navegador de la matriz verificado (o documentada su limitación) en el ADR 003.
- [ ] Items TR-007 a TR-010 actualizados en `docs/traceability.json`.
- Nota: Safari solo existe en macOS; requiere un equipo macOS. Brave y Chromium están registrados en kooky pero fuera del alcance de esta tarea.

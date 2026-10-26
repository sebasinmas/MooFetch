# TASK-10: Spike de la API móvil de Moodle (compuerta G-API-0)
- **Estado:** Hecho (2026-10-25) — resultado favorable; diseño en [ADR 006](../adr/006-autenticacion-api-movil.md)
- **Referencia:** TR-036 en [traceability.md](../traceability.md)

## Objetivo
Saber si el campus entrega un token de la API móvil con usuario y contraseña, y si ese token permite listar cursos, secciones y archivos. De eso depende que el login por API sea la entrada por defecto (TR-037) y cómo se obtiene el token si el campus usa SSO.

## Cómo ejecutarlo
```bash
bash scripts/spike-api-movil.sh https://campusvirtual.ufro.cl
```
El script pide usuario y contraseña a tu terminal (la contraseña no se muestra), los envía por HTTPS al campus y los descarta. **No imprime ni guarda el token ni la contraseña.** Solo muestra líneas `RESULTADO` con éxito/fallo, códigos de error y conteos.

## Qué reportar
Copia solo las líneas `RESULTADO` (nunca un token ni tu contraseña).

| Resultado | Qué significa |
| :--- | :--- |
| Token recibido y los pasos 2 a 4 en `OK` | Login directo funciona: la API será la entrada por defecto. |
| `invalidlogin` | El campus probablemente usa SSO y no acepta contraseña directa: el token se obtendrá por otro camino (claves de seguridad del perfil o QR de la app) y se ajusta el diseño. |
| `servicenotavailable`, `webservicesnotenabled` u otro | El servicio móvil no está disponible para tu cuenta: se mantiene la cookie como entrada por defecto. |
| Paso 2 `OK` pero una función `NO DISPONIBLE` | Se ve qué funciones faltan y se decide el alcance. |

Referencia de lo ya comprobado sin credenciales (2026-10-25): `login/token.php` responde JSON en `campusvirtual.ufro.cl`, lo que indica que los servicios web están activos.

## Resultado (2026-10-25, campusvirtual.ufro.cl)
| Paso | Resultado |
| :--- | :--- |
| `login/token.php` (usuario y contraseña) | Token recibido: el login directo funciona (no es SSO puro). |
| `core_webservice_get_site_info` | OK, **Moodle 3.5.4+ (build 20190215)**. |
| `core_enrol_get_users_courses`, `core_course_get_contents` | Disponibles. |
| Cursos del usuario | 10. |
| Primer curso (`core_course_get_contents`) | 5 secciones, 41 módulos, 8 archivos. |

Conclusión: la entrada por API móvil es viable en este campus y pasa a ser la opción por defecto del asistente. Ojo con la versión: Moodle 3.5 es antiguo, las pruebas del cliente deben usar respuestas de esa forma.

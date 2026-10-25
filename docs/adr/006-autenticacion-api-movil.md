# ADR 006: Autenticación y navegación por la API móvil de Moodle

- **Estado:** Aceptado — el spike del dueño (2026-10-25, TR-036) confirmó en `campusvirtual.ufro.cl` (Moodle 3.5.4+, build 2019) que el login directo entrega token y que la API lista cursos, secciones y archivos.
- **Área:** Plugin Moodle / Asistente / Seguridad

## 1. Contexto
Leer la cookie de sesión del navegador falla en Windows: los navegadores mantienen sus archivos en uso y Chrome/Edge cifran las cookies (`v20`) de un modo que `kooky` no puede deshacer (ver TR-029 a TR-031). La API móvil de Moodle (la que usa la app oficial) permite autenticarse con usuario y contraseña, obtener un **token** y consultar cursos y contenidos ya estructurados, sin tocar ningún navegador.

## 2. Decisión
1. **Entrada por defecto del asistente: usuario y contraseña del campus.** La cookie del navegador queda como alternativa (`--auth cookie`) y el pegado manual de enlaces como último recurso.
2. **Flujo de red** (todo por HTTPS; se rechaza `http://`):
   - `POST {campus}/login/token.php` con `username`, `password`, `service=moodle_mobile_app` → `token`. La contraseña se usa una sola vez y se descarta.
   - `core_webservice_get_site_info` → `userid`, versión y funciones disponibles.
   - `core_enrol_get_users_courses(userid)` → cursos del alumno.
   - `core_course_get_contents(courseid)` → secciones, módulos y archivos (`filename`, `fileurl`, `filesize`, `timemodified`, `filepath`). Se consideran los módulos `resource` y `folder` (los archivos de carpeta conservan su subcarpeta).
   - Descarga desde `fileurl` (`webservice/pluginfile.php/...`) con el token como parámetro `token`.
3. **El token nunca forma parte de `Task.URL`.** `domain.Task` gana el campo `Token`; el plugin lo agrega a la petición solo en el momento de hacerla. Así la URL que ven la interfaz, los eventos, `Result.URL` y el log **no contiene secretos por construcción**. Los errores de `net/http` (`*url.Error` incluye la URL de la petición) se limpian antes de salir del plugin.
4. **Almacenamiento:** token y contraseña viven solo en memoria. No se guardan en disco ni se recuerdan entre ejecuciones (recordarlos con el llavero del sistema queda fuera de este ADR). La contraseña se lee con eco desactivado y no pasa por logs, errores ni eventos.
5. **Redacción en logs:** el logger redacta tokens igual que cookies (nuevo tipo `logger.Token` con `slog.LogValuer`); además, cualquier URL registrada pasa por una limpieza que elimina `token=`/`wstoken=`.
6. **Errores de autenticación:** `invalidlogin`/`invalidtoken`/`accessexception` se mapean a `domain.ErrAuthenticationFailed` (exit 77) y activan el circuit breaker si ocurren durante la descarga. Una respuesta JSON con `errorcode` en el cuerpo de una descarga (Moodle responde 200) se reconoce como error y no como «archivo no PDF».
7. **Uso responsable:** las llamadas a la API son secuenciales con la misma pausa y `User-Agent` identificable que `scan`; el contenido de cada curso se pide solo para los cursos elegidos.
8. **Estructura en disco:** `<salida>/<Curso>/<NN - Sección>/<[subcarpeta]/>archivo`. Los nombres de curso y sección pasan por la misma limpieza segura que los archivos (nombres reservados de Windows, caracteres inválidos, longitud). Cada descarga lleva su subcarpeta (`domain.Task.OutputDir`).

## 3. Experiencia en el asistente
Logo → campus → **cómo entrar** (usuario y contraseña [por defecto] / cookie del navegador) → credenciales (usuario y contraseña enmascarada) → **cursos** (multi, sin ninguno marcado para no pedir todos a la vez; al menos uno) → por cada curso elegido: **secciones** (multi, todas marcadas) y **archivos** (multi, todos marcados, por nombre, con filtro) → descarga. Si el login falla se explica y se puede reintentar o cambiar a cookie.

## 4. Contrato entre paquetes
- `internal/plugins/moodle` expone la interfaz `MobileAPI` y los tipos `SiteInfo`, `APICourse`, `APISection`, `APIFile` (`api_types.go`); no importa `kernel` ni `tui`.
- `internal/tui` recibe el comportamiento por inyección (`AuthOptions.Login`, `Courses`, `Contents`), con sus propios tipos, igual que `Detect` y `Scan`; no importa `moodle`.
- `cmd/moofetch` hace de adaptador entre ambos.

## 5. Compatibilidad y riesgos
- El campus probado usa **Moodle 3.5.4+**: las pruebas del cliente deben usar respuestas con la forma de esa versión (por ejemplo `core_enrol_get_users_courses` sin campos nuevos de Moodle 4).
- Hay campus con SSO sin contraseña local o con el servicio móvil desactivado: la cookie sigue disponible y el error se explica.
- Un token es una credencial de larga duración: nunca se imprime ni se escribe (ver punto 3 y 5).

## 6. Fuera de alcance de este ADR
Recordar el token entre ejecuciones; modo no interactivo por API (`run`/`scan` siguen con cookie); sincronización incremental por `timemodified`; otros tipos de módulo (páginas, foros, tareas); campus con SSO puro.

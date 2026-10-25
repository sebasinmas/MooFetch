# ADR 005: Listar recursos de una página del campus desde la terminal (`scan`)

- **Estado:** Aceptado — verificado por el dueño con un curso real del campus el 2026-10-24 (TR-022). Las preguntas de la sección 3 quedan como mejoras posibles.
- **Área:** Plugin Moodle / CLI

## 1. Contexto
Hoy el usuario pega a mano los enlaces de cada recurso. Se quiere extraer los recursos de una página del campus. Una CLI **no puede saber cuál es la pestaña activa** del navegador (solo una extensión lo ve), pero sí puede pedir el HTML de una página que se le indique, enviando la cookie de sesión, y buscar los enlaces útiles (`/mod/resource/view.php?id=`, `/pluginfile.php/`, `/mod/folder/view.php?id=`).

## 2. Decisión
- Añadir el comando `moofetch scan <url-de-página>` y `Plugin.ScanPage` en `internal/plugins/moodle/scan.go`. Escribe un enlace por línea en **stdout** (se canaliza a `moofetch run`) y el diagnóstico en stderr.
- **Uso responsable como requisito:** peticiones estrictamente secuenciales con pausa más jitter (por defecto 1,5 s + hasta 0,5 s), `User-Agent` identificable, respeto de `Retry-After` en 429/503 con reintentos acotados, tope de 30 páginas por ejecución, tope de tamaño de respuesta, y parada inmediata (sin reintentos) si aparece la página de login.
- Las carpetas se expanden un solo nivel.
- El asistente interactivo usa el mismo escaneo (TR-025): pide la URL del curso, valida que el host sea el del campus elegido (la cookie solo se envía allí), muestra los nombres de los recursos con todo marcado y deja el pegado manual como alternativa.
- La cookie nunca se registra; se obtiene igual que en `run` (`-k`, detección en navegador, `MOODLE_SESSION`).

## 3. Mejoras posibles tras la verificación (TR-022)
1. ¿El HTML del curso trae los enlaces sin JavaScript? Se asumió marcado de Moodle 4.x (`a.aalink` con `span.instancename`).
2. ¿`resource/view.php` redirige a `pluginfile.php` o muestra una página intermedia?
3. ¿Hay enlaces `pluginfile` que no son material (avatares, imágenes de curso)? Si los hay, hará falta una lista de componentes permitidos (`mod_resource`, `mod_folder`, `mod_page`…).
4. Secciones colapsadas o paginadas, páginas de tipo URL y otros tipos de actividad.
5. Hoy el tope `--max-pages` solo se registra en el log de depuración; si importa, el llamador debería enterarse (error centinela o indicador).

## 4. Consecuencias y alternativas
- **Resultado de la prueba real:** fue satisfactoria, así que `scan` deja de ser experimental y se documenta en el README.
- **Si no basta** (contenido cargado por JavaScript, o el requisito real es «la pestaña activa»): se adelanta la extensión de navegador (TR-016) con un servidor en loopback: `127.0.0.1`, puerto efímero, token de emparejamiento y validación de `Origin`/`Host`, que entregue la lista de enlaces a `kernel.Dispatch`. Ver `docs/architecture.md` §4.2.
- `plugins/moodle` sigue sin importar `kernel` ni `tui`; la dependencia nueva es `golang.org/x/net/html`.

#!/usr/bin/env bash
# Spike G-API-0: ¿sirve la API móvil de Moodle en tu campus?
#
# Uso:  bash scripts/spike-api-movil.sh https://campusvirtual.ufro.cl
#
# Pide tu usuario y contraseña SOLO a tu terminal (la contraseña no se muestra),
# los envía por HTTPS al sitio que indiques y los descarta. NO imprime ni guarda
# el token ni la contraseña: al final solo ves éxito/fallo, códigos de error y
# conteos. Pega ese resultado en el chat; nunca pegues el token.
set -u
set +o history 2>/dev/null || true

SITE="${1:-}"
case "$SITE" in
  https://*) ;;
  *) echo "Indica la URL del campus con https://, por ejemplo: bash $0 https://campusvirtual.ufro.cl"; exit 2 ;;
esac
SITE="${SITE%/}"
command -v curl >/dev/null || { echo "Falta curl"; exit 2; }
command -v python3 >/dev/null || { echo "Falta python3"; exit 2; }

read -r -p "Usuario del campus: " USER_NAME
read -r -s -p "Contraseña (no se muestra): " PASS
echo

# jget <json> <expresión python sobre d>  -> imprime el resultado (nunca el token)
jget() { printf '%s' "$1" | python3 -c "import sys,json
try:
    d=json.load(sys.stdin)
except Exception:
    print('(respuesta no es JSON)'); sys.exit(0)
$2"; }

echo
echo "1) Pidiendo token a $SITE/login/token.php (service=moodle_mobile_app)..."
RESP=$(curl -sS -m 30 -X POST "$SITE/login/token.php" \
  --data-urlencode "username=$USER_NAME" \
  --data-urlencode "password=$PASS" \
  --data-urlencode "service=moodle_mobile_app")
unset PASS USER_NAME

TOKEN=$(jget "$RESP" "print(d.get('token','') if isinstance(d,dict) else '')")
if [ -z "$TOKEN" ]; then
  echo "   RESULTADO: SIN TOKEN"
  jget "$RESP" "print('   errorcode =', d.get('errorcode')); print('   mensaje   =', d.get('error') or d.get('message'))"
  echo "   Si dice invalidlogin, el campus puede usar SSO (Microsoft/CAS) y no aceptar contraseña directa."
  exit 1
fi
echo "   RESULTADO: token recibido (no se muestra)"

ws() { # ws <función> [clave=valor ...]  -> JSON en stdout
  local fn="$1"; shift
  local args=(--data-urlencode "wstoken=$TOKEN" --data-urlencode "wsfunction=$fn" --data-urlencode "moodlewsrestformat=json")
  for kv in "$@"; do args+=(--data-urlencode "$kv"); done
  curl -sS -m 40 -X POST "$SITE/webservice/rest/server.php" "${args[@]}"
}

echo "2) core_webservice_get_site_info..."
INFO=$(ws core_webservice_get_site_info)
jget "$INFO" "
if 'exception' in d or 'errorcode' in d:
    print('   RESULTADO: FALLÓ  errorcode =', d.get('errorcode'))
else:
    names={f.get('name') for f in d.get('functions',[])}
    print('   RESULTADO: OK  Moodle', d.get('release'))
    for fn in ('core_enrol_get_users_courses','core_course_get_contents'):
        print('   función', fn, '->', 'DISPONIBLE' if fn in names else 'NO DISPONIBLE')
"
USERID=$(jget "$INFO" "print(d.get('userid',''))")

echo "3) core_enrol_get_users_courses..."
COURSES=$(ws core_enrol_get_users_courses "userid=$USERID")
jget "$COURSES" "
if isinstance(d,dict):
    print('   RESULTADO: FALLÓ  errorcode =', d.get('errorcode'))
else:
    print('   RESULTADO: OK  cursos =', len(d))
"
CID=$(jget "$COURSES" "print(d[0]['id'] if isinstance(d,list) and d else '')")

if [ -n "$CID" ]; then
  echo "4) core_course_get_contents (primer curso)..."
  CONT=$(ws core_course_get_contents "courseid=$CID")
  jget "$CONT" "
if isinstance(d,dict):
    print('   RESULTADO: FALLÓ  errorcode =', d.get('errorcode'))
else:
    mods=[m for s in d for m in s.get('modules',[])]
    files=[c for m in mods for c in m.get('contents',[]) if c.get('type')=='file']
    print('   RESULTADO: OK  secciones =', len(d), ' módulos =', len(mods), ' archivos =', len(files))
"
fi
unset TOKEN RESP INFO COURSES CONT
echo
echo "Listo. Copia solo las líneas «RESULTADO» al chat."

#!/usr/bin/env bash
# Smoke test E2E: servidor + agente + plan + job + Kopia real (o fake) +
# restore point + restore de ficheros + reinicio persistente + auditoría.
# Uso: ./scripts/smoke.sh
# Con BDR_SMOKE_STORE=postgres el estado vive en PostgreSQL (requiere docker;
# levanta un contenedor efímero y una base por ejecución).
# Requisitos: go, curl, python3/python. Con `kopia` en el PATH se prueba el
# flujo real de repositorio; sin él se usa tools/fake-kopia.

set -euo pipefail

ROOT=$(cd "$(dirname "$0")/.." && pwd)
cd "$ROOT"

PORT=${PORT:-18080}
AGENT_TLS_PORT=${AGENT_TLS_PORT:-18083}
BASE="http://127.0.0.1:$PORT"
ADMIN_TOKEN=${ADMIN_TOKEN:-dev-admin-token}
SMOKE_STORE=${BDR_SMOKE_STORE:-file}
KRS=${BDR_SMOKE_KRS:-no}
KRS_PORT=${BDR_SMOKE_KRS_PORT:-18181}
KOPIA_KIND_ENV=filesystem
KOPIA_SERVER_URL=""
KOPIA_SERVER_FP=""
PG_CONTAINER=${BDR_SMOKE_PG_CONTAINER:-bdr-smoke-pg}
PG_PORT=${BDR_SMOKE_PG_PORT:-15432}
TMP=$(mktemp -d)
SRV_PID=""

cleanup() {
  if [[ -n "$SRV_PID" ]]; then
    kill "$SRV_PID" 2>/dev/null || true
    wait "$SRV_PID" 2>/dev/null || true
  fi
  if [[ -n "${KRS_PID:-}" ]]; then
    kill "$KRS_PID" 2>/dev/null || true
  fi
  if [[ -n "${SMDB:-}" ]]; then
    docker exec "$PG_CONTAINER" psql -U bdr -d postgres \
      -c "DROP DATABASE IF EXISTS $SMDB WITH (FORCE)" >/dev/null 2>&1 || true
  fi
  if [[ -n "${SMDB_REST:-}" ]]; then
    docker exec "$PG_CONTAINER" psql -U bdr -d postgres \
      -c "DROP DATABASE IF EXISTS $SMDB_REST WITH (FORCE)" >/dev/null 2>&1 || true
  fi
  if [[ "${PG_OWNED:-no}" == "yes" ]]; then
    docker rm -f "$PG_CONTAINER" >/dev/null 2>&1 || true
  fi
  if [[ "${BDR_SMOKE_KEEP:-no}" == "yes" ]]; then
    echo "[keep] TMP=$TMP"
  else
    rm -rf "$TMP"
  fi
}
trap cleanup EXIT

json_get() {
  python3 -I -c "import sys,json;print(json.load(sys.stdin)$1)"
}

STORE_ARGS=(--store file)
if [[ "$SMOKE_STORE" == "postgres" ]]; then
  echo "== postgres =="
  command -v docker >/dev/null 2>&1 || { echo "docker es necesario para BDR_SMOKE_STORE=postgres" >&2; exit 1; }
  if ! docker ps --format '{{.Names}}' | grep -qx "$PG_CONTAINER"; then
    docker rm -f "$PG_CONTAINER" >/dev/null 2>&1 || true
    docker run -d --name "$PG_CONTAINER" \
      -e POSTGRES_USER=bdr -e POSTGRES_PASSWORD=bdr \
      -p "$PG_PORT:5432" postgres:16-alpine >/dev/null
    PG_OWNED=yes
  fi
  for _ in $(seq 1 30); do
    if docker exec "$PG_CONTAINER" pg_isready -U bdr >/dev/null 2>&1; then
      break
    fi
    sleep 1
  done
  docker exec "$PG_CONTAINER" pg_isready -U bdr >/dev/null || { echo "PostgreSQL de smoke no responde" >&2; exit 1; }
  SMDB="bdr_smoke_$(date +%s)"
  docker exec "$PG_CONTAINER" psql -U bdr -d postgres -c "CREATE DATABASE $SMDB" >/dev/null
  STORE_ARGS=(--store postgres --db-url "postgres://bdr:bdr@127.0.0.1:$PG_PORT/$SMDB?sslmode=disable")
  echo "estado en PostgreSQL ($SMDB)"
fi

start_server() {
  BDR_ADMIN_TOKEN="$ADMIN_TOKEN" \
  BDR_KOPIA_KIND="$KOPIA_KIND_ENV" \
  BDR_KOPIA_REPO_PATH="$TMP/repo" \
  BDR_KOPIA_SERVER_URL="$KOPIA_SERVER_URL" \
  BDR_KOPIA_SERVER_FP="$KOPIA_SERVER_FP" \
  BDR_KOPIA_PASSWORD=smoke-password \
    ./bin/bdr-control \
    "${STORE_ARGS[@]}" \
    --addr "127.0.0.1:$PORT" \
    --agent-addr "127.0.0.1:$AGENT_TLS_PORT" \
    --agent-url "https://127.0.0.1:$AGENT_TLS_PORT" \
    --agent-tls-names 127.0.0.1,localhost \
    --data-dir "$TMP/server" \
    >>"$TMP/server.log" 2>&1 &
  SRV_PID=$!
  for _ in $(seq 1 50); do
    if curl -fsS "$BASE/healthz" >/dev/null 2>&1; then
      return 0
    fi
    sleep 0.2
  done
  echo "el servidor no arrancó" >&2
  cat "$TMP/server.log" >&2
  exit 1
}

mkdir -p bin

echo "== build =="
go build -o bin/bdr-control ./apps/server/cmd/bdr-control
go build -o bin/bdr-agent ./apps/agent/cmd/bdr-agent
cp bin/bdr-agent bin/bdr-agent-linux-amd64

# Kopia real si está; si no, fake.
KOPIA_BIN="$(command -v kopia || true)"
if [[ -z "$KOPIA_BIN" ]]; then
  KOPIA_BIN="$ROOT/bin/fake-kopia"
  case "$(uname -s)" in
    MINGW*|MSYS*|CYGWIN*) KOPIA_BIN="$ROOT/bin/fake-kopia.exe" ;;
  esac
  go build -o "$KOPIA_BIN" ./tools/fake-kopia
  echo "== kopia fake =="
else
  echo "== kopia real ($KOPIA_BIN) =="
fi

echo "== servidor =="
# Repositorio Kopia directo del plano de control (lo crearía el operador
# con el CLI estándar usando el kopia.config del directorio de datos).
mkdir -p "$TMP/server" "$TMP/repo"
KOPIA_PASSWORD=smoke-password KOPIA_CHECK_FOR_UPDATES=false \
  kopia repository create filesystem \
  --path "$TMP/repo" \
  --config-file "$TMP/server/kopia.config" \
  --cache-directory "$TMP/kcache" >/dev/null 2>&1

# Con BDR_SMOKE_KRS=yes el plano de control anuncia un Kopia Repository
# Server y los agentes reciben usuario y contraseña propios (aislamiento).
if [[ "$KRS" == "yes" ]]; then
  [[ -n "$KOPIA_BIN" && "$KOPIA_BIN" != *"fake-kopia"* ]] || { echo "BDR_SMOKE_KRS exige kopia real" >&2; exit 1; }
  echo "== kopia repository server =="
  openssl req -x509 -newkey rsa:2048 -nodes \
    -keyout "$TMP/krs.key" -out "$TMP/krs.crt" -days 30 \
    -subj "/CN=127.0.0.1" -addext "subjectAltName=IP:127.0.0.1,DNS:localhost" >/dev/null 2>&1
  KOPIA_SERVER_FP="sha256:$(openssl x509 -in "$TMP/krs.crt" -outform DER | shasum -a 256 | awk '{print $1}')"
  kopia server start \
    --config-file "$TMP/server/kopia.config" \
    --address "127.0.0.1:$KRS_PORT" \
    --tls-cert-file "$TMP/krs.crt" --tls-key-file "$TMP/krs.key" \
    --refresh-interval=2s \
    --cache-directory "$TMP/krs-cache" >>"$TMP/krs.log" 2>&1 &
  KRS_PID=$!
  for _ in $(seq 1 60); do
    if curl -ksS -o /dev/null "https://127.0.0.1:$KRS_PORT"; then
      break
    fi
    sleep 0.5
  done
  curl -ksS -o /dev/null "https://127.0.0.1:$KRS_PORT" || { echo "el KRS no arrancó" >&2; cat "$TMP/krs.log" >&2; exit 1; }
  KOPIA_KIND_ENV=server
  KOPIA_SERVER_URL="https://127.0.0.1:$KRS_PORT"
  # El CLI de kopia espera la huella sin el prefijo; el agente la normaliza.
  KRS_FP_HEX="${KOPIA_SERVER_FP#sha256:}"
  echo "krs listo (${KOPIA_SERVER_FP:0:19}…)"
fi

start_server
curl -fsS "$BASE/healthz" | json_get "['status']"
# El plano de control debe ver el repositorio.
REPO_STATUS=$(curl -fsS -H "Authorization: Bearer $ADMIN_TOKEN" "$BASE/api/v1/admin/repositories" | json_get "['items'][0]['status']")
echo "repositorio: $REPO_STATUS"

echo "== bootstrap de usuario admin (API de sesiones) =="
curl -fsS -X POST -H "Content-Type: application/json" \
  -d '{"email":"admin@smoke.test","password":"clave-de-smoke-123"}' \
  "$BASE/api/v1/auth/bootstrap" | json_get "['user']['email']" >/dev/null

echo "== token de enrolamiento =="
TOKEN=$(curl -fsS -X POST \
  -H "Authorization: Bearer $ADMIN_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"expires_in_seconds":600}' \
  "$BASE/api/v1/admin/enrollment-tokens" | json_get "['token']")
[[ -n "$TOKEN" ]] || { echo "token vacío" >&2; exit 1; }

echo "== agent enroll =="
./bin/bdr-agent enroll \
  --server "$BASE" \
  --token "$TOKEN" \
  --data-dir "$TMP/agent" >"$TMP/enroll.json"
AGENT_ID=$(json_get "['agent_id']" <"$TMP/enroll.json")
[[ -n "$AGENT_ID" ]] || { echo "agent_id vacío" >&2; exit 1; }

# Con el listener de agentes TLS anunciado, el enrolamiento debe entregar
# certificado y URL mTLS: todo el tráfico del agente irá por ahí.
AGENT_URL_CFG=$(python3 -I -c 'import json;print(json.load(open("'"$TMP"'/agent/agent.json")).get("agent_url",""))')
[[ "$AGENT_URL_CFG" == "https://127.0.0.1:$AGENT_TLS_PORT" ]] || { echo "agent_url no anunciado: '$AGENT_URL_CFG'" >&2; exit 1; }
[[ -f "$TMP/agent/agent.crt" ]] || { echo "falta el certificado del agente" >&2; exit 1; }
echo "agente con identidad mTLS ($AGENT_URL_CFG)"

if [[ "$KRS" == "yes" ]]; then
  # Aprovisiona el usuario del agente ahora y da tiempo al KRS a refrescar
  # su catálogo de usuarios antes del primer job.
  AGENT_KEY_EARLY=$(python3 -I -c 'import json;print(json.load(open("'"$TMP"'/agent/agent.json"))["api_key"])')
  curl -fsS -H "Authorization: Bearer $AGENT_KEY_EARLY" "$BASE/api/v1/agent/repository-credentials" >/dev/null
  sleep 3
fi

echo "== instalador preconfigurado (Añadir equipo) =="
# El servidor sirve binarios desde su directorio de descargas.
BDR_DL="$TMP/server/downloads"
mkdir -p "$BDR_DL" && cp bin/bdr-agent-linux-amd64 "$BDR_DL/"
INST=$(curl -fsS -X POST -H "Authorization: Bearer $ADMIN_TOKEN" -H "Content-Type: application/json" \
  -d '{"os":"linux","arch":"amd64","expires_in_seconds":600,"note":"installer"}' \
  "$BASE/api/v1/admin/installers")
INST_PATH=$(python3 -I -c 'import json,sys;print(json.loads(sys.argv[1])["installer_path"])' "$INST")
curl -fsS "$BASE$INST_PATH" -o "$TMP/installer.sh"
[[ -x /bin/systemctl ]] || export BDR_NO_SERVICE=1
BDR_INSTALL_DIR="$TMP/inst-bin" BDR_DATA_DIR="$TMP/inst-data" BDR_NO_SERVICE=1 \
  bash "$TMP/installer.sh" >"$TMP/installer.log" 2>&1 || { cat "$TMP/installer.log" >&2; exit 1; }
[[ -x "$TMP/inst-bin/bdr-agent" ]] || { echo "el instalador no dejó el binario" >&2; exit 1; }
NAGENTS=$(curl -fsS -H "Authorization: Bearer $ADMIN_TOKEN" "$BASE/api/v1/admin/agents" | python3 -I -c 'import sys,json;print(len(json.load(sys.stdin)["items"]))')
[[ "$NAGENTS" -ge 2 ]] || { echo "el agente del instalador no se enroló ($NAGENTS agentes)" >&2; exit 1; }
echo "instalador auto-enrolado: $NAGENTS agentes registrados"

echo "== plan con programación, asignación y job =="
mkdir -p "$TMP/data/sub"
printf 'contenido de prueba\n' >"$TMP/data/archivo.txt"
printf 'anidado\n' >"$TMP/data/sub/interno.txt"
mkdir "$TMP/data/tmp"
printf 'esto no se copia\n' >"$TMP/data/tmp/skip.log"

PLAN_ID=$(curl -fsS -X POST \
  -H "Authorization: Bearer $ADMIN_TOKEN" \
  -H "Content-Type: application/json" \
  -d "{\"name\":\"smoke\",\"kind\":\"file\",\"paths\":[\"$TMP/data\"],\"excludes\":[\"$TMP/data/tmp\"],\"schedule\":\"0 3 * * *\",\"retention\":{\"keep_latest\":3,\"keep_daily\":2,\"keep_weekly\":0,\"keep_monthly\":0,\"keep_annual\":0}}" \
  "$BASE/api/v1/admin/backup-plans" | json_get "['id']")
[[ -n "$PLAN_ID" ]] || { echo "plan_id vacío" >&2; exit 1; }

curl -fsS -X POST \
  -H "Authorization: Bearer $ADMIN_TOKEN" \
  -H "Content-Type: application/json" \
  -d "{\"agent_id\":\"$AGENT_ID\"}" \
  "$BASE/api/v1/admin/backup-plans/$PLAN_ID/assign" >/dev/null

echo "== job =="
JOB_ID=$(curl -fsS -X POST \
  -H "Authorization: Bearer $ADMIN_TOKEN" \
  -H "Idempotency-Key: smoke-1" \
  -H "Content-Type: application/json" \
  -d "{\"plan_id\":\"$PLAN_ID\"}" \
  "$BASE/api/v1/admin/agents/$AGENT_ID/backup" | json_get "['job_id']")
[[ -n "$JOB_ID" ]] || { echo "job_id vacío" >&2; exit 1; }

./bin/bdr-agent run --once \
  --data-dir "$TMP/agent" \
  --kopia-binary "$KOPIA_BIN" >"$TMP/job-run.log" 2>&1 || { cat "$TMP/job-run.log" >&2; exit 1; }

echo "== estado del job y restore point =="
curl -fsS -H "Authorization: Bearer $ADMIN_TOKEN" "$BASE/api/v1/admin/jobs/$JOB_ID" | python3 -I -c '
import sys, json
j = json.load(sys.stdin)
assert j["state"] == "succeeded", j
assert j["progress"] == 100, j
assert j.get("result"), j
snaps = j["result"]["snapshots"]
assert len(snaps) == 1, snaps
assert snaps[0]["files"] >= 2, snaps
print(json.dumps({"state": j["state"], "progress": j["progress"], "snapshot": snaps[0]["id"][:8]}, indent=2))
'

RP_ID=$(curl -fsS -H "Authorization: Bearer $ADMIN_TOKEN" \
  "$BASE/api/v1/admin/restore-points?agent_id=$AGENT_ID" | json_get "['items'][0]['id']")
[[ -n "$RP_ID" ]] || { echo "restore point no registrado" >&2; exit 1; }
echo "restore point: $RP_ID"

if [[ "$KOPIA_BIN" == *"fake-kopia"* ]]; then
  echo "== navegación omitida (kopia fake) =="
else
  echo "== verificación estructural del restore point =="
  curl -fsS -X POST \
    -H "Authorization: Bearer $ADMIN_TOKEN" \
    -H "Content-Type: application/json" \
    -d '{"files_percent":0}' \
    "$BASE/api/v1/admin/restore-points/$RP_ID/verify"
  sleep 2
  VERIFY=$(curl -fsS -H "Authorization: Bearer $ADMIN_TOKEN" \
    "$BASE/api/v1/admin/restore-points/$RP_ID" | json_get "['verify_state']")
  [[ "$VERIFY" == "structural_ok" ]] || { echo "verify_state=$VERIFY (esperaba structural_ok)" >&2; exit 1; }

  echo "== navegación y descarga de archivo =="
  curl -fsS -H "Authorization: Bearer $ADMIN_TOKEN" \
    "$BASE/api/v1/admin/restore-points/$RP_ID/files" | json_get "['entries'][0]['name']" >/dev/null
  curl -fsS -H "Authorization: Bearer $ADMIN_TOKEN" \
    "$BASE/api/v1/admin/restore-points/$RP_ID/files?path=sub" | json_get "['entries'][0]['name']"
  CONTENT=$(curl -fsS -H "Authorization: Bearer $ADMIN_TOKEN" \
    "$BASE/api/v1/admin/restore-points/$RP_ID/files/content?path=archivo.txt")
  [[ "$CONTENT" == *"contenido de prueba"* ]] || { echo "contenido descargado incorrecto: $CONTENT" >&2; exit 1; }

  echo "== búsqueda y descarga de directorio =="
  SR=$(curl -fsS -H "Authorization: Bearer $ADMIN_TOKEN" \
    "$BASE/api/v1/admin/restore-points/$RP_ID/files/search?q=interno")
  python3 -I -c '
import json, sys
d = json.loads(sys.argv[1])
items = [i for i in d["items"] if i["path"] == "sub/interno.txt"]
assert len(items) == 1, d
' "$SR"
  curl -fsS -H "Authorization: Bearer $ADMIN_TOKEN" \
    "$BASE/api/v1/admin/restore-points/$RP_ID/files/download?path=sub" -o "$TMP/dir.zip"
  python3 -I -c '
import json, sys, zipfile
z = zipfile.ZipFile(sys.argv[1])
names = set(z.namelist())
assert "interno.txt" in names, names
assert z.read("interno.txt") == b"anidado\n", z.read("interno.txt")
' "$TMP/dir.zip"
  # La raíz completa incluye los excluidos fuera y las subcarpetas dentro.
  curl -fsS -H "Authorization: Bearer $ADMIN_TOKEN" \
    "$BASE/api/v1/admin/restore-points/$RP_ID/files/download" -o "$TMP/root.zip"
  python3 -I -c '
import sys, zipfile
z = zipfile.ZipFile(sys.argv[1])
names = set(z.namelist())
assert "archivo.txt" in names and "sub/interno.txt" in names, names
assert not any(n.startswith("tmp/") for n in names), names
' "$TMP/root.zip"
  echo "búsqueda y ZIP de directorio: ok"

  echo "== restauración de archivo en el agente =="
  RESTORE_JOB=$(curl -fsS -X POST \
    -H "Authorization: Bearer $ADMIN_TOKEN" \
    -H "Idempotency-Key: smoke-restore-1" \
    -H "Content-Type: application/json" \
    -d "{\"restore_point_id\":\"$RP_ID\",\"items\":[\"archivo.txt\"],\"destination\":\"$TMP/restore\"}" \
    "$BASE/api/v1/admin/restores/files" | json_get "['job_id']")
  [[ -n "$RESTORE_JOB" ]] || { echo "restore job vacío" >&2; exit 1; }
  ./bin/bdr-agent run --once \
    --data-dir "$TMP/agent" \
    --kopia-binary "$KOPIA_BIN" >"$TMP/restore-run.log" 2>&1 || { cat "$TMP/restore-run.log" >&2; exit 1; }
  curl -fsS -H "Authorization: Bearer $ADMIN_TOKEN" "$BASE/api/v1/admin/jobs/$RESTORE_JOB" | python3 -I -c '
import sys, json
j = json.load(sys.stdin)
assert j["state"] == "succeeded", j
print("restore:", json.dumps(j["result"]["restored"][0], indent=2))
'
  [[ "$(cat "$TMP/restore/archivo.txt")" == *"contenido de prueba"* ]] || {
    echo "el archivo restaurado no coincide" >&2; exit 1; }
fi

echo "== renovación de certificado mTLS =="
./bin/bdr-agent renew-cert --data-dir "$TMP/agent" | json_get "['certificate_expires_at']" >/dev/null
./bin/bdr-agent run --once --data-dir "$TMP/agent" \
  --kopia-binary "$KOPIA_BIN" >"$TMP/post-renew.log" 2>&1 || { cat "$TMP/post-renew.log" >&2; exit 1; }
echo "renovación ok y heartbeat posterior con el certificado nuevo"

echo "== alertas del catálogo =="
# Un fallo provocado: kopia inexistente en un segundo agente.
TOKEN2=$(curl -fsS -X POST \
  -H "Authorization: Bearer $ADMIN_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"expires_in_seconds":600}' \
  "$BASE/api/v1/admin/enrollment-tokens" | json_get "['token']")
./bin/bdr-agent enroll --server "$BASE" --token "$TOKEN2" --data-dir "$TMP/agent2" >"$TMP/enroll2.json"
AGENT2_ID=$(json_get "['agent_id']" <"$TMP/enroll2.json")
[[ -n "$AGENT2_ID" ]] || { echo "agent_id del agente 2 vacío" >&2; exit 1; }
curl -fsS -X POST -H "Authorization: Bearer $ADMIN_TOKEN" -H "Content-Type: application/json" \
  -d "{\"plan_id\":\"$PLAN_ID\"}" "$BASE/api/v1/admin/agents/$AGENT2_ID/backup" >/dev/null
# Este agente usa un binario de kopia que no existe → job fallido + alerta.
KOPIA_MISSING=/no/existe/kopia ./bin/bdr-agent run --once --data-dir "$TMP/agent2" \
  --kopia-binary /no/existe/kopia >"$TMP/agent2-fail.log" 2>&1 || true
# La alerta es asíncrona: se sondea unos segundos antes de rendirse.
ALERTS_OK=no
for _ in $(seq 1 15); do
  if curl -fsS -H "Authorization: Bearer $ADMIN_TOKEN" "$BASE/api/v1/admin/alerts" | python3 -I -c '
import sys, json
d = json.load(sys.stdin)
assert "job_failed" in [a["kind"] for a in d["items"]], d
'; then
    ALERTS_OK=yes
    break
  fi
  sleep 0.5
done
[[ "$ALERTS_OK" == "yes" ]] || { echo "no llegó la alerta job_failed" >&2; exit 1; }
curl -fsS -H "Authorization: Bearer $ADMIN_TOKEN" "$BASE/api/v1/admin/alerts" | python3 -I -c '
import sys, json
kinds = [a["kind"] for a in json.load(sys.stdin)["items"]]
assert "job_failed" in kinds, kinds
print("alertas:", kinds)
'

echo "== revocación inmediata del agente 2 =="
# Un job pendiente para el agente 2 antes de revocar: no debe poder tomarlo.
curl -fsS -X POST -H "Authorization: Bearer $ADMIN_TOKEN" -H "Content-Type: application/json" \
  -d "{\"plan_id\":\"$PLAN_ID\"}" "$BASE/api/v1/admin/agents/$AGENT2_ID/backup" >/dev/null
REVOKED_JOB=$(curl -fsS -H "Authorization: Bearer $ADMIN_TOKEN" "$BASE/api/v1/admin/jobs?agent_id=$AGENT2_ID" \
  | json_get "['items'][0]['id']")
curl -fsS -X POST -H "Authorization: Bearer $ADMIN_TOKEN" -H "Content-Type: application/json" \
  -d '{"reason":"smoke"}' "$BASE/api/v1/admin/agents/$AGENT2_ID/revoke" >/dev/null
sleep 0.5
./bin/bdr-agent run --once --data-dir "$TMP/agent2" \
  --kopia-binary "$KOPIA_BIN" >"$TMP/agent2-revoked.log" 2>&1 || true
STATE_REVOKED=$(curl -fsS -H "Authorization: Bearer $ADMIN_TOKEN" "$BASE/api/v1/admin/jobs/$REVOKED_JOB" \
  | json_get "['state']")
[[ "$STATE_REVOKED" == "pending" ]] || { echo "FUGA: el agente revocado tomó el job (estado $STATE_REVOKED)" >&2; exit 1; }
# Además, la API no crea más jobs para un agente revocado (409).
CODE_REVOKE=$(curl -sS -o /dev/null -w '%{http_code}' -X POST -H "Authorization: Bearer $ADMIN_TOKEN" \
  -H "Content-Type: application/json" -d "{\"plan_id\":\"$PLAN_ID\"}" \
  "$BASE/api/v1/admin/agents/$AGENT2_ID/backup")
[[ "$CODE_REVOKE" == "409" ]] || { echo "esperaba 409 al crear backup de un revocado, got $CODE_REVOKE" >&2; exit 1; }
if ./bin/bdr-agent status --data-dir "$TMP/agent2" >/dev/null 2>&1; then
  echo "FUGA: el agente revocado sigue consultando el plano de control" >&2
  exit 1
fi
echo "agente revocado sin acceso: job intacto, API 409 y status rechazado"
# El job queda huerfano: se cancela (terminal) para el resumen final.
curl -fsS -X POST -H "Authorization: Bearer $ADMIN_TOKEN" \
  "$BASE/api/v1/admin/jobs/$REVOKED_JOB/cancel" >/dev/null

echo "== eventos SSE del último job =="
LAST_JOB=$(curl -fsS -H "Authorization: Bearer $ADMIN_TOKEN" "$BASE/api/v1/admin/jobs" | json_get "['items'][0]['id']")
curl -fsS -N -m 3 -H "Accept: text/event-stream" -H "Authorization: Bearer $ADMIN_TOKEN" \
  "$BASE/api/v1/admin/jobs/$LAST_JOB/events" | grep -q "event:" || echo "(sin eventos en vivo; ok)"

echo "== aislamiento en el Kopia Repository Server (solo modo KRS) =="
if [[ "$KRS" == "yes" ]]; then
  AGENT_KEY=$(python3 -I -c 'import json;print(json.load(open("'"$TMP"'/agent/agent.json"))["api_key"])')
  CREDS=$(curl -fsS -H "Authorization: Bearer $AGENT_KEY" "$BASE/api/v1/agent/repository-credentials")
  eval "$(python3 -I -c '
import json, sys, shlex
c = json.loads(sys.argv[1])
print("U1=" + shlex.quote(c["username"]))
print("PW1=" + shlex.quote(c["password"]))
print("H1=" + shlex.quote(c["hostname"]))
' "$CREDS")"
  [[ -n "$U1" && -n "$PW1" ]] || { echo "credenciales del agente vacías" >&2; exit 1; }
  echo "usuario del agente 1: $U1@$H1"

  # El cliente del agente 1 ve su propia fuente.
  KOPIA_PASSWORD="$PW1" kopia repository connect server --url "$KOPIA_SERVER_URL" \
    --server-cert-fingerprint="$KRS_FP_HEX" \
    --override-username="$U1" --override-hostname="$H1" \
    --no-use-keychain --no-persist-credentials \
    --config-file "$TMP/krs-a1.config" --cache-directory "$TMP/krs-a1c" >"$TMP/krs-a1.log" 2>&1
  VISIBLE=$(KOPIA_PASSWORD="$PW1" kopia snapshot list --json --config-file "$TMP/krs-a1.config" \
    2>"$TMP/krs-a1-list.log" | python3 -I -c '
import sys, json
snaps = json.load(sys.stdin)
print(sum(1 for s in snaps if any(p in s["source"]["path"] for p in sys.argv[1:])))
' "$TMP/data")
  [[ "$VISIBLE" -ge 1 ]] || { echo "el agente 1 debía ver su propio snapshot" >&2; exit 1; }
  echo "agente 1 ve su snapshot: sí ($VISIBLE)"

  # Un tercer agente (usuario distinto) no ve nada del agente 1.
  TOKEN3=$(curl -fsS -X POST -H "Authorization: Bearer $ADMIN_TOKEN" -H "Content-Type: application/json" \
    -d '{"expires_in_seconds":600}' "$BASE/api/v1/admin/enrollment-tokens" | json_get "['token']")
  ./bin/bdr-agent enroll --server "$BASE" --token "$TOKEN3" --data-dir "$TMP/agent3" >/dev/null
  AGENT3_KEY=$(python3 -I -c 'import json;print(json.load(open("'"$TMP"'/agent3/agent.json"))["api_key"])')
  CREDS3=$(curl -fsS -H "Authorization: Bearer $AGENT3_KEY" "$BASE/api/v1/agent/repository-credentials")
  sleep 3   # refresco del catálogo de usuarios en el KRS antes de conectar
  eval "$(python3 -I -c '
import json, sys, shlex
c = json.loads(sys.argv[1])
print("U3=" + shlex.quote(c["username"]))
print("PW3=" + shlex.quote(c["password"]))
print("H3=" + shlex.quote(c["hostname"]))
' "$CREDS3")"
  KOPIA_PASSWORD="$PW3" kopia repository connect server --url "$KOPIA_SERVER_URL" \
    --server-cert-fingerprint="$KRS_FP_HEX" \
    --override-username="$U3" --override-hostname="$H3" \
    --no-use-keychain --no-persist-credentials \
    --config-file "$TMP/krs-a3.config" --cache-directory "$TMP/krs-a3c" >"$TMP/krs-a3.log" 2>&1
  LEAKED=$(KOPIA_PASSWORD="$PW3" kopia snapshot list --json --config-file "$TMP/krs-a3.config" \
    2>"$TMP/krs-a3-list.log" | python3 -I -c '
import sys, json
snaps = json.load(sys.stdin)
print(sum(1 for s in snaps if any(p in s["source"]["path"] for p in sys.argv[1:])))
' "$TMP/data")
  [[ "$LEAKED" -eq 0 ]] || { echo "FUGA: el agente 3 ve $LEAKED snapshots del agente 1" >&2; exit 1; }
  echo "aislamiento usuario↔usuario: ok"

  # Credenciales robadas + hostname falso: kopia no liga el hostname en el
  # handshake (verificado en 0.23), así que quien roba usuario+contraseña de
  # un agente ve SOLO los datos de ese agente — nunca los de otros. La
  # revocación real de credenciales robadas es la rotación (se prueba luego).
  if KOPIA_PASSWORD="$PW1" kopia repository connect server --url "$KOPIA_SERVER_URL" \
      --server-cert-fingerprint="$KRS_FP_HEX" \
      --override-username="$U1" --override-hostname="$H3" \
      --no-use-keychain --no-persist-credentials \
      --config-file "$TMP/krs-cross.config" --cache-directory "$TMP/krs-xc" >"$TMP/krs-cross.log" 2>&1; then
    OTHERS=$(KOPIA_PASSWORD="$PW1" kopia snapshot list --json --config-file "$TMP/krs-cross.config" \
      2>/dev/null | python3 -I -c '
import sys, json
snaps = json.load(sys.stdin)
print(len(snaps) - sum(1 for s in snaps if any(p in s["source"]["path"] for p in sys.argv[1:])))
' "$TMP/data" 2>/dev/null || echo 99)
    [[ "$OTHERS" -eq 0 ]] || { echo "FUGA: credenciales del agente 1 ven datos de otros agentes" >&2; exit 1; }
    echo "credenciales robadas: solo ven los datos del propio agente"
  else
    echo "credenciales robadas + hostname falso: conexión rechazada"
  fi

  # Rotación administrativa: nueva contraseña válida; la antigua, no.
  ROT=$(curl -fsS -X POST -H "Authorization: Bearer $ADMIN_TOKEN" \
    "$BASE/api/v1/admin/agents/$AGENT_ID/repo-credentials/rotate")
  V2=$(python3 -I -c 'import json,sys;print(json.loads(sys.argv[1])["version"])' "$ROT")
  [[ "$V2" == "2" ]] || { echo "rotación: esperaba versión 2, got $V2" >&2; exit 1; }
  sleep 3   # refresco del catálogo de usuarios en el KRS
  CREDS_N=$(curl -fsS -H "Authorization: Bearer $AGENT_KEY" "$BASE/api/v1/agent/repository-credentials")
  PW1N=$(python3 -I -c 'import json,sys;print(json.loads(sys.argv[1])["password"])' "$CREDS_N")
  [[ "$PW1N" != "$PW1" ]] || { echo "la contraseña no cambió" >&2; exit 1; }
  sleep 3   # segunda espera: el GET re-aplica la contraseña si la rotación quedó pendiente
  if KOPIA_PASSWORD="$PW1" kopia snapshot list --json --config-file "$TMP/krs-a1.config" \
      >/dev/null 2>&1; then
    echo "FUGA: la contraseña antigua sigue aceptándose" >&2
    exit 1
  fi
  KOPIA_PASSWORD="$PW1N" kopia snapshot list --json --config-file "$TMP/krs-a1.config" \
      >"$TMP/krs-rot-list.log" 2>&1 || { echo "la nueva contraseña no funciona" >&2; tail -3 "$TMP/krs-rot-list.log" >&2; exit 1; }
  echo "rotación de credencial de repositorio: ok"
fi

echo "== backup y restauración de la base (solo modo postgres) =="
if [[ -n "${SMDB:-}" ]]; then
  docker exec "$PG_CONTAINER" pg_dump -U bdr -Fc "$SMDB" >"$TMP/bdr.dump"
  [[ -s "$TMP/bdr.dump" ]] || { echo "pg_dump vacío" >&2; exit 1; }
  SMDB_REST="${SMDB}_rest"
  docker exec "$PG_CONTAINER" psql -U bdr -d postgres -c "CREATE DATABASE $SMDB_REST" >/dev/null
  docker exec -i "$PG_CONTAINER" pg_restore -U bdr --no-owner -d "$SMDB_REST" <"$TMP/bdr.dump" >/dev/null 2>&1
  # El reinicio posterior sirve desde la base RESTAURADA: los asertos de
  # persistencia y de cadena de auditoría validan el backup de verdad.
  STORE_ARGS=(--store postgres --db-url "postgres://bdr:bdr@127.0.0.1:$PG_PORT/$SMDB_REST?sslmode=disable")
  echo "reinicio contra la base restaurada ($SMDB_REST)"
fi

echo "== reinicio y persistencia =="
kill "$SRV_PID" 2>/dev/null || true
wait "$SRV_PID" 2>/dev/null || true
SRV_PID=""
start_server

curl -fsS -H "Authorization: Bearer $ADMIN_TOKEN" "$BASE/api/v1/admin/agents" \
  | python3 -I -c '
import sys, json
d = json.load(sys.stdin)
# 2 agentes del flujo principal + el tercero del aislamiento (solo modo KRS).
assert len(d["items"]) >= 2, d
print(json.dumps({"agentes": len(d["items"])}, indent=2))
'
curl -fsS -H "Authorization: Bearer $ADMIN_TOKEN" "$BASE/api/v1/admin/jobs/$JOB_ID" \
  | python3 -I -c 'import sys,json; j=json.load(sys.stdin); assert j["state"]=="succeeded", j; print("job persistido:", j["id"])'
curl -fsS -H "Authorization: Bearer $ADMIN_TOKEN" "$BASE/api/v1/admin/restore-points" \
  | python3 -I -c 'import sys,json; d=json.load(sys.stdin); assert len(d["items"])>=1, d; print("restore points persistidos:", len(d["items"]))'

echo "== auditoría encadenada =="
curl -fsS -H "Authorization: Bearer $ADMIN_TOKEN" "$BASE/api/v1/admin/audit/verify" | python3 -I -c '
import sys, json
d = json.load(sys.stdin)
assert d["ok"] is True, d
assert d["events"] >= 5, d
print(json.dumps({"eventos": d["events"], "última_seq": d["last_seq"]}, indent=2))
'

echo "== token reutilizado (debe fallar) =="
CODE=$(curl -sS -o /dev/null -w '%{http_code}' -X POST \
  -H "Content-Type: application/json" \
  -d "{\"token\":\"$TOKEN\",\"hostname\":\"x\",\"os_family\":\"linux\",\"arch\":\"amd64\"}" \
  "$BASE/api/v1/agent/enroll")
[[ "$CODE" == "410" ]] || { echo "esperaba 410 al reutilizar el token, got $CODE" >&2; exit 1; }

echo "== resumen del sistema =="
curl -fsS -H "Authorization: Bearer $ADMIN_TOKEN" "$BASE/api/v1/admin/summary" | python3 -I -c '
import sys, json
d = json.load(sys.stdin)
assert d["agents_total"] >= 2, d
assert d["jobs_active"] == 0, d
assert d["restore_points"] >= 1, d
print(json.dumps({"agentes": d["agents_total"], "planes": d["plans_total"], "rps": d["restore_points"], "exito_24h": d["success_24h"]["percent"]}, indent=2))
'

echo "SMOKE OK"

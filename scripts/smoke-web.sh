#!/usr/bin/env bash
# Smoke test del panel embebido: el binario sirve el export estático y la API
# en el mismo origen. Verifica /healthz, panel HTML, fallback SPA y assets.
# Uso: ./scripts/smoke-web.sh
# Requisitos: go, node/npm, curl, python.

set -euo pipefail

ROOT=$(cd "$(dirname "$0")/.." && pwd)
cd "$ROOT"

API_PORT=${API_PORT:-18081}
API="http://127.0.0.1:$API_PORT"
ADMIN_TOKEN=${ADMIN_TOKEN:-dev-admin-token}
TMP=$(mktemp -d)
API_PID=""

cleanup() {
  if [[ -n "$API_PID" ]]; then
    kill "$API_PID" 2>/dev/null || true
    wait "$API_PID" 2>/dev/null || true
  fi
  rm -rf "$TMP"
}
trap cleanup EXIT

if [[ ! -f apps/server/internal/webui/out/_next/index.html && $(find apps/server/internal/webui/out -maxdepth 1 -name '_next*' | wc -l) -eq 0 ]]; then
  echo "== build panel (export estático a webui/out) =="
  (cd apps/web && npm run build)
  rm -rf apps/server/internal/webui/out
  mkdir -p apps/server/internal/webui/out
  cp -R apps/web/out/. apps/server/internal/webui/out/
fi

echo "== build server =="
mkdir -p bin
go build -o bin/bdr-control ./apps/server/cmd/bdr-control

echo "== arranque del control (panel embebido) =="
BDR_ADMIN_TOKEN="$ADMIN_TOKEN" ./bin/bdr-control \
  --addr "127.0.0.1:$API_PORT" \
  --data-dir "$TMP/state" >"$TMP/api.log" 2>&1 &
API_PID=$!
for _ in $(seq 1 50); do
  curl -fsS "$API/healthz" >/dev/null 2>&1 && break
  sleep 0.2
done
curl -fsS "$API/healthz" >/dev/null || { cat "$TMP/api.log" >&2; exit 1; }

echo "== healthz mismo origen =="
curl -fsS "$API/healthz" | python3 -I -c '
import sys, json
h = json.load(sys.stdin)
assert h["status"] == "ok", h
print("api version:", h["version"])
'

echo "== admin API en el mismo origen =="
curl -fsS -H "Authorization: Bearer $ADMIN_TOKEN" "$API/api/v1/admin/agents" | python3 -I -c '
import sys, json
d = json.load(sys.stdin)
assert "items" in d, d
print("agents:", len(d["items"]))
'

echo "== panel HTML =="
curl -fsS "$API/" | grep -q "BDR Console" || { echo "HTML inesperado" >&2; exit 1; }
echo "html ok"

echo "== fallback SPA =="
curl -fsS "$API/agents" | grep -q "BDR Console" || { echo "fallback SPA roto" >&2; exit 1; }
echo "spa ok"

echo "== assets _next =="
asset=$(curl -fsS "$API/" | grep -oE '/_next/static/[a-zA-Z0-9/._-]+\.js' | head -1)
[[ -n "$asset" ]]
curl -fsS -o /dev/null -w "%{http_code}\n" "$API$asset"
echo "assets ok"

echo "SMOKE WEB OK"

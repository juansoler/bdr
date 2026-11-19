#!/usr/bin/env bash
# =============================================================================
# demo.sh — arranca/para el entorno demo de BDR (control plane con panel
# embebido).
#
# Uso (desde cualquier sitio):
#   scripts/demo.sh start     # arranca lo que falte (idempotente)
#   scripts/demo.sh stop      # para el servicio
#   scripts/demo.sh restart   # para y arranca
#   scripts/demo.sh status    # estado
#   scripts/demo.sh start --build   # recompila el binario (con panel) antes
#
# Este script es LA forma canónica de levantar el servicio en demo:
# no lo arrunques a mano con nohup — este script fija todas las variables
# (token, repo Kopia, IPs), recompila si la versión del código es más nueva
# que el binario, arranca con nohup (sobrevive a sesiones) y verifica salud.
# El panel va embebido en bdr-control: make build lo exporta y compila.
# =============================================================================
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
IP="${DEMO_IP:-203.0.113.132}"          # IP LAN del Mac que sirve el demo
ADMIN_TOKEN="${DEMO_ADMIN_TOKEN:-dev-admin-token}"
CONTROL_LOG=/tmp/bdr-control.log
BUILD=0
ACTION=status
[ $# -gt 0 ] && ACTION="$1"
[ "${2:-}" = "--build" ] && BUILD=1
cd "$ROOT"

version() {
  grep -m1 'Version = ' packages/version/version.go | sed 's/.*"\(.*\)".*/\1/'
}

port_pid() { lsof -ti ":$1" 2>/dev/null || true; }

control_pid() {
  pgrep -f "bdr-control --dev" 2>/dev/null || lsof -ti :8080 2>/dev/null || true
}

healthz() { curl -fsS --max-time 3 "http://127.0.0.1:8080/healthz" 2>/dev/null || true; }

do_build() {
  echo "» compilando control plane (versión $(version))…"
  go build -trimpath \
    -ldflags "-X github.com/juansoler/bdr/packages/version.Version=$(version)" \
    -o bin/bdr-control ./apps/server/cmd/bdr-control
  echo "  binario listo: bin/bdr-control ($(version))"
}

start_control() {
  # Recompila si el binario no existe o la versión del código es más nueva.
  if [ ! -x bin/bdr-control ] || [ packages/version/version.go -nt bin/bdr-control ] || [ "$BUILD" = "1" ]; then
    do_build
  fi
  local pid
  pid=$(control_pid)
  if [ -n "$pid" ]; then
    echo "» control plane ya en marcha (pid $pid)"
    return
  fi
  echo "» arrancando control plane ($(version)) en :8080 + agentes :8443…"
  BDR_ADMIN_TOKEN="$ADMIN_TOKEN" \
  BDR_KOPIA_KIND=filesystem \
  BDR_KOPIA_REPO_PATH="$ROOT/data/demo/repo" \
  BDR_KOPIA_PASSWORD=demo-repo-password \
  nohup bin/bdr-control --dev \
    --addr 0.0.0.0:8080 \
    --public-url "http://$IP:8080" \
    --agent-addr 0.0.0.0:8443 \
    --agent-url "https://$IP:8443" \
    --agent-tls-names "$IP,localhost" \
    --data-dir "$ROOT/data/demo/control" \
    > "$CONTROL_LOG" 2>&1 &
  disown
  local ok=""
  for _ in $(seq 1 30); do
    if curl -fsS --max-time 2 http://127.0.0.1:8080/healthz >/dev/null 2>&1; then ok=1; break; fi
    sleep 1
  done
  if [ -z "$ok" ]; then
    echo "✗ el control plane no levantó — mira $CONTROL_LOG" >&2
    exit 1
  fi
  local hz
  hz=$(healthz | sed 's/.*"version":"\([^"]*\)".*/\1/')
  if [ "$hz" != "$(version)" ]; then
    echo "✗ ATENCIÓN: healthz reporta $hz pero el código es $(version) — binario desactualizado; ejecuta: scripts/demo.sh restart --build" >&2
    exit 1
  fi
  echo "  ✓ control plane $hz (8080 + agentes 8443) — log: $CONTROL_LOG"
}

do_start() {
  start_control
  echo
  echo "Servicios en marcha:"
  echo "  panel (embebido): http://$IP:8080/"
  echo "  control plane:    http://$IP:8080 (API + agentes :8443)"
  echo "Los agentes se reconectan solos en su próximo heartbeat."
}

do_stop() {
  echo "» parando control plane…"
  local cpid; cpid=$(control_pid)
  [ -n "$cpid" ] && kill $cpid 2>/dev/null
  sleep 2
  cpid=$(control_pid); [ -n "$cpid" ] && kill -9 $cpid 2>/dev/null
  echo "  parado."
}

do_status() {
  local hz; hz=$(healthz)
  echo "control plane: $(control_pid || echo 'PARADO') — ${hz:-sin respuesta}"
  echo "panel embebido: $(curl -fsS --max-time 2 -o /dev/null -w '%{http_code}' http://127.0.0.1:8080/ 2>/dev/null || echo 'sin respuesta')"
  echo "versión código: $(version)"
}

case "$ACTION" in
  start)   do_start ;;
  stop)    do_stop ;;
  restart) do_stop; do_start ;;
  status)  do_status ;;
  *) echo "uso: scripts/demo.sh {start|stop|restart|status} [--build]"; exit 2 ;;
esac

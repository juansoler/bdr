#!/usr/bin/env bash
# Instalador del agente BDR para Linux.
# Uso: sudo ./install.sh --server http://SERVIDOR:8080 --token TOKEN \
#        [--fingerprint sha256:HEX] [--data-dir /var/lib/bdr-agent] [--binary ./bdr-agent]
set -euo pipefail

SERVER=""; TOKEN=""; FP=""; DATA_DIR=/var/lib/bdr-agent; BINARY=""
while [[ $# -gt 0 ]]; do
  case "$1" in
    --server) SERVER=$2; shift 2 ;;
    --token) TOKEN=$2; shift 2 ;;
    --fingerprint) FP=$2; shift 2 ;;
    --data-dir) DATA_DIR=$2; shift 2 ;;
    --binary) BINARY=$2; shift 2 ;;
    *) echo "opción desconocida: $1" >&2; exit 2 ;;
  esac
done

[[ -n "$SERVER" && -n "$TOKEN" ]] || { echo "faltan --server o --token" >&2; exit 2; }
[[ $EUID -eq 0 ]] || { echo "ejecuta como root" >&2; exit 1; }

# El token va por variable de entorno para que no aparezca en `ps`.
export BDR_ENROLL_TOKEN="$TOKEN"

# Binario: --binary o el arch corretamente compilado junto a este script.
if [[ -z "$BINARY" ]]; then
  case "$(uname -m)" in
    x86_64) BINARY="$(dirname "$0")/bdr-agent-linux-amd64" ;;
    aarch64|arm64) BINARY="$(dirname "$0")/bdr-agent-linux-arm64" ;;
    *) echo "arquitectura no soportada: $(uname -m)" >&2; exit 1 ;;
  esac
fi
[[ -f "$BINARY" ]] || { echo "no se encontró el binario $BINARY" >&2; exit 1; }

install -m 0755 "$BINARY" /usr/local/bin/bdr-agent
install -m 0644 "$(dirname "$0")/bdr-agent.service" /etc/systemd/system/bdr-agent.service

getent passwd bdr-agent >/dev/null || useradd --system --group --home "$DATA_DIR" bdr-agent
mkdir -p "$DATA_DIR"
chmod 0700 "$DATA_DIR"
chown bdr-agent:bdr-agent "$DATA_DIR"

# Sin sudo -u: sistemas sin sudo (Debian minimal, pve) no lo tienen y el
# instalador ya exige root. Se enrola como root y después se transfiere el
# data-dir al usuario del servicio — la configuración (0600) queda con su
# dueño correcto, que era el motivo del drop de privilegios.
ENROLL_ARGS=(register --server "$SERVER" --data-dir "$DATA_DIR")
if [[ -n "$FP" ]]; then
  ENROLL_ARGS+=(--server-fingerprint "$FP")
fi

set +e
BDR_ENROLL_TOKEN="$BDR_ENROLL_TOKEN" /usr/local/bin/bdr-agent "${ENROLL_ARGS[@]}"
RC=$?
set -e
if [[ $RC -ne 0 ]]; then
  echo "el enrolamiento falló (código $RC); revisa el token y la conexión" >&2
  exit $RC
fi
unset BDR_ENROLL_TOKEN
chown -R bdr-agent:bdr-agent "$DATA_DIR"

systemctl daemon-reload
systemctl enable --now bdr-agent
sleep 2
systemctl --no-pager --lines=5 status bdr-agent || true

echo
echo "Instalación completa. Estado del agente:"
/usr/local/bin/bdr-agent status --data-dir "$DATA_DIR" || true

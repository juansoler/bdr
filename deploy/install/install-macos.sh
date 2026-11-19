#!/usr/bin/env bash
# =============================================================================
# install.sh — instalador del servidor BDR Self-Hosted (Linux y macOS).
#
# Descarga el bundle de la release, verifica el SHA-256, instala el binario
# y kopia, genera secretos, registra el servicio (systemd o launchd) y
# arranca el plano de control con el panel embebido.
#
# Uso recomendado (revisa antes de ejecutar — ejecuta código remoto):
#   curl -fsSL <URL-de-la-release>/install-linux.sh -o install.sh
#   less install.sh
#   sudo bash install.sh
#
# Opciones: --version vX.Y.Z  --base URL  --data-dir DIR  --no-service
# Variables: BDR_PUBLIC_HOST (host que alcanzan los agentes), BDR_PORT,
#            BDR_AGENT_PORT.
# =============================================================================
set -euo pipefail

VERSION=""
BASE_URL="https://github.com/juansoler/bdr/releases"
DATA_DIR=""
NO_SERVICE=0

while [[ $# -gt 0 ]]; do
  case "$1" in
    --version) VERSION="$2"; shift 2 ;;
    --base) BASE_URL="$2"; shift 2 ;;
    --data-dir) DATA_DIR="$2"; shift 2 ;;
    --no-service) NO_SERVICE=1; shift ;;
    *) echo "opción desconocida: $1" >&2; exit 2 ;;
  esac
done
[[ $EUID -eq 0 ]] || { echo "ejecuta como root (sudo)" >&2; exit 1; }
command -v curl >/dev/null 2>&1 || { echo "falta curl" >&2; exit 1; }

OS=$(uname -s); ARCH=$(uname -m)
case "$OS" in
  Linux) PLATFORM_OS=linux; BIN_DIR=/usr/local/bin; SVC_DIR=/etc/systemd/system; ETC=/etc/bdr ;;
  Darwin) PLATFORM_OS=darwin; BIN_DIR=/usr/local/bin; ETC=/etc/bdr ;;
  *) echo "sistema no soportado: $OS" >&2; exit 1 ;;
esac
case "$ARCH" in
  x86_64|amd64) ARCH=amd64 ;;
  aarch64|arm64) ARCH=arm64 ;;
  *) echo "arquitectura no soportada: $ARCH" >&2; exit 1 ;;
esac
PLATFORM="${PLATFORM_OS}-${ARCH}"

LATEST_URL="$BASE_URL/latest/download"
if [[ -z "$VERSION" ]]; then
  # La release estable expone su versión en latest.json (firmado; aquí solo
  # se lee para el nombre del bundle, la verificación de firma la hace el
  # propio bdr-control tras arrancar).
  VERSION=$(curl -fsSL "$LATEST_URL/latest.json" | sed -n 's/.*"version"[: ]*"\([^"]*\)".*/\1/p' | head -1)
  [[ -n "$VERSION" ]] || { echo "no se pudo leer la versión del canal" >&2; exit 1; }
fi
TAG="v${VERSION#v}"
DL="$BASE_URL/download/$TAG" # formato GitHub: /releases/download/<tag>/<fichero>

TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT
echo "== descargando bundle de bdr-control $VERSION ($PLATFORM)…"
# El fichero se descarga con su nombre real: shasum -c resuelve las rutas
# tal y como figuran en SHA256SUMS.
BUNDLE="bdr-control-$PLATFORM.tar.gz"
curl -fsSL "$DL/$BUNDLE" -o "$TMP/$BUNDLE"
curl -fsSL "$DL/SHA256SUMS" -o "$TMP/SHA256SUMS"
( cd "$TMP" && grep -F "$BUNDLE" SHA256SUMS | shasum -a 256 -c - >/dev/null ) \
  || { echo "el bundle no coincide con SHA256SUMS — NO continuar" >&2; exit 1; }

echo "== extrayendo…"
tar xzf "$TMP/$BUNDLE" -C "$TMP"
install -m 0755 "$TMP/bdr-control" "$BIN_DIR/bdr-control"
if [[ -f "$TMP/kopia" ]]; then
  install -m 0755 "$TMP/kopia" "$BIN_DIR/kopia"
fi
install -m 0644 "$TMP/LICENSE" "$TMP/NOTICE" "$ETC/" 2>/dev/null || true
if [[ -f "$TMP/THIRD_PARTY_NOTICES.md" ]]; then
  install -m 0644 "$TMP/THIRD_PARTY_NOTICES.md" "$ETC/"
fi

# Directorios y usuario del servicio.
if [[ $PLATFORM_OS = linux ]]; then
  DATA_DIR=${DATA_DIR:-/var/lib/bdr}
  getent passwd bdr >/dev/null || useradd --system --home "$DATA_DIR" --shell /usr/sbin/nologin bdr
  mkdir -p "$DATA_DIR/control"
  chown -R bdr:bdr "$DATA_DIR"
else
  DATA_DIR=${DATA_DIR:-/Library/Application Support/BDR}
  mkdir -p "$DATA_DIR/control"
fi
mkdir -p "$ETC"
touch "$ETC/bdr.env"; chmod 0600 "$ETC/bdr.env"

# Secretos: se generan solo si no existen (re-ejecuciones conservan credenciales).
if ! grep -q BDR_ADMIN_TOKEN "$ETC/bdr.env" 2>/dev/null; then
  cat >> "$ETC/bdr.env" <<EOF
BDR_ADMIN_TOKEN=$(openssl rand -base64 32 2>/dev/null || head -c 32 /dev/urandom | base64)
BDR_SECRET_KEY=$(openssl rand -base64 32 2>/dev/null || head -c 32 /dev/urandom | base64)
BDR_KOPIA_PASSWORD=$(openssl rand -base64 24 2>/dev/null || head -c 24 /dev/urandom | base64)
EOF
  chmod 0600 "$ETC/bdr.env"
fi

# Binarios de agentes para los instaladores del panel: se descargan de la
# release (el bundle del servidor no los lleva) y se verifican con SHA256SUMS.
mkdir -p /usr/local/lib/bdr/agents
curl -fsSL "$LATEST_URL/SHA256SUMS" -o "$TMP/SHA256SUMS"
for a in linux-amd64 linux-arm64 darwin-amd64 darwin-arm64 windows-amd64; do
  # if/fi y no $( && ): con set -e, una asignación cuyo comando de
  # sustitución falla aborta el script aunque el fallo sea esperado.
  name="bdr-agent-$a"
  if [[ $a = windows-* ]]; then name="$name.exe"; fi
  if grep -q "  $name\$" "$TMP/SHA256SUMS" 2>/dev/null || grep -qw "$name" "$TMP/SHA256SUMS"; then
    curl -fsSL "$LATEST_URL/$name" -o "/usr/local/lib/bdr/agents/$name"
    (cd /usr/local/lib/bdr/agents && grep -w "$name" "$TMP/SHA256SUMS" | shasum -a 256 -c - >/dev/null) \
      || { echo "el agente $name no coincide con SHA256SUMS — NO continuar" >&2; rm -f "/usr/local/lib/bdr/agents/$name"; exit 1; }
  fi
done

PUBLIC_HOST="${BDR_PUBLIC_HOST:-$(hostname -s || hostname)}"
PORT="${BDR_PORT:-8080}"; AGENT_PORT="${BDR_AGENT_PORT:-8443}"

start_service() {
  if [[ $PLATFORM_OS = linux ]]; then
    sed -e "s|BDR_PUBLIC_HOST|$PUBLIC_HOST|g" "$TMP/bdr-control.service" > "$SVC_DIR/bdr-control.service"
    systemctl daemon-reload
    systemctl enable --now bdr-control
    sleep 2; systemctl --no-pager --lines=5 status bdr-control || true
  else
    cat > /Library/LaunchDaemons/local.bdr.control.plist <<PLIST
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
  <key>Label</key><string>local.bdr.control</string>
  <key>ProgramArguments</key><array>
    <string>$BIN_DIR/bdr-control</string>
    <string>--addr</string><string>0.0.0.0:$PORT</string>
    <string>--agent-addr</string><string>0.0.0.0:$AGENT_PORT</string>
    <string>--public-url</string><string>http://$PUBLIC_HOST:$PORT</string>
    <string>--agent-url</string><string>https://$PUBLIC_HOST:$AGENT_PORT</string>
    <string>--agent-tls-names</string><string>$PUBLIC_HOST,localhost</string>
    <string>--data-dir</string><string>$DATA_DIR/control</string>
    <string>--downloads-dir</string><string>/usr/local/lib/bdr/agents</string>
  </array>
  <key>EnvironmentVariables</key><dict>
$(sed -n 's/^\([^#][^=]*\)=\(.*\)$/    <key>\1<\/key><string>\2<\/string>/p' "$ETC/bdr.env")
  </dict>
  <key>RunAtLoad</key><true/>
  <key>KeepAlive</key><true/>
  <key>StandardOutPath</key><string>$DATA_DIR/control.log</string>
  <key>StandardErrorPath</key><string>$DATA_DIR/control.log</string>
</dict></plist>
PLIST
    launchctl bootout system/local.bdr.control 2>/dev/null || true
    launchctl bootstrap system /Library/LaunchDaemons/local.bdr.control.plist \
      || launchctl load -w /Library/LaunchDaemons/local.bdr.control.plist
    echo "servicio local.bdr.control instalado (launchd)"
  fi
}

if [[ $NO_SERVICE -eq 1 ]]; then
  echo "sin servicio: arranca con"
  echo "  BDR_ADMIN_TOKEN=… $BIN_DIR/bdr-control --addr 0.0.0.0:$PORT --data-dir $DATA_DIR/control"
else
  # El fichero de unidad lo trae el bundle; si no, se genera.
  [[ -f "$TMP/bdr-control.service" ]] || cat > "$TMP/bdr-control.service" <<'UNIT'
[Unit]
Description=BDR Control
After=network-online.target
[Service]
Type=simple
User=bdr
EnvironmentFile=/etc/bdr/bdr.env
ExecStart=/usr/local/bin/bdr-control --addr 0.0.0.0:8080 --agent-addr 0.0.0.0:8443 --data-dir /var/lib/bdr/control --downloads-dir /usr/local/lib/bdr/agents
Restart=always
[Install]
WantedBy=multi-user.target
UNIT
  start_service
fi

echo
echo "== instalación completa =="
echo "panel:        http://$PUBLIC_HOST:$PORT/"
echo "agentes mTLS: $AGENT_PORT"
echo "token:        $ETC/bdr.env (BDR_ADMIN_TOKEN)"
echo "nota:         pon TLS delante (Caddy) o usa --tls-cert/--tls-key antes de exponer el panel"

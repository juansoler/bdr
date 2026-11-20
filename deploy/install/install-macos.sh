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
# Con terminal, pregunta en qué interfaz publicar (LAN, NetBird…) y si usar
# IP o un nombre DNS. Sin terminal (curl|bash) publica el hostname.
# Opciones: --version vX.Y.Z  --base URL  --data-dir DIR
#           --public-host <ip|nombre>  --no-service
# Variables: BDR_PUBLIC_HOST (host que alcanzan los agentes), BDR_PORT,
#            BDR_AGENT_PORT.
# =============================================================================
set -euo pipefail

VERSION=""
BASE_URL="https://github.com/juansoler/bdr/releases"
DATA_DIR=""
PUBLIC_HOST=""
NO_SERVICE=0

while [[ $# -gt 0 ]]; do
  case "$1" in
    --version) VERSION="$2"; shift 2 ;;
    --base) BASE_URL="$2"; shift 2 ;;
    --data-dir) DATA_DIR="$2"; shift 2 ;;
    --public-host) PUBLIC_HOST="$2"; shift 2 ;;
    --no-service) NO_SERVICE=1; shift ;;
    *) echo "opción desconocida: $1" >&2; exit 2 ;;
  esac
done
[[ $EUID -eq 0 ]] || { echo "ejecuta como root (sudo)" >&2; exit 1; }
command -v curl >/dev/null 2>&1 || { echo "falta curl" >&2; exit 1; }

OS=$(uname -s); ARCH=$(uname -m)
case "$OS" in
  Linux) PLATFORM_OS=linux; BIN_DIR=/opt/bdr/bin; SVC_DIR=/etc/systemd/system; ETC=/etc/bdr ;;
  Darwin) PLATFORM_OS=darwin; BIN_DIR=/opt/bdr/bin; ETC=/etc/bdr ;;
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
mkdir -p "$BIN_DIR"
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
  # El binario debe ser del usuario del servicio: la auto-actualización
  # renombra ficheros dentro de su directorio (no basta con permisos del
  # fichero). Los agentes quedan root-owned: el servicio solo los lee.
  chown -R bdr:bdr /opt/bdr/bin
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

# Conexión Kopia del plano de control: sin estas variables el control no
# puede aprovisionar credenciales de repositorio (los jobs fallan con 502).
if ! grep -q '^BDR_KOPIA_KIND=' "$ETC/bdr.env" 2>/dev/null; then
  cat >> "$ETC/bdr.env" <<'EOF'
BDR_KOPIA_KIND=filesystem
BDR_KOPIA_REPO_PATH=/opt/bdr/repo
EOF
fi

# Repositorio Kopia: el equivalente nativo al kopia-init de Compose. Una sola
# vez; el fichero de conexión queda junto al estado del control.
KOPIA_PASS=$(grep '^BDR_KOPIA_PASSWORD=' "$ETC/bdr.env" | cut -d= -f2-)
mkdir -p /opt/bdr/repo
if [[ $PLATFORM_OS = linux ]]; then
  chown -R bdr:bdr /opt/bdr/repo
fi
if [[ ! -f "$DATA_DIR/control/kopia.config" ]]; then
  echo "== inicializando repositorio Kopia en /opt/bdr/repo…"
  if [[ $PLATFORM_OS = linux ]]; then
    sudo -u bdr env KOPIA_PASSWORD="$KOPIA_PASS" "$BIN_DIR/kopia" \
      --config-file="$DATA_DIR/control/kopia.config" repository create filesystem \
      --path=/opt/bdr/repo --cache-directory="$DATA_DIR/kcache"
  else
    KOPIA_PASSWORD="$KOPIA_PASS" "$BIN_DIR/kopia" \
      --config-file="$DATA_DIR/control/kopia.config" repository create filesystem \
      --path=/opt/bdr/repo --cache-directory="$DATA_DIR/kcache"
  fi
fi

# Binarios de agentes para los instaladores del panel: se descargan de la
# release (el bundle del servidor no los lleva) y se verifican con SHA256SUMS.
mkdir -p /opt/bdr/agents
curl -fsSL "$LATEST_URL/SHA256SUMS" -o "$TMP/SHA256SUMS"
for a in linux-amd64 linux-arm64 darwin-amd64 darwin-arm64 windows-amd64; do
  # if/fi y no $( && ): con set -e, una asignación cuyo comando de
  # sustitución falla aborta el script aunque el fallo sea esperado.
  name="bdr-agent-$a"
  if [[ $a = windows-* ]]; then name="$name.exe"; fi
  if grep -q "  $name\$" "$TMP/SHA256SUMS" 2>/dev/null || grep -qw "$name" "$TMP/SHA256SUMS"; then
    curl -fsSL "$LATEST_URL/$name" -o "/opt/bdr/agents/$name"
    (cd /opt/bdr/agents && grep -w "$name" "$TMP/SHA256SUMS" | shasum -a 256 -c - >/dev/null) \
      || { echo "el agente $name no coincide con SHA256SUMS — NO continuar" >&2; rm -f "/opt/bdr/agents/$name"; exit 1; }
  fi
done

# pick_public_host: decide la dirección que publicarán agentes y one-liners.
# Prioridad: --public-host (arg) > BDR_PUBLIC_HOST (env) > pregunta
# interactiva (solo con terminal) > hostname.
# Con `curl | bash` el stdin es el pipe: sin terminal → hostname + aviso.
pick_public_host() {
  if [[ -n "${PUBLIC_HOST:-}" ]]; then return 0; fi
  local def
  def="$(hostname -s 2>/dev/null || hostname)"
  if [[ ! -t 0 ]]; then
    PUBLIC_HOST="$def"
    echo "aviso: instalación sin terminal — publicando con el hostname '$PUBLIC_HOST' (solo resuelve en LAN/mDNS)." >&2
    echo "       Para publicar en otra interfaz (p. ej. NetBird): BDR_PUBLIC_HOST=<ip|nombre> o --public-host <ip|nombre>." >&2
    return 0
  fi
  echo ""
  echo "== Dirección publicada =="
  echo "Agentes y one-liners alcanzarán el control por esta dirección."
  local -a cands=()
  if [[ $PLATFORM_OS = linux ]]; then
    while read -r iface ip; do
      cands+=("$iface|$ip")
    done < <(ip -4 -o addr show scope global 2>/dev/null | awk '{iface=$2; ip=$4; sub(/\/.*/,"",ip); print iface" "ip}' | sort -u)
  else
    local iface="" ip="" line
    while IFS= read -r line; do
      case "$line" in
        *": flags"*) iface="${line%%: *}"; iface="${iface// /}" ;;
        "	inet "*|"    inet "*|"        inet "*) ip="${line#*inet }"; ip="${ip%% *}"; ip="${ip%%--*}"; ip="$(echo "$ip" | tr -d ' ')" ;;
      esac
      if [[ -n "$iface" && -n "$ip" && "$ip" != 127.* && "$ip" != 169.254.* ]]; then
        cands+=("$iface|$ip"); iface=""; ip=""
      fi
    done < <(ifconfig 2>/dev/null)
  fi
  local n=${#cands[@]}
  echo ""
  local i=1
  for c in "${cands[@]}"; do
    local iface="${c%%|*}" ip="${c##*|}" nota=""
    if [[ "$ip" == 100.* || "$iface" == *netbird* ]]; then nota=" · NetBird"; fi
    echo "   $i) $ip ($iface$nota)"
    i=$((i+1))
  done
  echo "   $((n+1)) hostname: $def — solo resuelve en LAN/mDNS"
  echo "   $((n+2)) otro nombre DNS o IP (introducirlo)"
  local choice
  read -r -p "Publicar en [1-$((n+2)), Enter=hostname]: " choice || choice=""
  choice="${choice// /}"
  if [[ "$choice" =~ ^[0-9]+$ ]] && (( choice >= 1 && choice <= n )); then
    PUBLIC_HOST="${cands[$((choice-1))]##*|}"
  elif [[ "$choice" == "$((n+2))" ]]; then
    read -r -p "Nombre DNS o IP: " PUBLIC_HOST || true
  else
    PUBLIC_HOST="$def"
  fi
  if [[ -z "${PUBLIC_HOST:-}" ]]; then PUBLIC_HOST="$def"; fi
  echo "  → publicando como: $PUBLIC_HOST"
}
PUBLIC_HOST="${BDR_PUBLIC_HOST:-}"
pick_public_host
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
    <string>--downloads-dir</string><string>/opt/bdr/agents</string>
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
    # Arranque verificado: si el bootstrap falla (EIO con un servicio
    # cargado previamente…) se intenta el load heredado, y si el servicio
    # tampoco responde, el instalador FALLA — no imprime éxito sin más.
    if ! launchctl bootstrap system /Library/LaunchDaemons/local.bdr.control.plist 2>&1; then
      launchctl load -w /Library/LaunchDaemons/local.bdr.control.plist
    fi
    for _ in $(seq 1 10); do
      curl -fsS -m 2 http://127.0.0.1:$PORT/healthz >/dev/null 2>&1 && break
      sleep 1
    done
    if curl -fsS -m 2 http://127.0.0.1:$PORT/healthz >/dev/null 2>&1; then
      echo "servicio local.bdr.control en marcha (launchd)"
    else
      echo "ERROR: el servicio local.bdr.control no responde — revisa /Library/Application Support/BDR/control.log" >&2
      exit 1
    fi
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
ExecStart=/opt/bdr/bin/bdr-control --addr 0.0.0.0:8080 --agent-addr 0.0.0.0:8443 --data-dir /var/lib/bdr/control --downloads-dir /opt/bdr/agents --kopia-binary /opt/bdr/bin/kopia
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

#!/usr/bin/env bash
# =============================================================================
# uninstall-agent.sh — desinstala bdr-agent en Linux (systemd) y macOS (launchd).
#
# Por defecto SOLO detiene el servicio y elimina el binario: conserva la
# configuración (agent.json con la API key), la caché de Kopia y los datos
# de restauración del agente. Con --purge borra además esos datos.
# Las copias de seguridad en el repositorio del servidor NO se tocan nunca:
# para eliminarlas, usa el panel (revocar agente / limpiar restore points).
#
# Uso:
#   sudo bash uninstall-agent.sh            # conserva el data-dir
#   sudo bash uninstall-agent.sh --purge    # borra también el data-dir
#   sudo bash uninstall-agent.sh --purge --yes   # sin confirmación
#
# Variables de entorno (iguales que en la instalación):
#   BDR_INSTALL_DIR  directorio del binario  (default /usr/local/bin)
#   BDR_DATA_DIR     data-dir del agente     (Linux: /var/lib/bdr-agent;
#                                            macOS: /Library/Application Support/BDR)
#
# Si el agente se instaló con el paquete .deb, el binario y la unidad de
# systemd se gestionan con dpkg: este script los detecta y te indica el
# comando (`sudo apt remove --purge bdr-agent`).
# =============================================================================
set -euo pipefail

PURGE=0
ASSUME_YES=0
while [[ $# -gt 0 ]]; do
  case "$1" in
    --purge) PURGE=1; shift ;;
    --yes|-y) ASSUME_YES=1; shift ;;
    *) echo "opción desconocida: $1 (uso: --purge --yes)" >&2; exit 2 ;;
  esac
done
[[ $EUID -eq 0 ]] || { echo "ejecuta como root (sudo)" >&2; exit 1; }

OS=$(uname -s)
case "$OS" in
  Linux)  PLATFORM_OS=linux ;;
  Darwin) PLATFORM_OS=darwin ;;
  *) echo "sistema no soportado: $OS" >&2; exit 1 ;;
esac
BIN_DIR="${BDR_INSTALL_DIR:-/usr/local/bin}"
if [[ $PLATFORM_OS = linux ]]; then
  DATA_DIR="${BDR_DATA_DIR:-/var/lib/bdr-agent}"
  AGENT_USER=bdr-agent
else
  DATA_DIR="${BDR_DATA_DIR:-/Library/Application Support/BDR}"
fi

DEB_FILES=()

# rm_file borra un fichero salvo que pertenezca a un paquete .deb (dpkg):
# en ese caso lo apunta para indicar la desinstalación correcta.
rm_file() {
  local p="$1"
  if [[ ! -e "$p" ]]; then return 0; fi
  if command -v dpkg >/dev/null 2>&1 && dpkg -S "$p" >/dev/null 2>&1; then
    DEB_FILES+=("$p")
    return 0
  fi
  rm -f "$p"
}

confirm_purge() {
  if [[ $ASSUME_YES -eq 1 ]]; then return 0; fi
  echo ""
  echo "aviso: --purge borrará de forma IRREVERSIBLE los datos del agente:"
  echo "  $DATA_DIR  (agent.json con la API key, caché de Kopia, staging)"
  local reply=""
  if [[ -t 0 ]]; then
    read -r -p "¿Continuar? Escribe SI para confirmar: " reply || reply=""
  elif [[ -e /dev/tty ]]; then
    read -r -p "¿Continuar? Escribe SI para confirmar: " reply < /dev/tty || reply=""
  else
    echo "sin terminal para confirmar: aborta, o re-ejecuta con --yes" >&2
    return 1
  fi
  # tr y no ${reply,,}: bash 3.2 de macOS no soporta la expansión.
  reply=$(printf '%s' "$reply" | tr '[:upper:]' '[:lower:]')
  case "$reply" in
    si|sí|Sí|sI|SÍ|y|yes) return 0 ;;
    *) return 1 ;;
  esac
}

echo "== deteniendo bdr-agent…"
if [[ $PLATFORM_OS = linux ]]; then
  if command -v systemctl >/dev/null 2>&1; then
    systemctl stop bdr-agent 2>/dev/null || true
    systemctl disable bdr-agent 2>/dev/null || true
  fi
else
  launchctl bootout system/local.bdr.agent 2>/dev/null || true
fi
# Instalaciones sin servicio (--no-service): si el proceso sigue vivo, se para.
pkill -x bdr-agent 2>/dev/null || true
sleep 1
pkill -9 -x bdr-agent 2>/dev/null || true

echo "== eliminando servicio y binario…"
if [[ $PLATFORM_OS = linux ]]; then
  # Unidades: la del panel/manual en /etc y la del .deb en /lib|/usr/lib.
  for u in /etc/systemd/system/bdr-agent.service \
           /lib/systemd/system/bdr-agent.service \
           /usr/lib/systemd/system/bdr-agent.service; do
    rm_file "$u"
  done
  if command -v systemctl >/dev/null 2>&1; then
    systemctl daemon-reload 2>/dev/null || true
    systemctl reset-failed bdr-agent 2>/dev/null || true
  fi
else
  rm_file /Library/LaunchDaemons/local.bdr.agent.plist
fi
rm_file "$BIN_DIR/bdr-agent"
if [[ "$BIN_DIR" != /usr/local/bin && "$BIN_DIR" != /usr/bin ]]; then
  # Directorios de instalación no estándar: quitar solo si quedó vacío.
  if [[ -d "$BIN_DIR" ]] && [[ -z "$(ls -A "$BIN_DIR")" ]]; then
    rmdir "$BIN_DIR"
  fi
fi
if [[ ${#DEB_FILES[@]} -gt 0 ]]; then
  echo ""
  echo "aviso: estos ficheros pertenecen al paquete .deb de bdr-agent;"
  echo "       desinstálalo con el gestor de paquetes para completar:"
  echo "         sudo apt remove --purge bdr-agent"
fi

if [[ $PURGE -eq 1 ]]; then
  if confirm_purge; then
    echo "== purgando datos del agente…"
    if [[ $PLATFORM_OS = linux ]]; then
      rm -rf "$DATA_DIR"
    else
      # En macOS el data-dir por defecto se comparte con bdr-control
      # (el estado del control vive en <dir>/control): solo se borran los
      # ficheros del agente y el directorio queda si tiene algo más.
      for p in agent.json agent.log kopia restore-state lxc-staging tmp; do
        if [[ -e "$DATA_DIR/$p" ]]; then rm -rf "$DATA_DIR/$p"; fi
      done
      if [[ -d "$DATA_DIR" ]] && [[ -z "$(ls -A "$DATA_DIR")" ]]; then
        rmdir "$DATA_DIR"
      fi
    fi
    if [[ $PLATFORM_OS = linux ]]; then
      # Solo si el agente tenía usuario de servicio (.deb o install.sh).
      if getent passwd "$AGENT_USER" >/dev/null 2>&1; then
        userdel "$AGENT_USER" 2>/dev/null || true
      fi
    fi
  else
    echo "purgado cancelado: los datos se conservan en $DATA_DIR"
  fi
fi

echo ""
echo "== desinstalación completa =="
if [[ -d "$DATA_DIR" ]]; then
  echo "conservado: $DATA_DIR (agent.json, caché de Kopia…)"
  echo "            bórralo a mano o re-ejecuta con --purge"
fi
echo "nota: el equipo sigue aparecido en el panel — revócalo desde"
echo "      Equipos → <host> → Revocar para limpiar su estado en el control."

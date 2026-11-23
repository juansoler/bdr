#!/usr/bin/env bash
# =============================================================================
# uninstall-control.sh — desinstala bdr-control en Linux (systemd) y macOS (launchd).
#
# Por defecto SOLO detiene el servicio y elimina binarios: conserva el estado
# (/var/lib/bdr o /Library/Application Support/BDR), los secretos (/etc/bdr)
# y el repositorio Kopia (/opt/bdr/repo), que contiene las copias de
# seguridad. Con --purge lo borra TODO, backups incluidos (irreversible).
#
# Uso:
#   sudo bash uninstall-control.sh                # conserva estado, secretos y repo
#   sudo bash uninstall-control.sh --purge        # borra todo, INCLUIDOS LOS BACKUPS
#   sudo bash uninstall-control.sh --purge --yes  # sin confirmación
#
# Variables de entorno (iguales que en la instalación):
#   BDR_DATA_DIR   data-dir del control  (Linux: /var/lib/bdr;
#                                        macOS: /Library/Application Support/BDR)
#
# Antes de desinstalar: revoca o deja anotados los agentes que apuntaban a
# este servidor — al desaparecer el control, dejarán de poder hacer backups
# hasta que se reinstale o se reasignen a otro servidor.
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
  Linux)  PLATFORM_OS=linux; DATA_DIR="${BDR_DATA_DIR:-/var/lib/bdr}"; SVC_USER=bdr ;;
  Darwin) PLATFORM_OS=darwin; DATA_DIR="${BDR_DATA_DIR:-/Library/Application Support/BDR}" ;;
  *) echo "sistema no soportado: $OS" >&2; exit 1 ;;
esac
BIN_DIR=/opt/bdr/bin
AGENTS_DIR=/opt/bdr/agents
REPO_DIR=/opt/bdr/repo
ETC_DIR=/etc/bdr

confirm_purge() {
  if [[ $ASSUME_YES -eq 1 ]]; then return 0; fi
  echo ""
  echo "aviso: --purge borrará de forma IRREVERSIBLE:"
  echo "  $REPO_DIR   ← TODAS LAS COPIAS DE SEGURIDAD del repositorio local"
  echo "  $DATA_DIR   estado del plano de control (equipos, planes, auditoría)"
  echo "  $ETC_DIR    secretos (BDR_ADMIN_TOKEN, clave del repositorio Kopia)"
  echo "Si el repositorio es externo (NAS/S3), esta purga NO borra esos datos;"
  echo "limípalos aparte con el CLI de Kopia o el panel ANTES de purgar."
  local reply=""
  if [[ -t 0 ]]; then
    read -r -p "Escribe PURGAR para confirmar: " reply || reply=""
  elif [[ -e /dev/tty ]]; then
    read -r -p "Escribe PURGAR para confirmar: " reply < /dev/tty || reply=""
  else
    echo "sin terminal para confirmar: aborta, o re-ejecuta con --yes" >&2
    return 1
  fi
  [[ "$reply" = "PURGAR" ]]
}

echo "== deteniendo bdr-control…"
if [[ $PLATFORM_OS = linux ]]; then
  if command -v systemctl >/dev/null 2>&1; then
    systemctl stop bdr-control 2>/dev/null || true
    systemctl disable bdr-control 2>/dev/null || true
  fi
else
  launchctl bootout system/local.bdr.control 2>/dev/null || true
fi
pkill -x bdr-control 2>/dev/null || true
sleep 1
pkill -9 -x bdr-control 2>/dev/null || true

echo "== eliminando servicio y binarios…"
if [[ $PLATFORM_OS = linux ]]; then
  if [[ -e /etc/systemd/system/bdr-control.service ]]; then
    rm -f /etc/systemd/system/bdr-control.service
  fi
  if command -v systemctl >/dev/null 2>&1; then
    systemctl daemon-reload 2>/dev/null || true
    systemctl reset-failed bdr-control 2>/dev/null || true
  fi
else
  if [[ -e /Library/LaunchDaemons/local.bdr.control.plist ]]; then
    rm -f /Library/LaunchDaemons/local.bdr.control.plist
  fi
fi
rm -rf "$BIN_DIR" "$AGENTS_DIR"

if [[ $PURGE -eq 1 ]]; then
  if confirm_purge; then
    echo "== purgando estado, secretos y repositorio…"
    rm -rf "$DATA_DIR" "$ETC_DIR" "$REPO_DIR"
    if [[ $PLATFORM_OS = linux ]]; then
      # Solo se borra el usuario si lo creó el instalador (home=/var/lib/bdr):
      # un usuario bdr preexistente ajeno a BDR no se toca.
      if getent passwd "$SVC_USER" >/dev/null 2>&1; then
        if getent passwd "$SVC_USER" | cut -d: -f6 | grep -qx "/var/lib/bdr"; then
          userdel "$SVC_USER" 2>/dev/null || true
        fi
      fi
    fi
  else
    echo "purgado cancelado: estado, secretos y repositorio se conservan"
  fi
fi

# /opt/bdr queda si conservó el repositorio; si quedó vacío, fuera.
if [[ -d /opt/bdr ]] && [[ -z "$(ls -A /opt/bdr)" ]]; then
  rmdir /opt/bdr
fi

echo ""
echo "== desinstalación completa =="
preserved=0
if [[ -d "$REPO_DIR" ]]; then
  echo "  repositorio con backups: $REPO_DIR"
  preserved=1
fi
if [[ -d "$DATA_DIR" ]]; then
  echo "  estado del control:      $DATA_DIR"
  preserved=1
fi
if [[ -d "$ETC_DIR" ]]; then
  echo "  secretos (bdr.env):      $ETC_DIR"
  preserved=1
fi
if [[ $preserved -eq 1 ]]; then
  echo "conservado (borra a mano o re-ejecuta con --purge)"
fi
echo "nota: si publicabas el panel tras un proxy TLS (Caddy) o reglas de"
echo "      firewall propias, limpia también esa configuración."

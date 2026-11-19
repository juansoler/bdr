#!/usr/bin/env bash
# Utilidades comunes del laboratorio. Se usa con `source`.
# No contiene secretos ni modifica el sistema por sí sola.

set -euo pipefail

log() {
  printf '[%s] %s\n' "$(date -u +%H:%M:%SZ)" "$*" >&2
}

die() {
  log "ERROR: $*"
  exit 1
}

require_cmd() {
  local c
  for c in "$@"; do
    command -v "$c" >/dev/null 2>&1 || die "falta el comando '$c'"
  done
}

require_env() {
  local n
  for n in "$@"; do
    [[ -n "${!n:-}" ]] || die "define la variable de entorno '$n'"
  done
}

# Oculta valores sensibles en la salida de comandos.
redact() {
  sed -E 's/(token|password|secret|access-key|secret-access-key|api[_-]?key)=([^ &]+)/\1=***/gi'
}

sha256_file() {
  local f=$1
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$f" | awk '{print $1}'
  elif command -v shasum >/dev/null 2>&1; then
    shasum -a 256 "$f" | awk '{print $1}'
  else
    die "no hay sha256sum ni shasum"
  fi
}

# Ejecuta un comando y guarda salida y código en el directorio de evidencia.
run_capture() {
  local out=$1
  shift
  mkdir -p "$(dirname "$out")"
  log "ejecutando: $*"
  {
    printf '$ %s\n' "$*"
    set +e
    "$@" 2>&1
    printf '\n[exit=%d]\n' "$?"
    set -e
  } | tee "$out"
}

timestamp_utc() {
  date -u +%Y-%m-%dT%H:%M:%SZ
}

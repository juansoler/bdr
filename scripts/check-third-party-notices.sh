#!/usr/bin/env bash
# Comprueba que THIRD_PARTY_NOTICES.md cubre todas las dependencias Go
# enlazadas en los binarios del producto. Evita que una dependencia nueva
# llegue a un release sin su aviso de licencia (RNF-12).
set -euo pipefail
cd "$(dirname "$0")/.."

NOTICES=THIRD_PARTY_NOTICES.md
[[ -f "$NOTICES" ]] || { echo "falta $NOTICES" >&2; exit 1; }

missing=0
# Unión de las plataformas que se publican: un módulo puede entrar solo en
# Linux, Darwin o Windows (p. ej. xgb solo en Linux).
mods=$(
  for target in linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 windows/amd64; do
    GOOS=${target%/*} GOARCH=${target#*/} \
      go list -deps -f '{{with .Module}}{{if not .Main}}{{.Path}} {{.Version}} {{.Dir}}{{end}}{{end}}' \
      ./apps/server/cmd/bdr-control ./apps/agent/cmd/bdr-agent ./cmd/bdr-sql
  done | sed '/^$/d' | sort -u
)
while read -r mod _ver _dir; do
  [[ -n "$mod" ]] || continue
  if ! grep -qF "$mod" "$NOTICES"; then
    echo "sin aviso de terceros: $mod" >&2
    missing=1
  fi
done <<< "$mods"

# El runtime de Go va embebido en los binarios y también debe figurar.
if ! grep -q "Runtime de Go" "$NOTICES"; then
  echo "falta el aviso del runtime de Go en $NOTICES" >&2
  missing=1
fi

if ((missing)); then
  echo "actualiza $NOTICES con los componentes que faltan" >&2
  exit 1
fi
echo "avisos de terceros: OK"

#!/usr/bin/env bash
# Comprueba que las evidencias de Fase 0 están completas.
# Uso: ./labs/gate-check.sh [directorio-de-evidencia]

set -euo pipefail

HERE=$(cd "$(dirname "$0")" && pwd)
ROOT=$(cd "$HERE/.." && pwd)
EVIDENCE=${1:-"$HERE/evidence"}

fail=0
printf '%-6s %-10s %s\n' "ID" "ESTADO" "ARCHIVO"
printf '%-6s %-10s %s\n' "------" "----------" "---------"

for n in 01 02 03 04 05 06 07 08 09; do
  id="G${n}"
  file=$(find "$EVIDENCE" -maxdepth 2 -type f -name "${id}.md" -print -quit 2>/dev/null || true)
  if [[ -z "$file" ]]; then
    printf '%-6s %-10s %s\n' "$id" "FALTA" "${id}.md"
    fail=1
    continue
  fi

  if grep -qiE '^\*\*Resultado:\*\*[[:space:]]*pendiente' "$file"; then
    printf '%-6s %-10s %s\n' "$id" "PENDIENTE" "$file"
    fail=1
    continue
  fi

  if ! grep -qE '^[[:space:]]*- \[x\]' "$file"; then
    printf '%-6s %-10s %s\n' "$id" "SIN DECISIÓN" "$file"
    fail=1
    continue
  fi

  printf '%-6s %-10s %s\n' "$id" "OK" "$file"
done

if [[ $fail -eq 1 ]]; then
  echo
  echo "Puerta de Fase 0 ABIERTA: faltan evidencias o decisiones."
  exit 1
fi

echo
echo "Puerta de Fase 0 CERRADA con las 9 evidencias."
echo "Actualiza docs/decisiones.md y docs/poc-restauracion.md antes de pasar a Fase 1."

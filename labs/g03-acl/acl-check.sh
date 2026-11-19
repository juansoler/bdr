#!/usr/bin/env bash
# G0.3 — Comprueba el aislamiento de ACL del Repository Server.
# Requiere dos configuraciones de cliente ya conectadas:
#   CFG_A  cliente del agente A
#   CFG_B  cliente del agente B
# y un snapshot de B que A no debería poder listar/restaurar.
#
# Uso:
#   CFG_A=/tmp/a.config CFG_B=/tmp/b.config B_SNAPSHOT_ID=k... ./acl-check.sh
#
# No modifica el repositorio salvo un snapshot de prueba creado por A.

set -euo pipefail

HERE=$(cd "$(dirname "$0")" && pwd)
# shellcheck source=../common/lib.sh
source "$HERE/../common/lib.sh"

require_cmd kopia jq
require_env CFG_A CFG_B B_SNAPSHOT_ID

OUT=${OUT:-"$HERE/../evidence/$(date +%Y-%m-%d)"}
mkdir -p "$OUT"
WORK=$(mktemp -d)
trap 'rm -rf "$WORK"' EXIT

log "1) A lista sus snapshots"
kopia snapshot list --config-file="$CFG_A" --json >"$OUT/g03-a-list.json" 2>"$OUT/g03-a-list.err" || true
A_COUNT=$(jq 'length' "$OUT/g03-a-list.json" 2>/dev/null || echo "error")

log "2) A intenta listar snapshots de B (por etiqueta o por ID)"
kopia snapshot list --config-file="$CFG_A" --json >"$OUT/g03-a-all.json" 2>&1 || true
if jq -e --arg id "$B_SNAPSHOT_ID" '.[] | select(.id == $id)' "$OUT/g03-a-all.json" >/dev/null 2>&1; then
  echo "VISIBLE: A puede ver el snapshot de B" >"$OUT/g03-a-sees-b.txt"
else
  echo "NO VISIBLE: A no puede ver el snapshot de B (o el JSON no es una lista)" >"$OUT/g03-a-sees-b.txt"
fi

log "3) A intenta restaurar el snapshot de B"
set +e
kopia snapshot restore "$B_SNAPSHOT_ID" "$WORK/restore-b" \
  --config-file="$CFG_A" >"$OUT/g03-a-restore-b.txt" 2>&1
RC_RESTORE=$?
set -e
printf 'exit=%d\n' "$RC_RESTORE" >>"$OUT/g03-a-restore-b.txt"

log "4) A crea un snapshot propio"
mkdir -p "$WORK/a-data"
printf 'agent-a %s\n' "$(date -u +%FT%TZ)" >"$WORK/a-data/a.txt"
set +e
kopia snapshot create "$WORK/a-data" --config-file="$CFG_A" --json \
  --tag=bdr:lab=g03 --tag=bdr:agent=a >"$OUT/g03-a-create.json" 2>&1
RC_CREATE=$?
set -e
printf 'exit=%d\n' "$RC_CREATE" >"$OUT/g03-a-create.exit"

cat >"$OUT/g03-summary.md" <<EOF
# G0.3 — Resumen automático de ACL

- Fecha: $(timestamp_utc)
- Snapshots visibles para A: $A_COUNT
- Snapshot de B solicitado: $B_SNAPSHOT_ID
- A puede verlo: $(cat "$OUT/g03-a-sees-b.txt")
- Código de restauración de B por A: $RC_RESTORE
- Código de creación de A: $RC_CREATE

## Interpretación

- Si A **no** ve ni restaura B y **sí** crea su propio snapshot, la ACL básica aísla manifiestos.
- Si A ve o restaura B, el aislamiento no es suficiente: hay que aceptar el dominio de confianza o separar repositorios.
- Las ACL de Kopia no aíslan contenidos por content ID; esta prueba solo cubre manifiestos y políticas.
EOF

log "resumen: $OUT/g03-summary.md"

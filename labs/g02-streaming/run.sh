#!/usr/bin/env bash
# G0.2 — Comportamiento de `kopia snapshot create --stdin-file`.
# Crea un repositorio temporal, genera datos y prueba corte + reanudación.
#
# Uso:
#   SIZE_MB=256 FORM=a ./labs/g02-streaming/run.sh
#   FORM=b SIZE_MB=64 ./labs/g02-streaming/run.sh   # prueba con argumento de origen
#
# Requisitos: kopia, jq, dd, timeout (Linux/WSL).

set -euo pipefail

HERE=$(cd "$(dirname "$0")" && pwd)
# shellcheck source=../common/lib.sh
source "$HERE/../common/lib.sh"

require_cmd kopia jq dd timeout

SIZE_MB=${SIZE_MB:-256}
FORM=${FORM:-a}
OUT=${OUT:-"$HERE/../evidence/$(date +%Y-%m-%d)"}
mkdir -p "$OUT"

WORK=$(mktemp -d)
trap 'rm -rf "$WORK"' EXIT

CONFIG="$WORK/kopia.config"
REPO="$WORK/repo"
DATA="$WORK/data.bin"

if [[ -z "${KOPIA_PASSWORD:-}" ]]; then
  KOPIA_PASSWORD=$(openssl rand -base64 24 2>/dev/null || date +%s%N)
  export KOPIA_PASSWORD
  log "KOPIA_PASSWORD generada para este experimento; no se guarda en evidencia"
fi

log "WORK=$WORK FORM=$FORM SIZE_MB=$SIZE_MB"
log "creando repositorio temporal"
kopia repository create filesystem --path="$REPO" --config-file="$CONFIG" >/dev/null

log "generando $SIZE_MB MiB de datos"
dd if=/dev/urandom of="$DATA" bs=1M count="$SIZE_MB" status=none
sha256_file "$DATA" >"$OUT/g02-source.sha256"
printf '%s\n' "$(cat "$OUT/g02-source.sha256")  data.bin" >"$OUT/g02-source.sha256"

run_stream() {
  local label=$1
  local timeout_s=$2
  local outfile=$3
  local rc=0

  log "fase $label (timeout ${timeout_s}s)"
  set +e
  if [[ "$FORM" == "a" ]]; then
    cat "$DATA" | timeout -s KILL "$timeout_s" kopia snapshot create \
      --config-file="$CONFIG" \
      --json \
      --description="lab=g02 phase=$label" \
      --tags bdr:lab=g02,bdr:phase="$label",bdr:seg=0 \
      --stdin-file=disk0.seg0 >"$outfile" 2>&1
  else
    timeout -s KILL "$timeout_s" kopia snapshot create "$DATA" \
      --config-file="$CONFIG" \
      --json \
      --description="lab=g02 phase=$label" \
      --tags bdr:lab=g02,bdr:phase="$label",bdr:seg=0 \
      --stdin-file=disk0.seg0 >"$outfile" 2>&1
  fi
  rc=$?
  set -e
  printf '%d\n' "$rc" >"$outfile.exit"
  log "fase $label terminó con código $rc"
}

run_stream "A_interrupted" 10 "$OUT/g02-phase-a.json"
run_stream "B_resume" 900 "$OUT/g02-phase-b.json"

log "recolectando estado"
kopia snapshot list --config-file="$CONFIG" --json >"$OUT/g02-snapshots.json" || true
kopia repository status --config-file="$CONFIG" >"$OUT/g02-repo-status.txt" 2>&1 || true
kopia repository status --config-file="$CONFIG" --json >"$OUT/g02-repo-status.json" 2>&1 || true
du -sb "$REPO" >"$OUT/g02-repo-size.txt" 2>/dev/null || du -sk "$REPO" >"$OUT/g02-repo-size.txt"

SNAPS=$(jq 'length' "$OUT/g02-snapshots.json" 2>/dev/null || echo "?")
REPO_BYTES=$(awk '{print $1}' "$OUT/g02-repo-size.txt" 2>/dev/null || echo "?")
RC_A=$(cat "$OUT/g02-phase-a.json.exit" 2>/dev/null || echo "?")
RC_B=$(cat "$OUT/g02-phase-b.json.exit" 2>/dev/null || echo "?")

cat >"$OUT/g02-summary.md" <<EOF
# G0.2 — Resumen automático

- Fecha: $(timestamp_utc)
- FORM: $FORM
- Tamaño de datos: $SIZE_MB MiB
- Código fase A (corte): $RC_A
- Código fase B (reanudación): $RC_B
- Snapshots tras B: $SNAPS
- Tamaño del repositorio: $REPO_BYTES bytes
- SHA-256 del origen: $(cat "$OUT/g02-source.sha256" 2>/dev/null || echo n/a)

## Qué mirar

1. ¿La fase A dejó un snapshot parcial o ninguno?
2. ¿La fase B volvió a subir todo el flujo o solo lo que faltaba?
3. ¿Kopia informó de checkpoint o reanudación?
4. ¿El repositorio quedó consistente (\`kopia snapshot verify\`)?
5. ¿El tiempo de B se acerca al de una subida completa o a la parte restante?

Completa \`G02.md\` con la plantilla de evidencia y adjunta estos archivos.
EOF

log "resumen: $OUT/g02-summary.md"

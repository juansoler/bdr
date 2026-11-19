#!/usr/bin/env bash
# G0.9 — Rendimiento base de Kopia.
# Crea un repositorio temporal, mide backup completo, segundo backup sin cambios
# y segundo backup con ~5 % de cambios.
#
# Uso: SIZE_MB=1024 ./labs/g09-performance/run.sh
# Requisitos: kopia, jq, dd, du (Linux/WSL).

set -euo pipefail

HERE=$(cd "$(dirname "$0")" && pwd)
# shellcheck source=../common/lib.sh
source "$HERE/../common/lib.sh"

require_cmd kopia jq dd du

SIZE_MB=${SIZE_MB:-1024}
CHANGE_MB=${CHANGE_MB:-$((SIZE_MB / 20))}
OFFSET_MB=${OFFSET_MB:-$((SIZE_MB / 4))}
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
fi

kopia repository create filesystem --path="$REPO" --config-file="$CONFIG" >/dev/null
dd if=/dev/urandom of="$DATA" bs=1M count="$SIZE_MB" status=none

measure() {
  local label=$1
  local out_json=$2
  local start end rc
  start=$(date +%s%N)
  set +e
  kopia snapshot create "$DATA" \
    --config-file="$CONFIG" \
    --json \
    --description="lab=g09 phase=$label" \
    --tag=bdr:lab=g09 --tag=bdr:phase="$label" \
    >"$out_json" 2>"$out_json.err"
  rc=$?
  set -e
  end=$(date +%s%N)
  printf '%s\n' "$(( (end - start) / 1000000 ))" >"$out_json.ms"
  printf '%s\n' "$rc" >"$out_json.exit"
  printf '%s\n' "$(du -sb "$REPO" | awk '{print $1}')" >"$out_json.repo_bytes"
  log "$label: ${out_json}.ms ms, exit $rc"
}

measure "1_full" "$OUT/g09-1-full.json"
measure "2_no_changes" "$OUT/g09-2-no-changes.json"

log "modificando $CHANGE_MB MiB en el offset $OFFSET_MB MiB"
dd if=/dev/urandom of="$DATA" bs=1M count="$CHANGE_MB" seek="$OFFSET_MB" conv=notrunc status=none
measure "3_five_percent" "$OUT/g09-3-five-percent.json"

kopia snapshot list --config-file="$CONFIG" --json >"$OUT/g09-snapshots.json" || true
kopia repository status --config-file="$CONFIG" --json >"$OUT/g09-repo-status.json" || true

B1=$(cat "$OUT/g09-1-full.json.repo_bytes")
B2=$(cat "$OUT/g09-2-no-changes.json.repo_bytes")
B3=$(cat "$OUT/g09-3-five-percent.json.repo_bytes")
T1=$(cat "$OUT/g09-1-full.json.ms")
T2=$(cat "$OUT/g09-2-no-changes.json.ms")
T3=$(cat "$OUT/g09-3-five-percent.json.ms")

cat >"$OUT/g09-summary.md" <<EOF
# G0.9 — Resumen automático

- Fecha: $(timestamp_utc)
- Datos: $SIZE_MB MiB; cambio: $CHANGE_MB MiB en offset $OFFSET_MB MiB
- Backup 1 (completo): ${T1} ms, repo ${B1} bytes
- Backup 2 (sin cambios): ${T2} ms, repo ${B2} bytes
- Backup 3 (~5 % cambios): ${T3} ms, repo ${B3} bytes
- Crecimiento tras backup 2: $((B2 - B1)) bytes
- Crecimiento tras backup 3: $((B3 - B2)) bytes

## Qué mirar

1. El backup 2 no debe aumentar apreciablemente el repositorio.
2. El backup 3 debe crecer solo por los chunks nuevos.
3. La diferencia de tiempos muestra el coste de releer el disco.
4. Añade RAM máxima y CPU con \`/usr/bin/time -v\` y \`pidstat\`.
EOF

log "resumen: $OUT/g09-summary.md"

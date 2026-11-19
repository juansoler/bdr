#!/usr/bin/env bash
# Captura de entorno Linux (solo lectura).
# Uso: ./labs/common/capture-env.sh <directorio-de-evidencia>

set -euo pipefail

OUT=${1:-}
[[ -n "$OUT" ]] || { echo "uso: $0 <directorio-de-evidencia>" >&2; exit 2; }
mkdir -p "$OUT"

TS=$(date -u +%Y-%m-%dT%H:%M:%SZ)
TXT="$OUT/env-linux.txt"

{
  echo "=== BDR lab environment ==="
  echo "timestamp_utc: $TS"
  echo "hostname: $(hostname 2>/dev/null || echo unknown)"
  echo "uname: $(uname -a 2>/dev/null || echo unknown)"
  echo "user: $(id 2>/dev/null || echo unknown)"
  echo

  echo "=== versions ==="
  for c in kopia go jq curl aws mc ssss-split pveversion; do
    if command -v "$c" >/dev/null 2>&1; then
      case "$c" in
        kopia) printf '%s: ' "$c"; kopia --version 2>&1 | head -1 ;;
        go) printf '%s: ' "$c"; go version 2>&1 ;;
        pveversion) printf '%s: ' "$c"; pveversion 2>&1 | head -1 ;;
        *) printf '%s: %s\n' "$c" "$(command -v "$c")" ;;
      esac
    else
      echo "$c: NOT FOUND"
    fi
  done
  echo

  echo "=== os release ==="
  cat /etc/os-release 2>/dev/null || echo "no /etc/os-release"
  echo

  echo "=== block devices (lsblk) ==="
  lsblk -o NAME,SIZE,TYPE,FSTYPE,MOUNTPOINT,MODEL,SERIAL 2>/dev/null || echo "lsblk no disponible"
  echo

  echo "=== partitions (sfdisk) ==="
  for d in /dev/sd? /dev/vd? /dev/nvme?n?; do
    [[ -b "$d" ]] || continue
    echo "--- $d"
    sfdisk -d "$d" 2>/dev/null || echo "sfdisk no disponible o sin permiso"
  done
  echo

  echo "=== lvm ==="
  command -v pvs >/dev/null 2>&1 && { pvs 2>&1; vgs 2>&1; lvs 2>&1; } || echo "LVM no disponible"
  echo

  echo "=== btrfs ==="
  command -v btrfs >/dev/null 2>&1 && btrfs filesystem show 2>&1 || echo "btrfs no disponible"
  echo

  echo "=== zfs ==="
  command -v zfs >/dev/null 2>&1 && zfs list 2>&1 || echo "zfs no disponible"
  echo

  echo "=== security ==="
  if [[ -d /sys/firmware/efi ]]; then echo "firmware: uefi"; else echo "firmware: bios"; fi
  if command -v mokutil >/dev/null 2>&1; then mokutil --sb-state 2>&1 || true; fi
  if command -v lsmod >/dev/null 2>&1; then
    echo "--- dkms / snapshot modules"
    lsmod | grep -E 'blksnap|dattobd|dm_era' || echo "ninguno cargado"
  fi
  echo

  echo "=== mounts ==="
  findmnt -no TARGET,SOURCE,FSTYPE,OPTIONS 2>/dev/null || mount
} >"$TXT" 2>&1

echo "evidencia escrita en: $TXT"

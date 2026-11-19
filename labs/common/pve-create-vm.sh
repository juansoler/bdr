#!/usr/bin/env bash
# Crea una VM en Proxmox VE con import-from. Dry-run por defecto.
#
# Ejemplo (no ejecuta nada):
#   PVE_URL=https://pve.example:8006/api2/json \
#   PVE_TOKEN='bdr@pve!lab=00000000-0000-0000-0000-000000000000' \
#   ./pve-create-vm.sh --node pve01 --storage local-zfs --vmid 210 \
#     --name win11-test --ostype win11 --bios ovmf --bus sata --net e1000 \
#     --bridge vmbr0 --disk-path /var/tmp/win11.raw --tpm --secureboot
#
# Añade --apply para ejecutar. Requiere curl.

set -euo pipefail

HERE=$(cd "$(dirname "$0")" && pwd)
# shellcheck source=lib.sh
source "$HERE/lib.sh"

APPLY=0
NODE=""
STORAGE=""
VMID=""
NAME=""
CORES=4
MEMORY=4096
OSTYPE="l26"
BIOS="ovmf"
MACHINE="q35"
BUS="scsi"
NET="virtio"
BRIDGE="vmbr0"
DISK_PATH=""
EFIDISK=""
TPM=0
SECUREBOOT=0
START=0
INSECURE=0

usage() {
  cat <<'EOF'
Uso: pve-create-vm.sh --node N --storage S --vmid ID --name NOMBRE --disk-path RUTA [opciones]

Obligatorio:
  --node, --storage, --vmid, --name, --disk-path

Opciones:
  --cores N            (default 4)
  --memory MiB         (default 4096)
  --ostype TIPO        (default l26; win11 para Windows 11)
  --bios ovmf|seabios  (default ovmf)
  --machine TIPO       (default q35)
  --bus scsi|sata      (default scsi; sata para la primera fase de Windows)
  --net virtio|e1000   (default virtio; e1000 para la primera fase de Windows)
  --bridge NOMBRE      (default vmbr0)
  --efidisk VOLID      (por defecto "<storage>:1,efitype=4m")
  --tpm                (añade tpmstate0 v2.0)
  --secureboot         (pre-enrolled-keys=1 en efidisk0)
  --start              (arranca la VM tras crearla)
  --insecure           (curl -k; solo laboratorio)
  --apply              (ejecuta; sin esto solo imprime el comando)

Entorno:
  PVE_URL    https://host:8006/api2/json
  PVE_TOKEN  user@realm!tokenid=uuid
EOF
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --node) NODE=$2; shift 2 ;;
    --storage) STORAGE=$2; shift 2 ;;
    --vmid) VMID=$2; shift 2 ;;
    --name) NAME=$2; shift 2 ;;
    --cores) CORES=$2; shift 2 ;;
    --memory) MEMORY=$2; shift 2 ;;
    --ostype) OSTYPE=$2; shift 2 ;;
    --bios) BIOS=$2; shift 2 ;;
    --machine) MACHINE=$2; shift 2 ;;
    --bus) BUS=$2; shift 2 ;;
    --net) NET=$2; shift 2 ;;
    --bridge) BRIDGE=$2; shift 2 ;;
    --disk-path) DISK_PATH=$2; shift 2 ;;
    --efidisk) EFIDISK=$2; shift 2 ;;
    --tpm) TPM=1; shift ;;
    --secureboot) SECUREBOOT=1; shift ;;
    --start) START=1; shift ;;
    --insecure) INSECURE=1; shift ;;
    --apply) APPLY=1; shift ;;
    -h|--help) usage; exit 0 ;;
    *) die "argumento desconocido: $1" ;;
  esac
done

require_env PVE_URL PVE_TOKEN
[[ -n "$NODE" && -n "$STORAGE" && -n "$VMID" && -n "$NAME" && -n "$DISK_PATH" ]] || {
  usage
  die "faltan --node, --storage, --vmid, --name o --disk-path"
}

EFIDISK=${EFIDISK:-"$STORAGE:1,efitype=4m,pre-enrolled-keys=$([[ $SECUREBOOT -eq 1 ]] && echo 1 || echo 0)"}

ARGS=(
  --data-urlencode "vmid=$VMID"
  --data-urlencode "name=$NAME"
  --data-urlencode "cores=$CORES"
  --data-urlencode "memory=$MEMORY"
  --data-urlencode "ostype=$OSTYPE"
  --data-urlencode "bios=$BIOS"
  --data-urlencode "machine=$MACHINE"
  --data-urlencode "scsihw=virtio-scsi-pci"
  --data-urlencode "efidisk0=$EFIDISK"
  --data-urlencode "net0=$NET,bridge=$BRIDGE"
  --data-urlencode "serial0=socket"
  --data-urlencode "agent=1"
)

if [[ "$BUS" == "sata" ]]; then
  ARGS+=(--data-urlencode "sata0=$STORAGE:0,import-from=$DISK_PATH")
  ARGS+=(--data-urlencode "boot=order=sata0")
else
  ARGS+=(--data-urlencode "scsi0=$STORAGE:0,import-from=$DISK_PATH")
  ARGS+=(--data-urlencode "boot=order=scsi0")
fi

if [[ $TPM -eq 1 ]]; then
  ARGS+=(--data-urlencode "tpmstate0=$STORAGE:1,version=v2.0")
fi

CURL=(curl -sS)
[[ $INSECURE -eq 1 ]] && CURL+=(-k)
CURL+=(-H "Authorization: PVEAPIToken=$PVE_TOKEN")

if [[ $APPLY -ne 1 ]]; then
  log "DRY-RUN. Comando que se ejecutaría:"
  printf '%s ' "${CURL[@]}" | sed -E 's/(PVEAPIToken=)[^ ]+/\1***/'
  printf '%q ' "${ARGS[@]}"
  printf '%q\n' "$PVE_URL/nodes/$NODE/qemu"
  log "añade --apply para ejecutarlo"
  exit 0
fi

log "creando VM $VMID en $NODE"
RESP=$("${CURL[@]}" "${ARGS[@]}" "$PVE_URL/nodes/$NODE/qemu")
printf '%s\n' "$RESP" | jq . 2>/dev/null || printf '%s\n' "$RESP"

if [[ $START -eq 1 ]]; then
  log "arrancando VM $VMID"
  "${CURL[@]}" -X POST "$PVE_URL/nodes/$NODE/qemu/$VMID/status/start" | jq . 2>/dev/null || true
fi

log "consulta el estado con:"
log "  curl -sS -k -H 'Authorization: PVEAPIToken=***' '$PVE_URL/nodes/$NODE/qemu/$VMID/status/current'"

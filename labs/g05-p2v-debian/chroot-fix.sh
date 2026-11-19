#!/usr/bin/env bash
# Prepara un disco Debian restaurado para arrancar con VirtIO en Proxmox.
# Dry-run por defecto; --apply modifica el disco restaurado.
#
# Uso:
#   sudo ./chroot-fix.sh --root /dev/sda2 --efi /dev/sda1
#   sudo ./chroot-fix.sh --root /dev/sda2 --efi /dev/sda1 --apply
#
# Requiere: root, mount, chroot, update-initramfs o dracut.
# No toca el equipo anfitrión más allá del montaje temporal.

set -euo pipefail

APPLY=0
ROOT_DEV=""
EFI_DEV=""

usage() {
  echo "uso: $0 --root /dev/sdaN [--efi /dev/sdaM] [--apply]" >&2
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --root) ROOT_DEV=$2; shift 2 ;;
    --efi) EFI_DEV=$2; shift 2 ;;
    --apply) APPLY=1; shift ;;
    -h|--help) usage; exit 0 ;;
    *) echo "argumento desconocido: $1" >&2; usage; exit 2 ;;
  esac
done

[[ -n "$ROOT_DEV" ]] || { usage; exit 2; }
[[ $EUID -eq 0 ]] || { echo "requiere root" >&2; exit 1; }

MNT=$(mktemp -d /mnt/bdr-chroot.XXXXXX)

cleanup() {
  set +e
  for d in dev proc sys; do umount "$MNT/$d" 2>/dev/null; done
  [[ -n "$EFI_DEV" ]] && umount "$MNT/boot/efi" 2>/dev/null
  umount "$MNT" 2>/dev/null
  rmdir "$MNT" 2>/dev/null
}
trap cleanup EXIT

if [[ $APPLY -ne 1 ]]; then
  cat <<EOF
DRY-RUN. Se ejecutaría:
  mount $ROOT_DEV $MNT
  mount $EFI_DEV $MNT/boot/efi
  mount --bind /dev $MNT/dev; mount --bind /proc $MNT/proc; mount --bind /sys $MNT/sys
  chroot $MNT:
    añadir virtio_pci, virtio_blk, virtio_scsi a /etc/initramfs-tools/modules
    update-initramfs -u -k all
    update-grub
  revisar /etc/fstab y /etc/network/interfaces o netplan
  umount -R $MNT
Añade --apply para ejecutarlo sobre el disco restaurado.
EOF
  exit 0
fi

mount "$ROOT_DEV" "$MNT"
if [[ -n "$EFI_DEV" ]]; then
  mkdir -p "$MNT/boot/efi"
  mount "$EFI_DEV" "$MNT/boot/efi"
fi
for d in dev proc sys; do mount --bind "/$d" "$MNT/$d"; done

chroot "$MNT" /bin/bash -c '
set -e
if [[ -f /etc/initramfs-tools/modules ]]; then
  for m in virtio_pci virtio_blk virtio_scsi; do
    grep -qxF "$m" /etc/initramfs-tools/modules || echo "$m" >> /etc/initramfs-tools/modules
  done
  update-initramfs -u -k all
elif command -v dracut >/dev/null 2>&1; then
  dracut -f --add-drivers "virtio_pci virtio_blk virtio_scsi"
else
  echo "No se encontró initramfs-tools ni dracut" >&2
  exit 1
fi
if command -v update-grub >/dev/null 2>&1; then update-grub; fi
echo "--- /etc/fstab ---"
cat /etc/fstab
echo "--- configuración de red ---"
ls -l /etc/network/interfaces /etc/netplan 2>/dev/null || true
cat /etc/network/interfaces 2>/dev/null || true
'

echo "Listo. Revisa a mano:"
echo "  - /etc/fstab usa UUID correctos"
echo "  - la interfaz de red (ens18/virtio) coincide con la configuración"
echo "  - la consola serie está en la cmdline (console=ttyS0)"
echo "Desmonta con: umount -R $MNT"

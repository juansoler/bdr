# G0.5 — P2V Debian

**Objetivo:** arrancar una imagen de Debian 12/13 como VM en Proxmox con VirtIO, corregir initramfs, fstab y red, y documentar el procedimiento reproducible.

**Riesgo:** crea VMs y modifica el disco restaurado. Solo laboratorio.

---

## 1. Imagen de origen

- `qemu-img convert -O raw` desde un disco qcow2, o
- `dd` de un disco de laboratorio a `/var/tmp/debian.raw`.

Variantes: ext4 sin LVM y LVM.

---

## 2. Crear la VM

```bash
export PVE_URL=https://pve.example:8006/api2/json
export PVE_TOKEN='bdr@pve!lab=00000000-0000-0000-0000-000000000000'

./labs/common/pve-create-vm.sh \
  --node pve01 --storage local-zfs --vmid 220 \
  --name debian-p2v --ostype l26 --bios ovmf --machine q35 \
  --bus scsi --net virtio --bridge vmbr0 \
  --disk-path /var/tmp/debian.raw
```

Revisa el dry-run y añade `--apply`.

---

## 3. Corregir el arranque

Si el initramfs no incluye VirtIO, el arranque falla con:

```
ALERT! /dev/disk/by-uuid/... does not exist
```

Monta los discos restaurados en el nodo y ejecuta:

```bash
sudo ./labs/g05-p2v-debian/chroot-fix.sh --root /dev/sda2 --efi /dev/sda1
```

Revisa el dry-run. Si es correcto:

```bash
sudo ./labs/g05-p2v-debian/chroot-fix.sh --root /dev/sda2 --efi /dev/sda1 --apply
```

El script:

- monta raíz y EFI;
- hace bind de `/dev`, `/proc` y `/sys`;
- añade `virtio_pci`, `virtio_blk` y `virtio_scsi` al initramfs;
- ejecuta `update-initramfs -u -k all` y `update-grub`;
- muestra `/etc/fstab` y la configuración de red.

Después revisa a mano:

- `/etc/fstab` usa UUID correctos.
- La interfaz de red (`ens18` u otra) coincide con la configuración; si no, prueba `net.ifnames=0` en la cmdline o reescribe la configuración.
- Añade `console=ttyS0` a la cmdline para la consola serie.

---

## 4. Arranque y verificación

```bash
# Consola serie
qm terminal 220

# Dentro de la VM
ip a
systemctl --failed
cat /etc/fstab
```

Criterios:

- Arranca sin intervención en ≥9/10 intentos.
- Red funcional (DHCP o IP fija).
- Sin unidades fallidas.
- Consola serie operativa.

---

## 5. Variantes

| Variante | Qué cambia |
|---|---|
| ext4 sin LVM | Imagen crash-consistent; comprobar journal |
| LVM | Snapshot LVM y arranque |
| Btrfs | Snapshot Btrfs |
| BIOS | SeaBIOS y GRUB legacy |
| UEFI | OVMF y GRUB EFI |

---

## 6. Evidencia

- Comando de creación y estado.
- Consola serie (`qm terminal` o captura).
- `ip a`, `systemctl --failed`, `fstab`.
- Tiempo hasta login.
- Conclusión para `docs/11-restauracion-p2v-proxmox.md` y `docs/08-agente-linux.md`.

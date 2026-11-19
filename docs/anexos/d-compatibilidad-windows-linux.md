# Anexo D — Compatibilidad de sistemas y arranque

**Documento:** anexos/d-compatibilidad-windows-linux  
**Versión:** 1.0

---

## 1. Matriz de sistemas

| Origen | Firmware | FS | Backup imagen | Restore Proxmox | Notas |
|---|---|---|---|---|---|
| Windows 11 | UEFI + Secure Boot + TPM 2.0 | NTFS | Sí | OVMF + vTPM + Secure Boot | BitLocker: aviso |
| Windows 11 | UEFI sin Secure Boot | NTFS | Sí | OVMF | Ajustar claves |
| Windows 11 | BIOS | NTFS | Sí | SeaBIOS | Legacy |
| Windows Server 2025 | UEFI | NTFS/ReFS | Sí | OVMF | Servicios y drivers |
| Windows Server 2025 | BIOS | NTFS | Sí | SeaBIOS | Legacy |
| Debian 12/13 | UEFI | ext4 | Sí, crash-consistent | OVMF + VirtIO | chroot/initramfs |
| Debian 12/13 | UEFI | LVM | Sí, aplicación | OVMF + VirtIO | Snapshot LVM |
| Debian 12/13 | UEFI | Btrfs | Sí, aplicación | OVMF + VirtIO | Snapshot Btrfs |
| Debian 12/13 | UEFI | ZFS | Sí, aplicación | OVMF + VirtIO | Snapshot ZFS |
| Debian 12/13 | BIOS | ext4/LVM | Sí | SeaBIOS | GRUB |
| VM Proxmox | UEFI/BIOS | Varios | PBS | PBS | No agente |

---

## 2. Firmware y arranque

| Elemento | Decisión |
|---|---|
| UEFI | OVMF; `efidisk0` |
| BIOS | SeaBIOS |
| Secure Boot | `pre-enrolled-keys=1` si el origen lo usaba; 0 si claves propias |
| TPM | vTPM 2.0 para Windows 11 |
| Machine | `q35` para Windows moderno |
| SCSI | `virtio-scsi-pci` |
| Red | `virtio` en Linux; `e1000` en fase A Windows |
| Consola | `serial0: socket`, `console=ttyS0` en Linux |

---

## 3. Windows

| Tema | Comportamiento | Acción |
|---|---|---|
| VirtIO | Windows no trae drivers | SATA+e1000 primero; instalar VirtIO; cambiar bus |
| BitLocker | vTPM nuevo rompe el sellado | Avisar; clave de recuperación o reinstalar sin cifrado |
| Secure Boot | Claves propias pueden fallar | Detectar; no forzar `pre-enrolled-keys=1` |
| BCD | Puede requerir reparación | `bcdboot`/`bootrec` desde WinPE |
| Storage Spaces | No soportado | Abortar en preflight |
| Volúmenes dinámicos | No soportado | Abortar en preflight |
| Hello/PIN | Ligado al TPM | Re-enrolar tras P2V |

---

## 4. Linux

| Tema | Comportamiento | Acción |
|---|---|---|
| VirtIO | initramfs puede no incluirlo | chroot + `update-initramfs`/`dracut` |
| fstab | UUID estables; el disco restaurado los conserva | Revisar |
| Red | Nombres predecibles cambian | `net.ifnames=0` o reescribir configuración |
| GRUB | Byte a byte, suele funcionar | `grub-install` si falla |
| Secure Boot | shim firmado; DKMS sin firma falla | Firmar/MOK o desactivar |
| SELinux/AppArmor | Puede requerir ajustes | Revisar |
| LVM | Snapshot thin/clásico | Preflight de espacio |
| Btrfs/ZFS | Snapshot nativo | Preflight |

---

## 5. Proxmox VE

| Versión | Soporte | Notas |
|---|---|---|
| 8.2+ | Sí | Content type `import` |
| 9.x | Sí | Validar API |
| Anterior a 8.2 | Parcial | `import-from` existe, sin content type `import` |

| Storage | `import-from` | Notas |
|---|---|---|
| dir | Sí | Ruta local o volumen |
| lvmthin | Sí | Validar sparse |
| zfspool | Sí | Validar sparse |
| ceph | Validar | Puede requerir staging |
| nfs/cifs | Validar | Origen local al nodo |

---

## 6. PBS

| Versión | S3 | Notas |
|---|---|---|
| 4.0 | Sí | Introducción |
| 4.1 | Sí | Rate limiting S3 |
| 4.2.11 | Sí | Documentación consultada |
| 3.x | No | Sin S3 nativo |

---

## 7. Kopia

| Elemento | Compatibilidad |
|---|---|
| Windows | Sí, binario nativo |
| Linux | Sí, binario nativo |
| Backends | filesystem, S3, SFTP, WebDAV, rclone, Azure, GCS, B2 |
| Repository Server | Sí, ACL por usuario |
| Object Lock | S3, GOVERNANCE/COMPLIANCE |
| Verify | Estructural y contenido |
| Sync | `sync-to` |

---

## 8. Matriz de pruebas mínima

| Caso | Esperado |
|---|---|
| Win11 UEFI sin BitLocker → VM | Arranque y login |
| Win11 UEFI con BitLocker → VM | Aviso; arranque con clave o sin cifrado |
| Server 2025 UEFI → VM | Arranque y servicios |
| Server 2025 BIOS → VM | Arranque legacy |
| Debian ext4 → VM | Arranque y red |
| Debian LVM → VM | Arranque y red |
| Debian Btrfs → VM | Arranque y red |
| VM Proxmox → PBS → restore | Arranque |
| NAS/S3 | Backup, incremental, restore |
| Corte de red | Reanuda sin corrupción |

---

## 9. Limitaciones conocidas

- Windows P2V puede requerir intervención para VirtIO.
- BitLocker puede exigir clave de recuperación.
- ext4 sin LVM es crash-consistent.
- Sin CBT, la imagen relee el disco completo.
- Kopia no es un motor de imagen nativo.
- PBS S3 requiere caché local y no gestiona el bucket.
- Object Lock no está documentado para el datastore S3 de PBS.

---

## 10. Versiones a fijar

- Kopia (exacta).
- Agente (exacta).
- PVE/PBS soportados.
- Windows builds probados.
- Debian 12/13.
- virtio-win.
- QEMU/libguestfs si se usa inyección.

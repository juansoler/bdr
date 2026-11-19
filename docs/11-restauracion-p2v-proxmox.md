# Restauración P2V/V2V a Proxmox

**Documento:** 11-restauracion-p2v-proxmox  
**Versión:** 1.0  
**Requisitos:** RF-19, RF-23, RF-24  
**Estrategia principal:** agente PVE + `import-from`  
**Fallback:** ISO de rescate (ver `12-iso-rescate.md`)

---

## 1. Objetivo

Convertir un punto de recuperación de una máquina con agente en una **VM nueva, arrancable y verificada** en Proxmox VE, desde el panel, sin intervención manual en el caso Linux y con intervención documentada en Windows cuando los drivers lo exijan.

---

## 2. Entradas

| Entrada | Origen |
|---|---|
| Restore point | Catálogo |
| Manifiesto | Repositorio/PostgreSQL |
| Clúster, nodo, storage | Selección en el wizard |
| VMID, nombre, CPU, RAM | Usuario o valores del inventario |
| Bridge, firmware, TPM, Secure Boot | Usuario o manifiesto |
| Credenciales PVE | Token del agente PVE |
| Credenciales del repositorio | Credencial efímera del agente PVE |

---

## 3. Preflight

El control plane y el agente PVE validan antes de crear nada:

| Comprobación | Acción si falla |
|---|---|
| Manifiesto íntegro y `schema_version` conocida | Abortar `E_MANIFEST_SCHEMA` |
| Todos los discos y segmentos presentes | Abortar; ofrecer reintentar |
| Espacio en staging ≥ tamaño virtual × 1.1 (o datos únicos si sparse) | Abortar `E_PVE_SPACE` |
| Espacio en storage destino ≥ tamaño virtual × 1.1 | Abortar `E_PVE_SPACE` |
| VMID libre (`GET /cluster/nextid`) | Reintentar con otro |
| Storage soporta `images` y `import-from` | Abortar con explicación |
| Firmware del manifiesto coherente con la VM | Ajustar o abortar |
| Windows 11: OVMF + vTPM 2.0 + Secure Boot si el origen lo usaba | Ajustar o advertir |
| BitLocker: clave custodiada o decisión de reinstalar sin cifrado | Abortar `E_BITLOCKER_LOCKED` si no hay decisión |
| Storage Spaces / volúmenes dinámicos | Abortar `E_SNAPSHOT_UNSUPPORTED` |
| VM de producción existente con el mismo VMID | Abortar; nunca sobrescribir |
| `test_mode` | Aislar red y nombre; no tocar producción |

El preflight devuelve un informe con advertencias y bloqueos. La UI lo muestra antes de confirmar.

---

## 4. Secuencia

### Paso 0 — Reserva

- Semáforo local por nodo (máximo N restauraciones concurrentes).
- Directorio de staging y cuota.
- Job `RESTORE_VM` en estado `preflight`.

### Paso 1 — Descarga y ensamblado

Para cada disco y segmento:

1. Verificar sha256 del manifiesto.
2. `kopia snapshot restore <snapshot_id> <staging>/disk<N>.seg<k>` (o montaje y lectura).
3. Escribir en `disk<N>.raw` en el offset correspondiente (archivo disperso).
4. Verificar sha256 y tamaño.
5. Reportar progreso.

Si falla un segmento, reintentar solo ese segmento. Si persiste, abortar y limpiar.

### Paso 2 — Creación de la VM

```http
POST /api2/json/nodes/{node}/qemu

vmid=<id>
name=<nombre>
cores=<n>
memory=<MiB>
ostype=win11|l26
bios=ovmf|seabios
machine=q35
scsihw=virtio-scsi-pci
efidisk0=<storage>:1,efitype=4m,pre-enrolled-keys=0|1
tpmstate0=<storage>:1,version=v2.0
scsi0=<storage>:0,import-from=<staging/disk0.raw>
scsi1=<storage>:0,import-from=<staging/disk1.raw>
net0=e1000,bridge=<bridge>
boot=order=scsi0
serial0=socket
```

- Para Windows, crear con `sata0` en lugar de `scsi0` en la fase A.
- Para Linux, `scsi0` VirtIO y `net0 virtio`.
- Añadir `ide2=<virtio-win.iso>,media=cdrom` si se necesita instalar drivers.
- `import-from` requiere que el archivo sea local al nodo o un volumen con content type `import`.

### Paso 3 — Ajuste de arranque

- Verificar que el disco importado es el de arranque.
- Ajustar `boot order` al disco principal.
- Para UEFI, comprobar `efidisk0` y las claves.
- Para Windows 11, comprobar vTPM y Secure Boot.

### Paso 4 — Primer arranque

- Arrancar (`POST .../status/start`).
- Monitorizar estado, uptime y consola.
- Windows: fase SATA+e1000.
- Linux: VirtIO + consola serie.

### Paso 5 — Adaptación

- Windows: instalar VirtIO y cambiar bus (Fase 2 asistida; Fase 3 automática).
- Linux: chroot/initramfs/fstab/red antes del arranque o en el primer arranque.

### Paso 6 — Verificación

- Uptime estable, sin reinicios.
- Consola con hitos.
- Guest agent si existe.
- Captura de pantalla como evidencia.
- Registrar resultado y duración.

### Paso 7 — Cierre

- Limpiar staging.
- Liberar semáforo.
- Mantener la VM si falla el arranque, para diagnóstico.
- Registrar intervenciones y observaciones.

---

## 5. Windows

### 5.1 Fase A: SATA + e1000

Motivo: Windows incluye drivers para SATA y e1000; evita `0x7B INACCESSIBLE_BOOT_DEVICE` y la pérdida de red.

1. Crear la VM con `sata0` apuntando al disco importado.
2. `net0=e1000`.
3. Arrancar.
4. Comprobar que llega al escritorio/login y tiene red.

### 5.2 Fase B: VirtIO

Tres estrategias, en orden de preferencia:

1. **Inyección offline (Fase 3):** montar el volumen NTFS con libguestfs/`virt-customize`, cargar el hive `SYSTEM` y marcar `viostor`/`vioscsi`/`netkvm` con `Start=0`, más `CriticalDeviceDatabase`. Requiere virtio-win y validación por build.
2. **Asistida por consola (Fase 2):** arrancar con SATA+e1000, adjuntar ISO virtio-win y un usuario de recuperación; el operador instala drivers y cambia el bus. Requiere intervención humana.
3. **Cambio en caliente:** `qm set --delete sata0 --scsi0 <volid>` y `net0 model=virtio` con la VM parada, tras confirmar drivers. Validar en Fase 0.

**No se promete P2V Windows totalmente desatendido en Fase 2.**

### 5.3 BitLocker

- Si el backup es `raw_disk`, el disco restaurado está cifrado y el vTPM nuevo no tiene la clave: Windows pedirá la clave de recuperación.
- Si el backup es `vss_volumes`, los datos están en claro y el volumen restaurado no está cifrado; BitLocker no se arrastra.
- La UI debe advertir antes de restaurar.
- No se extraen claves automáticamente.
- El usuario debe tener la clave de recuperación o aceptar reinstalar sin BitLocker.

### 5.4 Windows 11

- `ostype=win11`, `machine=q35`, `bios=ovmf`.
- vTPM 2.0 (`tpmstate0`).
- Secure Boot (`efidisk0` con `pre-enrolled-keys=1` si el origen lo usaba; 0 si usaba claves propias).
- Sin TPM/Secure Boot, Windows 11 puede no arrancar o perder funcionalidades.
- El vTPM nuevo rompe Hello/PIN/credenciales ligadas al TPM; avisar.

### 5.5 BCD y arranque

- Si no arranca, arrancar desde la ISO de Windows o WinPE y ejecutar `bootrec /rebuildbcd`, `bcdboot C:\Windows /s S: /f UEFI`.
- Verificar la partición EFI y el orden de arranque.
- Documentar el procedimiento en el runbook.

---

## 6. Linux

### 6.1 VirtIO

- Crear con `scsi0` VirtIO y `net0 virtio`.
- Si el initramfs no incluye `virtio_pci`, `virtio_blk`, `virtio_scsi`, el arranque falla con `ALERT! /dev/... does not exist`.

### 6.2 Chroot

Con los discos restaurados montados en el nodo:

```bash
mount /dev/<root> /mnt
mount /dev/<efi> /mnt/boot/efi
for d in dev proc sys; do mount --bind /$d /mnt/$d; done
chroot /mnt
# añadir virtio_pci, virtio_blk, virtio_scsi a /etc/initramfs-tools/modules
update-initramfs -u -k all
# revisar /etc/fstab por UUID
# revisar /etc/network/interfaces o netplan
exit
umount -R /mnt
```

Para otras distros: `dracut -f --add-drivers "virtio_pci virtio_blk virtio_scsi"`.

### 6.3 Red

- El P2V cambia la NIC; los nombres predecibles (`ens18`) pueden no coincidir.
- Opciones: `net.ifnames=0` en la cmdline o reescribir la configuración.
- En el MVP, dejar la VM en una bridge con DHCP y avisar.
- Añadir `console=ttyS0` para verificar por consola serie.

### 6.4 Secure Boot

- Debian con shim firmado funciona.
- Si el origen usaba claves propias o módulos DKMS sin firmar, advertir.
- No activar Secure Boot a ciegas si el origen no lo usaba.

### 6.5 GRUB

- El disco restaurado es byte a byte, así que GRUB y las particiones ya están.
- Si no arranca, `grub-install` y `update-grub` desde chroot.
- Verificar la partición EFI y el orden de arranque.

---

## 7. Verificación de arranque

| Comprobación | Linux | Windows |
|---|---|---|
| Estado de VM | `running`, uptime creciente | Igual |
| Sin reinicios | uptime no se resetea | Igual |
| Consola | hitos en serie | screendump |
| Guest agent | `agent/ping` | `agent/ping` si está |
| Login | `login:` en consola | captura de pantalla |
| Red | ping/DHCP | ping/DHCP |
| Resultado | `succeeded` o `failed_boot` | Igual |

Si falla:

- No destruir automáticamente.
- Adjuntar logs, captura y estado.
- Dejar la VM para diagnóstico.
- Liberar staging solo tras decisión.

---

## 8. Rollback y limpieza

- Si falla antes de crear la VM: borrar staging y liberar semáforo.
- Si falla después de crear la VM: dejarla para diagnóstico; no borrar discos sin confirmación.
- Si el usuario cancela: detener la VM si se creó, limpiar staging, registrar.
- Nunca tocar los snapshots de origen.

---

## 9. Modo de prueba

- VM temporal con nombre `bdr-test-<id>` y VMID en un rango reservado.
- Red aislada (bridge sin gateway o VLAN de pruebas).
- Sin acceso a producción.
- Destrucción automática tras la verificación, con registro de evidencia.
- Nunca reutilizar el VMID de una VM de producción.

---

## 10. Criterios de aceptación

- La VM se crea con el firmware y recursos correctos.
- El disco restaurado tiene el tamaño y layout esperados.
- Linux arranca sin intervención en ≥9/10 intentos.
- Windows arranca con intervención documentada en ≥8/10 intentos.
- La verificación produce evidencia (uptime, consola/captura).
- No quedan recursos huérfanos.
- El fallo deja la VM diagnosticable y no destruye datos.

---

## 11. Preguntas abiertas (Fase 0)

1. ¿`import-from` funciona con archivos dispersos en `dir`, `lvmthin` y `zfspool`?
2. ¿El cambio SATA→VirtIO reutilizando el volid es fiable?
3. ¿La inyección offline de VirtIO funciona con Secure Boot y vTPM nuevo?
4. ¿Windows 11 arranca con vTPM nuevo sin clave de recuperación si el disco se restauró en claro?
5. ¿El chroot de Debian es suficiente o hace falta más?
6. ¿Cómo se verifica automáticamente Windows sin guest agent?
7. ¿Cuánto tarda importar 2 TB y qué límites prácticos hay?

# G0.4 — P2V Windows

**Objetivo:** obtener una VM de Windows 11 o Server 2025 en Proxmox a partir de una imagen completa, documentar el arranque con SATA+e1000, la transición a VirtIO y el comportamiento con BitLocker/vTPM.

**Riesgo:** crea VMs y escribe discos. Solo laboratorio; el script es dry-run por defecto.

---

## 1. Imagen de origen

Antes de existir el agente, usa una imagen de laboratorio:

- Desde una VM: `qemu-img convert -f qcow2 -O raw disco.qcow2 /var/tmp/win11.raw`.
- Desde Disk2VHD: `qemu-img convert -f vhdx -O raw win11.vhdx /var/tmp/win11.raw`.
- Desde un disco físico de laboratorio: `dd if=/dev/sdX of=/var/tmp/win11.raw bs=4M status=progress`.

Coloca el archivo en el nodo Proxmox, en una ruta legible por `pvedaemon` o en un storage con content type `import`.

Comprueba el tamaño y el layout:

```bash
qemu-img info /var/tmp/win11.raw
```

---

## 2. Crear la VM (fase A: SATA + e1000)

Dry-run:

```bash
export PVE_URL=https://pve.example:8006/api2/json
export PVE_TOKEN='bdr@pve!lab=00000000-0000-0000-0000-000000000000'

./labs/common/pve-create-vm.sh \
  --node pve01 --storage local-zfs --vmid 210 \
  --name win11-p2v --ostype win11 --bios ovmf --machine q35 \
  --bus sata --net e1000 --bridge vmbr0 \
  --disk-path /var/tmp/win11.raw --tpm --secureboot --start
```

Si el comando impreso es correcto, añade `--apply`.

Verifica:

```bash
curl -sS -k -H "Authorization: PVEAPIToken=$PVE_TOKEN" \
  "$PVE_URL/nodes/pve01/qemu/210/status/current" | jq .
```

### Comprobación de arranque

- Consola: noVNC en la interfaz de Proxmox, o `qm terminal 210` en el nodo.
- Captura: `qm monitor 210` y `screendump /var/tmp/win11.ppm` (convierte a PNG para adjuntar).
- Criterio mínimo: uptime creciente, sin reinicios, login o pantalla de inicio.

---

## 3. Fase B: VirtIO

1. Adjunta la ISO de virtio-win:

```bash
curl -sS -k -H "Authorization: PVEAPIToken=$PVE_TOKEN" \
  --data-urlencode 'ide2=local:iso/virtio-win.iso,media=cdrom' \
  "$PVE_URL/nodes/pve01/qemu/210/config"
```

2. Dentro de Windows, instala `viostor`, `vioscsi`, `netkvm` y, si procede, el guest agent.
3. Apaga la VM.
4. Cambia el bus y la NIC:

```bash
# Localiza el volumen actual
curl -sS -k -H "Authorization: PVEAPIToken=$PVE_TOKEN" \
  "$PVE_URL/nodes/pve01/qemu/210/config" | jq '.data.sata0'

# Cambia sata0 por scsi0 y net0 a virtio (ajusta el volid)
curl -sS -k -H "Authorization: PVEAPIToken=$PVE_TOKEN" \
  --data-urlencode 'delete=sata0' \
  --data-urlencode 'scsi0=local-zfs:vm-210-disk-0' \
  --data-urlencode 'net0=virtio,bridge=vmbr0' \
  "$PVE_URL/nodes/pve01/qemu/210/config"
```

5. Arranca y verifica de nuevo.

**P-07:** confirma si el cambio de bus reutilizando el volid funciona en tu versión. Si no, documenta la alternativa (`qm importdisk` o clonar el disco).

---

## 4. Variantes obligatorias

| Variante | Configuración | Qué observar |
|---|---|---|
| Windows 11 sin BitLocker | OVMF, vTPM, Secure Boot | Arranque y login |
| Windows 11 con BitLocker | OVMF, vTPM nuevo | ¿Pide clave? ¿Arranca sin ella? |
| Windows 11 con Secure Boot desactivado en origen | `pre-enrolled-keys=0` | Arranque |
| Server 2025 UEFI | OVMF | Servicios y red |
| Server 2025 BIOS | SeaBIOS, sin EFI | Arranque legacy |
| Imagen `raw_disk` con BitLocker | Sin descifrar | Clave de recuperación |
| Imagen `vss_volumes` | Datos en claro | Sin BitLocker tras restaurar |

---

## 5. Criterios de aceptación

- La VM se crea con el firmware y los recursos esperados.
- La fase A arranca con SATA+e1000.
- La fase B arranca con VirtIO tras instalar drivers.
- El comportamiento de BitLocker está documentado con evidencia.
- La tasa de éxito se registra (por ejemplo, 8/10).
- No se destruye ninguna VM de producción.

---

## 6. Evidencia

- Salida JSON de creación y estado.
- Captura de pantalla o consola.
- `qemu-img info` de la imagen.
- Configuración final de la VM.
- Tabla de variantes con resultado.
- Conclusión para `docs/11-restauracion-p2v-proxmox.md`.

# Agente Linux

**Documento:** 08-agente-linux  
**Versión:** 1.0  
**Sistemas:** Debian 12/13  
**Binario:** `bdr-agent` (Go, `GOOS=linux`)

---

## 1. Responsabilidades

1. Registro mTLS y heartbeat.
2. Inventario de hardware, discos, particiones, LVM, Btrfs, ZFS, red y seguridad.
3. Snapshots consistentes según el sistema de archivos.
4. Backup de archivos con Kopia.
5. Captura de imagen por segmentos con manifiesto.
6. Restauración de archivos.
7. Cola local, modo offline y reanudación.
8. Actualización firmada.

---

## 2. Servicio y privilegios

- Unidad systemd `bdr-agent.service`, `Type=notify`, reinicio automático.
- Ejecuta como root o con `CAP_SYS_ADMIN`, `CAP_DAC_READ_SEARCH` y acceso a `/dev` para snapshots y lectura de dispositivos.
- Sin puertos de escucha; solo salida mTLS.
- Configuración en `/etc/bdr/config.yaml` (0600).
- Estado en `/var/lib/bdr/`.
- Logs JSON en `/var/log/bdr/` o journald.
- `Nice=10`, `IOSchedulingClass=best-effort`, `IOSchedulingPriority=7`, `CPUWeight`/`IOWeight` bajos.

---

## 3. Estructura de paquetes (Go)

```
cmd/agent/
internal/agent/
  service/          systemd, señales, reinicio
  config/
  enroll/
  transport/        mTLS, reintentos, offline
  scheduler/
  inventory/
    inventory_linux.go
  snapshot/
    lvm/            thin y clásico
    btrfs/          subvolúmenes
    zfs/            datasets
    freeze/         fsfreeze breve
    cbt/            blksnap/dattobd (opt-in)
  stream/           lector, segmentos, journal
  kopia/
  manifest/
  progress/
  limits/
  restore/
  update/
  logging/
```

---

## 4. Inventario

| Grupo | Campos |
|---|---|
| Host | hostname, FQDN, distro/versión, kernel, arquitectura, uptime, agente |
| Seguridad | UEFI/BIOS, Secure Boot, TPM, LUKS, módulos DKMS |
| Discos | `/sys/block`, modelo, serial, tamaño, bus, GPT/MBR, GUID |
| Particiones | `lsblk --json`, offset, tamaño, type GUID, FS, UUID, mountpoint, flags |
| Volúmenes | LVM (PV/VG/LV), Btrfs subvols, ZFS datasets, mdraid |
| Red | interfaces, MAC, IPs, rutas, DNS, nombres predecibles |
| Arranque | GRUB/systemd-boot, ESP, initramfs, fstab |

Usar `lsblk --json` y `/sys` como fuentes primarias; evitar parsear texto humano cuando exista JSON.

---

## 5. Snapshots

### 5.1 Orden de preferencia

1. **LVM**
   - Thin snapshot si el LV es thin; clásico con tamaño calculado si no.
   - Requiere espacio libre en el VG; preflight.
   - Activar, leer, desactivar y eliminar al terminar.
2. **Btrfs**
   - Snapshot de subvolumen de solo lectura.
   - Leer el subvolumen o el dispositivo según el caso.
3. **ZFS**
   - Snapshot de dataset; leer el zvol/dataset correspondiente.
4. **ext4/XFS sin gestor de volúmenes**
   - **Modo por defecto:** imagen crash-consistent con `fsfreeze` breve para metadatos y lectura directa; marcar `consistency=crash` en el manifiesto y en la UI.
   - **Opt-in Fase 3:** `blksnap` o `dattobd` para snapshot real y CBT.
   - No congelar un volumen de varios TB durante horas: `fsfreeze` solo el tiempo necesario para capturar metadatos, no durante toda la lectura.

### 5.2 blksnap / dattobd

| Módulo | Licencia | Soporte | Notas |
|---|---|---|---|
| `blksnap` (Veeam) | GPL-2 | Kernels 5.10–6.10; Debian 12 requiere `dh-dkms` | COW con change tracker; puede dar CBT |
| `dattobd` (Datto) | GPL-2 | ext4/XFS; fixes para Debian 12 | COW en archivo `.datto` en el propio volumen |

Riesgos:

- DKMS se rompe con actualizaciones de kernel.
- Secure Boot exige firma de módulos y MOK.
- No se promete consistencia de aplicación en ext4 sin LVM en Fase 2.
- Si el módulo no está firmado o falla al cargar, el agente falla con mensaje accionable; no intenta forzar.

### 5.3 Detección y decisión

El agente reporta en el inventario:

- FS de cada partición.
- Presencia de LVM/Btrfs/ZFS.
- Si hay un módulo CBT disponible y firmado.
- Nivel de consistencia que puede ofrecer.

El plan puede exigir `application` y rechazar un equipo que solo ofrece `crash`.

---

## 6. Captura de imagen

### 6.1 Secuencia

1. Inventario y preflight de espacio.
2. Crear snapshot del volumen o decidir lectura crash-consistent.
3. Leer layout de particiones (`sfdisk -d` o `sgdisk --backup`) antes y después.
4. Segmentar cada disco (1–8 GiB).
5. Para cada segmento, ejecutar Kopia con `--stdin-file`.
6. Reportar segmento y sha256.
7. Cerrar manifiesto y subirlo.
8. Eliminar snapshots temporales.

### 6.2 Comandos de referencia

```bash
sfdisk -d /dev/sda > layout.sfdisk
sgdisk --backup=layout.gpt /dev/sda

kopia snapshot create \
  --config-file=/var/lib/bdr/kopia.config \
  --json \
  --description="rp=<run_id> disk=<n> seg=<k>" \
  --tag=bdr:kind=image --tag=bdr:run=<run_id> \
  --tag=bdr:disk=<n> --tag=bdr:seg=<k> \
  --stdin-file=disk<n>.seg<k>
```

### 6.3 LVM

```bash
lvcreate --snapshot --name bdr_snap --size <size> <vg>/<lv>
# leer /dev/<vg>/bdr_snap
lvremove -f <vg>/bdr_snap
```

Validar espacio libre en el VG antes; si no hay, fallar con `E_SNAPSHOT_UNSUPPORTED` o degradar a crash-consistent según plan.

### 6.4 Btrfs

```bash
btrfs subvolume snapshot -r /mnt/@ /mnt/@bdr_snap
# leer el subvolumen
btrfs subvolume delete /mnt/@bdr_snap
```

### 6.5 ZFS

```bash
zfs snapshot pool/dataset@bdr_<run_id>
# leer el zvol/dataset
zfs destroy pool/dataset@bdr_<run_id>
```

---

## 7. Backup de archivos

```bash
kopia snapshot create /ruta \
  --config-file=/var/lib/bdr/kopia.config \
  --json \
  --description="plan=<id> run=<run_id>" \
  --tag=bdr:plan=<id> --tag=bdr:kind=file \
  --parallel=4 --checkpoint-interval=10m
```

Exclusiones típicas: `/proc`, `/sys`, `/dev`, `/tmp`, `/run`, `/var/lib/docker/overlay2` (según plan), caches.

---

## 8. Restauración de archivos

1. Preflight de espacio y permisos.
2. `kopia snapshot restore <snapshot_id> <destino>` o restauración selectiva.
3. Preservar propietario, permisos, ACLs y xattrs cuando sea posible.
4. Verificar hashes si el plan lo exige.
5. Reportar.

---

## 9. Cola local y offline

- Cola persistente en `/var/lib/bdr/state/queue.json`.
- Journal de segmentos por `run_id`.
- Si el repositorio no está accesible, el job se pausa; el heartbeat sigue.
- Al volver, reconciliar por `job_id`/`lease_id` y reanudar por segmento.
- No se ejecuta un plan nuevo sin confirmación si cambió la versión.

---

## 10. Límites

- `nice`/`ionice` bajos; `CPUWeight`/`IOWeight` de systemd.
- Token bucket de red antes de Kopia.
- Backpressure por buffer acotado.
- Pausa entre segmentos al acabar la ventana.
- Límite de paralelismo de Kopia por plan.

---

## 11. Instalación y actualización

- Paquete `.deb` firmado; unidad systemd; usuario/grupo `bdr`.
- `bdr-agent register --server https://... --token ...`.
- Actualización con binario firmado, swap atómico y rollback.
- Compatibilidad con Debian 12/13 y kernels soportados; matriz en `anexos/d`.

---

## 12. Seguridad

- Sin shell remoto; solo operaciones definidas.
- Configuración 0600; clave privada protegida (0600 o TPM2).
- Redacción de secretos en logs.
- Usuario Kopia con ACL limitada.
- Validación de huella del servidor.
- No se ejecuta como root de dominio; solo root local o capacidades mínimas.

---

## 13. Preguntas abiertas (Fase 0)

1. ¿LVM thin snapshot es estable con la carga real y el espacio disponible?
2. ¿Btrfs/ZFS snapshots funcionan en el hardware objetivo?
3. ¿`blksnap`/`dattobd` cargan en Debian 12 con Secure Boot y kernel objetivo?
4. ¿La imagen crash-consistent de ext4 arranca tras restaurar?
5. ¿El initramfs de Debian incluye virtio tras el P2V? ¿Se corrige con chroot?
6. ¿Los nombres de interfaz (`ens18`) rompen la red tras el P2V?
7. ¿`--stdin-file` mantiene el rendimiento con discos de 1–2 TB?

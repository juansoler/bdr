# Agente Proxmox y orquestación PBS

**Documento:** 09-agente-proxmox-pbs  
**Versión:** 1.0  
**Sistemas:** Proxmox VE 8.2+/9.x, Proxmox Backup Server 4.x

---

## 1. Papel del agente PVE

`bdr-agent-pve` es un servicio instalado en cada nodo de Proxmox que:

1. Se registra con mTLS y reporta estado del nodo.
2. Descubre nodos, storages, bridges y VMs.
3. Recibe órdenes de restauración autorizadas.
4. Descarga segmentos del repositorio y los escribe en staging.
5. Crea la VM con `import-from`.
6. Comprueba el arranque y reporta evidencia.
7. Puede ejecutar operaciones PBS locales (backup, verify, prune, gc) si el diseño lo requiere.

**No es un shell remoto.** Solo ejecuta el conjunto de operaciones definido y auditado.

---

## 2. Instalación y servicio

- Paquete `.deb` firmado; unidad systemd `bdr-agent-pve.service`.
- Ejecuta como root (necesario para escribir en storages y usar QEMU).
- Sin puertos de escucha; solo salida mTLS al control plane.
- Configuración en `/etc/bdr-pve/config.yaml` (0600).
- Estado y staging en `/var/lib/bdr-pve/` y `/var/tmp/bdr-restore/` (configurable).
- Logs JSON en journald/`/var/log/bdr-pve/`.

---

## 3. Token de API de PVE

Crear un token con **separación de privilegios** (no hereda los del usuario):

| Privilegio | Uso |
|---|---|
| `VM.Allocate` | Crear VM |
| `VM.Config.Disk` | Discos |
| `VM.Config.HWType` | Firmware, TPM, machine |
| `VM.Config.Network` | Red |
| `VM.Config.Options` | Opciones |
| `VM.PowerMgmt` | Arrancar/parar |
| `VM.Audit` | Consultar |
| `VM.Monitor` | Consola/screendump |
| `Datastore.AllocateSpace` | Espacio en `/vms` y storage destino |
| `Datastore.Audit` | Consultar espacio |

**No conceder:** `Datastore.Allocate` (crear storages), `Sys.Modify`, `Permissions.Modify`, `VM.Console` amplio, acceso a otros pools.

Paths restringidos a `/vms` y al storage destino. Rotación periódica y allowlist de IP.

---

## 4. Descubrimiento

- `GET /api2/json/nodes` → nodos y estado.
- `GET /api2/json/nodes/{node}/storage` → storages, tipo, contenido, espacio.
- `GET /api2/json/nodes/{node}/network` → bridges.
- `GET /api2/json/cluster/nextid` → VMID libre.
- `GET /api2/json/nodes/{node}/qemu` → VMs.
- `GET /api2/json/nodes/{node}/qemu/{vmid}/status/current` → estado.

Se cachea en PostgreSQL y se refresca periódicamente; el preflight usa datos frescos.

---

## 5. Staging

- Directorio local con espacio suficiente, preferiblemente un storage dedicado.
- Cuota por job; preflight `espacio libre >= tamaño_virtual × 1.1` o `>= datos_únicos` si se usa archivo disperso.
- El agente descarga segmentos y escribe por offset en un archivo disperso `disk<N>.raw`.
- Verifica sha256 de cada segmento contra el manifiesto antes de aceptarlo.
- Si falla un segmento, reintenta solo ese segmento.
- Limpieza garantizada al terminar o cancelar.

**Alternativa preferible:** storage con content type `import` (PVE 8.2+). El agente escribe el artefacto en el directorio de import y usa `import-from=<storage>:import/<archivo>`, evitando rutas locales no legibles por `pvedaemon`.

---

## 6. Creación de la VM

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
scsi0=<storage>:0,import-from=<path-o-volid>
scsi1=... (discos de datos)
net0=e1000,bridge=<bridge>        # fase A Windows
net0=virtio,bridge=<bridge>       # fase B / Linux
boot=order=scsi0
serial0=socket
```

Notas:

- `import-from` acepta ruta local legible por el nodo o volumen con content type `import`.
- `pre-enrolled-keys=1` instala claves de Microsoft y habilita Secure Boot; usar 0 si el origen no usaba Secure Boot o usaba claves propias.
- `tpmstate0 version=v2.0` crea un vTPM nuevo: cambia el EK y rompe sellado de BitLocker/Hello.
- Para Windows 11: `ostype=win11`, `machine=q35`, `bios=ovmf`, vTPM 2.0, Secure Boot si el origen lo usaba.
- Añadir `ide2=<iso virtio-win>,media=cdrom` si se instalarán drivers desde consola.

---

## 7. Arranque y comprobación

### 7.1 Windows

1. Crear con **SATA** (`sata0`) y **e1000** para evitar 0x7B y pérdida de red.
2. Arrancar y comprobar uptime y consola.
3. Instalar drivers VirtIO (ISO o inyección offline en Fase 3).
4. Cambiar a `virtio-scsi-pci` y `virtio` con la VM parada.
5. Comprobar de nuevo.

### 7.2 Linux

1. Crear con VirtIO (`scsi0`, `net0 virtio`).
2. Montar los discos restaurados y hacer chroot: `mount`, `bind` de `/dev`, `/proc`, `/sys`, `update-initramfs -u -k all` con `virtio_pci`, `virtio_blk`, `virtio_scsi`.
3. Revisar `/etc/fstab` (UUID), red y `console=ttyS0`.
4. Arrancar y comprobar consola serie.

### 7.3 Comprobación

- `GET /nodes/{node}/qemu/{vmid}/status/current`: `status=running`, uptime creciente, sin reinicios.
- Linux: consola serie con hitos (`GRUB`, `Booting`, `login:`).
- Windows con guest agent: `POST /agent/ping`, `agent/exec`.
- Windows sin guest agent: uptime + actividad de disco + `screendump` para evidencia visual.
- Si falla: dejar la VM para diagnóstico, marcar `failed_boot`, adjuntar logs/captura, no destruir automáticamente.

---

## 8. Orquestación PBS

### 8.1 Backup

```http
POST /api2/json/nodes/{node}/vzdump
  vmid=<id>
  storage=<pbs-storage>
  mode=snapshot
  prune-backups=<policy>
  notes-template=BDR plan=<id>
```

El nodo PVE habla con PBS; el control plane solo orquesta.

### 8.2 Catálogo

`GET /api2/json/admin/datastore/{store}/snapshots` con token `DatastoreReader`; sincronización periódica a PostgreSQL.

### 8.3 Verificación

`POST /api2/json/admin/datastore/{store}/verify` con token `DatastoreOperator`; estado en `pbs_snapshots`.

### 8.4 Prune y GC

`POST .../prune` y `POST .../gc` en ventana programada, con advisory lock y auditoría.

### 8.5 Restore

`POST /api2/json/nodes/{node}/qemu` con el volumen PBS como `archive` (equivalente a `qmrestore`); `live-restore` si la versión lo soporta.

### 8.6 Tokens PBS

- `DatastoreBackup` para backup.
- `DatastoreReader` para listar/restaurar.
- `DatastoreOperator` para verify/prune/gc.
- Sin `DatastoreAdmin` salvo operación puntual auditada.
- Verificación de huella TLS y allowlist de IP.

### 8.7 Datastore S3 de PBS

- PBS 4.x; caché local persistente de 64–128 GiB.
- Un datastore S3 solo lo usa una instancia PBS.
- PBS no crea el bucket ni gestiona su ACL.
- `reuse-datastore` + `overwrite-in-use` para reconstruir.
- Object Lock no documentado; usar política del bucket o réplica PBS.

---

## 9. Modelo de seguridad

- El agente corre como root: es el mayor riesgo del sistema.
- Binario firmado y actualizaciones firmadas.
- Sin puertos de escucha; solo mTLS saliente.
- Token PVE de mínimo privilegio y paths restringidos.
- Staging acotado y auditado.
- No se aceptan comandos arbitrarios; solo operaciones del conjunto definido.
- El compromiso del control plane + agente PVE equivale al del clúster: documentarlo en el modelo de amenazas.
- Registrar cada operación que muta estado con `request_id` y actor.

---

## 10. Manejo de fallos

| Fallo | Acción |
|---|---|
| Token PVE inválido | `E_PVE_AUTH`, no reintentar |
| Permisos insuficientes | `E_PVE_PRIV`, no reintentar |
| Espacio insuficiente | `E_PVE_SPACE`, abortar antes de crear VM |
| `import-from` falla | `E_PVE_IMPORT`, limpiar staging, dejar VM si existe para diagnóstico |
| VM no arranca | `E_BOOT_TIMEOUT`, adjuntar evidencia, no destruir |
| PBS no responde | Reintentar con backoff; no bloquear otros jobs |
| Segmento corrupto | Reintentar ese segmento; si persiste, fallar |

---

## 11. Preguntas abiertas (Fase 0)

1. ¿`import-from` funciona igual en `dir`, `lvmthin` y `zfspool` con archivos dispersos?
2. ¿El content type `import` está disponible en la versión instalada?
3. ¿El cambio SATA→VirtIO reutilizando el volid funciona de forma fiable?
4. ¿La inyección offline de VirtIO funciona con Secure Boot y vTPM nuevo?
5. ¿Qué endpoints exactos de PBS 4.x permiten verify/prune/gc/restore con tokens mínimos?
6. ¿`live-restore` aplica a todos los tipos de VM?
7. ¿El agente puede escribir en el storage `import` sin privilegios adicionales?

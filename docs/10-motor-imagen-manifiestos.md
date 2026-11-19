# Motor de imagen y manifiestos

**Documento:** 10-motor-imagen-manifiestos  
**Versión:** 1.1  
**Requisitos relacionados:** RF-01, RF-07, RF-13, RF-14, RF-19, RF-24

---

## 1. Objetivo

Definir un formato de imagen **propio y versionado** que permita reconstruir una máquina en otro hardware o como VM, sin acoplar la recuperación a los detalles internos de Kopia.

Kopia se usa como almacén de segmentos (deduplicación, cifrado, compresión) en Fase 2. El manifiesto y el contrato de recuperación son nuestros. Si en el futuro se cambia el backend, el manifiesto sigue siendo válido.

---

## 2. Modos de imagen

| Modo | Plataforma | Qué captura | Consistencia | Uso |
|---|---|---|---|---|
| `vss_volumes` | Windows | Layout del disco en crudo + contenido de cada volumen desde VSS | Aplicación | Predeterminado |
| `raw_disk` | Windows | Disco físico completo, incluido espacio no asignado y ciphertext | Crash | Fallback, discos pequeños, BitLocker |
| `lvm_volumes` | Linux | Layout + contenido de cada LV desde snapshot | Aplicación | Predeterminado con LVM |
| `btrfs_volumes` | Linux | Layout + subvolúmenes desde snapshot | Aplicación | Predeterminado con Btrfs |
| `zfs_volumes` | Linux | Layout + datasets desde snapshot | Aplicación | Predeterminado con ZFS |
| `crash_direct` | Linux ext4/XFS sin LVM | Layout + lectura directa | Crash | Debian ext4 sin LVM; advertencia explícita |

El plan elige el modo; el agente puede degradar si no es posible y el plan lo permite.

---

## 3. Unidad de captura: segmento

- Un segmento es un rango contiguo de un disco, de tamaño fijo configurable (1–8 GiB; máximo 16 GiB).
- Cada segmento se envía como un snapshot independiente de Kopia con `--stdin-file`.
- Un segmento completado se registra con nombre, offset, tamaño, sha256 y snapshot ID.
- Los nombres son deterministas: `disk<D>.seg<SSSS>`.
- La segmentación permite reanudar, paralelizar y acotar memoria.

**Por qué segmentar:** un flujo stdin no es direccionable y no se puede reanudar a mitad como un archivo. El segmento es la unidad de reanudación.

---

## 4. Manifiesto

### 4.1 Campos

| Campo | Tipo | Obligatorio | Descripción |
|---|---|---|---|
| `schema_version` | int | Sí | Versión del formato |
| `manifest_id` | uuid | Sí | ID del manifiesto |
| `run_id` | uuid | Sí | Ejecución de backup |
| `agent_id` | uuid | Sí | Equipo origen |
| `host` | object | Sí | hostname, OS, versión, arch |
| `kind` | string | Sí | `image` |
| `image_mode` | string | Sí | Modo de captura |
| `consistency` | string | Sí | `application` o `crash` |
| `captured_at` | RFC3339 | Sí | Inicio |
| `repository` | object | Sí | ID, URL, usuario Kopia |
| `security` | object | Sí | boot_mode, secure_boot, tpm, bitlocker, luks |
| `disks` | array | Sí | Un objeto por disco físico: índice, tabla, GUID, sector, tamaño y particiones |
| `streams` | array | Sí | Segmentos |
| `unallocated_bytes_captured` | bool | Sí | Si se capturó espacio no asignado |
| `restore_hints` | object | Sí | Firmware esperado, needs_virtio, buses iniciales |
| `tooling` | object | Sí | Versión de agente/Kopia, sha256 del manifiesto |

### 4.2 Ejemplo

```json
{
  "schema_version": 1,
  "manifest_id": "0192f0c4-...",
  "run_id": "0192f0c5-...",
  "agent_id": "0192f0c6-...",
  "host": {
    "hostname": "srv01",
    "os_family": "windows",
    "os_version": "Server 2025",
    "arch": "amd64"
  },
  "kind": "image",
  "image_mode": "vss_volumes",
  "consistency": "application",
  "captured_at": "2026-11-16T02:00:00Z",
  "repository": {
    "id": "0192a...",
    "server_url": "https://repo.example:51515",
    "kopia_username": "agent-0192f0c6"
  },
  "security": {
    "boot_mode": "uefi",
    "secure_boot": true,
    "tpm": "2.0",
    "bitlocker": [
      {"volume": "C:", "state": "on", "recovery_key_custodied": true}
    ]
  },
  "disks": [
    {
      "index": 0,
      "table": "gpt",
      "disk_guid": "...",
      "sector_size": 512,
      "total_bytes": 512110190592,
      "partitions": [
        {
          "index": 1,
          "offset": 1048576,
          "size": 104857600,
          "type_guid": "c12a7328-f81f-11d2-ba4b-00a0c93ec93b",
          "role": "efi",
          "fs": "fat32",
          "volume_guid": "..."
        },
        {
          "index": 2,
          "offset": 105906176,
          "size": 16777216,
          "type_guid": "e3c9e316-0b5c-4db8-817d-f92df00215ae",
          "role": "msr"
        },
        {
          "index": 3,
          "offset": 122683392,
          "size": 511987507200,
          "type_guid": "ebd0a0a2-b9e5-4433-87c0-68b6b72699c7",
          "role": "data",
          "fs": "ntfs",
          "volume_guid": "...",
          "mountpoint": "C:"
        }
      ]
    }
  ],
  "streams": [
    {
      "disk": 0,
      "segment": 0,
      "name": "disk0.seg0000",
      "offset": 0,
      "bytes": 4294967296,
      "sha256": "hex...",
      "kopia_snapshot_id": "k...",
      "state": "complete"
    }
  ],
  "unallocated_bytes_captured": false,
  "restore_hints": {
    "system_disk": 0,
    "boot_partition_index": 1,
    "expected_firmware": "ovmf",
    "needs_virtio": true,
    "windows_first_boot_bus": "sata",
    "windows_first_boot_nic": "e1000"
  },
  "tooling": {
    "agent_version": "1.0.0",
    "kopia_version": "0.x.y",
    "manifest_sha256": "hex..."
  }
}
```

### 4.3 Reglas

- El manifiesto es **inmutable** una vez cerrado.
- Durante el backup, el estado parcial vive en PostgreSQL (fuente de verdad para reanudar).
- El manifiesto final se sube como snapshot `bdr:kind=manifest` para autocontención del repositorio.
- Un restaurador rechaza `schema_version` desconocida.
- `sha256` de cada segmento se verifica antes de restaurar.
- No se guardan secretos ni claves en el manifiesto.
- `disks` contiene **todos** los discos físicos del origen, ordenados por `index`; `streams[].disk` referencia ese índice. `restore_hints.system_disk` también es un índice de `disks`.
- El orden de `disks` es estable: si el origen añade o quita discos, se genera un manifiesto nuevo, no se reordena el anterior.
- Un manifiesto final solo contiene segmentos en estado `complete`; los parciales viven en PostgreSQL durante la ejecución.

---

## 5. Reglas de captura

### 5.1 Windows

1. Preflight de VSS y espacio.
2. Crear shadow set con todos los volúmenes (incluidos ESP/recovery).
3. Leer layout de disco en crudo.
4. Para `vss_volumes`, leer cada volumen desde `\\?\GLOBALROOT\Device\HarddiskVolumeShadowCopyN`.
5. Segmentar y subir.
6. Leer layout de nuevo; si cambió, marcar `E_DISK_LAYOUT_CHANGED`.
7. Cerrar manifiesto y shadow set.

### 5.2 Linux

1. Preflight de espacio en VG/pool.
2. Crear snapshot LVM/Btrfs/ZFS o decidir crash-consistent.
3. Leer layout (`sfdisk -d`, `sgdisk --backup`).
4. Segmentar y subir.
5. Leer layout de nuevo.
6. Destruir snapshot temporal.

### 5.3 Espacio no asignado

- En `vss_volumes`/`lvm_volumes` no se captura espacio no asignado; se documenta.
- En `raw_disk` sí se captura.
- La restauración crea un disco del tamaño virtual original; el espacio no asignado se rellena con ceros o queda disperso.

### 5.4 Cambios durante la captura

- El layout debe ser estable; si cambia, el restore point se marca inconsistente.
- El contenido puede cambiar; la consistencia la da el snapshot (VSS/LVM/Btrfs/ZFS) o se acepta crash-consistent.
- En `crash_direct`, el journal del FS se reproduce al arrancar; no se garantiza consistencia de aplicación.

---

## 6. Reanudación

- Journal local `state/<run_id>.json`:
  - `run_id`, `manifest_id`, `image_mode`;
  - por disco: segmentos completados con nombre, offset, tamaño, sha256, snapshot ID.
- Al reanudar:
  1. Leer journal.
  2. Verificar contra el servidor que los snapshots existen por tag (`bdr:run`, `bdr:disk`, `bdr:seg`).
  3. Saltar los completados.
  4. Continuar desde el primer segmento incompleto.
- Si el servidor no reconoce un segmento, se reenvía.
- La idempotencia por nombre determinista evita duplicados lógicos.

---

## 7. Restauración

### 7.1 Ensamblado

- **Preferido:** archivo disperso `disk<N>.raw` del tamaño virtual; escribir cada segmento en su offset con `seek`.
- **Alternativa:** concatenar segmentos en orden; requiere el doble de espacio temporal.
- Verificar sha256 de cada segmento antes de escribirlo.
- Al terminar, verificar el tamaño y, si es posible, el layout.

### 7.2 Inyección en Proxmox

- `import-from` acepta el archivo raw/qcow2 o un volumen con content type `import`.
- El agente PVE hace staging local y crea la VM.
- Para Windows, arranque SATA+e1000; para Linux, VirtIO.
- Ver `11-restauracion-p2v-proxmox.md`.

### 7.3 Bare-metal

- La ISO de rescate descarga los segmentos y escribe en el disco físico.
- Recrea tabla de particiones y arranque.
- Ver `12-iso-rescate.md`.

---

## 8. Verificación

| Nivel | Qué comprueba |
|---|---|
| 1 | Estructura del repositorio y existencia de blobs (`kopia snapshot verify`) |
| 2 | Descarga/descifra/descomprime una muestra (`--verify-files-percent`) |
| 3 | Reconstruye la imagen y comprueba layout/tamaño |
| 4 | Arranca una VM temporal en red aislada |
| 5 | Simulación DR completa |

La verificación de contenido con `--verify-files-percent=100` puede ser costosa; se programa por ventana y por criticidad.

---

## 9. Versionado y compatibilidad

- `schema_version` se incrementa ante cambios incompatibles.
- Un cambio aditivo (campo nuevo opcional) no incrementa la versión mayor.
- El restaurador debe soportar al menos la versión actual y la anterior.
- Los manifiestos antiguos permanecen en el repositorio; no se migran destructivamente.
- La versión del agente y de Kopia se registra para reproducibilidad.

---

## 10. CBT (backlog Fase 3)

Sin CBT, cada backup relee el disco completo. Opciones:

- **Windows:** USN journal + caché de hashes por bloque; validar que detecta cambios de forma fiable.
- **Linux:** `dm-era`, `blksnap` o `dattobd` para change tracking.
- **Formato:** añadir `changed_blocks` al manifiesto y un modo de captura incremental.
- **Riesgo:** un CBT incorrecto produce backups inconsistentes; debe validarse con pruebas de corrupción y comparación con backup completo.

No se promete CBT en Fase 2.

---

## 11. Preguntas abiertas

1. ¿`--stdin-file` mantiene la deduplicación entre segmentos y entre ejecuciones?
2. ¿Cuál es el tamaño de segmento óptimo para 1 GbE y 10 GbE?
3. ¿El archivo disperso se importa correctamente en `dir`, `lvmthin` y `zfspool`?
4. ¿Kopia puede restaurar un segmento a un offset concreto sin materializar todo el archivo?
5. ¿El manifiesto debe incluir hashes por bloque o basta por segmento?
6. ¿Cómo se detecta un cambio de layout durante la captura sin falsos positivos?

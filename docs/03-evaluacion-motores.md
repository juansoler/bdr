# Evaluación de motores open source

**Documento:** 03-evaluacion-motores  
**Versión:** 1.0  
**Objetivo:** evaluar Kopia, UrBackup, Duplicacy y Proxmox Backup Server como motores del producto, con licencia, capacidades reales y encaje en los requisitos.

---

## 1. Criterios

| Criterio | Peso | Qué se evalúa |
|---|---|---|
| Licencia | Alto | Uso propio, redistribución y evolución futura |
| Backup de archivos | Alto | Incremental, dedup, cifrado, restauración granular |
| Imagen de máquina | Alto | Captura consistente, layout, arranque |
| Destinos | Alto | NAS, S3, repositorio central |
| Verificación | Alto | Integridad y restauración comprobable |
| Integración Proxmox | Alto | API, PBS, P2V |
| Madurez y mantenimiento | Medio | Actividad, comunidad, estabilidad de API |
| Complejidad operativa | Medio | Dependencias, despliegue, actualizaciones |

---

## 2. Kopia

### 2.1 Licencia y naturaleza

Kopia es Apache-2.0, con concesión de patentes. Se distribuye como CLI y GUI; el proyecto fue adquirido por Veeam en 2022, pero sigue siendo open source sin funciones bloqueadas.

### 2.2 Capacidades verificadas

- Deduplicación por contenido con chunking de longitud variable.
- Compresión Zstandard y cifrado AES-256-GCM/ChaCha20 en cliente.
- Backends: filesystem, S3, SFTP, WebDAV, rclone, Azure, GCS, B2.
- Repositorio central con `kopia server start`, usuarios y ACL (`READ`, `APPEND`, `FULL`).
- Los clientes del servidor **no reciben la contraseña maestra**; se autentican con usuario/contraseña del servidor.
- `kopia snapshot verify` valida la estructura; `--verify-files-percent=100` descarga, descifra y descomprime para verificar contenido.
- `kopia repository sync-to` replica a otro backend, con `--delete` y `--point-in-time`.
- Object Lock S3 con `--retention-mode=GOVERNANCE|COMPLIANCE` y `--retention-period`; el bucket debe crearse con lock y versioning.
- Retención por políticas (`--keep-daily`, `--keep-weekly`, etc.).
- Restauración granular y montaje.

### 2.3 Imagen de disco

Kopia **no es un motor de imagen de disco con CBT**. Sin embargo, `kopia snapshot create --stdin-file=<nombre>` permite crear un snapshot a partir de un flujo de entrada, y el snapshot se marca como manual. El caso de uso original incluye exportar un dispositivo de bloque y pasarlo por stdin.

Limitaciones:

- Un flujo no es direccionable: no hay reanudación nativa comparable a la de un archivo.
- No captura layout de particiones ni arranque por sí mismo.
- No hay CBT; la deduplicación reduce almacenamiento y red, no la lectura del disco.
- La CLI no es una API estable; hay que fijar versión y envolverla.

### 2.4 Repository Server

`kopia server start` expone el repositorio por HTTPS. Los usuarios se gestionan con `kopia server user add <usuario@host>` y ACL. Los cambios de usuario pueden tardar 5–10 minutos o aplicarse al reiniciar; se fuerza con `kopia server refresh` o SIGHUP.

**Limitación importante:** las ACL protegen manifiestos y políticas, no los contenidos. Los contenidos son globales por content ID. Un agente que conozca un ID podría leer contenido de otro. Por tanto, los agentes de un mismo repositorio deben considerarse del mismo dominio de confianza, o separarse en repositorios por grupo.

### 2.5 Papel en el producto

- Motor de archivos y repositorio.
- Almacén de segmentos de imagen en Fase 2.
- Réplica NAS→S3.
- Verificación estructural y de contenido.

No se importan paquetes internos de Kopia; se usa el binario como subproceso, con versión fijada y contract tests.

---

## 3. UrBackup

### 3.1 Licencia

AGPLv3+ (con CLA que permite licencias adicionales). Es software libre y usable en producción propia. El proyecto está respaldado por Infscape, que vende add-ons comerciales.

### 3.2 Capacidades

- Backup de archivos e imagen, completo e incremental, con el sistema en marcha.
- Imagen en VHD(X) en Windows o raw sobre Btrfs/ZFS/ReFS/XFS.
- Incrementales de imagen enviando solo sectores usados/modificados.
- VSS en Windows y snapshots LVM/Btrfs/ZFS en Linux.
- Restauración mediante USB/ISO de rescate.
- Deduplicación a nivel de archivo (mismo archivo entre equipos).
- Panel web, informes y alertas por email.
- Clientes Windows, Linux y FreeBSD.

### 3.3 Limitaciones

- Imagen solo NTFS (Windows) o extX/XFS (Linux).
- MBR/GPT con partición primaria; sin volúmenes dinámicos.
- Sin S3 nativo en la versión open source; el S3 llega con el appliance comercial de Infscape.
- Deduplicación por archivo, no por chunks; no comparable a Kopia.
- Restaurar una imagen a Proxmox falla con frecuencia por drivers/arranque; los foros recomiendan Disk2VHD u otras herramientas.
- No es un motor de recuperación P2V automatizada.

### 3.4 Papel en el producto

**Referencia conceptual y banco de pruebas.** Se estudia cómo resuelve la captura en caliente, el incremental por sectores y el restore ISO. **No se incorpora como dependencia**: su licencia AGPLv3 impondría obligaciones si se distribuyera, no encaja con S3 y no resuelve el P2V.

---

## 4. Duplicacy

### 4.1 Licencia

No es open source. La CLI es gratuita solo para uso personal, con excepciones para restaurar/verificar/copiar/podar. La GUI Personal cuesta 20 USD el primer equipo y 10 USD por equipo adicional; la GUI Comercial y la CLI con licencia cuestan 50 USD por equipo y año. Cada licencia cubre un equipo.

### 4.2 Capacidades

- Incrementales, deduplicación, cifrado, compresión, almacenamiento cloud, retención.
- CLI y GUI.
- Solo archivos; no hace imagen completa de máquina.

### 4.3 Papel en el producto

**Descartado.** No cumple el requisito de imagen, su licencia no es adecuada para un producto propio y no aporta nada que Kopia no cubra mejor para archivos.

---

## 5. Proxmox Backup Server

### 5.1 Licencia

AGPLv3. Se usa como servidor independiente, no se incorpora código.

### 5.2 Capacidades

- Backup incremental de VMs y contenedores por bloque sucio.
- Deduplicación, compresión y cifrado.
- Verificación SHA-256 programable.
- Integración nativa con Proxmox VE.
- Sincronización entre PBS.
- Datastore S3 nativo desde PBS 4.x (documentación 4.2.11).
- Restauración completa o de archivos, y live-restore según versión.

### 5.3 Limitaciones del datastore S3

- Requiere caché local persistente de 64–128 GiB; no se puede usar un datastore regular preexistente.
- Un datastore S3 solo puede ser usado por una instancia PBS a la vez.
- PBS no crea el bucket ni gestiona su ACL.
- Reconstrucción con `reuse-datastore` y `overwrite-in-use`, usando el mismo nombre de datastore.
- Object Lock no está documentado para el datastore S3; la inmutabilidad se resuelve con política del bucket o con otro PBS/réplica.

### 5.4 Papel en el producto

Motor de backup/restore de VMs de Proxmox. El control plane lo orquesta por API, cataloga snapshots y lanza verify/prune/gc. **No se reimplementa.**

---

## 6. Comparativa

| Capacidad | Kopia | UrBackup | Duplicacy | PBS |
|---|---|---|---|---|
| Licencia | Apache-2.0 | AGPLv3+ | Comercial | AGPLv3 |
| Archivos incrementales | Excelente | Sí | Excelente | No aplica |
| Imagen completa | Vía flujos | Sí | No | Solo VMs |
| Incremental de imagen | Dedup sin CBT | Sectores modificados | No | Bloque sucio |
| Dedup | Chunks | Archivo | Chunks | Chunks |
| Compresión | Zstd | Sí | Sí | Zstd |
| Cifrado cliente | Sí | Limitado | Sí | Sí |
| NAS | Sí | Sí | Sí | Sí |
| S3 | Sí | No nativo | Sí | Sí (4.x) |
| Repositorio central | Sí | Sí | Sí | Sí |
| Restore granular | Sí | Sí | Sí | Sí |
| Restore ISO | No | Sí | No | No aplica |
| P2V a Proxmox | Desarrollo propio | Frágil | No | No aplica |
| API estable | CLI, fijar versión | API propia | CLI | API REST |
| Complejidad operativa | Media | Media | Baja | Media |

---

## 7. Decisión

| Motor | Rol | Fase |
|---|---|---|
| **Kopia** | Repositorio, archivos, cifrado, dedup, réplica, segmentos de imagen | 1–3 |
| **PBS** | Backup/restore de VMs de Proxmox | 1–3 |
| **UrBackup** | Referencia y PoC; no dependencia | 0 |
| **Duplicacy** | No se usa | — |

### 7.1 Qué tomamos de UrBackup sin usar su código

- La idea de imagen en caliente con VSS/snapshots.
- El incremental por sectores usados/modificados (concepto, no implementación).
- La ISO de rescate como producto de primera clase.
- La advertencia de que restaurar una imagen a un hipervisor distinto falla si no se adaptan drivers y arranque.

### 7.2 Qué tomamos de PBS

- Su modelo de incremental por bloque sucio para VMs.
- Su verificación programable.
- Su integración con PVE.
- Su datastore S3, con las limitaciones documentadas.

### 7.3 Qué construimos nosotros

- El manifiesto de imagen versionado.
- La segmentación y reanudación de flujos.
- La captura VSS/snapshot y la composición del disco.
- La restauración P2V/V2V y la reparación de arranque.
- La consola, los planes, la cola y la auditoría.
- La ISO de rescate y el break-glass.

---

## 8. Preguntas abiertas (Fase 0)

1. ¿`--stdin-file` permite checkpoint o reanudación? ¿Qué throughput y memoria consume con 1 TB?
2. ¿La ACL del Repository Server aísla realmente a un agente (listar, restaurar, escribir solo su prefijo)?
3. ¿`sync-to` es seguro con el repositorio en escritura concurrente sobre filesystem local y NFS?
4. ¿Object Lock + `--delete` mantiene un crecimiento aceptable?
5. ¿`blksnap`/`dattobd` funcionan en Debian 12 con Secure Boot y el kernel objetivo?
6. ¿La API de PBS 4.x permite verify/prune/gc/restore con tokens mínimos?
7. ¿Kopia puede restaurar un segmento de un disco de 2 TB a un archivo disperso con un pico de espacio aceptable?

Las respuestas se registran en `decisiones.md` y `anexos/b-poc-0-plan-evidencias.md`.

---

## 9. Fuentes

- Kopia: <https://kopia.io/docs/>, referencia CLI y código fuente.
- Kopia Repository Server: <https://kopia.io/docs/repository-server/>.
- Kopia consistency: <https://kopia.io/docs/advanced/consistency/>.
- UrBackup: <https://www.urbackup.org/features.html>.
- Duplicacy: <https://duplicacy.com/buy.html>.
- PBS: <https://pbs.proxmox.com/docs/storage.html> y notas de PBS 4.0/4.1.
- Proxmox VE API: documentación de `import-from` y `qm importdisk`.

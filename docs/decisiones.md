# Registro de decisiones y verificaciones

**Documento:** decisiones  
**Versión:** 1.2  
**Regla:** ningún `[VERIFICAR]` queda sin fuente, experimento o decisión explícita.

---

## 1. Cómo se usa

- **Resuelto:** afirmación comprobada en documentación oficial, código fuente o prueba real; se cita la fuente y la fecha.
- **Pendiente PoC:** requiere una prueba en Fase 0; se asocia a un experimento y a un criterio de salida.
- **Decisión:** elección tomada por el propietario o por el diseño, con consecuencias.
- **Descartado:** alternativa evaluada y no adoptada, con motivo.

La fecha de verificación es 2026-11-16 salvo que se indique otra.

---

## 2. Verificaciones resueltas

| ID | Afirmación | Fuente | Conclusión |
|---|---|---|---|
| D-01 | Kopia es Apache-2.0 | Documentación y repositorio de Kopia | Apto para uso y redistribución con NOTICE |
| D-02 | Kopia permite snapshot desde stdin | `cli/command_snapshot_create.go`, flag `--stdin-file`; issue #683 | `kopia snapshot create --stdin-file=<nombre>`; snapshot manual |
| D-03 | Kopia verifica snapshots | `cli/command_snapshot_verify.go`; docs de consistencia | `snapshot verify`; `--verify-files-percent`, `--file-parallelism`, `--max-errors` |
| D-04 | Kopia replica repositorios | Docs de Kopia y ejemplos de comunidad | `repository sync-to <backend>` con `--delete`/`--point-in-time`; confirmar flags en la versión fijada |
| D-05 | Kopia soporta Object Lock S3 | Foro/docs de Kopia | `--retention-mode=GOVERNANCE\|COMPLIANCE` + `--retention-period`; bucket con lock y versioning; Kopia no crea el bucket |
| D-06 | Kopia Repository Server tiene ACL | Docs de Repository Server | `kopia server start`, `kopia server user add`, ACL READ/APPEND/FULL |
| D-07 | Los clientes no reciben la contraseña maestra | Docs de Repository Server | El servidor usa `KOPIA_PASSWORD`; el cliente usa usuario/contraseña del servidor |
| D-08 | Las ACL de Kopia no aíslan contenidos | Docs de Repository Server | Solo manifiestos y políticas; contenidos globales por content ID |
| D-09 | Los cambios de usuario de Kopia tardan | Docs de Repository Server | 5–10 min; forzar con `kopia server refresh` o SIGHUP |
| D-10 | Kopia distingue verificación estructural y de contenido | Docs de consistencia | Estructural no descarga blobs; `--verify-files-percent=100` sí |
| D-11 | PBS 4.x soporta S3 | Notas de PBS 4.0/4.1; documentación 4.2.11 | Datastore S3 nativo; caché local 64–128 GiB; una instancia por datastore; PBS no crea bucket ni ACL |
| D-12 | Proxmox VE permite importar disco al crear VM | Docs/foro de PVE; qemu-server | `POST /nodes/{node}/qemu` con `scsiN: <storage>:0,import-from=<path-o-volid>`; origen local; content type `import` en PVE 8.2+ |
| D-13 | UrBackup es AGPLv3+ y tiene limitaciones | Web oficial y foro | Imagen NTFS/extX/XFS, MBR/GPT primarias, sin S3 nativo, dedup por archivo; restore P2V frágil |
| D-14 | Duplicacy no es open source | Página de compra | CLI gratis solo uso personal; GUI/CLI comercial 50 USD/equipo/año; sin imagen completa |
| D-15 | Hay librerías Go para VSS | pkg.go.dev | `SmithOperatingSolutions/go-vss` (COM) y `mxk/go-vss` (WMI); validar madurez |
| D-16 | blksnap/dattobd existen y soportan Debian 12 | README de Veeam blksnap y Datto dattobd | GPL-2, DKMS; blksnap kernels 5.10–6.10; Debian 12 requiere `dh-dkms`; dattobd con fixes para Debian 12 |
| D-17 | El backup agentless de Proxmox es un patrón establecido | Análisis funcional de referencia (docs/02) | PVE 9.1/9.2; backup/recovery cross-platform; run as VM; instant restore; TPM en qcow2 |
| D-18 | Sin CBT propio para Proxmox en fases tempranas | Análisis funcional (docs/02) | El backup de VM depende del snapshot del storage |
| D-19 | LXC no se asume en fases tempranas | Análisis funcional (docs/02) | No se asume soporte; solo VMs |
| D-20 | El flag de Kopia es `--stdin-file`, no `--stdin` | Código fuente | Usar `--stdin-file`; `--stdin` no existe como flag |

---

## 3. Decisiones de arquitectura

| ID | Decisión | Alternativas | Consecuencia | ADR |
|---|---|---|---|---|
| A-01 | Servidor y agentes en Go | Node/TS, híbrido | Un lenguaje, binarios, mTLS | 0001 |
| A-02 | Kopia Repository Server central | Acceso directo por agente | Una custodia de clave; SPOF | 0002 |
| A-03 | PBS para VMs de Proxmox | Implementar backup de VM | No se reimplementa; dependencia de PBS | 0003 |
| A-04 | Manifiesto propio + Kopia como almacén | Formato Kopia puro | Desacopla recuperación; más código | 0004 |
| A-05 | Agente PVE + `import-from` | ISO primero | Rápido; ISO como fallback | 0005 |
| A-06 | River sobre PostgreSQL | Asynq/Redis | Menos servicios; menos ecosistema | 0006 |
| A-07 | Segmentación de flujos | Un solo stream | Reanudación y paralelismo; más snapshots | 0004 |
| A-08 | `vss_volumes` por defecto | `raw_disk` | Evita BitLocker en restore; no captura espacio no asignado | 0004 |
| A-09 | Windows P2V con intervención en Fase 2 | Prometer desatendido | Honestidad; inyección offline en Fase 3 | 0005 |
| A-10 | Sin CBT en Fase 2 | Implementar CBT ya | Menos riesgo; ventanas más largas | 0004 |
| A-11 | **Motor SQL Server: conexión mediante el agente de plataforma instalado en el propio servidor SQL** (decisión del propietario, 2026-11-19) | Conexión remota desde la consola; ambas modalidades | Orquestación y staging locales; las credenciales de SQL no viajan por la red; no se expone TDS a la red de gestión | 0008 |
| A-12 | **Motor SQL Server: backups nativos (Full/Diff/Log) orquestados por T-SQL; alcance todas las bases incluidas las de sistema** (decisión del propietario, 2026-11-19); sin deduplicación de `.bak` y sin copia de `.mdf`/`.ldf` en caliente | Motor propio; copia de `.mdf`/`.ldf`; solo bases de usuario | SQL Server genera y restaura; la plataforma orquesta, almacena y verifica; `tempdb` excluida | 0008 |

---

## 4. Pendientes de prueba real (Fase 0)

| ID | Pregunta | Experimento | Criterio de salida |
|---|---|---|---|
| P-01 | ¿go-vss crea shadow set atómico con ESP/recovery en Server 2025? | G0.1 | Shadow set completo y lectura raw estable |
| P-02 | ¿La vista VSS de BitLocker desbloqueado es en claro? | G0.1 | Restore sin clave o decisión documentada |
| P-03 | ¿`--stdin-file` admite checkpoint/reanudación? | G0.2 | Medición y decisión de segmentación |
| P-04 | ¿La ACL de Kopia aísla a un agente? | G0.3 | Acceso cruzado denegado o dominio de confianza aceptado |
| P-05 | ¿`sync-to` es seguro con escritura concurrente en NAS/NFS? | G0.6 | Réplica consistente o plan B |
| P-06 | ¿`import-from` funciona en dir/lvmthin/zfspool con sparse? | G0.4 | VM creada en los tres |
| P-07 | ¿El cambio SATA→VirtIO reutilizando volid es fiable? | G0.4 | Cambio exitoso y arranque |
| P-08 | ¿La inyección offline de VirtIO funciona con Secure Boot? | G0.4/Fase 3 | Windows arranca sin intervención |
| P-09 | ¿Windows 11 arranca con vTPM nuevo sin clave si el disco es claro? | G0.4 | Arranque o procedimiento documentado |
| P-10 | ¿blksnap/dattobd cargan en Debian 12 con Secure Boot? | G0.7 | Carga y snapshot; decisión de alcance |
| P-11 | ¿La API de PBS 4.x permite verify/prune/gc/restore con tokens mínimos? | Fase 1/2 | Endpoints y permisos documentados |
| P-12 | ¿Cómo se verifica el arranque Windows sin guest agent? | G0.4 | Criterio aceptado (uptime+screendump) |
| P-13 | ¿Cuánta RAM consume el índice de Kopia por TB? | G0.9 | Dimensionamiento |
| P-14 | ¿Rendimiento NAS vs local para el backend? | G0.9 | Throughput aceptable |
| P-15 | ¿Crecimiento de Object Lock tras meses de `--delete`? | G0.6 | Crecimiento medido y aceptable |
| P-16 | ¿El agente PVE escribe en storage `import` sin privilegios extra? | G0.4 | Escritura y import exitosos |
| P-17 | ¿Conexión no interactiva al Repository Server con la versión fijada? | G0.3 | Credenciales por env/flags o config |
| P-18 | ¿River cubre los casos o se necesita Asynq? | Fase 0/1 | Decisión de cola |
| P-19 | ¿Qué versión de Kopia se fija? | Fase 0 | Versión y contract tests |
| P-20 | ¿El NAS permite montaje local y snapshots? | G0.9 | Sí/no y plan B |
| P-21 | ¿Qué mecanismo permite rotar la contraseña del repositorio sin recrearlo? | G0.8 | Procedimiento verificado o decisión de migración a repositorio nuevo |
| P-22 | ¿Cuál es la sintaxis exacta de etiquetas de ACL (`hostname`, `path`, `username`) en la versión fijada? | G0.3 | ACL de ejemplo validada |
| P-23 | ¿`snapshot create --stdin-file` exige un argumento de origen o funciona sin él? | G0.2 | Comando probado y documentado |
| P-24 | ¿Driver TDS en Go (`go-mssqldb`) soporta Windows Auth (SSPI) y SQL Server 2019/2022/2025? | G0.10/Fase 3 | Conexión y descubrimiento verificados |
| P-25 | ¿Enumeración de instancias SQL por registro de Windows es fiable en Server 2025? | G0.10/Fase 3 | Instancias predeterminada y con nombre detectadas |
| P-26 | ¿Qué roles/permisos mínimos exige BACKUP/RESTORE y acceso al staging por la cuenta de servicio SQL? | Fase 3 | Mínimo privilegio documentado y probado |
| P-27 | ¿Campos de LSN de `msdb.dbo.backupset` suficientes para validar cadenas? | Fase 3 | Cadena validada en restore de prueba |
| P-28 | ¿Procedimiento de restauración de `master` (single-user) documentado y probado? | Fase 3/4 | Procedimiento con pasos y evidencia |
| P-29 | ¿Comportamiento de backup en Always On: réplica preferida, `COPY_ONLY` en secundarios? | Fase 4 | Plan de backup por grupo definido |

---

## 5. Decisiones pendientes del propietario

| ID | Pregunta | Impacto | Fase |
|---|---|---|---|
| O-01 | ¿Proveedor S3 y Object Lock? | Réplica e inmutabilidad | 0/3 |
| O-02 | ¿GOVERNANCE o COMPLIANCE? | Coste y reversibilidad | 0/3 |
| O-03 | ¿Custodios de los 5 shares? | Custodia | 0 |
| O-04 | ¿Aprobación de dos personas para P2V? | Operación | 2 |
| O-05 | ¿Ventana de backup y RPO/RTO? | Dimensionamiento | 0/1 |
| O-06 | ¿Se necesita aislamiento por grupo (varios repositorios)? | Arquitectura | 0 |
| O-07 | ¿Se implementa CBT? | Ventana de backup | 3 |
| O-08 | ¿Windows P2V desatendido es requisito o suficiente con intervención? | Alcance Fase 2/3 | 0/2 |

---

## 6. Descartados

| Alternativa | Motivo |
|---|---|
| Node/Fastify/Prisma para el servidor | Se eligió Go para unificar con agentes y simplificar despliegue |
| Redis/BullMQ | River evita un servicio extra; Asynq queda como alternativa |
| Duplicacy | Licencia comercial, sin imagen |
| UrBackup como dependencia | AGPLv3, sin S3 nativo, P2V frágil |
| Kopia como formato de recuperación | Se usa como almacén; el manifiesto es propio |
| ISO como estrategia principal | Se eligió agente PVE + import-from; ISO como fallback |
| Backup de VMs Proxmox propio | PBS ya lo hace mejor |
| CBT en Fase 2 | Riesgo y tiempo; se pospone a Fase 3 |
| Multi-tenant/billing | Fuera de alcance |
| Copia de `.mdf`/`.ldf` en caliente como backup SQL | Inconsistente; no permite restaurar una base concreta ni PITR |
| Motor propio de backup SQL Server | SQL Server ya genera backups nativos, cadenas y LSN; la plataforma orquesta |
| Conexión remota desde la consola sin agente SQL (para el motor SQL) | Requiere TDS en red de gestión y permisos remotos sobre staging; el agente local es más seguro |
| Deduplicación interna de `.bak`/`.trn` | SQL ya comprime; el beneficio es bajo y rompe la equivalencia con el formato nativo |

---

## 7. Actualización

- Cada `[VERIFICAR]` nuevo se añade a la tabla 4 con experimento.
- Cada experimento completado actualiza la tabla 2 y elimina la fila pendiente.
- Las decisiones de arquitectura se registran en `adr/`.
- La fecha de verificación se actualiza al revalidar una fuente.

---

## 8. Decisiones registradas por evidencia — 2026-11-18 (G01, G02, G03, G04)

Evidencias en `labs/evidence/2026-11-18/` (G01.md, G02.md, G03.md, G04.md).
Entorno: Proxmox VE 9.2.25 nodo `pve`, kopia 0.23.1, Windows 11 24H2 real (VM 105 del propietario).

| Punto | Decisión con evidencia |
|---|---|
| **VSS — librería del agente Windows** (G01) | **WMI/CIM (enfoque mxk/go-vss o CIM directo). DESCARTADA SmithOperatingSolutions/go-vss (COM)**: en 24H2 amd64 el CreateShadowCopy devuelve device object vacío («ABI/vtable mismatch»). WMI verificado: sombra de C: en 3,5 s, lectura raw por GLOBALROOT con firma NTFS correcta |
| **VSS — alcance de volúmenes** (G01) | Solo C: y volúmenes de datos se sombrean. **ESP/Recovery no soportan sombra (Win32_ShadowCopy return 4 en <1 s)** → lectura directa vía `\\?\Volume{GUID}\` sin sombra. MSR no aplica. Sombras multi-volumen atómicas requieren `diskshadow` (ausente en Home): por edición, las sombras serán secuenciales |
| **P-23 — stdin-file** (G02) | **FORM A rota en kopia 0.23.1** («no snapshot sources»); **FORM B funciona** (`snapshot create <PATH> --stdin-file=…` con PATH existente) y restaura byte-idéntico |
| **Checkpoint de streaming** (G02) | `stdin-file` **no tiene checkpoint**: un corte re-lee todo el stream (32 GB → 5m49s completos tras corte a 90 s). El dedup mitiga solo con stream idéntico. **El motor de imagen lleva journal propio por segmento** (docs/10); los cortes dejan huérfanos hasta la GC |
| **Throughput de streaming** (G02) | ~95 MB/s sostenidos nodo→repo (32 GB en 5m49s), repo final 23,4 GB por dedup de bloques a cero |
| **P-06 — import-from a zfspool** (G04) | Verificado: import-from a DATA1 (ZFS) restaura un disco íntegro y arrancable (sparse respetado: 9,98 GiB reales de 32) |
| **P-07 — swap SATA→VirtIO reutilizando volid** (G04) | La reconfiguración con volid funciona a nivel de config; validación de arranque pendiente con imagen sana (la 1ª pasada usó una imagen en OOBE) |
| **P-09 — Win11 con vTPM nuevo** (G04) | Sin BitLocker: arranca sin clave con vTPM nuevo (verificado). Con BitLocker: pendiente (se requerirá copiar tpmstate junto al disco) |
| **P-12 — verificación de arranque sin guest agent** (G04) | Criterio aceptado y usado: uptime creciente + screendump (escritorio/login visible). El guest agent es opcional; sin él, ACPI tarda más y no hay exec remoto |
| **P05 — P2V Debian** (G05) | **Verificado**: restauración de Debian 13.7 (ext4 sin LVM, UEFI) arranca a sistema funcionando con SSH. **El conector debe reescribir el netplan** del disco restaurado (cloud-init fija la MAC del origen: match macaddress → match por nombre `en*` con dhcp4). El initramfs NO necesitó corrección en cloud images (virtio incluido). Verificación de arranque por serial + SSH (VGA negra esperada). Variante LVM pendiente |
| **G07 — Captura ext4 sin LVM** (G07) | **blksnap (rama VAL-13.1) compila, carga y genera imagen point-in-time de la raíz ext4 MONTADA** (vbsnap-8-1, verificado con marcador VERSION-1/2 y montaje ro). **DM snapshot es inutilizable para la raíz montada** (apertura exclusiva vs holder del fs, kernel 6.12). blksnap NO está empaquetado en Debian 6.12 → el agente lo instala por versión de kernel (precompilado o DKMS; con SB ON requiere firma MOK — pendiente de variante). Requisito operativo: diff storage en **disco separado** del origen. Para VMs en Proxmox la captura es a nivel de host (ZFS snapshot, ya validado en G04/G05) |
| **G06 — Object Lock S3/MinIO** (G06) | **Verificado en MinIO**: repositorio Kopia directo sobre S3-compatible + restauración byte-idéntica (SHA-256); purga de versiones bloqueadas **DENEGADA** a usuario limitado y a root sin bypass (InvalidRequest). Decisiones: bucket de réplica con Object Lock + GOVERNANCE ≥ RPO; **agentes con credenciales IAM limitadas sin bypass** (root de MinIO salta incluso COMPLIANCE); el motor de réplica trata InvalidRequest/AccessDenied como inmutabilidad activa. Pendiente: AWS S3 real y crecimiento de versiones (P-15) |
| **G06bis — Object Lock en Silo** (G06) | **Segundo proveedor S3-compatible verificado** (Silo/MinIO-fork en LXC Debian 13): réplica kopia sync-to desde el plano de control (endpoint/bucket/creds desde el panel), blobs bloqueados con GOVERNANCE, purga denegada como root. Silo no empaqueta mc ni awscli pero implementa SigV4 completo |
| **P2V — regla de oro del conector** (G04) | El clon debe migrar **efidisk (NVRAM) y tpmstate como copias íntegras** junto al disco: recrearlos manualmente falla (0/3); el clon completo arranca a escritorio (1/1). Además: captura exige sistema configurado y apagado limpio |

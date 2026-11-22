# Backlog

**Documento:** 20-backlog  
**Versión:** 1.2  
**Convención:** `EP-xx` épica, `US-xxx` historia. Prioridad MoSCoW; fase 0–4.

---

## 1. Épicas

| ID | Épica | Fase | Prioridad |
|---|---|---|---|
| EP-01 | PoC y evidencias | 0 | Must |
| EP-02 | Monorepo, CI y despliegue | 1 | Must |
| EP-03 | Identidad, enrolamiento y mTLS | 1 | Must |
| EP-04 | Inventario y heartbeat | 1 | Must |
| EP-05 | Planes, programación y retención | 1 | Must |
| EP-06 | Cola de jobs y catálogo | 1 | Must |
| EP-07 | Backup/restore de archivos con Kopia | 1 | Must |
| EP-08 | Panel mínimo | 1 | Must |
| EP-09 | Seguridad del panel (2FA, RBAC, auditoría) | 1 | Must |
| EP-10 | Alertas y notificaciones | 1 | Must |
| EP-11 | PBS: catálogo y restore básico | 1/2 | Must |
| EP-12 | Motor de imagen y manifiesto | 2 | Must |
| EP-13 | Captura Windows VSS | 2 | Must |
| EP-14 | Captura Linux | 2 | Must |
| EP-15 | Segmentación y reanudación | 2 | Must |
| EP-16 | Agente PVE y `import-from` | 2 | Must |
| EP-17 | Restauración Windows | 2 | Must |
| EP-18 | Restauración Linux | 2 | Must |
| EP-19 | ISO de rescate y break-glass | 2 | Must |
| EP-20 | Verificación niveles 1–2 | 2 | Must |
| EP-21 | Réplica S3 y Object Lock | 3 | Must |
| EP-22 | Verificación niveles 3–4 | 3 | Must |
| EP-23 | Inyección offline de VirtIO | 3 | Should |
| EP-24 | Actualizaciones firmadas | 3 | Must |
| EP-25 | HA y DR del control plane | 3 | Should |
| EP-26 | CBT opt-in | 3 | Could |
| EP-27 | Informes y cumplimiento | 3/4 | Should |
| EP-28 | Búsqueda global y CDP | 4 | Could |
| EP-29 | Instant restore / run VM from backup | 4 | Could |
| EP-30 | Motor SQL Server: backups nativos | 3 | Must |
| EP-31 | Motor SQL Server: recuperación avanzada | 3/4 | Should |
| EP-32 | Motor SQL Server: entornos avanzados | 4 | Could |
| EP-33 | Acceso por pantalla (VNC) — Windows sin dependencia externa | 3 | Should |

---

## 2. Historias

### EP-01 — PoC

| ID | Historia | Aceptación | Fase |
|---|---|---|---|
| US-001 | Ejecutar G0.1 VSS | Shadow set con ESP/recovery/C: y lectura raw documentada | 0 |
| US-002 | Ejecutar G0.2 streaming | 200 GB, corte y reanudación medidos | 0 |
| US-003 | Ejecutar G0.3 ACL | Acceso cruzado probado y decisión | 0 |
| US-004 | Ejecutar G0.4 P2V Windows | VM arrancada y procedimiento | 0 |
| US-005 | Ejecutar G0.5 P2V Debian | VM arrancada y procedimiento | 0 |
| US-006 | Ejecutar G0.6 Object Lock | Réplica y borrado denegado | 0 |
| US-007 | Ejecutar G0.7 ext4 | Decisión blksnap/dattobd/crash | 0 |
| US-008 | Ejecutar G0.8 custodia | Restore en host limpio | 0 |
| US-009 | Ejecutar G0.9 rendimiento | Métricas y dimensionamiento | 0 |

### EP-02 — Monorepo y despliegue

| ID | Historia | Aceptación | Fase |
|---|---|---|---|
| US-010 | Estructura `apps/server`, `apps/agent`, `apps/web` | Compila y tests pasan | 1 |
| US-011 | Docker Compose con PostgreSQL, CP, worker, panel, proxy | `up` levanta todo | 1 |
| US-012 | Migraciones y seed | BD inicial creada | 1 |
| US-013 | CI: lint, test, build, contract tests | Pipeline verde | 1 |

### EP-03 — Identidad

| ID | Historia | Aceptación | Fase |
|---|---|---|---|
| US-020 | Token de enrolamiento de un solo uso | No reutilizable; TTL | 1 |
| US-021 | CSR y emisión de certificado | mTLS funciona | 1 |
| US-022 | Renovación y revocación | Revocado no conecta en <5 min | 1 |
| US-023 | Usuario de Kopia por agente | ACL limitada | 1 |

### EP-04 — Inventario

| ID | Historia | Aceptación | Fase |
|---|---|---|---|
| US-030 | Inventario Windows | Discos, particiones, UEFI, TPM, BitLocker | 1 |
| US-031 | Inventario Linux | Discos, LVM/Btrfs/ZFS, red | 1 |
| US-032 | Heartbeat y estado | Online/offline correcto | 1 |
| US-033 | Histórico por hash | Cambios visibles | 1 |

### EP-05 — Planes

| ID | Historia | Aceptación | Fase |
|---|---|---|---|
| US-040 | CRUD de planes | Validación de cron, rutas, retención | 1 |
| US-041 | Asignación a equipos/grupos | Se aplica sin config individual | 1 |
| US-042 | Retención GFS | Limpieza correcta | 1 |
| US-043 | Ventanas y límites | Se respetan | 1 |
| US-044 | CRUD de grupos y membresía | Un equipo puede estar en varios grupos; sin ciclos | 1 |
| US-045 | Resolución de planes efectivos | Asignación directa prevalece; conflicto visible | 1 |

### EP-06 — Cola

| ID | Historia | Aceptación | Fase |
|---|---|---|---|
| US-050 | River y estados | Jobs fluyen | 1 |
| US-051 | Leases y reintentos | Job vuelve a cola si expira | 1 |
| US-052 | Idempotencia | Misma clave no duplica | 1 |
| US-053 | SSE de eventos | UI recibe progreso | 1 |

### EP-07 — Archivos

| ID | Historia | Aceptación | Fase |
|---|---|---|---|
| US-060 | Backup de archivos | Snapshot creado y reportado | 1 |
| US-061 | Restore de archivos | Archivo restaurado y verificado | 1 |
| US-062 | Navegación de snapshots | Ruta visible | 1/2 |
| US-063 | Exclusiones | Rutas excluidas | 1 |

### EP-08 — Panel

| ID | Historia | Aceptación | Fase |
|---|---|---|---|
| US-070 | Dashboard | Estado, jobs, capacidad | 1 |
| US-071 | Inventario | Tabla y ficha | 1 |
| US-072 | Planes | Crear/asignar | 1 |
| US-073 | Restore points | Listar y restaurar | 1 |
| US-074 | Jobs | Estado y logs | 1 |
| US-075 | Modal de instalación con token de un solo uso | Token visible una vez, caducidad y revocación | 1 |
| US-076 | Explorador de snapshots | Navegar, buscar y descargar por streaming | 1/2 |
| US-077 | Wizard de restauración P2V | Cinco pasos, preflight, step-up y evidencia | 2 |
| US-078 | Permisos por rol en la UI | Acciones ocultas/deshabilitadas y API coherente | 1 |
| US-079 | Estados vacío, error y carga | Diseñados y probados | 1 |

### EP-09 — Seguridad del panel

| ID | Historia | Aceptación | Fase |
|---|---|---|---|
| US-080 | Login + TOTP | Sin TOTP no entra | 1 |
| US-081 | RBAC | Roles aplicados | 1 |
| US-082 | Auditoría | Acciones registradas | 1 |
| US-083 | Secretos cifrados | Nada en claro | 1 |

### EP-10 — Alertas

| ID | Historia | Aceptación | Fase |
|---|---|---|---|
| US-090 | Email | Fallo notifica | 1 |
| US-091 | Webhook | Evento enviado | 1 |
| US-092 | Deduplicación | No spam | 1 |
| US-093 | Acuse/resolución | Estado correcto | 1 |

### EP-11 — PBS

| ID | Historia | Aceptación | Fase |
|---|---|---|---|
| US-100 | Configurar PBS | Conexión probada | 1/2 |
| US-101 | Lanzar vzdump | Snapshot creado | 2 |
| US-102 | Catálogo | Snapshots listados | 2 |
| US-103 | Restore | VM restaurada | 2 |
| US-104 | Verify/prune/gc | Operaciones auditadas | 2 |

### EP-12 — Imagen

| ID | Historia | Aceptación | Fase |
|---|---|---|---|
| US-110 | Manifiesto v1 | Esquema validado | 2 |
| US-111 | Segmentación | Segmentos y hashes | 2 |
| US-112 | Journal y reanudación | Corte y reanudación | 2 |
| US-113 | Snapshot del manifiesto | Autocontención | 2 |

### EP-13 — VSS

| ID | Historia | Aceptación | Fase |
|---|---|---|---|
| US-120 | Shadow set | ESP/recovery/C: | 2 |
| US-121 | Lectura raw | Datos correctos | 2 |
| US-122 | Delta | Aborto por umbral | 2 |
| US-123 | BitLocker | Detección y aviso | 2 |

### EP-14 — Linux

| ID | Historia | Aceptación | Fase |
|---|---|---|---|
| US-130 | LVM snapshot | Captura y limpieza | 2 |
| US-131 | Btrfs/ZFS | Captura y limpieza | 2 |
| US-132 | ext4 crash-consistent | Imagen y advertencia | 2 |
| US-133 | Layout | sfdisk/sgdisk | 2 |

### EP-15 — Reanudación

| ID | Historia | Aceptación | Fase |
|---|---|---|---|
| US-140 | Estado por segmento | Sin reenvío | 2 |
| US-141 | Verificación de snapshots | Salta completados | 2 |
| US-142 | Nombres deterministas | Sin duplicados | 2 |

### EP-16 — Agente PVE

| ID | Historia | Aceptación | Fase |
|---|---|---|---|
| US-150 | Instalación | Servicio activo | 2 |
| US-151 | Descubrimiento | Nodos/storages/VMs | 2 |
| US-152 | Staging | Archivo disperso | 2 |
| US-153 | import-from | VM creada | 2 |
| US-154 | Verificación | Arranque detectado | 2 |

### EP-17 — Windows restore

| ID | Historia | Aceptación | Fase |
|---|---|---|---|
| US-160 | SATA+e1000 | Arranque | 2 |
| US-161 | VirtIO asistido | Cambio de bus | 2 |
| US-162 | BCD | Reparación documentada | 2 |
| US-163 | BitLocker | Aviso y decisión | 2 |

### EP-18 — Linux restore

| ID | Historia | Aceptación | Fase |
|---|---|---|---|
| US-170 | VirtIO | Arranque | 2 |
| US-171 | chroot/initramfs | Arranque | 2 |
| US-172 | Red | DHCP/nombres | 2 |
| US-173 | Consola serie | Evidencia | 2 |

### EP-19 — ISO

| ID | Historia | Aceptación | Fase |
|---|---|---|---|
| US-180 | Construcción | ISO arranca BIOS/UEFI | 2 |
| US-181 | Restore bare-metal | Disco restaurado | 2 |
| US-182 | Break-glass | Sin control plane | 2 |
| US-183 | Firma/hash | Verificable | 2 |

### EP-20 — Verificación

| ID | Historia | Aceptación | Fase |
|---|---|---|---|
| US-190 | Nivel 1 | Programado | 2 |
| US-191 | Nivel 2 | Muestra/100 % | 2 |
| US-192 | Evidencia | Panel y logs | 2 |

### EP-21 — Réplica

| ID | Historia | Aceptación | Fase |
|---|---|---|---|
| US-200 | sync-to | Réplica completa | 3 |
| US-201 | Object Lock | Borrado denegado | 3 |
| US-202 | Verificación destino | Snapshots presentes | 3 |
| US-203 | Alertas | Atraso/capacidad | 3 |

### EP-22 — Niveles 3–4

| ID | Historia | Aceptación | Fase |
|---|---|---|---|
| US-210 | Nivel 3 | Imagen reconstruida | 3 |
| US-211 | Nivel 4 | VM arranca | 3 |
| US-212 | VM temporal | Aislada y destruida | 3 |

### EP-23 — VirtIO offline

| ID | Historia | Aceptación | Fase |
|---|---|---|---|
| US-220 | Inyección | Windows arranca | 3 |
| US-221 | Secure Boot | Claves correctas | 3 |
| US-222 | Rollback | Reversible | 3 |

### EP-24 — Actualizaciones

| ID | Historia | Aceptación | Fase |
|---|---|---|---|
| US-230 | Firma | Binario rechazado sin firma | 3 |
| US-231 | Rollout | Por porcentaje | 3 |
| US-232 | Rollback | Versión anterior | 3 |

### EP-25 — HA/DR

| ID | Historia | Aceptación | Fase |
|---|---|---|---|
| US-240 | Réplicas CP | Sin downtime | 3 |
| US-241 | Backup de BD | Restore probado | 3 |
| US-242 | DR desde S3 | RTO medido | 3 |

### EP-26 — CBT

| ID | Historia | Aceptación | Fase |
|---|---|---|---|
| US-250 | USN Windows | Cambios detectados | 3 |
| US-251 | blksnap/dattobd | Cambios detectados | 3 |
| US-252 | Validación | Comparación con full | 3 |

### EP-27 — Informes

| ID | Historia | Aceptación | Fase |
|---|---|---|---|
| US-260 | Informe periódico | Datos reales | 3/4 |
| US-261 | Cumplimiento | Retención y verificación | 3/4 |

### EP-28/29 — Avanzado

| ID | Historia | Aceptación | Fase |
|---|---|---|---|
| US-270 | Búsqueda global | Archivo localizado | 4 |
| US-271 | CDP | Cambios continuos | 4 |
| US-272 | Instant restore | VM desde backup | 4 |

### EP-30 — Motor SQL Server: backups nativos

| ID | Historia | Aceptación | Fase |
|---|---|---|---|
| US-280 | Registro y descubrimiento de instancias (RF-40) | Instancias, versión, edición, bases y modelo de recuperación visibles | 3 |
| US-281 | Backups Full/Diff/Log con compresión y checksum (RF-41) | Cadena completa generada por plan | 3 |
| US-282 | Staging local y transferencia al repositorio | Archivo accesible por la cuenta de servicio SQL; transferido y limpiado | 3 |
| US-283 | Metadatos, cadenas y LSN (RF-41) | Cadena reconstruida desde el catálogo | 3 |
| US-284 | Restauración de una base completa (RF-42) | Base restaurada en otra instancia y operativa | 3 |
| US-285 | Bases de sistema master/model/msdb (RF-46) | Registradas y copiadas; procedimiento de master documentado | 3/4 |
| US-286 | `RESTORE VERIFYONLY` programado (RF-44) | Cada backup verificado y alerta si falla | 3 |
| US-287 | Custodia de certificados de cifrado y TDE (RF-45) | Certificados registrados; alerta si falta | 3 |

### EP-31 — Motor SQL Server: recuperación avanzada

| ID | Historia | Aceptación | Fase |
|---|---|---|---|
| US-290 | Point-in-Time Recovery con `STOPAT` (RF-43) | Base recuperada a una hora concreta | 3/4 |
| US-291 | Restaurar con otro nombre/instancia/ubicación (RF-43) | `MOVE` aplicado y base operativa | 3/4 |
| US-292 | Restauración de prueba en instancia aislada (RF-44) | VM/instancia temporal creada y destruida | 3/4 |
| US-293 | `DBCC CHECKDB` programado (RF-44) | Resultado en el catálogo | 3/4 |
| US-294 | Restauración de certificados y claves (RF-45) | Backup cifrado restaurado en otra instancia | 3/4 |

### EP-32 — Motor SQL Server: entornos avanzados

| ID | Historia | Aceptación | Fase |
|---|---|---|---|
| US-300 | Always On Availability Groups (réplica preferida) | Backup en la réplica preferida; `COPY_ONLY` en secundarios | 4 |
| US-301 | Políticas por instancia y base de datos | Configuración granular aplicada | 4 |
| US-302 | Alertas de RPO e informes de protección SQL | RPO atrasado alerta; informe generado | 4 |

### EP-33 — Acceso por pantalla (VNC): Windows sin dependencia externa

El acceso por pantalla v0.15.0 funciona ya en macOS (Compartir pantalla
nativo) y Linux (x11vnc bajo demanda). En Windows requiere un servidor VNC
ya instalado en el equipo (escucha solo en localhost). Esta épica elimina
esa dependencia externa.

| ID | Historia | Aceptación | Fase |
|---|---|---|---|
| US-310 | Servidor RFB propio en el agente Windows | Escritorio visible en el panel sin VNC de terceros; captura por GDI/DXGI vía syscalls (CGO_ENABLED=0); solo loopback | 3 |
| US-311 | Decisión documentada sobre helper externo | Si se evalúa bundlar un VNC (GPL): análisis de licencia en NOTICE y del impacto en bundles/SHA256SUMS del release | 3 |

---

## 3. MVP (Fase 1)

Historias Must de EP-02 a EP-11, excluyendo imagen/P2V/ISO/S3 y el motor de SQL Server (EP-30–EP-32, Fase 3).

---

## 4. Definition of Done

- Código compila y tests pasan.
- Requisito enlazado en `21-matriz-trazabilidad.md`.
- Documentación actualizada.
- Evidencia de prueba.
- Auditoría y logs.
- Sin secretos.
- Revisión de seguridad para cambios sensibles.
- Runbook si aplica.

---

## 5. Dependencias

- EP-03 antes de EP-04/05/07.
- EP-06 antes de EP-07/11.
- EP-12 antes de EP-13/14/15.
- EP-15 antes de EP-16/17/18.
- EP-16 antes de EP-17/18.
- EP-20 antes de EP-22.
- EP-21 antes de EP-25/DR.
- EP-03/06/21 antes de EP-30 (agente, cola y repositorio operativos); EP-30 antes de EP-31/32.

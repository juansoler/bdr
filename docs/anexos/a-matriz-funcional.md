# Anexo A — Matriz funcional detallada

**Documento:** anexos/a-matriz-funcional  
**Versión:** 1.0  
**Uso:** referencia para priorizar el producto y justificar lo que queda fuera de alcance.

---

## 1. Backup

| Funcionalidad | Descripción | Nuestra decisión | Fase | Nota |
|---|---|---|---|---|
| Full Image Backup | Imagen de discos y particiones | Sí | 2 | Manifiesto propio |
| File-Level Backup | Archivos y carpetas | Sí | 1 | Kopia |
| Incremental Backup | Cambios desde la última copia | Sí | 1/2 | Archivos real; imagen por dedup |
| Differential Backup | Cambios desde el último full | No | — | Incremental + retención cubre |
| Block-Level Backup | Bloques modificados (CBT) | Opt-in | 3 | USN/blksnap/dattobd |
| Deduplication | Evita almacenar repetido | Sí | 1 | Kopia por chunks |
| Compression | Reduce tamaño | Sí | 1 | Zstd |
| Encryption | Cifrado en cliente | Sí | 1 | AES-256-GCM |
| Scheduling | Programación | Sí | 1 | Planes y ventanas |
| Retention | Conservación | Sí | 1 | GFS |
| Multiple Destinations | Varios destinos | Sí | 1/3 | NAS + S3 |
| Validation | Comprobación | Sí | 2/3 | Niveles 1–3 |
| Application-Aware | Consistencia SQL/AD | Parcial | 2/3 | VSS writers |
| CDP | Captura continua | No | 4 | Backlog |
| Immutable | Inmutabilidad | Sí | 3 | Object Lock |
| Replication | Réplica | Sí | 3 | sync-to |
| Search | Buscar archivos | Sí, básica | 2/4 | Navegación |
| NAS Backup | Carpetas compartidas | Sí | 1 | Rutas montadas |
| Tape/SAN | Cintas/SAN | No | — | Fuera de alcance |

---

## 2. Recuperación

| Funcionalidad | Descripción | Nuestra decisión | Fase | Nota |
|---|---|---|---|---|
| File Recovery | Archivos/carpetas | Sí | 1 | Kopia |
| Bare-Metal | Sin SO | Sí | 2 | ISO |
| Universal Restore | Hardware distinto | Sí | 2/3 | P2V/V2V |
| P2V | Físico a VM | Sí | 2 | Agente PVE |
| V2V | VM a VM | Sí | 2 | Mismo flujo |
| V2P | VM a físico | No | — | Evolutivo |
| Restore to new VM | VM nueva | Sí | 2 | import-from |
| Instant Restore | VM desde backup | No | 4 | Backlog |
| Run VM from Backup | Arrancar desde repo | No | 4 | Backlog |
| One-Click | Recuperación simple | Sí | 2 | Wizard |
| Granular | Archivos/particiones/discos | Sí | 1/2 | — |
| Automated Testing | Prueba de arranque | Sí | 3 | Nivel 4 |
| DR/Runbooks | Failover/failback | Parcial | 3/4 | Runbooks |
| Cross-platform | Entre hipervisores | Sí | 2 | P2V |

---

## 3. Virtualización

| Funcionalidad | Descripción | Nuestra decisión | Fase | Nota |
|---|---|---|---|---|
| Proxmox agentless | Backup de VMs sin agente | Sí, PBS | 1/2 | No reimplementar |
| Proxmox restore | Restaurar VM | Sí, PBS | 2 | API |
| Proxmox instant restore | VM desde backup | No | 4 | Backlog |
| Cross-platform recovery | P2V/V2V | Sí | 2 | Agente PVE |
| TPM qcow2 | TPM en disco | Sí, vTPM | 2 | Windows 11 |
| LXC | Contenedores | No confirmado | — | No asumir |
| CBT Proxmox | Bloques cambiados | No | — | Limitación de la referencia comercial |

---

## 4. Gestión

| Funcionalidad | Descripción | Nuestra decisión | Fase |
|---|---|---|---|
| Consola central | Inventario y planes | Sí | 1 |
| Alertas email | Fallos/ausencia | Sí | 1 |
| Webhooks | Integraciones | Sí | 1 |
| Informes | Estado/cumplimiento | Sí | 3 |
| Auditoría | Acciones | Sí | 1 |
| 2FA | TOTP | Sí | 1 |
| RBAC | Roles | Sí | 1 |
| Grupos | Agrupación | Sí | 1 |
| API | Integraciones | Sí | 1 |
| Multi-tenant | Clientes | No | — |
| Facturación | Licencias | No | — |

---

## 5. Ciberseguridad (fuera de alcance)

| Funcionalidad | Nuestra decisión |
|---|---|
| Antimalware/antivirus | No |
| EDR | No |
| Prevención de exploits | No |
| Evaluación de vulnerabilidades | No |
| Gestión de parches | No |
| Filtrado de URL | No |
| Escritorio remoto | No |
| Data Discovery | No |
| HDD Health | Solo indicador básico, no módulo |
| Archivado M365 | No |
| Google Workspace | No |

---

## 6. Almacenamiento y nube

| Funcionalidad | Nuestra decisión | Fase |
|---|---|---|
| NAS local | Sí | 1 |
| S3 compatible | Sí | 3 |
| la referencia comercial Cloud | No | — |
| Object Lock | Sí | 3 |
| Replicación | Sí | 3 |
| Archivado | No | — |
| Cintas | No | — |

---

## 7. Resumen de prioridades

- **Must:** imagen, archivos, incremental/dedup, cifrado, planes, retención, multi-destino, validación, bare-metal, P2V, PBS, granular, inmutabilidad, verificación.
- **Should:** informes, búsqueda, aplicación-aware.
- **Could:** instant restore, CDP, V2P, run VM from backup.
- **Won't:** ciberseguridad, parches, escritorio remoto, M365, multi-tenant, facturación, cintas.

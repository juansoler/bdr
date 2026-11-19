# Índice de documentación

**Versión del paquete:** 1.3  
**Fecha:** 19 de noviembre de 2026  
**Estado:** diseño aprobado; implementación no iniciada.

Este índice resume qué contiene cada documento, su estado y de qué depende. El paquete completo está en español; el código y los comentarios técnicos podrán estar en inglés.

---

## 1. Estado

| Estado | Significado |
|---|---|
| **Aprobado** | Revisado y vigente como contrato |
| **Revisión** | Contenido completo pendiente de validación en Fase 0 |
| **Plantilla** | Estructura lista para completar con evidencia |
| **Histórico** | Se conserva por trazabilidad; no se edita |

La versión del paquete es 1.3. Cada documento indica su propia versión y estado; los cambios de esta revisión están en `23-registro-de-cambios.md`. Los puntos que requieren prueba real están en `decisiones.md` y `anexos/b-poc-0-plan-evidencias.md`.

---

## 2. Documentos

| Documento | Contenido | Estado | Depende de |
|---|---|---|---|
| [`00-informe-ejecutivo.md`](00-informe-ejecutivo.md) | Visión, alcance, arquitectura, fases, riesgos, construir/comprar | Aprobado | 01–03, decisiones |
| [`01-especificacion-maestra.md`](01-especificacion-maestra.md) | 46 RF + 12 RNF, prioridades y criterios de aceptación | Aprobado | 02, 03, 04 |
| [`02-analisis-funcional.md`](02-analisis-funcional.md) | Análisis funcional del alcance y decisión de réplica | Aprobado | — |
| [`03-evaluacion-motores.md`](03-evaluacion-motores.md) | Kopia, UrBackup, Duplicacy, PBS | Aprobado | — |
| [`04-arquitectura.md`](04-arquitectura.md) | Componentes, flujos, puertos, fallos, interfaces | Aprobado | 01, 05, 06 |
| [`05-modelo-datos.md`](05-modelo-datos.md) | Modelo PostgreSQL, grupos, invariantes, retención de metadatos | Aprobado | 04, 06 |
| [`06-api.md`](06-api.md) | Contrato REST de agentes y administración | Aprobado | 05 |
| [`07-agente-windows.md`](07-agente-windows.md) | Agente Windows, VSS, BitLocker, segmentación | Revisión | 10, 11 |
| [`08-agente-linux.md`](08-agente-linux.md) | Agente Linux, LVM/Btrfs/ZFS, ext4, reanudación | Revisión | 10, 11 |
| [`09-agente-proxmox-pbs.md`](09-agente-proxmox-pbs.md) | Agente PVE, staging, `import-from`, PBS | Revisión | 11, 15 |
| [`10-motor-imagen-manifiestos.md`](10-motor-imagen-manifiestos.md) | Formato de imagen, manifiesto multi-disco, CBT | Aprobado | 05, `esquemas/` |
| [`11-restauracion-p2v-proxmox.md`](11-restauracion-p2v-proxmox.md) | Preflight, VM, arranque, reparación | Revisión | 07–10 |
| [`12-iso-rescate.md`](12-iso-rescate.md) | ISO, bare-metal, break-glass | Revisión | 14 |
| [`13-seguridad.md`](13-seguridad.md) | Amenazas, PKI, mTLS, secretos, mínimo privilegio | Aprobado | 06, 14 |
| [`14-custodia-clave-maestra.md`](14-custodia-clave-maestra.md) | Shamir 3-de-5, break-glass, rotación | Aprobado | 12, 13 |
| [`15-almacenamiento-retencion.md`](15-almacenamiento-retencion.md) | NAS, S3, Object Lock, GFS, capacidad, PBS S3 | Revisión | 13 |
| [`16-verificacion-pruebas.md`](16-verificacion-pruebas.md) | Niveles 1–5, matriz de pruebas, evidencias | Aprobado | 11, 15 |
| [`17-operacion-runbooks.md`](17-operacion-runbooks.md) | Alta, revocación, restore, DR, ransomware | Aprobado | 11–16 |
| [`18-riesgos.md`](18-riesgos.md) | Registro de riesgos y aceptación | Aprobado | 00, 19 |
| [`19-roadmap.md`](19-roadmap.md) | Fases, puertas y esfuerzo | Aprobado | 18, 20 |
| [`20-backlog.md`](20-backlog.md) | Épicas e historias | Aprobado | 19 |
| [`21-matriz-trazabilidad.md`](21-matriz-trazabilidad.md) | RF/RNF → documento → prueba → fase | Aprobado | 01, 16 |
| [`22-panel-ui-ux.md`](22-panel-ui-ux.md) | Navegación, pantallas, wizard, permisos, accesibilidad | Aprobado | 01, 06, 11 |
| [`24-motor-sql-server.md`](24-motor-sql-server.md) | Motor SQL Server: descubrimiento, backups nativos, cadenas, restauración, verificación, certificados | Revisión | 04, 05, 06, 13, 15, `adr/0008` |
| [`23-registro-de-cambios.md`](23-registro-de-cambios.md) | Historial de versiones del paquete | Histórico | Todos |
| [`decisiones.md`](decisiones.md) | Verificaciones, decisiones y pendientes de PoC | Aprobado | — |
| [`glosario.md`](glosario.md) | Términos y siglas | Aprobado | — |
| [`poc-restauracion.md`](poc-restauracion.md) | Registro de evidencias de Fase 0 | Plantilla | anexos/b |

---

## 3. Anexos y ADRs

| Documento | Contenido |
|---|---|
| [`anexos/a-matriz-funcional.md`](anexos/a-matriz-funcional.md) | Matriz funcional detallada y decisiones |
| [`anexos/b-poc-0-plan-evidencias.md`](anexos/b-poc-0-plan-evidencias.md) | Plan de los 9 experimentos de Fase 0 |
| [`anexos/c-comandos-kopia-verificados.md`](anexos/c-comandos-kopia-verificados.md) | Comandos de Kopia y cautelas |
| [`anexos/d-compatibilidad-windows-linux.md`](anexos/d-compatibilidad-windows-linux.md) | Matriz de sistemas, firmware y storages |
| [`anexos/e-contrato-cli-kopia.md`](anexos/e-contrato-cli-kopia.md) | Contrato de la CLI y contract tests |
| [`anexos/f-diagramas.md`](anexos/f-diagramas.md) | Diagramas Mermaid |
| [`esquemas/manifest-v1.schema.json`](esquemas/manifest-v1.schema.json) | JSON Schema del manifiesto |
| [`esquemas/README.md`](esquemas/README.md) | Uso y validación de esquemas |
| [`adr/0001-go-server-agente.md`](adr/0001-go-server-agente.md) | Go para servidor y agentes |
| [`adr/0002-kopia-repository-server.md`](adr/0002-kopia-repository-server.md) | Repository Server central |
| [`adr/0003-pbs-para-vms-proxmox.md`](adr/0003-pbs-para-vms-proxmox.md) | PBS para VMs |
| [`adr/0004-imagen-kopia-con-manifiesto-propio.md`](adr/0004-imagen-kopia-con-manifiesto-propio.md) | Manifiesto propio |
| [`adr/0005-restauracion-pve-agent-import-from.md`](adr/0005-restauracion-pve-agent-import-from.md) | Agente PVE e `import-from` |
| [`adr/0006-cola-river-postgresql.md`](adr/0006-cola-river-postgresql.md) | River sobre PostgreSQL |
| [`adr/0007-store-postgres-jsonb.md`](adr/0007-store-postgres-jsonb.md) | Estado como documento JSONB transaccional |
| [`adr/0008-motor-sql-server-backups-nativos.md`](adr/0008-motor-sql-server-backups-nativos.md) | Motor SQL Server: backups nativos orquestados por el agente |

---

## 4. Rutas de lectura

### Dirección y aprobación

1. `00-informe-ejecutivo.md`
2. `01-especificacion-maestra.md`
3. `18-riesgos.md`
4. `19-roadmap.md`

### Implementación del control plane

1. `04-arquitectura.md`
2. `05-modelo-datos.md`
3. `06-api.md`
4. `13-seguridad.md`
5. `22-panel-ui-ux.md`

### Implementación de agentes y recuperación

1. `07-agente-windows.md`
2. `08-agente-linux.md`
3. `10-motor-imagen-manifiestos.md`
4. `09-agente-proxmox-pbs.md`
5. `24-motor-sql-server.md`
6. `11-restauracion-p2v-proxmox.md`
7. `12-iso-rescate.md`

### Operación y recuperación

1. `14-custodia-clave-maestra.md`
2. `15-almacenamiento-retencion.md`
3. `16-verificacion-pruebas.md`
4. `17-operacion-runbooks.md`

### Antes de programar

1. `anexos/b-poc-0-plan-evidencias.md`
2. `../labs/README.md` (kit ejecutable y captura de entorno)
3. `decisiones.md` (tabla de pendientes)
4. `poc-restauracion.md`

---

## 5. Convenciones

- `RF-xx` y `RNF-xx` son los identificadores de requisitos.
- **Must / Should / Could / Won't** son prioridades MoSCoW.
- **Fase 0–4** según `19-roadmap.md`.
- `[VERIFICAR]` marca una afirmación que necesita fuente oficial o prueba real; se resuelve en `decisiones.md`.
- «Diseñado» no significa «implementado» ni «verificado».
- Una cifra de capacidad o rendimiento sin fuente es ilustrativa y debe medirse en Fase 0.

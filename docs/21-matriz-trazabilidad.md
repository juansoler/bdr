# Matriz de trazabilidad

**Documento:** 21-matriz-trazabilidad  
**Versión:** 1.2  
**Regla:** ningún requisito Must se marca como cumplido sin documento, prueba y evidencia.

Estados: **Diseñado** (documentado, sin código), **En curso**, **Verificado** (prueba con evidencia), **Pendiente PoC** (requiere Fase 0).

---

## 1. Requisitos funcionales

| ID | Documento principal | Prueba | Fase | Estado |
|---|---|---|---|---|
| RF-01 Imagen completa | 07, 08, 10, 11 | P2V Windows/Linux | 2 | Pendiente PoC |
| RF-02 Archivos/carpetas | 07, 08, 06 | Restore de archivo | 1 | Diseñado |
| RF-03 Incrementales/dedup | 03, 10, 15 | Segundo backup sin cambios | 1/2 | Pendiente PoC |
| RF-04 Compresión | 03, 10, 15 | Restore expande correctamente | 1 | Diseñado |
| RF-05 Cifrado cliente | 03, 13, 14 | Backend ilegible sin clave | 1 | Diseñado |
| RF-06 VSS Windows | 07, 10 | Backup con escrituras | 2 | Pendiente PoC |
| RF-07 Snapshots Linux | 08, 10 | Debian ext4/LVM restore | 2 | Pendiente PoC |
| RF-08 Planes | 01, 05, 06, 17, 22 | Plan a grupo | 1 | Diseñado |
| RF-09 Retención GFS | 05, 15 | Limpieza correcta | 1 | Diseñado |
| RF-10 Réplica NAS→S3 | 04, 15 | Fallo S3 no bloquea | 3 | Diseñado |
| RF-11 PBS VMs | 09, 15 | Backup/restore VM | 1/2 | Pendiente PoC |
| RF-12 Inventario | 07, 08, 05 | Panel muestra datos | 1 | Diseñado |
| RF-13 Manifiesto | 10 | Versión desconocida rechazada | 2 | Diseñado |
| RF-14 Reanudación | 07, 08, 10 | Corte y reanudación | 2 | Pendiente PoC |
| RF-15 Agente offline | 07, 08, 17 | Corte de red | 1 | Diseñado |
| RF-16 Límites | 07, 08 | Throughput y prioridad | 1 | Diseñado |
| RF-17 Restore archivos | 06, 07, 08, 17 | Archivo restaurado | 1 | Diseñado |
| RF-18 Bare-metal ISO | 11, 12, 17 | Disco restaurado | 2 | Pendiente PoC |
| RF-19 P2V/V2V Proxmox | 11, 17, 22 | VM arranca | 2 | Pendiente PoC |
| RF-20 Restore PBS | 09, 17 | VM arranca | 2 | Pendiente PoC |
| RF-21 Verificación automática | 16 | VM temporal | 3 | Diseñado |
| RF-22 Verificación 1–3 | 16 | Niveles ejecutados | 2/3 | Diseñado |
| RF-23 Break-glass | 12, 14, 17 | Restore sin CP | 2 | Pendiente PoC |
| RF-24 Preflight/avisos | 11, 16, 22 | Aborto accionable | 2 | Diseñado |
| RF-25 Panel | 00, 06, 20, 22 | Operador identifica equipo | 1 | Diseñado |
| RF-26 Alertas | 04, 17, 22 | Fallo notifica | 1 | Diseñado |
| RF-27 Informes | 00, 19 | Informe real | 3 | Diseñado |
| RF-28 Registro/revocación | 04, 06, 13, 22 | Token no reutilizable | 1 | Diseñado |
| RF-29 Auditoría | 05, 13 | Acción registrada | 1 | Diseñado |
| RF-30 2FA | 06, 13, 22 | Sin TOTP no entra | 1 | Diseñado |
| RF-31 Grupos | 05, 06, 22 | Plan automático y conflicto resuelto | 1 | Diseñado |
| RF-32 Navegación snapshots | 06, 16, 22 | Archivo localizado | 2 | Diseñado |
| RF-33 Repositorios/destinos | 04, 06, 15 | Destino probado | 1 | Diseñado |
| RF-34 API versionada | 06 | Registro y consulta | 1 | Diseñado |
| RF-35 Actualizaciones firmadas | 07, 08, 13 | Binario sin firma rechazado | 3 | Diseñado |
| RF-36 Inmutabilidad | 13, 15 | Borrado denegado | 3 | Pendiente PoC |
| RF-37 Credenciales separadas | 13, 15 | Agente no borra repo | 3 | Diseñado |
| RF-38 Custodia de clave | 14 | Simulacro 3/5 | 0/1 | Pendiente PoC |
| RF-39 Eventos de seguridad | 13, 22 | Alerta de borrado masivo | 3 | Diseñado |
| RF-40 Descubrimiento SQL | 24 | Instancia registrada con bases visibles | 3 | Diseñado |
| RF-41 Backups nativos SQL | 24 | Cadena Full/Diff/Log generada | 3 | Diseñado |
| RF-42 Restauración de base | 24 | Base restaurada en otra instancia | 3 | Diseñado |
| RF-43 PITR SQL | 24 | Base recuperada a una hora concreta | 3/4 | Diseñado |
| RF-44 Verificación SQL | 24 | `VERIFYONLY` registrado y alerta | 3 | Diseñado |
| RF-45 Certificados SQL/TDE | 24 | Backup cifrado restaurado con certificado | 3 | Diseñado |
| RF-46 Bases de sistema | 24 | master/model/msdb copiados; master probado | 3/4 | Diseñado |

---

## 2. Requisitos no funcionales

| ID | Documento principal | Prueba | Fase | Estado |
|---|---|---|---|---|
| RNF-01 mTLS | 04, 13 | Certificado ajeno rechazado | 1 | Diseñado |
| RNF-02 Integridad | 10, 16 | Corrupción detectada | 1/2 | Diseñado |
| RNF-03 Resiliencia | 07, 08, 16 | Corte/reinicio | 1/2 | Pendiente PoC |
| RNF-04 Rendimiento | 07, 08 | Límites respetados | 1 | Diseñado |
| RNF-05 Observabilidad | 04 | Traza por job_id | 1 | Diseñado |
| RNF-06 Reproducibilidad | 04 | `compose up` limpio | 1 | Diseñado |
| RNF-07 Compatibilidad | 16, anexos/d | Matriz por versión | 1/2 | Pendiente PoC |
| RNF-08 Idioma | README, UI | Revisión | 1 | Diseñado |
| RNF-09 Escalabilidad | 04, 16 | 50 agentes | 3 | Diseñado |
| RNF-10 Mantenibilidad Kopia | 03, 04, anexos/e | Contract tests | 1 | Diseñado |
| RNF-11 Recuperación plataforma | 14, 17 | Reconstrucción | 1 | Diseñado |
| RNF-12 Licencias | 03, README | Revisión | 1 | Diseñado |

---

## 3. Requisitos de referencia mapeados

| Capacidad | Requisito | Documento |
|---|---|---|
| Full image | RF-01 | 02, 10 |
| File-level | RF-02 | 02, 07, 08 |
| Incremental | RF-03 | 02, 03 |
| Dedup/compression | RF-03/04 | 02, 03 |
| Encryption | RF-05 | 02, 13 |
| Scheduling/retention | RF-08/09 | 02, 05 |
| Multiple destinations | RF-10 | 02, 15 |
| Validation | RF-21/22 | 02, 16 |
| Bare-metal | RF-18 | 02, 12 |
| P2V/V2V | RF-19 | 02, 11 |
| Proxmox agentless | RF-11 | 02, 09 |
| Granular recovery | RF-17/32 | 02, 06 |
| Immutability | RF-36 | 02, 15 |
| Cross-platform | RF-19 | 02, 11 |
| SQL Server backup | RF-40–RF-46 | 24 |
| Cyber modules | Fuera de alcance | 02 |

---

## 4. Cobertura por fase

| Fase | Requisitos principales |
|---|---|
| 0 | RF-38, RNF-07, RNF-10; evidencias de RF-01/03/07/14/19/23/36 |
| 1 | RF-02/04/05/08/09/12/15/16/17/25/26/28/29/30/31/33/34; RNF-01/04/05/06/08/11 |
| 2 | RF-01/06/07/11/13/14/18/19/20/22/23/24/32; RNF-02/03/07 |
| 3 | RF-10/21/27/35/36/37/39/40/41/42/44/45; RNF-09 |
| 4 | RF-21 avanzado, RF-43/46, búsqueda, CDP, instant restore |

---

## 5. Reglas de actualización

- Cada historia del backlog enlaza su RF.
- Cada RF Verificado enlaza evidencia (test, log, captura, job).
- Un RF sin prueba no puede pasar a Verificado.
- Un cambio de alcance actualiza esta matriz y `01-especificacion-maestra.md`.
- Los RF fuera de alcance se marcan explícitamente como Won't.

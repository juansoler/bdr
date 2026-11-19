# Registro de cambios del paquete documental

**Documento:** 23-registro-de-cambios  
**Formato:** versiones del paquete, no del producto.

---

## 1. v1.3 — Motor de SQL Server

**Fecha:** 2026-11-19  
**Motivo:** incorporar el quinto motor funcional (protección de bases de datos SQL Server) como módulo independiente, con las decisiones del propietario cerradas.

### Decisiones cerradas por el propietario

| Decisión | Valor | ADR |
|---|---|---|
| Conexión a SQL Server | Agente de plataforma en el propio servidor SQL (orquestación y staging locales) | 0008 (A-11) |
| Alcance de bases | Todas, incluidas `master`/`model`/`msdb`; `tempdb` excluida | 0008 (A-12) |

### Añadido

| Artefacto | Contenido |
|---|---|
| `24-motor-sql-server.md` | Diseño completo del motor SQL: arquitectura, descubrimiento, backups nativos (Full/Diff/Log), cadenas y LSN, bases de sistema, restauración y PITR, verificación, certificados TDE, almacenamiento y fases. |
| `adr/0008-motor-sql-server-backups-nativos.md` | ADR del motor SQL: backups nativos orquestados por el agente, sin motor propio ni copia de `.mdf`/`.ldf` en caliente. |
| RF-40–RF-46 | Nuevos requisitos funcionales (sección 5.5 de la especificación maestra). |
| EP-30/EP-31/EP-32 con US-280–US-302 | Épicas e historias del motor SQL en el backlog. |
| D11/D12 en roadmap | Puntos de decisión del motor SQL, resueltos. |
| P-24–P-29 | Pendientes de verificación (driver TDS, permisos, Always On, restauración de `master`). |
| §23 en `22-panel-ui-ux.md` | Pantalla «Microsoft SQL Server» del panel. |

### Documentos modificados

`01-especificacion-maestra.md` (v1.2: alcance, RF-40–RF-46, RNF-07, MVP), `19-roadmap.md` (v1.2: motor SQL en Fase 3, puerta, «no hacer», D11/D12), `20-backlog.md` (v1.2: EP-30–EP-32, dependencias), `21-matriz-trazabilidad.md` (v1.2: RF-40–RF-46, cobertura por fase, mapeo funcional), `22-panel-ui-ux.md` (v1.1: §23 y navegación), `README.md` (v1.3: índice), `decisiones.md` (v1.2: A-11/A-12, P-24–P-29, descartados).

### Compatibilidad

- Cambio de alcance, no de implementación: no afecta al manifiesto ni a formatos existentes.
- El motor SQL no está en el MVP de Fase 1; su desarrollo se planifica en Fase 3 con interfaces reservadas desde el diseño.

---

## 2. v1.2 — Kit ejecutable de Fase 0

**Fecha:** 2026-11-16  
**Motivo:** convertir el plan de PoC en un laboratorio ejecutable y verificable.

### Añadido

| Artefacto | Contenido |
|---|---|
| `labs/README.md` | Reglas de seguridad, flujo, matriz y puerta. |
| `labs/common/` | Utilidades, captura de entorno Linux/Windows, plantilla de evidencia y creación de VM con `import-from`. |
| `labs/g01-vss/` | Probe COM (`SmithOperatingSolutions/go-vss`) y probe WMI (`mxk/go-vss`), con lectura raw y comparativa. |
| `labs/g02-streaming/` | Script de corte y reanudación de `--stdin-file`. |
| `labs/g03-acl/` | Comprobación de aislamiento de ACL y tiempos de revocación. |
| `labs/g04-p2v-windows/` | Procedimiento P2V, SATA+e1000, VirtIO y BitLocker. |
| `labs/g05-p2v-debian/` | Procedimiento P2V y arreglo de chroot/initramfs. |
| `labs/g06-object-lock/` | Réplica, Object Lock, borrado y coste. |
| `labs/g07-ext4/` | Crash-consistent, blksnap y dattobd. |
| `labs/g08-custody/` | Ceremonia Shamir y break-glass. |
| `labs/g09-performance/` | Medición de dedup, tiempos y crecimiento. |
| `labs/gate-check.sh` | Comprobación de las nueve evidencias. |
| `.gitignore` | Excluye evidencias, secretos y binarios. |

### Verificado en esta máquina

- Captura de entorno Windows ejecutada (solo lectura) y JSON válido.
- `bash -n` en todos los scripts.
- `gofmt` parsea ambos probes.
- Dry-run de `pve-create-vm.sh` con `import-from` y `tpmstate0`.
- `gate-check.sh` detecta correctamente las nueve evidencias ausentes.

### Limitación conocida

- Go local es 1.20.3; los probes requieren Go ≥ 1.21 (WMI) y ≥ 1.25 (COM). Deben compilarse con una versión más reciente.
- Kopia no está instalado en esta máquina; G0.2, G0.3 y G0.9 se ejecutan en el laboratorio Linux.

---

## 3. v1.1 — Revisión de consistencia y cierre de huecos

**Fecha:** 2026-11-16  
**Motivo:** revisión del paquete v1.0 con criterio de auditoría. Se corrigieron defectos que habrían bloqueado la implementación y se añadieron los artefactos que faltaban.

### Defectos corregidos

| ID | Defecto | Corrección | Documentos |
|---|---|---|---|
| F-01 | Los grupos eran requisito (RF-08, RF-31), pero no existían tablas ni endpoints. | Se añadieron `agent_groups` y `agent_group_members`; `plan_assignments` pasa a ser polimórfica con `CHECK num_nonnulls = 1`; se definió la resolución de planes efectivos. | 05, 06, 01, 21 |
| F-02 | `E_VSS_WRITER` y `E_KOPIA_CORRUPT` se usaban sin estar en el catálogo. | Se añadieron al catálogo, junto con `E_PLAN_CONFLICT` y `E_GROUP_CYCLE`. | 06, 22 |
| F-03 | El enum `image_mode` del modelo solo tenía 3 valores; el motor define 6. | Se alineó el enum con el documento 10. | 05 |
| F-04 | El manifiesto usaba `disk_layout` singular: no podía representar una máquina con varios discos. | Se reemplazó por `disks[]`, con índices estables y reglas de orden. | 10 |
| F-05 | RF-12 mencionaba inventario de «software», que está fuera de alcance. | Se cambió a «hardware y sistema operativo». | 01 |
| F-06 | RF-31 era *Should* aunque RF-08 (*Must*) exige asignar planes a grupos. | Se elevó a *Must* con criterio de resolución de conflictos. | 01 |
| F-07 | No había diseño de la interfaz, que era el Sprint 4 de la especificación original. | Se añadió el documento de panel, con navegación, pantallas, wizard, permisos y accesibilidad. | 22 |

### Añadidos

| Artefacto | Contenido |
|---|---|
| `22-panel-ui-ux.md` | Diseño funcional del panel en español, incluido el modal de instalación y el wizard P2V. |
| `esquemas/manifest-v1.schema.json` | JSON Schema Draft 2020-12 del manifiesto de imagen. |
| `esquemas/README.md` | Validación, garantías y límites del esquema. |
| `README.md` en `docs/` | Índice con estado, dependencias y rutas de lectura. |
| `23-registro-de-cambios.md` | Este documento. |
| Retención de metadatos | Valores por defecto para `audit_log`, `job_events` y `job_attempts`. |
| Descarga de archivos por streaming | Endpoint y reglas de seguridad en 06. |
| Informes en API | Endpoints de generación y programación. |

### Decisiones y pendientes

- Se añadieron P-21 (rotación de la contraseña del repositorio), P-22 (sintaxis exacta de etiquetas de ACL) y P-23 (argumento de origen con `--stdin-file`).
- Se añadió la cautela sobre el comando de instalación de una línea en la UI: se trata como secreto y se recomienda el paquete firmado.

### Compatibilidad

- El formato de manifiesto sigue siendo `schema_version: 1`; el cambio de `disk_layout` a `disks` ocurre **antes** de que exista ningún manifiesto real, por lo que no requiere migración.
- El modelo de datos no tiene implementación todavía; los cambios son de diseño.

---

## 4. v1.0 — Paquete inicial

**Fecha:** 2026-11-16

- Informe ejecutivo.
- Especificación maestra con 39 RF y 12 RNF.
- Análisis funcional y evaluación de motores.
- Arquitectura, modelo de datos y API.
- Diseño de agentes Windows, Linux y PVE; motor de imagen; P2V; ISO.
- Seguridad, custodia de clave, almacenamiento, verificación, runbooks, riesgos, roadmap, backlog y trazabilidad.
- ADRs 0001–0006 y anexos A–F.
- Registro de decisiones con 20 verificaciones resueltas y 20 pendientes de PoC.

---

## 5. Cómo registrar un cambio

1. Describir el cambio y el motivo.
2. Identificar documentos afectados y actualizarlos en el mismo commit.
3. Si cambia el alcance, actualizar `01-especificacion-maestra.md` y `21-matriz-trazabilidad.md`.
4. Si cambia una decisión, añadir o actualizar un ADR y `decisiones.md`.
5. Si cambia el manifiesto de forma incompatible, incrementar `schema_version` y crear un esquema nuevo.
6. Añadir la entrada aquí con fecha y versión.

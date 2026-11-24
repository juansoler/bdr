# Motor de SQL Server

**Documento:** 24-motor-sql-server  
**Versión:** 1.0  
**Estado:** Revisión (diseño aprobado por el propietario; pendiente de validar en laboratorio con SQL Server real)  
**Requisitos:** RF-40 a RF-46  
**ADR relacionado:** `adr/0008-motor-sql-server-backups-nativos.md`  
**Depende de:** `04-arquitectura.md`, `05-modelo-datos.md`, `06-api.md`, `13-seguridad.md`, `15-almacenamiento-retencion.md`

---

## 1. Propósito y alcance

Este documento define el **quinto motor funcional** de la plataforma: protección de bases de datos Microsoft SQL Server a nivel nativo, complementario a los motores de archivos (Kopia), imagen, Proxmox/PBS y recuperación.

La distinción es la misma que en el resto de la plataforma:

- **Backup de máquina completa** (motor de imagen): protege Windows Server, discos, configuración y aplicaciones.
- **Backup de SQL Server** (este motor): protege las bases de datos con el mecanismo nativo de SQL Server, permitiendo restaurar una base concreta o recuperar los datos hasta un momento determinado sin restaurar el servidor entero.

**Regla de oro:** no se sustituyen las copias nativas de SQL Server por una copia de los archivos `.mdf`/`.ldf` en caliente. La copia de archivos en caliente es inconsistente y no permite restaurar una base concreta ni hacer recuperación a un punto en el tiempo.

### 1.1 Decisiones cerradas por el propietario (2026-11-19)

| Decisión | Valor | Consecuencia |
|---|---|---|
| Conexión a SQL Server | **Agente de plataforma instalado en el propio servidor SQL** | Orquestación local, staging local, transferencia al repositorio. La conexión remota desde la consola queda descartada para este módulo (A-11). |
| Alcance de bases de datos | **Todas, incluidas las de sistema** (`master`, `model`, `msdb`) | `tempdb` queda excluida (no se restaura con backup convencional). La restauración de instancia completa es un caso especial (RF-46, A-12). |

---

## 2. Arquitectura del módulo

```
┌────────────────────────────────────────────────────────────────┐
│ Consola centralizada                                           │
│ Planes SQL · Programaciones · Retención · Restauraciones ·     │
│ Alertas · Certificados custodiados                             │
└──────────────────────────┬─────────────────────────────────────┘
                           │ (mTLS, cola de jobs)
┌──────────────────────────┴─────────────────────────────────────┐
│ SQL Server Connector (dentro del agente de plataforma, Go)     │
│ Conexión TDS · Descubrimiento · Ejecución T-SQL ·              │
│ Seguimiento de cadenas y LSN                                   │
└──────────────────────────┬─────────────────────────────────────┘
                           │
┌──────────────────────────┴─────────────────────────────────────┐
│ SQL Server (motor nativo)                                      │
│ BACKUP DATABASE · BACKUP LOG · RESTORE DATABASE · VERIFYONLY   │
└──────────────────────────┬─────────────────────────────────────┘
                           │ archivos .bak/.trn en staging local
┌──────────────────────────┴─────────────────────────────────────┐
│ Repositorio de backups de la plataforma                        │
│ NAS (principal) · S3 (réplica) · Cifrado · Retención ·         │
│ Replicación · Metadatos en PostgreSQL                          │
└────────────────────────────────────────────────────────────────┘
```

### 2.1 Reparto de responsabilidades

| Componente | Responsabilidad |
|---|---|
| SQL Server | Generar backups nativos y restaurar bases de datos. |
| Agente de plataforma (Go) | Orquestar operaciones T-SQL, gestionar staging, transferir archivos, verificar. |
| Repositorio de la plataforma | Almacenar, cifrar, replicar y conservar los backups. |
| PostgreSQL | Metadatos: instancias, bases, cadenas de backup, LSN, puntos de recuperación. |
| NAS | Restauración rápida local. |
| S3 | Copia externa e inmutabilidad (Object Lock). |

**Principio:** no se desarrolla un motor de backup de SQL Server propio. Microsoft ya proporciona un mecanismo nativo, probado y preparado para producción; la plataforma lo **orquesta** (A-12).

---

## 3. Descubrimiento y conexión

### 3.1 Conexión

- El agente ya presente en el Windows Server se conecta a SQL Server mediante un driver TDS en Go (`go-mssqldb` o equivalente) `[VERIFICAR — P-24]`.
- Autenticación **Windows** (SSPI con la cuenta de servicio del agente) como principal; autenticación **SQL Server** (usuario/contraseña cifrados en el almacén de secretos) como alternativa.
- Instancias predeterminadas y con nombre; cada instancia se registra como entidad independiente vinculada al agente.
- Las credenciales de conexión se almacenan cifradas (envelope encryption, RF-05/13) y con **privilegios mínimos**: un rol con permisos `BACKUP DATABASE`/`BACKUP LOG` para el plan de backup; permisos de restauración solo en operaciones autorizadas con step-up `[VERIFICAR — P-26]`.

### 3.2 Descubrimiento

- Enumeración de instancias locales desde el registro de Windows `[VERIFICAR — P-24]` y conexión a cada una.
- Por instancia: versión (`SERVERPROPERTY('ProductVersion')`), edición (`SERVERPROPERTY('Edition')`), bases de datos y su modelo de recuperación (`sys.databases`), estado online/offline/restoring.
- Las bases de sistema se marcan con tratamiento especial (`master`, `model`, `msdb`); `tempdb` no se lista como protegible.
- Monitorización del estado de las bases en cada heartbeat del agente.

---

## 4. Copias de seguridad

### 4.1 Tipos

| Tipo | Comando | Función |
|---|---|---|
| Full | `BACKUP DATABASE` | Copia completa de la base de datos. |
| Differential | `BACKUP DATABASE ... WITH DIFFERENTIAL` | Cambios desde el último backup completo de referencia. |
| Transaction Log | `BACKUP LOG` | Registros de transacciones desde el último backup del log. |
| COPY_ONLY | `WITH COPY_ONLY` | Backup que no rompe la cadena (usos puntuales). |

Con una cadena correcta de backups de log y el modelo de recuperación **Full**, se puede restaurar una base hasta un momento concreto. No basta con guardar archivos `.bak` sin controlar la secuencia y sus dependencias.

### 4.2 Ejecución

```sql
BACKUP DATABASE [MiBaseDatos]
TO DISK = N'D:\SQLBackups\MiBaseDatos_FULL.bak'
WITH
    COMPRESSION,
    CHECKSUM,
    STATS = 10;
```

```sql
BACKUP LOG [MiBaseDatos]
TO DISK = N'D:\SQLBackups\MiBaseDatos_LOG.trn'
WITH
    COMPRESSION,
    CHECKSUM,
    STATS = 10;
```

- **Compresión y checksum** nativos de SQL Server, activados por defecto en el plan.
- **Cifrado nativo** de SQL Server cuando la edición lo soporte; al activarlo, el certificado o clave correspondiente se registra como **elemento de recuperación independiente** (§8.3), porque perderlo impide restaurar los backups cifrados.
- **Staging local:** SQL Server escribe el archivo en una carpeta de staging. La carpeta debe ser accesible para la **cuenta de servicio de SQL Server**, no solo para el agente; el agente valida esto con una prueba de escritura en el alta `[VERIFICAR — P-26]`.
- Tras la transferencia al repositorio, el agente elimina el archivo de staging y verifica el espacio libre antes de cada backup.

### 4.3 Metadatos y cadenas

Tras cada backup, el agente registra en la plataforma:

- Tipo, fecha, tamaño, resultado y checksum.
- LSN de la cadena (`msdb.dbo.backupset`): `database_backup_lsn`, `checkpoint_lsn`, `first_lsn`, `last_lsn` `[VERIFICAR — P-27]`.
- Dependencias: qué diferencial depende de qué full; qué logs dependen de qué full/diff.
- Ubicación en el repositorio (NAS y S3).

Con esto la consola puede construir automáticamente la **cadena de restauración** para un punto de recuperación.

---

## 5. Bases de sistema y casos especiales

| Caso | Tratamiento |
|---|---|
| `master` | Backup Full diario. La restauración es un procedimiento especial (modo single-user); documentado en fase 3 `[VERIFICAR — P-28]`. |
| `model` | Backup Full diario. |
| `msdb` | Backup Full diario; contiene jobs, historial de backups y planes de mantenimiento. |
| `tempdb` | No se restaura con backup convencional; excluida del inventario protegible. |
| **SQL Server Express** | No dispone de SQL Server Agent en las versiones habituales; la plataforma programa los backups sin depender de él. |
| **Always On Availability Groups** | Selección de la réplica preferida de backup (`sys.fn_hadr_backup_is_preferred_replica`); un backup full en un secundario solo es `COPY_ONLY` `[VERIFICAR — P-29]`. |
| **TDE** | Preservar certificados y claves de la base como elementos de recuperación; sin ellos la base restaurada no se abre. |
| **Compatibilidad de versiones** | No se puede restaurar un backup de una versión más reciente sobre una versión anterior; el motor registra la versión de origen y bloquea la restauración a destino más antiguo. |

---

## 6. Restauración

### 6.1 Selección desde la consola

El administrador selecciona:

- Servidor SQL de origen y base de datos.
- Punto de recuperación (último, o fecha y hora exacta).
- Servidor SQL de destino.
- Nombre de la base restaurada (puede ser distinto del original).
- Ubicación de los archivos MDF/LDF (reubicación con `MOVE`).

### 6.2 Flujo de restauración

Para recuperar una base hasta las 14:30:

1. Descargar del repositorio el último backup completo anterior a las 14:30 y restaurarlo con `NORECOVERY`.
2. Restaurar el diferencial correspondiente, si existe, también con `NORECOVERY`.
3. Aplicar los backups de log necesarios en orden (validando la continuidad de LSN).
4. Aplicar `STOPAT = 'fecha y hora'` en la secuencia de restauración.
5. Finalizar con `WITH RECOVERY`.

```sql
RESTORE DATABASE [MiBaseDatos]
FROM DISK = N'D:\Staging\MiBaseDatos_FULL.bak'
WITH
    NORECOVERY,
    MOVE N'MiBaseDatos' TO N'D:\Data\MiBaseDatos.mdf',
    MOVE N'MiBaseDatos_log' TO N'D:\Data\MiBaseDatos_log.ldf';

RESTORE DATABASE [MiBaseDatos]
FROM DISK = N'D:\Staging\MiBaseDatos_LOG.trn'
WITH
    RECOVERY,
    STOPAT = N'2026-11-19T14:30:00';
```

### 6.3 Validaciones previas

- Verificación de la cadena completa antes de ejecutar (continuidad de LSN y dependencias).
- Comprobación de versión: el destino debe ser igual o más reciente que el origen.
- Preflight de espacio en el destino para los archivos de datos y log.
- **Toda restauración exige autorización explícita (step-up) y queda auditada** (RF-29, RF-30).

### 6.4 Alcance de restauración por fase

| Capacidades | Fase |
|---|---|
| Restaurar una base completa (cadena full + diff + logs) | MVP del motor (Fase 3) |
| Point-in-Time Recovery (`STOPAT`) | Fase 3/4 |
| Restaurar con otro nombre / otra instancia / otra ubicación | Fase 3/4 |
| Restauración de certificados y claves | Fase 3/4 |
| Restauración de instancia completa (`master` + `model` + `msdb`) | Fase 4 |

---

## 7. Verificación y monitorización

- **`RESTORE VERIFYONLY`** programado sobre cada backup transferido al repositorio; el resultado queda en los metadatos.
- **Restauraciones de prueba** en una instancia SQL aislada (VM temporal, igual que RF-21) — fase 3/4.
- **`DBCC CHECKDB`** opcional, programado por base de datos y con su resultado en el catálogo.
- **Alertas:** fallo de backup, copia de log atrasada (riesgo de RPO), verificación fallida, backup cifrado sin certificado custodiado. Reutilizan el sistema de alertas de RF-26.

---

## 8. Integración con el almacenamiento

| Componente | Responsabilidad |
|---|---|
| SQL Server | Generar backups y restaurar bases. |
| Agente Go | Orquestar operaciones y transferir archivos. |
| Repositorio | Almacenar, cifrar y conservar los backups. |
| PostgreSQL | Guardar metadatos, cadenas y puntos de recuperación. |
| NAS | Restauración rápida local. |
| S3 | Copia externa y protección frente a desastres. |

### 8.1 Formato y deduplicación

- Se conservan los **formatos nativos** `.bak`/`.trn`. SQL Server ya comprime sus backups, lo que reduce el beneficio de la deduplicación posterior; **no se deduplican internamente** con el mismo procedimiento que las imágenes de disco (A-12). El repositorio los trata como objetos o archivos protegidos.
- **Layout en el repositorio:** los medios se ordenan por origen y fecha como `<prefijo-destino>/<máquina-agente>/<instancia>/<AAAA-MM-DD>/<base>_<TIPO>_<timestamp>.bak` (p. ej. `sql/SRV-SQL01/MSSQLSERVER/2026-11-24/app_FULL_20261124_020000.bak`). La máquina es el hostname del agente (host SQL) y la instancia la del plan (`MSSQLSERVER` cuando apunta a la predeterminada). La custodia de certificados comparte la misma jerarquía.
- La retención se aplica como en el resto de la plataforma (GFS configurable por instancia o base de datos).
- La réplica a S3 con Object Lock reutiliza el mecanismo de RF-10/RF-36.

### 8.2 Transferencia

- El agente transfiere el archivo del staging al repositorio en el mismo job de backup; si la transferencia falla, el archivo `.bak` se conserva en staging y el job se reintenta (el archivo ya generado por SQL Server no se regenera).
- Íntegridad: checksum calculado en origen y verificado en el repositorio.

### 8.3 Certificados y claves como elementos de recuperación

- Los certificados de cifrado de backups nativos y los certificados/claves de TDE se registran como **artefactos de recuperación independientes**, cifrados con envelope encryption y sujetos a la custodia de clave maestra (doc 14).
- La consola alerta si existe un backup cifrado cuyo certificado no está registrado.
- La restauración de un backup cifrado en otra instancia exige recuperar primero el certificado (§6.4, fase 3/4).

---

## 9. Metadatos (modelo de datos)

Se incorporarán a `05-modelo-datos.md` en la revisión del modelo, con entidades equivalentes a:

- `sql_instances`: instancia registrada (agente, nombre, versión, edición, tipo de autenticación).
- `sql_databases`: base protegida (instancia, modelo de recuperación, estado, tratamiento especial).
- `sql_backups`: backup registrado (tipo, fecha, tamaño, LSN inicio/fin, `database_backup_lsn`, `checkpoint_lsn`, checksum, referencia de certificado, ubicaciones NAS/S3, estado de verificación).
- `sql_restore_chains`: derivada de `sql_backups` para construir la secuencia de restauración de un punto de recuperación.

Las operaciones de restauración se registran como jobs del sistema (RF-29) con su preflight y evidencia.

---

## 10. Diseño en la consola

Sección independiente **«Protección de datos → Microsoft SQL Server»** (detalle en `22-panel-ui-ux.md` §23):

- **Tarjetas de estado:** instancias registradas, bases protegidas, último backup, historial de restauraciones.
- **Plan de protección SQL:** configuración por instancia o base de datos:

| Operación | Programación por defecto |
|---|---|
| Full | Domingo 01:00 |
| Differential | Cada día 02:00 |
| Transaction Log | Cada 15 minutos |
| Verificación (`VERIFYONLY`) | Diaria |
| Retención | 30 días |

- **Wizard de restauración:** origen → base → punto de recuperación → destino → nombre y ubicación → preflight → step-up → ejecución con evidencia.
- **Pantalla de certificados:** lista de certificados/claves de cifrado y TDE custodiados, con alerta de ausencia.

---

## 11. Fases y requisitos

| Fase | Contenido |
|---|---|
| **MVP del motor (Fase 3)** | Conectar con SQL Server en Go; descubrir instancias y bases; backups Full/Diff/Log; transferencia a NAS y S3; metadatos y cadenas; restaurar una base completa; `RESTORE VERIFYONLY`; custodia de certificados. |
| **Recuperación avanzada (Fase 3/4)** | Point-in-Time Recovery; restauración con otro nombre/instancia/ubicación; restauración de certificados y claves; restauraciones de prueba aisladas; `DBCC CHECKDB` programado. |
| **Entornos avanzados (Fase 4)** | Always On Availability Groups; políticas por instancia y base; restauraciones masivas; alertas de incumplimiento de RPO; informes de protección SQL. |

El motor SQL **no está en el MVP de Fase 1**, pero se reservan desde el diseño sus interfaces, entidades y gestión de tareas (misma cola de jobs, mismos jobs y auditoría).

---

## 12. Pendientes de verificación

| ID | Pregunta | Criterio de salida |
|---|---|---|
| P-24 | ¿Driver TDS en Go (`go-mssqldb`) soporta Windows Auth (SSPI) y las versiones objetivo (SQL Server 2019/2022/2025)? | Conexión y descubrimiento verificados en laboratorio |
| P-25 | ¿Enumeración de instancias por registro de Windows es fiable en Server 2025? | Instancias predeterminada y con nombre detectadas |
| P-26 | ¿Qué roles/permisos mínimos exactos exige BACKUP/RESTORE y acceso al staging por la cuenta de servicio SQL? | Mínimo privilegio documentado y probado |
| P-27 | ¿Campos de LSN de `msdb.dbo.backupset` suficientes para validar cadenas en las versiones objetivo? | Cadena validada en restore de prueba |
| P-28 | ¿Procedimiento de restauración de `master` (single-user) documentado y probado? | Procedimiento con pasos y evidencia |
| P-29 | ¿Comportamiento de backup en Always On: réplica preferida, `COPY_ONLY` en secundarios? | Plan de backup por grupo definido |

Estos pendientes se registran en `decisiones.md` y se resuelven con el experimento **G0.10** (`labs/g10-sql-server/`) o al inicio de la Fase 3.

---

## 13. Control de cambios

- Cambios de alcance: actualizar `01-especificacion-maestra.md` y `21-matriz-trazabilidad.md`.
- Cambios de decisión: actualizar `adr/0008` y `decisiones.md`.
- Los pendientes `[VERIFICAR]` se resuelven en `decisiones.md` con evidencia.

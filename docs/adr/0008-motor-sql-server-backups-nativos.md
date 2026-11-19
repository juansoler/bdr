# ADR-0008: Motor de SQL Server con backups nativos orquestados por el agente

- **Estado:** Aceptado
- **Fecha:** 2026-11-19
- **Relacionado:** `24-motor-sql-server.md`, `01-especificacion-maestra.md` (RF-40–RF-46), `adr/0001-go-server-agente.md`, `adr/0002-kopia-repository-server.md`

## Contexto

La plataforma protege archivos (Kopia), máquinas completas (motor de imagen), VMs de Proxmox (PBS) y recuperación bare-metal (ISO). Falta la protección de bases de datos Microsoft SQL Server, que es una necesidad prioritaria en entornos con Windows Server y SQL Server. Las opciones eran:

1. **Copia de archivos `.mdf`/`.ldf`** con el agente de archivos existente.
2. **Desarrollar un motor propio de backup de SQL Server.**
3. **Orquestar el mecanismo nativo de SQL Server** (`BACKUP DATABASE`/`BACKUP LOG`) desde un conector dentro del agente de plataforma.

## Decisión

Se adopta la opción 3, con dos decisiones cerradas por el propietario:

1. **Conexión:** el agente de plataforma se instala en el propio servidor SQL y orquesta localmente. Se descarta la conexión remota desde la consola para este módulo: evita exponer TDS a la red de gestión, evita permisos remotos sobre rutas de staging y las credenciales de SQL no viajan por la red. La carpeta de staging debe ser accesible para la cuenta de servicio de SQL Server, no solo para el agente.
2. **Alcance:** todas las bases de datos, incluidas las de sistema (`master`, `model`, `msdb`); `tempdb` queda excluida. La restauración de instancia completa es un caso especial documentado.

Además: los backups se generan **en formato nativo** (`.bak`/`.trn`, con compresión y checksum de SQL Server) y el repositorio de la plataforma los almacena, cifra y replica **sin deduplicación interna** (SQL ya comprime; la dedup de `.bak` aporta poco y rompe la equivalencia con el formato nativo). Los certificados de cifrado de backups y de TDE se custodian como elementos de recuperación independientes.

## Alternativas

1. **Copia de `.mdf`/`.ldf` en caliente:** inconsistente (sin VSS de aplicación), no permite restaurar una base concreta ni point-in-time; descartada.
2. **Motor propio:** reimplanta lo que SQL Server ya hace bien (cadenas, LSN, VERIFYONLY); coste y riesgo desproporcionados.
3. **Conexión remota desde la consola sin agente SQL:** menos superficie de instalación, pero requiere acceso de red a SQL, un servicio con permisos sobre rutas remotas y mayor superficie de ataque; descartada para el MVP del motor.

## Consecuencias

- La plataforma orquesta, no reemplaza: el motor SQL es un **quinto motor funcional** con su conector dentro del agente (Go/TDS), la misma cola de jobs, la misma auditoría y el mismo repositorio.
- El catálogo guarda cadenas y LSN (`msdb.dbo.backupset`) para poder construir restauraciones con `NORECOVERY`/`STOPAT` de forma automática.
- El MVP del motor no está en Fase 1; sus interfaces, entidades y gestión de tareas se reservan desde el diseño (RF-40–RF-46, Fase 3).
- Quedan pendientes de verificación los puntos P-24 a P-29 de `24-motor-sql-server.md` (driver TDS, permisos mínimos, Always On, restauración de `master`).

## Criterios de revisión

- Si aparece un entorno donde no se pueda instalar el agente de plataforma en el servidor SQL, reevaluar la conexión remota como modalidad adicional.
- Si SQL Server aporta un mecanismo de streaming (VDI) que elimine el staging completo, evaluarlo como segunda fase del conector.

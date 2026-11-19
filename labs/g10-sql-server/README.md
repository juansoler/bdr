# G0.10 — Motor SQL Server: descubrimiento, backups nativos y restauración

**Objetivo:** cerrar los pendientes P-24 a P-29 de `docs/24-motor-sql-server.md` con una instancia real de SQL Server en laboratorio: enumeración de instancias, conexión TDS con Windows Auth, backups Full/Diff/Log, cadenas y LSN, `RESTORE VERIFYONLY`, restauración en otra instancia y el procedimiento de `master`.

**Decide:** driver TDS en Go, permisos mínimos del conector, tratamiento de bases de sistema y diseño del plan de backup por instancia (RF-40–RF-46, Fase 3).

**Nota:** este experimento **no forma parte de la puerta de Fase 0** (gate-check revisa G0.1–G0.9). Se ejecuta al inicio de la Fase 3, o antes si se quiere adelantar riesgo. Es destructivo en parte: **snapshot de la VM antes de empezar**.

---

## 1. Requisitos

| Herramienta | Uso | Nota |
|---|---|---|
| VM Windows Server 2025 | Instancia SQL | Snapshot antes de tocar nada |
| SQL Server 2022 **Developer** | Motor nativo | Gratuita; soporta compresión, cifrado y Always On. **No vale Express como instancia principal** (sin compresión ni cifrado de backups) |
| SQL Server Express (opcional) | Variante sin SQL Agent | Para probar que la plataforma programa sin depender de él |
| Go ≥ 1.21 | Compilar `sql-probe.go` | `go-mssqldb` requiere ≥ 1.21; anotar la versión usada |
| 2ª VM (opcional) | Always On (P-29) | Solo en Fase 4 |

---

## 2. Preparar el entorno

1. **Snapshot de la VM** (la restauración de `master` en single-user puede dejar la instancia inaccesible).
2. En la VM, con PowerShell **elevado**:

```powershell
powershell -NoProfile -ExecutionPolicy Bypass `
  -File .\sql-setup.ps1 -StagingPath D:\SQLBackups -TestDb g10db -Apply
```

El script (dry-run sin `-Apply`):

- Crea la carpeta de staging y da permisos NTFS a la **cuenta de servicio de SQL Server** (no solo al administrador) — valida P-26.
- Crea la base de prueba `g10db` con una tabla y filas.
- Crea el login SQL `agent_lab` con la contraseña de la variable `G10_SQL_PASSWORD` (nunca en el script) y le concede el mínimo: `db_backupoperator` en `g10db` y `msdb` + `SELECT` sobre `msdb.dbo.backupset`.
- Recuerda cómo restaurar `master` en single-user (§7).

3. Ejecutar `capture-env.ps1` de `labs/common/` y guardar la salida en `labs/evidence/$LAB_DATE/`.

---

## 3. Compilar y ejecutar el probe

En la VM (o en esta máquina para `discover`, que solo lee el registro):

```powershell
go build -o sql-probe.exe .\sql-probe.go
```

### 3.1 Descubrimiento (P-24, P-25)

```powershell
.\sql-probe.exe discover | Out-File labs\evidence\$LAB_DATE\g10-discover.json
```

Espera: instancias predeterminada y con nombre detectadas por registro, con versión, edición y ruta de instalación.

### 3.2 Inventario (RF-40)

Con Windows Auth (SSPI) y también con el login mínimo:

```powershell
.\sql-probe.exe inventory -server localhost | Out-File labs\evidence\$LAB_DATE\g10-inventory-win.json
.\sql-probe.exe inventory -server localhost -user agent_lab -password "$env:G10_SQL_PASSWORD" |
  Out-File labs\evidence\$LAB_DATE\g10-inventory-min.json
```

Espera: bases con modelo de recuperación, estado y marcado de `master`/`model`/`msdb` como especiales; `tempdb` excluida.

### 3.3 Cadena de backups (RF-41, P-27)

```powershell
.\sql-probe.exe backup -server localhost -db g10db -type full -out D:\SQLBackups\g10_FULL.bak
.\sql-probe.exe backup -server localhost -db g10db -type diff -out D:\SQLBackups\g10_DIFF.bak
.\sql-probe.exe backup -server localhost -db g10db -type log  -out D:\SQLBackups\g10_LOG.trn
.\sql-probe.exe chain -server localhost -db g10db | Out-File labs\evidence\$LAB_DATE\g10-chain.json
```

Entre el full y el diff, inserta filas en `g10db` para que el diferencial no sea vacío. Con el login mínimo (`agent_lab`) repite el backup full: si falla por permisos, anota el error — define el mínimo real (P-26).

### 3.4 Verificación (RF-44)

```powershell
.\sql-probe.exe verify -file D:\SQLBackups\g10_FULL.bak
.\sql-probe.exe verify -file D:\SQLBackups\g10_DIFF.bak
.\sql-probe.exe verify -file D:\SQLBackups\g10_LOG.trn
```

Prueba también la corrupción: modifica 1 byte del `.bak` copiado y verifica que `VERIFYONLY` falla (evidencia del criterio de RF-44).

### 3.5 Restauración (RF-42/RF-43)

```powershell
# En la misma instancia, con otro nombre (no destructiva)
.\sql-probe.exe restore -server localhost -file D:\SQLBackups\g10_FULL.bak `
  -database g10_restored -confirm

# Cadena completa: full NORECOVERY + diff NORECOVERY + log RECOVERY
.\sql-probe.exe restore -server localhost -file D:\SQLBackups\g10_FULL.bak -database g10_pit -norecovery -confirm
.\sql-probe.exe restore -server localhost -file D:\SQLBackups\g10_DIFF.bak -database g10_pit -norecovery -confirm
.\sql-probe.exe restore -server localhost -file D:\SQLBackups\g10_LOG.trn -database g10_pit -recovery -confirm
```

Comprueba que `g10_restored` y `g10_pit` contienen las filas esperadas. La variante **en otra instancia** (segunda instancia con nombre o segunda VM) valida RF-42.

---

## 4. Procedimiento manual de `master` (P-28)

No se automatiza: single-user es un procedimiento delicado. Documéntalo paso a paso:

1. Detén SQL Server Agent.
2. Arranca la instancia en modo single-user: `net start MSSQLSERVER /m"SQLCMD"`.
3. Conecta con `sqlcmd -S localhost -E` y ejecuta `RESTORE DATABASE master FROM DISK = N'D:\SQLBackups\master_FULL.bak' WITH REPLACE;`.
4. Reinicia la instancia en modo normal.
5. Verifica `msdb`/`model` restaurados y el estado general.

Anota cada paso con su salida y el tiempo total. Si falla, restaura el snapshot de la VM y documenta el fallo.

---

## 5. Variante Express (opcional)

Con una instancia Express registrada: repite 3.1–3.4 sin SQL Agent. Espera: backups sin compresión (edición Express no la soporta) `[VERIFICAR]` y programación 100 % gestionada por la plataforma.

---

## 6. Always On (P-29, opcional, Fase 4)

Con dos VMs y Developer Edition:

1. Crea un grupo de disponibilidad con `g10db`.
2. Comprueba `sys.fn_hadr_backup_is_preferred_replica('g10db')` en cada réplica.
3. Intenta `BACKUP DATABASE` en el secundario: espera error o `COPY_ONLY` obligatorio; anota el comportamiento exacto.
4. Decide el plan de backup por grupo (réplica preferida + logs desde el primario o réplica preferida).

---

## 7. Criterios de aceptación

| ID | Criterio |
|---|---|
| P-24 | Windows Auth (SSPI) funciona con `go-mssqldb` en Server 2025 + SQL Server 2022; conexión SQL Auth también |
| P-25 | Instancias predeterminada y con nombre detectadas por registro |
| P-26 | Mínimo privilegio documentado: qué concede `db_backupoperator` y qué falta; staging accesible por la cuenta de servicio SQL |
| P-27 | `database_backup_lsn`/`checkpoint_lsn`/`first_lsn`/`last_lsn` suficientes para reconstruir y validar la cadena |
| P-28 | Procedimiento de `master` ejecutado con evidencia (o fallo documentado) |
| P-29 | Comportamiento de backup en AG documentado (si se ejecuta) |

---

## 8. Evidencia

En `labs/evidence/$LAB_DATE/`:

- `g10-discover.json`, `g10-inventory-win.json`, `g10-inventory-min.json`.
- `g10-backup-full.txt`, `g10-backup-diff.txt`, `g10-backup-log.txt` (salida con STATS).
- `g10-chain.json` y `g10-verify-*.txt` (incluye la prueba de corrupción).
- `g10-restore.txt` (restauración y comprobación de filas).
- `g10-master.md` (procedimiento manual paso a paso).
- `g10-summary.md` (plantilla de `labs/common/evidence-template.md` completada).

Al terminar: actualizar `docs/decisiones.md` (P-24–P-29), `docs/24-motor-sql-server.md` §12 y, si hay sorpresas, `adr/0008`.

---

## 9. Qué NO se hace

- No restaurar `master` en una instancia sin snapshot previo.
- No usar producción ni datos reales: solo `g10db` sintética.
- No dejar la contraseña `G10_SQL_PASSWORD` en scripts ni en evidencia.
- No probar Always On con una sola VM.

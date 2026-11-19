# sql-setup.ps1 — G0.10: preparación de la VM con SQL Server.
#
# DRY-RUN por defecto: sin -Apply solo muestra lo que haría.
# La contraseña del login de pruebas viene de la variable de entorno
# G10_SQL_PASSWORD (nunca en este fichero ni en la evidencia).
#
# Uso (PowerShell elevado en la VM):
#   powershell -NoProfile -ExecutionPolicy Bypass -File .\sql-setup.ps1 -StagingPath D:\SQLBackups -TestDb g10db -Apply
#
# Requisitos: SQL Server 2022 Developer (o Express para la variante),
# sqlcmd disponible (viene con SQL Server), permisos sysadmin para preparar.

[CmdletBinding()]
param(
  [string]$StagingPath = 'D:\SQLBackups',
  [string]$TestDb      = 'g10db',
  [string]$AgentLogin  = 'agent_lab',
  [switch]$Apply
)

$ErrorActionPreference = 'Stop'
$plan = [System.Collections.Generic.List[string]]::new()
function Note($msg) { $plan.Add($msg); Write-Host "  $msg" }

# --- 1. Cuenta de servicio de SQL Server -----------------------------------

Write-Host "== Cuenta de servicio de SQL Server =="
$svc = Get-CimInstance Win32_Service -Filter "Name='MSSQLSERVER'" -ErrorAction SilentlyContinue
if (-not $svc) {
  $svc = Get-CimInstance Win32_Service -Filter "Name LIKE 'MSSQL$%'" -ErrorAction SilentlyContinue
}
if (-not $svc) { throw "No hay servicio de SQL Server (MSSQLSERVER ni instancias con nombre)." }
$svcAccount = $svc.StartName
if ($svcAccount -eq 'LocalSystem' -or $svcAccount -eq 'NT AUTHORITY\SYSTEM') {
  $svcAccount = 'NT AUTHORITY\SYSTEM'
}
Write-Host "Servicio: $($svc.Name) — cuenta: $svcAccount"

# --- 2. Carpeta de staging accesible para SQL Server (P-26) -----------------

Note "Crear carpeta de staging: $StagingPath"
Note "Conceder NTFS a la cuenta de servicio SQL: $svcAccount — Modify"
Note "Conceder NTFS al grupo BUILTIN\Administrators — Modify (agente de plataforma)"

# --- 3. Base de prueba -------------------------------------------------------

Note "Crear base de prueba '$TestDb' (FULL recovery) con tabla y filas"
Note "El modelo de recuperación FULL permite probar BACKUP LOG (cadenas)"

# --- 4. Login mínimo del conector (P-26) ------------------------------------

Note "Crear login SQL '$AgentLogin' (contraseña desde `$env:G10_SQL_PASSWORD`)"
Note "Conceder db_backupoperator en '$TestDb' (BACKUP DATABASE/LOG)"
Note "Conceder db_backupoperator en msdb + SELECT en msdb.dbo.backupset (cadenas)"
Note "NO conceder: db_owner, sysadmin, permisos de restauración"

if (-not $Apply) {
  Write-Host "`nDRY-RUN: nada modificado. Repite con -Apply para ejecutar."
  exit 0
}

# --- Aplicar -----------------------------------------------------------------

if (-not (Test-Path $StagingPath)) {
  New-Item -ItemType Directory -Path $StagingPath | Out-Null
}
icacls $StagingPath /grant "${svcAccount}:(OI)(CI)M" | Out-Null
icacls $StagingPath /grant 'BUILTIN\Administrators:(OI)(CI)M' | Out-Null
Write-Host "Staging listo: $StagingPath"

$pass = $env:G10_SQL_PASSWORD
if (-not $pass) { throw "Define `$env:G10_SQL_PASSWORD antes de -Apply." }
if ($pass.Length -lt 12) { throw "La contraseña de laboratorio debe tener >= 12 caracteres." }

function Invoke-Sql([string]$Database, [string]$Query) {
  & sqlcmd -S localhost -E -b -d $Database -Q $Query
  if ($LASTEXITCODE -ne 0) { throw "sqlcmd falló: $Query" }
}

Invoke-Sql 'master' "IF DB_ID('$TestDb') IS NULL BEGIN CREATE DATABASE $TestDb; ALTER DATABASE $TestDb SET RECOVERY FULL; END;"
Invoke-Sql $TestDb  "IF OBJECT_ID('dbo.items') IS NULL CREATE TABLE dbo.items (id INT IDENTITY, value NVARCHAR(64), created DATETIME2 DEFAULT SYSDATETIME());"
Invoke-Sql $TestDb  "IF NOT EXISTS (SELECT 1 FROM dbo.items) INSERT INTO dbo.items (value) SELECT 'row-' + CAST(n AS NVARCHAR) FROM (VALUES (1),(2),(3),(4),(5),(6),(7),(8),(9),(10)) v(n);"

Invoke-Sql 'master' "IF SUSER_ID('$AgentLogin') IS NULL CREATE LOGIN $AgentLogin WITH PASSWORD = '$pass', CHECK_POLICY = ON;"
Invoke-Sql $TestDb  "IF USER_ID('$AgentLogin') IS NULL CREATE USER $AgentLogin FOR LOGIN $AgentLogin; ALTER ROLE db_backupoperator ADD MEMBER $AgentLogin;"
Invoke-Sql 'msdb'   "IF USER_ID('$AgentLogin') IS NULL CREATE USER $AgentLogin FOR LOGIN $AgentLogin; ALTER ROLE db_backupoperator ADD MEMBER $AgentLogin; GRANT SELECT ON dbo.backupset TO $AgentLogin;"

Write-Host "`nHecho. Prueba de escritura sugerida (valida P-26):"
Write-Host "  .\sql-probe.exe backup -server localhost -db $TestDb -type full -out $StagingPath\$TestDb_FULL.bak -user $AgentLogin"
Write-Host "Recordatorio: snapshot de la VM antes de probar restauraciones (README §4)."

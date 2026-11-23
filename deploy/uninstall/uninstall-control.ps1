# =============================================================================
# uninstall-control.ps1 — desinstala bdr-control en Windows.
#
# Por defecto SOLO detiene el servicio, elimina binarios, reglas de firewall
# y variables de entorno de máquina: conserva el estado (equipos, planes,
# auditoría) y los secretos en C:\ProgramData\BDR. Con -Purge lo borra todo.
#
# El repositorio Kopia puede estar en otra ruta del disco, en un NAS o en
# S3: esta purga NO lo alcanza. Revisa C:\ProgramData\BDR\control\kopia.config
# (o la configuración del panel) para localizarlo y limpiarlo aparte.
#
# Uso (PowerShell como administrador):
#   .\uninstall-control.ps1                # conserva C:\ProgramData\BDR
#   .\uninstall-control.ps1 -Purge         # borra también estado y secretos
#   .\uninstall-control.ps1 -Purge -Yes    # sin confirmación
# =============================================================================
param(
  [switch]$Purge,
  [switch]$Yes
)

$ErrorActionPreference = "Stop"

$principal = New-Object Security.Principal.WindowsPrincipal([Security.Principal.WindowsIdentity]::GetCurrent())
if (-not $principal.IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)) {
  throw "Ejecuta este script como administrador."
}

$installDir = "$env:ProgramFiles\BDR"
$dataDir = "$env:ProgramData\BDR"

Write-Host "== deteniendo bdr-control…"
if (Get-Service bdr-control -ErrorAction SilentlyContinue) {
  Stop-Service bdr-control -Force -ErrorAction SilentlyContinue
  # El borrado elimina la clave de registro del servicio, incluida la
  # configuración de recuperación automática del SCM.
  & sc.exe delete bdr-control | Out-Null
  Start-Sleep -Seconds 2
  Write-Host "   servicio bdr-control eliminado"
}
Get-Process bdr-control -ErrorAction SilentlyContinue | Stop-Process -Force -ErrorAction SilentlyContinue

Write-Host "== eliminando variables de entorno de máquina…"
foreach ($name in @("BDR_ADMIN_TOKEN", "BDR_SECRET_KEY", "BDR_KOPIA_PASSWORD")) {
  [Environment]::SetEnvironmentVariable($name, $null, "Machine")
}

Write-Host "== eliminando reglas de firewall…"
# Si cambiaste los puertos y renombraste las reglas, bórralas a mano con
# Get-NetFirewallRule | Where-Object DisplayName -like "BDR*".
Remove-NetFirewallRule -DisplayName "BDR Control" -ErrorAction SilentlyContinue
Remove-NetFirewallRule -DisplayName "BDR Agentes mTLS" -ErrorAction SilentlyContinue

Write-Host "== eliminando binarios…"
foreach ($name in @("bdr-control.exe", "kopia.exe", "agents.json", "agents")) {
  $p = Join-Path $installDir $name
  if (Test-Path $p) { Remove-Item -Recurse -Force $p }
}
# El directorio de instalación se comparte con bdr-agent: solo se quita
# si ha quedado vacío.
if ((Test-Path $installDir) -and -not (Get-ChildItem $installDir -Force)) {
  Remove-Item -Force $installDir
}

if ($Purge) {
  Write-Host ""
  Write-Warning ("-Purge borrará de forma IRREVERSIBLE el estado y los secretos:")
  Write-Warning "  $dataDir\control  (equipos, planes, auditoría, secret.key)"
  Write-Warning "  $dataDir\bdr.env  (BDR_ADMIN_TOKEN, claves)"
  Write-Warning "El repositorio Kopia (las copias) puede estar en otra ruta, NAS o S3:"
  Write-Warning "esta purga NO lo borra. Revísalo antes de continuar."
  if (-not $Yes) {
    $reply = Read-Host "Escribe PURGAR para confirmar"
    if ($reply -cne "PURGAR") {
      Write-Host "purgado cancelado: el estado se conserva en $dataDir"
      return
    }
  }
  Write-Host "== purgando estado y secretos…"
  foreach ($name in @("control", "bdr.env", "control.log")) {
    $p = Join-Path $dataDir $name
    if (Test-Path $p) { Remove-Item -Recurse -Force $p }
  }
  if ((Test-Path $dataDir) -and -not (Get-ChildItem $dataDir -Force)) {
    Remove-Item -Force $dataDir
  }
}

Write-Host ""
Write-Host "== desinstalación completa =="
if ((Test-Path $dataDir) -and (Get-ChildItem $dataDir -Force)) {
  Write-Host "conservado: $dataDir (estado del control, secretos…)"
  Write-Host "            bórralo a mano o re-ejecuta con -Purge"
}
Write-Host "nota: los agentes que apuntaban a este servidor dejarán de conectar;"
Write-Host "      revócalos desde el panel de otro control o reinstala este."

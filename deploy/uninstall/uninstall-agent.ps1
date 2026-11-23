# =============================================================================
# uninstall-agent.ps1 — desinstala bdr-agent en Windows.
#
# Cubre las dos formas de instalación: el servicio de Windows (instalador del
# panel) y la tarea programada "BDR Agent" (deploy/agent/windows/install.ps1).
#
# Por defecto SOLO detiene el servicio y elimina el binario: conserva la
# configuración (agent.json con la API key) y la caché de Kopia en
# C:\ProgramData\BDR. Con -Purge borra también esos datos (irreversible).
# Las copias de seguridad en el repositorio del servidor NO se tocan: para
# eliminarlas, usa el panel (revocar agente / limpiar restore points).
#
# Uso (PowerShell como administrador):
#   .\uninstall-agent.ps1                # conserva C:\ProgramData\BDR
#   .\uninstall-agent.ps1 -Purge         # borra también los datos del agente
#   .\uninstall-agent.ps1 -Purge -Yes    # sin confirmación
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

function Remove-AgentData {
  # El data-dir se comparte con bdr-control (bdr.env, control\): solo se
  # borran los ficheros del agente y el directorio queda si tiene algo más.
  foreach ($name in @("agent.json", "agent.log", "kopia", "restore-state", "lxc-staging", "tmp")) {
    $p = Join-Path $dataDir $name
    if (Test-Path $p) { Remove-Item -Recurse -Force $p }
  }
}

Write-Host "== deteniendo bdr-agent…"

# Variante 1: servicio de Windows (instalador del panel).
if (Get-Service bdr-agent -ErrorAction SilentlyContinue) {
  Stop-Service bdr-agent -Force -ErrorAction SilentlyContinue
  # El borrado elimina la clave de registro del servicio, incluida la
  # configuración de recuperación automática del SCM.
  & sc.exe delete bdr-agent | Out-Null
  Start-Sleep -Seconds 2
  Write-Host "   servicio bdr-agent eliminado"
}

# Variante 2: tarea programada (instalador de deploy/agent/windows).
$task = Get-ScheduledTask -TaskName "BDR Agent" -ErrorAction SilentlyContinue
if ($task) {
  Stop-ScheduledTask -TaskName "BDR Agent" -ErrorAction SilentlyContinue
  Unregister-ScheduledTask -TaskName "BDR Agent" -Confirm:$false
  Write-Host "   tarea programada 'BDR Agent' eliminada"
}

# Procesos vivos (instalaciones sin servicio).
Get-Process bdr-agent -ErrorAction SilentlyContinue | Stop-Process -Force -ErrorAction SilentlyContinue

Write-Host "== eliminando binario…"
$agentExe = Join-Path $installDir "bdr-agent.exe"
if (Test-Path $agentExe) { Remove-Item -Force $agentExe }
# El directorio de instalación se comparte con bdr-control: solo se quita
# si ha quedado vacío.
if ((Test-Path $installDir) -and -not (Get-ChildItem $installDir -Force)) {
  Remove-Item -Force $installDir
}

if ($Purge) {
  Write-Host ""
  Write-Warning ("-Purge borrará de forma IRREVERSIBLE los datos del agente en:")
  Write-Warning "  $dataDir  (agent.json con la API key, caché de Kopia, staging)"
  if (-not $Yes) {
    $reply = Read-Host "¿Continuar? Escribe SI para confirmar"
    if ($reply -notin @("SI", "Si", "si", "sí", "SÍ", "Y", "y", "YES", "Yes", "yes")) {
      Write-Host "purgado cancelado: los datos se conservan en $dataDir"
      return
    }
  }
  Write-Host "== purgando datos del agente…"
  Remove-AgentData
  if ((Test-Path $dataDir) -and -not (Get-ChildItem $dataDir -Force)) {
    Remove-Item -Force $dataDir
  }
}

Write-Host ""
Write-Host "== desinstalación completa =="
if ((Test-Path $dataDir) -and (Get-ChildItem $dataDir -Force)) {
  Write-Host "conservado: $dataDir (agent.json, caché de Kopia…)"
  Write-Host "            bórralo a mano o re-ejecuta con -Purge"
}
Write-Host "nota: el equipo sigue apareciendo en el panel — revócalo desde"
Write-Host "      Equipos → <host> → Revocar para limpiar su estado en el control."

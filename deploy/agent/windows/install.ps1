# Instalador del agente BDR para Windows.
# Ejecutar en PowerShell como administrador:
#   .\install.ps1 -Server http://SERVIDOR:8080 -Token TOKEN `
#     [-Fingerprint sha256:HEX] [-Binary .\bdr-agent-windows-amd64.exe]
param(
  [Parameter(Mandatory = $true)][string]$Server,
  [Parameter(Mandatory = $true)][string]$Token,
  [string]$Fingerprint,
  [string]$Binary,
  [string]$DataDir = "C:\ProgramData\BDR\agent"
)

$ErrorActionPreference = "Stop"

# Comprobaciones de privilegios.
$principal = New-Object Security.Principal.WindowsPrincipal([Security.Principal.WindowsIdentity]::GetCurrent())
if (-not $principal.IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)) {
  throw "Ejecuta este script como administrador."
}

# Binario: parámetro o el amd64 junto a este script.
if (-not $Binary) {
  $Binary = Join-Path $PSScriptRoot "bdr-agent-windows-amd64.exe"
}
if (-not (Test-Path $Binary)) {
  throw "No se encontró el binario del agente: $Binary"
}

$installDir = "C:\Program Files\BDR"
New-Item -ItemType Directory -Force -Path $installDir | Out-Null
New-Item -ItemType Directory -Force -Path $DataDir | Out-Null

Copy-Item $Binary (Join-Path $installDir "bdr-agent.exe") -Force

# El directorio de datos contiene la API key del agente: solo SYSTEM y administradores.
icacls $DataDir /inheritance:r /grant "SYSTEM:(OI)(CI)F" /grant "Administrators:(OI)(CI)F" | Out-Null

$agentExe = Join-Path $installDir "bdr-agent.exe"
$enrollArgs = @("register", "--server", $Server, "--data-dir", $DataDir)
if ($Fingerprint) { $enrollArgs += @("--server-fingerprint", $Fingerprint) }

# Enrolamiento. El token se pasa como argumento del proceso hijo (memoria del
# proceso, no se escribe en disco); el binario guarda agent.json con 0600.
& $agentExe @enrollArgs --token $Token
if ($LASTEXITCODE -ne 0) {
  throw "El enrolamiento falló (código $LASTEXITCODE). Comprueba el token y la conexión."
}

# Tarea programada: arranca con el sistema como SYSTEM y se relanza si muere.
$taskName = "BDR Agent"
$action = New-ScheduledTaskAction -Execute $agentExe -Argument "run --data-dir `"$DataDir`""
$trigger = New-ScheduledTaskTrigger -AtStartup
$settings = New-ScheduledTaskSettingsSet -RestartCount 999 -RestartInterval (New-TimeSpan -Minutes 1) `
  -ExecutionTimeLimit ([TimeSpan]::Zero) -AllowStartIfOnBatteries -DontStopIfGoingOnBatteries `
  -StartWhenAvailable
$principal = New-ScheduledTaskPrincipal -UserId "SYSTEM" -LogonType ServiceAccount -RunLevel Highest
Register-ScheduledTask -TaskName $taskName -Action $action -Trigger $trigger `
  -Settings $settings -Principal $principal -Force | Out-Null
Start-ScheduledTask -TaskName $taskName

Start-Sleep -Seconds 3
Write-Host ""
Write-Host "Instalación completa. Estado del agente:"
& $agentExe status --data-dir $DataDir
Write-Host ""
Write-Host "Tarea programada: $taskName (arranque automático como SYSTEM)"
Write-Host "Logs: Get-ScheduledTaskInfo $taskName / Visor de eventos"

# =============================================================================
# install-windows.ps1 — instalador del servidor BDR Self-Hosted en Windows.
#
# Descarga el bundle de la release, verifica el SHA-256, instala el binario
# con kopia, crea el servicio bdr-control y abre el firewall.
#
# Uso recomendado (revisa antes de ejecutar — ejecuta código remoto):
#   irm <URL-de-la-release>/install-windows.ps1 -OutFile install-windows.ps1
#   notepad install-windows.ps1
#   .\install-windows.ps1
#
# Opciones: -Version v0.8.0  -BaseUrl URL  -Port 8080  -AgentPort 8443
#           -NoService
# =============================================================================
param(
  [string]$Version,
  [string]$BaseUrl = "https://github.com/juansoler/bdr/releases",
  [int]$Port = 8080,
  [int]$AgentPort = 8443,
  [string]$PublicHost = "",
  [switch]$NoService
)

$ErrorActionPreference = "Stop"

# Comprobaciones de privilegios.
$principal = New-Object Security.Principal.WindowsPrincipal([Security.Principal.WindowsIdentity]::GetCurrent())
if (-not $principal.IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)) {
  throw "Ejecuta este script como administrador."
}

$latest = "$BaseUrl/latest/download"
$platform = "windows-amd64"

if (-not $Version) {
  $manifest = Invoke-RestMethod -Uri "$latest/latest.json" -UseBasicParsing
  $Version = "v" + $manifest.version
}
$dl = "$BaseUrl/download/$Version"  # formato GitHub: /releases/download/<tag>/<fichero>

$tmp = Join-Path $env:TEMP ("bdr-install-" + [guid]::NewGuid().ToString("N"))
New-Item -ItemType Directory -Force -Path $tmp | Out-Null
try {
  Write-Host "== descargando bundle bdr-control $Version ($platform)…"
  Invoke-WebRequest -Uri "$dl/bdr-control-$platform.zip" -OutFile "$tmp\bundle.zip" -UseBasicParsing
  Invoke-WebRequest -Uri "$dl/SHA256SUMS" -OutFile "$tmp\SHA256SUMS" -UseBasicParsing

  Write-Host "== verificando SHA-256…"
  $expected = (Select-String -Path "$tmp\SHA256SUMS" -Pattern "bdr-control-$platform\.zip").Line -split "\s+" | Select-Object -First 1
  if (-not $expected) { throw "SHA256SUMS no contiene el bundle de $platform" }
  $actual = (Get-FileHash "$tmp\bundle.zip" -Algorithm SHA256).Hash.ToLower()
  if ($actual -ne $expected.ToLower()) {
    throw "el bundle no coincide con SHA256SUMS — NO continuar"
  }

  Write-Host "== extrayendo…"
  Expand-Archive -Path "$tmp\bundle.zip" -DestinationPath $tmp -Force

  $installDir = "C:\Program Files\BDR"
  $dataDir = "C:\ProgramData\BDR"
  New-Item -ItemType Directory -Force -Path $installDir, "$dataDir\control" | Out-Null
  Copy-Item "$tmp\bdr-control.exe" (Join-Path $installDir "bdr-control.exe") -Force
  if (Test-Path "$tmp\kopia.exe") {
    Copy-Item "$tmp\kopia.exe" (Join-Path $installDir "kopia.exe") -Force
  }
  # agents.json junto al binario: alimenta las ofertas de autoactualización
  # de agentes de ESTA versión del control.
  if (Test-Path "$tmp\agents.json") {
    Copy-Item "$tmp\agents.json" (Join-Path $installDir "agents.json") -Force
  }
  # Directorio de datos: solo SYSTEM y administradores.
  icacls $dataDir /inheritance:r /grant "SYSTEM:(OI)(CI)F" /grant "Administrators:(OI)(CI)F" | Out-Null

  # Agentes para los instaladores del panel.
  $agentsDir = Join-Path $installDir "agents"
  New-Item -ItemType Directory -Force -Path $agentsDir | Out-Null
  Copy-Item "$tmp\bdr-agent-*" $agentsDir -Force -ErrorAction SilentlyContinue

  # Secretos: se generan solo si no existen (re-instalaciones conservan credenciales).
  $envFile = Join-Path $dataDir "bdr.env"
  if (-not (Test-Path $envFile)) {
    $bytes = New-Object byte[] 32
    $rng = [System.Security.Cryptography.RandomNumberGenerator]::Create()
    $rng.GetBytes($bytes)
    $adminToken = [Convert]::ToBase64String($bytes)
    $rng.GetBytes($bytes)
    $secretKey = [Convert]::ToBase64String($bytes)
    $rng.GetBytes($bytes)
    $kopiaPassword = [Convert]::ToBase64String($bytes)
    Set-Content -Path $envFile -Value "BDR_ADMIN_TOKEN=$adminToken`nBDR_SECRET_KEY=$secretKey`nBDR_KOPIA_PASSWORD=$kopiaPassword" -Encoding ascii
  }
  $env = Get-Content $envFile | ConvertFrom-StringData
  icacls $envFile /inheritance:r /grant "SYSTEM:(OI)(CI)F" /grant "Administrators:(OI)(CI)F" | Out-Null

  if (-not $NoService) {
    if (Get-Service bdr-control -ErrorAction SilentlyContinue) {
      Stop-Service bdr-control -ErrorAction SilentlyContinue
      & sc.exe delete bdr-control | Out-Null
      Start-Sleep -Seconds 2
    }
    $exe = Join-Path $installDir "bdr-control.exe"
    # Dirección publicada: parámetro -PublicHost (IP NetBird o DNS de la malla)
  # o el nombre del equipo (mDNS/LAN) por defecto.
  if ($PublicHost) {
    $hostForAgents = $PublicHost
  } else {
    $hostForAgents = "$env:COMPUTERNAME.$($env:USERDNSDOMAIN)"
  }
  $binPath = "`"$exe`" --addr 0.0.0.0:$Port --agent-addr 0.0.0.0:$AgentPort --public-url `"http://$hostForAgents`:$Port`" --agent-url `"https://$hostForAgents`:$AgentPort`" --agent-tls-names `"$hostForAgents,localhost`" --data-dir `"$dataDir\control`" --downloads-dir `"$agentsDir`" --kopia-binary `"$installDir\kopia.exe`""
    New-Service -Name bdr-control -DisplayName "BDR Control" -BinaryPathName $binPath -StartupType Automatic | Out-Null
    sc.exe failure bdr-control reset= 86400 actions= restart/5000/restart/10000/restart/300000 | Out-Null
    sc.exe description bdr-control "Plano de control y panel de BDR Self-Hosted" | Out-Null

    # Secretos del servicio: variables de entorno de máquina (lícito en un
    # host dedicado; alternativa: montar un proxy con secretos propios).
    foreach ($line in Get-Content $envFile) {
      if ($line -match "^([^=]+)=(.*)$") {
        [Environment]::SetEnvironmentVariable($Matches[1], $Matches[2], "Machine")
      }
    }

    # Firewall: panel+API y listener de agentes.
    New-NetFirewallRule -DisplayName "BDR Control" -Direction Inbound -Protocol TCP -LocalPort $Port -Action Allow -Profile Any | Out-Null
    New-NetFirewallRule -DisplayName "BDR Agentes mTLS" -Direction Inbound -Protocol TCP -LocalPort $AgentPort -Action Allow -Profile Any | Out-Null

    try {
      Start-Service bdr-control -ErrorAction Stop
      Write-Host "servicio bdr-control en marcha"
    } catch {
      Write-Warning "el servicio no arrancó: $_"
      Write-Host "intenta manualmente: Start-Service bdr-control"
    }
  }

  Write-Host ""
  Write-Host "== instalación completa =="
  Write-Host "panel:        http://localhost:$Port/"
  Write-Host "agentes mTLS: $AgentPort"
  Write-Host "token:        $envFile (BDR_ADMIN_TOKEN)"
  Write-Host "nota:         pon TLS delante (proxy con certificado válido) antes de exponer el panel"
} finally {
  Remove-Item -Recurse -Force $tmp -ErrorAction SilentlyContinue
}

# Captura de entorno Windows (solo lectura).
# Uso (consola elevada recomendada para VSS/BitLocker):
#   powershell -NoProfile -ExecutionPolicy Bypass -File capture-env.ps1 -OutDir <dir>
# No crea instantáneas ni modifica configuración.

[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)]
    [string]$OutDir
)

$ErrorActionPreference = 'Continue'
New-Item -ItemType Directory -Force -Path $OutDir | Out-Null
$stamp = (Get-Date).ToUniversalTime().ToString('o')

function Try-Get {
    param([scriptblock]$Block)
    try { & $Block } catch { $null }
}

function Write-Text {
    param([string]$Name, [string]$Content)
    $path = Join-Path $OutDir $Name
    Set-Content -Path $path -Value $Content -Encoding UTF8
}

$os = Try-Get { Get-CimInstance Win32_OperatingSystem }
$cs = Try-Get { Get-CimInstance Win32_ComputerSystem }
$bios = Try-Get { Get-CimInstance Win32_BIOS }
$firmwareRaw = Try-Get { (Get-ComputerInfo -Property BiosFirmwareType).BiosFirmwareType }
$firmware = switch ($firmwareRaw) {
    1 { 'bios' }
    2 { 'uefi' }
    'Bios' { 'bios' }
    'Uefi' { 'uefi' }
    default { 'unknown' }
}
$secureBoot = Try-Get { Confirm-SecureBootUEFI }
$tpm = Try-Get {
    Get-CimInstance -Namespace 'root/cimv2/security/microsofttpm' -ClassName Win32_Tpm |
        Select-Object SpecVersion, IsEnabled_InitialValue, IsActivated_InitialValue, ManufacturerVersion
}
$disks = Try-Get {
    Get-Disk | Select-Object Number, FriendlyName, SerialNumber, Size, PartitionStyle, BusType, IsBoot, IsSystem
}
$partitions = Try-Get {
    Get-Partition | Select-Object DiskNumber, PartitionNumber, DriveLetter, Offset, Size, Type, GptType
}
$volumes = Try-Get {
    Get-Volume | Select-Object DriveLetter, FileSystemLabel, FileSystem, Size, SizeRemaining, HealthStatus
}
$physical = Try-Get {
    Get-PhysicalDisk | Select-Object DeviceId, FriendlyName, MediaType, BusType, Size, HealthStatus
}
$shadow = Try-Get {
    Get-CimInstance Win32_ShadowCopy | Select-Object ID, VolumeName, InstallDate, State, DeviceObject
}
$bitlocker = Try-Get {
    Get-BitLockerVolume | Select-Object MountPoint, VolumeStatus, ProtectionStatus, EncryptionPercentage,
        @{Name = 'KeyProtectorTypes'; Expression = { $_.KeyProtector | ForEach-Object { $_.KeyProtectorType } }}
}

$data = [ordered]@{
    timestamp_utc   = $stamp
    hostname        = $env:COMPUTERNAME
    user            = $env:USERNAME
    os_caption      = if ($os) { $os.Caption } else { $null }
    os_version      = if ($os) { $os.Version } else { $null }
    os_build        = if ($os) { $os.BuildNumber } else { $null }
    manufacturer    = if ($cs) { $cs.Manufacturer } else { $null }
    model           = if ($cs) { $cs.Model } else { $null }
    bios_version    = if ($bios) { $bios.SMBIOSBIOSVersion } else { $null }
    firmware        = $firmware
    secure_boot     = $secureBoot
    tpm             = $tpm
    bitlocker       = $bitlocker
    disks           = $disks
    partitions      = $partitions
    volumes         = $volumes
    physical_disks  = $physical
    shadow_copies   = $shadow
}

$jsonPath = Join-Path $OutDir 'env-windows.json'
$data | ConvertTo-Json -Depth 8 | Set-Content -Path $jsonPath -Encoding UTF8

$commands = [ordered]@{
    'vss-writers.txt'   = 'vssadmin list writers'
    'vss-shadows.txt'   = 'vssadmin list shadows'
    'bitlocker.txt'     = 'manage-bde -status'
    'volumes.txt'       = 'mountvol'
}
foreach ($name in $commands.Keys) {
    $cmd = $commands[$name]
    try {
        $output = & cmd.exe /c "$cmd 2>&1"
        Write-Text -Name $name -Content ("`$ $cmd`r`n" + ($output -join "`r`n"))
    } catch {
        Write-Text -Name $name -Content ("`$ $cmd`r`nERROR: " + $_.Exception.Message)
    }
}

Write-Host "Evidencia escrita en: $OutDir"
Write-Host "  - env-windows.json"
Write-Host "  - vss-writers.txt, vss-shadows.txt, bitlocker.txt, volumes.txt"

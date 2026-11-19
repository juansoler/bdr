//go:build windows

package inventory

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"github.com/juansoler/bdr/packages/contracts"
)

var (
	kernel32                 = syscall.NewLazyDLL("kernel32.dll")
	procGlobalMemoryStatusEx = kernel32.NewProc("GlobalMemoryStatusEx")
	procGetDiskFreeSpaceExW  = kernel32.NewProc("GetDiskFreeSpaceExW")
)

type memoryStatusEx struct {
	Length               uint32
	MemoryLoad           uint32
	TotalPhys            uint64
	AvailPhys            uint64
	TotalPageFile        uint64
	AvailPageFile        uint64
	TotalVirtual         uint64
	AvailVirtual         uint64
	AvailExtendedVirtual uint64
}

func collectPlatform(ctx context.Context, inv *contracts.Inventory) {
	collectMemory(inv)
	collectDisksFallback(inv)

	win, err := collectWindows(ctx)
	if err != nil {
		inv.Extra["windows_inventory_error"] = err.Error()
		return
	}
	if win.OS != nil && win.OS.Caption.String() != "" {
		inv.OSVersion = win.OS.Caption.String()
	} else {
		major, minor := syscall.GetVersion()
		inv.OSVersion = fmt.Sprintf("Windows %d.%d", major, minor)
	}
	applyWindows(win, inv)
}

func collectMemory(inv *contracts.Inventory) {
	var m memoryStatusEx
	m.Length = uint32(unsafe.Sizeof(m))
	if r, _, err := procGlobalMemoryStatusEx.Call(uintptr(unsafe.Pointer(&m))); r != 0 {
		inv.RAMMB = int64(m.TotalPhys / (1024 * 1024))
	} else {
		inv.Extra["ram_error"] = err.Error()
	}
}

// collectDisksFallback deja al menos el volumen del sistema si PowerShell falla.
func collectDisksFallback(inv *contracts.Inventory) {
	drive := `C:\`
	p, err := syscall.UTF16PtrFromString(drive)
	if err != nil {
		return
	}
	var free, total, totalFree uint64
	r, _, _ := procGetDiskFreeSpaceExW.Call(
		uintptr(unsafe.Pointer(p)),
		uintptr(unsafe.Pointer(&free)),
		uintptr(unsafe.Pointer(&total)),
		uintptr(unsafe.Pointer(&totalFree)),
	)
	if r != 0 {
		inv.Disks = append(inv.Disks, contracts.Disk{
			Name:       "C:",
			Mountpoint: drive,
			TotalBytes: int64(total),
			FreeBytes:  int64(free),
		})
	}
}

// --- Inventario vía PowerShell (best-effort) ---

const windowsInventoryScript = `$ErrorActionPreference = 'SilentlyContinue'
[Console]::OutputEncoding = [System.Text.Encoding]::UTF8
$disks = @()
try { $disks = @(Get-Disk | Select-Object Number,FriendlyName,SerialNumber,Size,PartitionStyle,BusType,IsBoot,IsSystem) } catch {}
$parts = @()
try { $parts = @(Get-Partition | Select-Object DiskNumber,PartitionNumber,DriveLetter,Size,Type,Offset) } catch {}
$vols = @()
try { $vols = @(Get-Volume | Select-Object DriveLetter,FileSystemLabel,FileSystem,FileSystemType,Size,SizeRemaining) } catch {}
$sb = $null
try { $sb = Confirm-SecureBootUEFI } catch {}
$tpm = $null
try { $t = Get-Tpm; $tpm = [pscustomobject]@{ Present = $t.TpmPresent; Ready = $t.TpmReady; Version = [string]$t.ManufacturerVersion } } catch {}
$bl = @()
try { $bl = @(Get-BitLockerVolume | Select-Object MountPoint,VolumeStatus,ProtectionStatus,LockStatus,VolumeType) } catch {}
$fw = $null
try { $fw = [string](Get-ComputerInfo -Property BiosFirmwareType).BiosFirmwareType } catch {}
$os = $null
try { $os = Get-CimInstance Win32_OperatingSystem | Select-Object Caption,Version,BuildNumber } catch {}
[pscustomobject]@{
  disks = @($disks)
  partitions = @($parts)
  volumes = @($vols)
  secure_boot = $sb
  tpm = $tpm
  bitlocker = @($bl)
  firmware = $fw
  os = $os
} | ConvertTo-Json -Depth 6 -Compress`

type windowsInventory struct {
	Disks []struct {
		Number         flexInt64  `json:"Number"`
		FriendlyName   flexString `json:"FriendlyName"`
		SerialNumber   flexString `json:"SerialNumber"`
		Size           flexInt64  `json:"Size"`
		PartitionStyle flexString `json:"PartitionStyle"`
		BusType        flexString `json:"BusType"`
		IsBoot         flexBool   `json:"IsBoot"`
		IsSystem       flexBool   `json:"IsSystem"`
	} `json:"disks"`
	Partitions []struct {
		DiskNumber      flexInt64  `json:"DiskNumber"`
		PartitionNumber flexInt64  `json:"PartitionNumber"`
		DriveLetter     flexString `json:"DriveLetter"`
		Size            flexInt64  `json:"Size"`
		Type            flexString `json:"Type"`
		Offset          flexInt64  `json:"Offset"`
	} `json:"partitions"`
	Volumes []struct {
		DriveLetter     flexString `json:"DriveLetter"`
		FileSystemLabel flexString `json:"FileSystemLabel"`
		FileSystem      flexString `json:"FileSystem"`
		FileSystemType  flexString `json:"FileSystemType"`
		Size            flexInt64  `json:"Size"`
		SizeRemaining   flexInt64  `json:"SizeRemaining"`
	} `json:"volumes"`
	SecureBoot *bool `json:"secure_boot"`
	TPM        *struct {
		Present *bool      `json:"Present"`
		Ready   *bool      `json:"Ready"`
		Version flexString `json:"Version"`
	} `json:"tpm"`
	Bitlocker []struct {
		MountPoint       flexString `json:"MountPoint"`
		VolumeStatus     flexString `json:"VolumeStatus"`
		ProtectionStatus flexString `json:"ProtectionStatus"`
		LockStatus       flexString `json:"LockStatus"`
		VolumeType       flexString `json:"VolumeType"`
	} `json:"bitlocker"`
	Firmware flexString `json:"firmware"`
	OS       *struct {
		Caption     flexString `json:"Caption"`
		Version     flexString `json:"Version"`
		BuildNumber flexString `json:"BuildNumber"`
	} `json:"os"`
}

func collectWindows(ctx context.Context) (windowsInventory, error) {
	var out windowsInventory
	runCtx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	cmd := exec.CommandContext(runCtx, "powershell.exe", "-NoProfile", "-NonInteractive",
		"-ExecutionPolicy", "Bypass", "-Command", windowsInventoryScript)
	b, err := cmd.Output()
	if err != nil {
		if runCtx.Err() != nil {
			return out, fmt.Errorf("PowerShell agotó el tiempo de inventario: %w", runCtx.Err())
		}
		return out, fmt.Errorf("PowerShell falló: %w", err)
	}
	// PowerShell puede añadir BOM o líneas vacías.
	data := strings.TrimSpace(strings.TrimPrefix(string(b), "\ufeff"))
	if err := json.Unmarshal([]byte(data), &out); err != nil {
		return out, fmt.Errorf("salida de PowerShell no es JSON válido: %w", err)
	}
	return out, nil
}

func applyWindows(win windowsInventory, inv *contracts.Inventory) {
	volByLetter := map[string]struct {
		fsType string
		label  string
	}{}
	for _, v := range win.Volumes {
		letter := strings.ToUpper(strings.TrimSpace(v.DriveLetter.String()))
		fs := v.FileSystem.String()
		if fs == "" {
			fs = v.FileSystemType.String()
		}
		volByLetter[letter] = struct {
			fsType string
			label  string
		}{fsType: fs, label: v.FileSystemLabel.String()}
		if letter != "" {
			inv.Disks = append(inv.Disks, contracts.Disk{
				Name:       letter + ":",
				Mountpoint: letter + `:\`,
				FSType:     fs,
				TotalBytes: v.Size.Int64(),
				FreeBytes:  v.SizeRemaining.Int64(),
			})
		}
	}

	for _, d := range win.Disks {
		bd := contracts.BlockDevice{
			Name:      fmt.Sprintf("Disco %d", d.Number.Int64()),
			Type:      "disk",
			SizeBytes: d.Size.Int64(),
			Model:     strings.TrimSpace(d.FriendlyName.String()),
			Serial:    strings.TrimSpace(d.SerialNumber.String()),
			PartTable: normalizePartitionStyle(d.PartitionStyle.String()),
			Removable: strings.EqualFold(d.BusType.String(), "USB"),
		}
		for _, p := range win.Partitions {
			if p.DiskNumber.Int64() != d.Number.Int64() {
				continue
			}
			letter := strings.ToUpper(strings.TrimSpace(p.DriveLetter.String()))
			name := fmt.Sprintf("Disco %d Partición %d", d.Number.Int64(), p.PartitionNumber.Int64())
			mount := ""
			fsType := ""
			if letter != "" {
				name = letter + ":"
				mount = letter + `:\`
				fsType = volByLetter[letter].fsType
			}
			bd.Children = append(bd.Children, contracts.BlockDevice{
				Name:       name,
				Type:       "part",
				SizeBytes:  p.Size.Int64(),
				FSType:     fsType,
				Mountpoint: mount,
			})
		}
		inv.BlockDevices = append(inv.BlockDevices, bd)
	}

	fw := &contracts.Firmware{}
	switch {
	case strings.Contains(strings.ToLower(win.Firmware.String()), "uefi"):
		fw.Mode = "uefi"
	case strings.Contains(strings.ToLower(win.Firmware.String()), "bios"):
		fw.Mode = "bios"
	}
	fw.SecureBoot = win.SecureBoot
	if win.TPM != nil && win.TPM.Present != nil {
		fw.TPM = win.TPM.Present
		fw.TPMVersion = strings.TrimSpace(win.TPM.Version.String())
	}
	if fw.Mode != "" || fw.SecureBoot != nil || fw.TPM != nil {
		inv.Firmware = fw
	}

	for _, bl := range win.Bitlocker {
		if strings.EqualFold(bl.VolumeStatus.String(), "FullyDecrypted") &&
			strings.EqualFold(bl.ProtectionStatus.String(), "Off") {
			continue
		}
		inv.EncryptedVolumes = append(inv.EncryptedVolumes, contracts.EncryptedVolume{
			Volume:     bl.MountPoint.String(),
			Kind:       "bitlocker",
			Protection: bl.ProtectionStatus.String(),
			Locked:     strings.EqualFold(bl.LockStatus.String(), "Locked"),
		})
	}
}

func normalizePartitionStyle(s string) string {
	switch strings.ToUpper(strings.TrimSpace(s)) {
	case "GPT":
		return "gpt"
	case "MBR":
		return "dos"
	default:
		return strings.ToLower(s)
	}
}

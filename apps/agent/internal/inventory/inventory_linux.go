//go:build linux

package inventory

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"github.com/juansoler/bdr/packages/contracts"
)

func collectPlatform(ctx context.Context, inv *contracts.Inventory) {
	inv.OSVersion = linuxPrettyName()
	inv.RAMMB = linuxRAMMB()

	devices, encrypted, disks := collectBlockDevices(ctx)
	inv.BlockDevices = devices
	inv.EncryptedVolumes = encrypted
	inv.Disks = append(inv.Disks, disks...)
	inv.Disks = appendRootDiskIfMissing(inv.Disks)
	collectFirmware(inv)
}

// --- Discos y particiones ---

type lsblkOutput struct {
	BlockDevices []lsblkNode `json:"blockdevices"`
}

type lsblkNode struct {
	Name      flexString  `json:"name"`
	Type      flexString  `json:"type"`
	Size      flexInt64   `json:"size"`
	Model     flexString  `json:"model"`
	Serial    flexString  `json:"serial"`
	FSType    flexString  `json:"fstype"`
	Mount     flexString  `json:"mountpoint"`
	UUID      flexString  `json:"uuid"`
	PTType    flexString  `json:"pttype"`
	Removable flexBool    `json:"rm"`
	Children  []lsblkNode `json:"children"`
}

func collectBlockDevices(ctx context.Context) ([]contracts.BlockDevice, []contracts.EncryptedVolume, []contracts.Disk) {
	var nodes []lsblkNode
	if out, err := exec.CommandContext(ctx, "lsblk", "-J", "-b", "-o",
		"NAME,TYPE,SIZE,MODEL,SERIAL,FSTYPE,MOUNTPOINT,UUID,PTTYPE,RM").Output(); err == nil {
		var parsed lsblkOutput
		if err := json.Unmarshal(out, &parsed); err == nil {
			nodes = parsed.BlockDevices
		}
	}
	if len(nodes) == 0 {
		nodes = lsblkFromSysfs()
	}

	devices := make([]contracts.BlockDevice, 0, len(nodes))
	for _, n := range nodes {
		devices = append(devices, convertLsblkNode(n))
	}

	var encrypted []contracts.EncryptedVolume
	var disks []contracts.Disk
	seenCrypt := map[string]bool{}
	seenMount := map[string]bool{}
	walkDevices(devices, func(d contracts.BlockDevice) {
		if strings.EqualFold(d.FSType, "crypto_LUKS") || d.Type == "crypt" {
			if !seenCrypt[d.Name] {
				seenCrypt[d.Name] = true
				encrypted = append(encrypted, contracts.EncryptedVolume{
					Volume: d.Name, Kind: "luks", Locked: d.Type == "crypt",
				})
			}
		}
		if d.Mountpoint != "" && d.FSType != "" && !strings.EqualFold(d.FSType, "crypto_LUKS") {
			if !seenMount[d.Mountpoint] {
				seenMount[d.Mountpoint] = true
				disks = append(disks, contracts.Disk{
					Name:       d.Name,
					Mountpoint: d.Mountpoint,
					FSType:     d.FSType,
					TotalBytes: d.SizeBytes,
					FreeBytes:  freeBytes(d.Mountpoint),
				})
			}
		}
	})
	return devices, encrypted, disks
}

func convertLsblkNode(n lsblkNode) contracts.BlockDevice {
	d := contracts.BlockDevice{
		Name:       n.Name.String(),
		Type:       normalizeBlockType(n.Type.String()),
		SizeBytes:  n.Size.Int64(),
		Model:      strings.TrimSpace(n.Model.String()),
		Serial:     strings.TrimSpace(n.Serial.String()),
		FSType:     n.FSType.String(),
		Mountpoint: n.Mount.String(),
		UUID:       n.UUID.String(),
		PartTable:  strings.ToLower(n.PTType.String()),
		Removable:  n.Removable.Bool(),
	}
	for _, c := range n.Children {
		d.Children = append(d.Children, convertLsblkNode(c))
	}
	return d
}

func normalizeBlockType(t string) string {
	t = strings.ToLower(strings.TrimSpace(t))
	switch {
	case t == "":
		return "disk"
	case strings.HasPrefix(t, "raid"), t == "md":
		return "raid"
	case t == "mpath":
		return "raid"
	case t == "loop", t == "rom", t == "part", t == "lvm", t == "crypt", t == "disk":
		return t
	default:
		return t
	}
}

func walkDevices(devs []contracts.BlockDevice, fn func(contracts.BlockDevice)) {
	for _, d := range devs {
		fn(d)
		walkDevices(d.Children, fn)
	}
}

// lsblkFromSysfs es el respaldo cuando lsblk no está disponible.
func lsblkFromSysfs() []lsblkNode {
	entries, err := os.ReadDir("/sys/block")
	if err != nil {
		return nil
	}
	var out []lsblkNode
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		node := lsblkNode{
			Name: flexString(name),
			Type: flexString("disk"),
			Size: flexInt64(readSysfsSize(filepath.Join("/sys/block", name))),
		}
		node.Model = flexString(readTrimmed("/sys/block/" + name + "/device/model"))
		node.Serial = flexString(readTrimmed("/sys/block/" + name + "/device/serial"))
		partEntries, _ := os.ReadDir("/sys/block/" + name)
		for _, p := range partEntries {
			pn := p.Name()
			if pn == name || !strings.HasPrefix(pn, name) || !allDigits(strings.TrimPrefix(pn, name)) {
				continue
			}
			node.Children = append(node.Children, lsblkNode{
				Name: flexString(pn),
				Type: flexString("part"),
				Size: flexInt64(readSysfsSize(filepath.Join("/sys/block", name, pn))),
			})
		}
		out = append(out, node)
	}
	return out
}

// readSysfsSize lee el tamaño de un dispositivo en /sys (sectores de 512 B).
func readSysfsSize(dir string) int64 {
	s := strings.TrimSpace(readFileString(filepath.Join(dir, "size")))
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0
	}
	// El kernel expone sectores de 512 bytes.
	return n * 512
}

func readTrimmed(path string) string {
	return strings.TrimSpace(readFileString(path))
}

func readFileString(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return string(b)
}

func allDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func freeBytes(mountpoint string) int64 {
	var st syscall.Statfs_t
	if err := syscall.Statfs(mountpoint, &st); err != nil {
		return 0
	}
	return int64(st.Bavail) * int64(st.Bsize)
}

func appendRootDiskIfMissing(disks []contracts.Disk) []contracts.Disk {
	for _, d := range disks {
		if d.Mountpoint == "/" {
			return disks
		}
	}
	var st syscall.Statfs_t
	if err := syscall.Statfs("/", &st); err == nil {
		disks = append(disks, contracts.Disk{
			Name:       "/",
			Mountpoint: "/",
			TotalBytes: int64(st.Blocks) * int64(st.Bsize),
			FreeBytes:  int64(st.Bavail) * int64(st.Bsize),
		})
	}
	return disks
}

// --- Firmware y seguridad de plataforma ---

func collectFirmware(inv *contracts.Inventory) {
	fw := &contracts.Firmware{}
	if _, err := os.Stat("/sys/firmware/efi"); err == nil {
		fw.Mode = "uefi"
		if sb, ok := secureBootState(); ok {
			fw.SecureBoot = &sb
		}
	} else {
		fw.Mode = "bios"
	}
	if present, version, ok := tpmState(); ok {
		fw.TPM = &present
		fw.TPMVersion = version
	}
	inv.Firmware = fw
}

// secureBootState lee la variable EFI SecureBoot. El archivo tiene 4 bytes de
// atributos y a continuación el valor (1 = activado, 0 = desactivado).
func secureBootState() (bool, bool) {
	matches, err := filepath.Glob("/sys/firmware/efi/efivars/SecureBoot-*")
	if err != nil || len(matches) == 0 {
		return false, false
	}
	b, err := os.ReadFile(matches[0])
	if err != nil || len(b) < 5 {
		return false, false
	}
	return b[4] == 1, true
}

func tpmState() (bool, string, bool) {
	if _, err := os.Stat("/sys/class/tpm/tpm0"); err != nil {
		return false, "", false
	}
	version := strings.TrimSpace(readFileString("/sys/class/tpm/tpm0/tpm_version_major"))
	if version != "" && !strings.Contains(version, ".") {
		version += ".0"
	}
	if version == "" {
		desc := strings.ToLower(readFileString("/sys/class/tpm/tpm0/device/description"))
		switch {
		case strings.Contains(desc, "2.0"):
			version = "2.0"
		case strings.Contains(desc, "1.2"):
			version = "1.2"
		default:
			version = "2.0"
		}
	}
	return true, version, true
}

// --- Sistema operativo y memoria ---

func linuxPrettyName() string {
	b, err := os.ReadFile("/etc/os-release")
	if err != nil {
		return "linux"
	}
	for _, line := range strings.Split(string(b), "\n") {
		if strings.HasPrefix(line, "PRETTY_NAME=") {
			return strings.Trim(strings.TrimSpace(strings.TrimPrefix(line, "PRETTY_NAME=")), `"`)
		}
	}
	return "linux"
}

func linuxRAMMB() int64 {
	b, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return 0
	}
	for _, line := range strings.Split(string(b), "\n") {
		if strings.HasPrefix(line, "MemTotal:") {
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				if kb, err := strconv.ParseInt(fields[1], 10, 64); err == nil {
					return kb / 1024
				}
			}
			break
		}
	}
	return 0
}

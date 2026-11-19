// Enumeración de discos para captura de imagen: Linux vía lsblk.
package jobs

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"

	pkgimg "github.com/juansoler/bdr/packages/image"
)

// lsblkDisk es el árbol de lsblk -b -J.
type lsblkDisk struct {
	Name      string      `json:"name"`
	Size      int64       `json:"size"`
	Type      string      `json:"type"`
	Removable bool        `json:"rm"`
	ReadOnly  bool        `json:"ro"`
	PTType    string      `json:"pttype"`
	Model     string      `json:"model"`
	Serial    string      `json:"serial"`
	LogSec    int64       `json:"log-sec"`
	Children  []lsblkPart `json:"children"`
}

type lsblkPart struct {
	Name       string `json:"name"`
	Size       int64  `json:"size"`
	PartType   string `json:"parttype"`
	FSType     string `json:"fstype"`
	Label      string `json:"label"`
	UUID       string `json:"uuid"`
	MountPoint string `json:"mountpoint"`
}

// enumerateDisksLinux lista discos fijos (no extraíbles, no bucle) con sus
// particiones vía `lsblk -b -J`. Índice = orden de enumeración.
func enumerateDisksLinux() ([]pkgimg.Disk, error) {
	out, err := runProbe(30, "lsblk", "-b", "-J", "-o",
		"NAME,SIZE,TYPE,RM,RO,PTTYPE,MODEL,SERIAL,LOG-SEC")
	if err != nil {
		return nil, fmt.Errorf("lsblk: %s", err.Error())
	}
	var tree struct {
		Blockdevices []lsblkDisk `json:"blockdevices"`
	}
	if err := json.Unmarshal([]byte(out), &tree); err != nil {
		return nil, fmt.Errorf("lsblk json: %w", err)
	}
	var disks []pkgimg.Disk
	for i, d := range tree.Blockdevices {
		if d.Type != "disk" || d.Removable || d.ReadOnly {
			continue
		}
		dev := "/dev/" + d.Name
		disk := pkgimg.Disk{
			Index:      i,
			DevicePath: dev,
			Table:      strings.ToLower(d.PTType),
			TotalBytes: d.Size,
			SectorSize: int(d.LogSec),
		}
		if disk.SectorSize <= 0 {
			disk.SectorSize = 512
		}
		if st, err := os.Stat(dev); err == nil && st.Size() > 0 {
			disk.TotalBytes = st.Size()
		}
		for _, p := range d.Children {
			disk.Partitions = append(disk.Partitions, pkgimg.Partition{
				Offset:     partitionOffsetLinux(d.Name, p.Name),
				Size:       p.Size,
				Role:       roleFromGUID(p.PartType, p.FSType),
				FSLabel:    p.Label,
				FSUUID:     p.UUID,
				Filesystem: p.FSType,
				MountPoint: p.MountPoint,
			})
		}
		disks = append(disks, disk)
	}
	return disks, nil
}

// partitionOffsetLinux obtiene el offset en bytes de una partición desde
// /sys (start × sector size). Si falla, se deja 0 (informativo).
func partitionOffsetLinux(diskName, partName string) int64 {
	start, err := readSysInt("/sys/block/" + diskName + "/" + partName + "/start")
	if err != nil {
		return 0
	}
	sector, _ := readSysInt("/sys/block/" + diskName + "/queue/logical_block_size")
	if sector <= 0 {
		sector = 512
	}
	return start * sector
}

func readSysInt(path string) (int64, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	return strconv.ParseInt(strings.TrimSpace(string(b)), 10, 64)
}

// GUIDs de partición con rol conocido (GPT).
const (
	guidEFI  = "c12a7328-f81f-11d2-ba4b-00a0c93ec93b"
	guidMSR  = "e3c9e316-0b5c-4db8-817d-f92df00215ae"
	guidRec  = "de94bba4-06d1-4d40-a16a-bfd50179d6ac"
	guidBIOS = "21686148-6449-6e6f-744e-656564454649"
)

func roleFromGUID(partType, fsType string) pkgimg.VolumeRole {
	switch strings.ToLower(partType) {
	case guidEFI:
		return pkgimg.RoleESP
	case guidMSR:
		return pkgimg.RoleMSR
	case guidRec:
		return pkgimg.RoleRecovery
	case guidBIOS:
		return pkgimg.RoleOther
	}
	if strings.HasPrefix(strings.ToLower(fsType), "swap") {
		return pkgimg.RoleSwap
	}
	return ""
}

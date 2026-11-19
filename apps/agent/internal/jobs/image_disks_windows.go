//go:build windows

// Enumeración de discos para captura de imagen en Windows vía la API nativa
// (CreateFile + DeviceIoControl sobre \\.\PhysicalDriveN). No depende de
// PowerShell ni de módulos: funciona en el contexto del servicio SYSTEM y en
// cualquier política de ejecución. Requiere privilegios de administrador.
package jobs

import (
	"encoding/binary"
	"fmt"
	"unsafe"

	pkgimg "github.com/juansoler/bdr/packages/image"

	"golang.org/x/sys/windows"
)

const (
	ioctlDiskGetDriveGeometryEx = 0x700A0 // CTL_CODE(IOCTL_DISK_BASE, 0x28, BUFFERED, ANY)
	ioctlDiskGetDriveLayoutEx   = 0x70050 // CTL_CODE(IOCTL_DISK_BASE, 0x14, BUFFERED, ANY)
	ioctlDiskGetLengthInfo      = 0x7405C // CTL_CODE(IOCTL_DISK_BASE, 0x17, BUFFERED, ANY)

	mediaTypeFixed = 12 // FixedMedia

	partitionStyleMBR = 0
	partitionStyleGPT = 1
)

// diskGeometryEx — sizeof 32 (DISK_GEOMETRY alineada a 8 + DiskSize).
type diskGeometryEx struct {
	Cylinders         int64
	MediaType         uint32
	TracksPerCylinder uint32
	SectorsPerTrack   uint32
	BytesPerSector    uint32
	DiskSize          int64
}

// partitionInfoEx — sizeof 144 (union GPT alineada a 8).
type partitionInfoEx struct {
	PartitionStyle   uint32
	StartingOffset   int64
	PartitionLength  int64
	PartitionNumber  uint32
	RewritePartition uint8
	_                [3]byte
	PartitionType    [16]byte // GUID (GPT) o primer byte = tipo MBR
	PartitionId      [16]byte
	Attributes       uint64
	Name             [36]uint16
}

const partitionInfoExSize = 144

// GUIDs GPT en su representación en bytes (little-endian en los 3 primeros
// campos), para comparar raw sin formatear.
var (
	guidEFIRaw  = mustGUID("c12a7328-f81f-11d2-ba4b-00a0c93ec93b")
	guidMSRRaw  = mustGUID("e3c9e316-0b5c-4db8-817d-f92df00215ae")
	guidRecRaw  = mustGUID("de94bba4-06d1-4d40-a16a-bfd50179d6ac")
	guidSPURaw  = mustGUID("db97dba9-5849-4b26-83f2-b299e2e1d3f8") // Windows Storage Spaces
	guidLUKSRaw = mustGUID("740fa474-c4bc-40ae-8caa-6ca533d4a53a")
)

func mustGUID(s string) [16]byte {
	var g [16]byte
	binary.LittleEndian.PutUint32(g[0:4], parseHex32(s[0:8]))
	binary.LittleEndian.PutUint16(g[4:6], parseHex16(s[9:13]))
	binary.LittleEndian.PutUint16(g[6:8], parseHex16(s[14:18]))
	copy(g[8:16], mustHex(s[19:23]+s[24:36]))
	return g
}

func parseHex32(s string) uint32 {
	var v uint32
	for i := 0; i < 4; i++ {
		v |= uint32(mustHex(s[i*2 : i*2+2])[0]) << (8 * i)
	}
	return v
}

func parseHex16(s string) uint16 {
	var v uint16
	for i := 0; i < 2; i++ {
		v |= uint16(mustHex(s[i*2 : i*2+2])[0]) << (8 * i)
	}
	return v
}

func mustHex(s string) []byte {
	if len(s)%2 != 0 {
		s = "0" + s
	}
	out := make([]byte, len(s)/2)
	for i := 0; i < len(out); i++ {
		hi := hexVal(s[i*2])
		lo := hexVal(s[i*2+1])
		out[i] = byte(hi<<4 | lo)
	}
	return out
}

func hexVal(c byte) int {
	switch {
	case c >= '0' && c <= '9':
		return int(c - '0')
	case c >= 'a' && c <= 'f':
		return int(c-'a') + 10
	case c >= 'A' && c <= 'F':
		return int(c-'A') + 10
	}
	return 0
}

// enumerateDisksWindows recorre PhysicalDrive0..63, mantiene los discos
// fijos y saca la tabla de particiones con GET_DRIVE_LAYOUT_EX.
func enumerateDisksWindows() ([]pkgimg.Disk, error) {
	var disks []pkgimg.Disk
	for i := 0; i < 64; i++ {
		dev := fmt.Sprintf(`\\.\PhysicalDrive%d`, i)
		h, err := windows.CreateFile(
			windows.StringToUTF16Ptr(dev),
			windows.GENERIC_READ,
			windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE,
			nil,
			windows.OPEN_EXISTING,
			0,
			0,
		)
		if err != nil {
			// No existe más disco físico: fin del recorrido.
			break
		}
		disk, perr := probeWindowsDisk(h, i, dev)
		_ = windows.CloseHandle(h)
		if perr != nil {
			continue // disco no consultable (offline, permisos): se salta
		}
		disks = append(disks, disk)
	}
	return disks, nil
}

func probeWindowsDisk(h windows.Handle, index int, dev string) (pkgimg.Disk, error) {
	// Geometría: tamaño real, tamaño de sector y tipo de medio.
	var geo diskGeometryEx
	var ret uint32
	if err := windows.DeviceIoControl(h, ioctlDiskGetDriveGeometryEx,
		nil, 0, (*byte)(unsafe.Pointer(&geo)), uint32(unsafe.Sizeof(geo)), &ret, nil); err != nil {
		return pkgimg.Disk{}, err
	}
	if geo.MediaType != mediaTypeFixed || geo.DiskSize <= 0 {
		return pkgimg.Disk{}, fmt.Errorf("no fijo")
	}
	disk := pkgimg.Disk{
		Index:      index,
		DevicePath: dev,
		TotalBytes: geo.DiskSize,
		SectorSize: int(geo.BytesPerSector),
	}
	if disk.SectorSize <= 0 {
		disk.SectorSize = 512
	}

	// Tamaño exacto direccionable (GET_LENGTH_INFO): la geometría puede
	// redondear a cilindros y devolver más sectores de los legibles, lo que
	// rompe la captura en el último tramo con "sector no encontrado".
	var lengthInfo struct{ Length int64 }
	if err := windows.DeviceIoControl(h, ioctlDiskGetLengthInfo,
		nil, 0, (*byte)(unsafe.Pointer(&lengthInfo)), uint32(unsafe.Sizeof(lengthInfo)), &ret, nil); err == nil {
		if lengthInfo.Length > 0 {
			disk.TotalBytes = lengthInfo.Length
		}
	}

	// Tabla de particiones. El tamaño del buffer depende del número de
	// particiones: se consulta con margen y se reintenta más grande si hace
	// falta.
	entries := 64
	for attempt := 0; attempt < 3; attempt++ {
		buf := make([]byte, 48+uintptr(entries)*partitionInfoExSize)
		if err := windows.DeviceIoControl(h, ioctlDiskGetDriveLayoutEx,
			nil, 0, &buf[0], uint32(len(buf)), &ret, nil); err != nil {
			entries *= 4
			continue
		}
		style := binary.LittleEndian.Uint32(buf[0:4])
		count := binary.LittleEndian.Uint32(buf[4:8])
		if style == partitionStyleGPT {
			disk.Table = "gpt"
		} else if style == partitionStyleMBR {
			disk.Table = "mbr"
		}
		for n := 0; n < int(count) && n < entries; n++ {
			off := 48 + n*partitionInfoExSize
			var p partitionInfoEx
			copy((*[144]byte)(unsafe.Pointer(&p))[:], buf[off:off+partitionInfoExSize])
			if p.PartitionLength <= 0 {
				continue
			}
			disk.Partitions = append(disk.Partitions, pkgimg.Partition{
				Index:      int(p.PartitionNumber),
				Offset:     p.StartingOffset,
				Size:       p.PartitionLength,
				Role:       roleFromRawGUID(p.PartitionStyle, p.PartitionType),
				Filesystem: "ntfs", // informativo; NTFS por defecto en Windows
			})
		}
		return disk, nil
	}
	return pkgimg.Disk{}, fmt.Errorf("tabla de particiones no consultable")
}

// roleFromRawGUID clasifica la partición por su GUID GPT (o tipo MBR).
func roleFromRawGUID(style uint32, raw [16]byte) pkgimg.VolumeRole {
	if style == partitionStyleMBR {
		switch raw[0] {
		case 0xEE: // Protective MBR
			return pkgimg.RoleOther
		case 0xEF: // EFI System (MBR)
			return pkgimg.RoleESP
		case 0x0C, 0x07:
			return pkgimg.RoleData
		}
		return pkgimg.RoleOther
	}
	switch {
	case raw == guidEFIRaw:
		return pkgimg.RoleESP
	case raw == guidMSRRaw:
		return pkgimg.RoleMSR
	case raw == guidRecRaw || raw == guidSPURaw:
		return pkgimg.RoleRecovery
	}
	return pkgimg.RoleData
}

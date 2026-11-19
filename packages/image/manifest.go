// Package image define el manifiesto v1 y los tipos de captura de imagen
// completa de máquina. El manifiesto es inmutable: una vez escrito, un
// restaurador debe rechazar versiones desconocidas.
//
// El esquema JSON de referencia es docs/esquemas/manifest-v1.schema.json.
// Los tipos siguen exactamente el esquema para que la serialización JSON
// sea compatible con el restaurador y con cualquier herramienta externa.
package image

import (
	"encoding/json"
	"fmt"
	"time"
)

// SchemaVersion es la versión del esquema del manifiesto.
const SchemaVersion = 1

// ImageMode es el mecanismo de captura usado.
type ImageMode string

const (
	ModeVSSVolumes   ImageMode = "vss_volumes"   // Windows con VSS
	ModeRawDisk      ImageMode = "raw_disk"      // disco completo sin snapshots (apagado)
	ModeLVMVolumes   ImageMode = "lvm_volumes"   // Linux con LVM
	ModeBtrfsVolumes ImageMode = "btrfs_volumes" // Linux con Btrfs/ZFS
)

// VolumeRole clasifica las particiones según su función en el arranque.
type VolumeRole string

const (
	RoleESP      VolumeRole = "esp"      // EFI System Partition
	RoleMSR      VolumeRole = "msr"      // Microsoft Reserved
	RoleOS       VolumeRole = "os"       // Sistema operativo
	RoleData     VolumeRole = "data"     // Datos
	RoleRecovery VolumeRole = "recovery" // WinRE / recovery
	RoleSwap     VolumeRole = "swap"
	RoleBoot     VolumeRole = "boot"
	RoleOther    VolumeRole = "other"
)

// SegmentState es el estado de un segmento en el journal.
type SegmentState string

const (
	SegmentPending   SegmentState = "pending"
	SegmentUploading SegmentState = "uploading"
	SegmentCompleted SegmentState = "completed"
	SegmentFailed    SegmentState = "failed"
)

// Host identifica la máquina de origen.
type Host struct {
	Hostname  string `json:"hostname"`
	OSFamily  string `json:"os_family"` // windows | linux
	OSVersion string `json:"os_version"`
	Arch      string `json:"arch"`
}

// Partition describe una partición del disco.
type Partition struct {
	Index       int        `json:"index"`
	Offset      int64      `json:"offset"`
	Size        int64      `json:"size"`
	Role        VolumeRole `json:"role"`
	FSLabel     string     `json:"fs_label,omitempty"`
	FSUUID      string     `json:"fs_uuid,omitempty"`
	Filesystem  string     `json:"filesystem,omitempty"`
	MountPoint  string     `json:"mount_point,omitempty"`
	Unallocated bool       `json:"unallocated,omitempty"` // espacio libre entre particiones
}

// Disk describe un disco completo con su tabla de particiones.
type Disk struct {
	Index      int         `json:"index"`
	Table      string      `json:"table"` // gpt | mbr | none
	SectorSize int         `json:"sector_size"`
	TotalBytes int64       `json:"total_bytes"`
	Partitions []Partition `json:"partitions"`
	DevicePath string      `json:"device_path,omitempty"`
}

// Stream es un segmento de captura (bloque de datos del disco).
type Stream struct {
	Disk            int          `json:"disk"`
	Segment         int          `json:"segment"`
	Name            string       `json:"name"` // p. ej. disk0-seg0012
	Offset          int64        `json:"offset"`
	Bytes           int64        `json:"bytes"`
	SHA256          string       `json:"sha256"`
	KopiaSnapshotID string       `json:"kopia_snapshot_id,omitempty"`
	State           SegmentState `json:"state"`
	// Blob es la clave del objeto en el bucket del destino (upload directo
	// por segmento); vacío en capturas antiguas (staging+kopia).
	Blob string `json:"blob,omitempty"`
	// Zero marca segmentos enteramente a cero: no se suben y la
	// restauración escribe ceros en su offset.
	Zero bool `json:"zero,omitempty"`
}

// Repository identifica el repositorio Kopia de destino.
type Repository struct {
	ID            string `json:"id"`
	ServerURL     string `json:"server_url,omitempty"`
	KopiaUsername string `json:"kopia_username,omitempty"`
}

// Security captura el estado de seguridad del sistema.
type Security struct {
	BootMode   string `json:"boot_mode"` // uefi | bios
	SecureBoot bool   `json:"secure_boot"`
	TPM        bool   `json:"tpm"`
	BitLocker  bool   `json:"bitlocker"`
}

// RestoreHints guía al restaurador.
type RestoreHints struct {
	SystemDisk          int    `json:"system_disk"`
	BootPartitionIndex  int    `json:"boot_partition_index"`
	ExpectedFirmware    string `json:"expected_firmware"` // uefi | bios
	NeedsVirtio         bool   `json:"needs_virtio"`
	BitLockerSuspension bool   `json:"bitlocker_suspension,omitempty"`
}

// Tooling registra las versiones del tooling de captura.
type Tooling struct {
	AgentVersion   string `json:"agent_version"`
	KopiaVersion   string `json:"kopia_version,omitempty"`
	ManifestSHA256 string `json:"manifest_sha256,omitempty"`
}

// VM captura la configuración de una VM de Proxmox respaldada (qm config),
// para restaurarla en una instancia nueva con las mismas características.
type VM struct {
	VMID   int               `json:"vmid"`
	Name   string            `json:"name,omitempty"`
	Node   string            `json:"node,omitempty"`
	Config map[string]string `json:"config"` // líneas crudas de qm config
}

// LXC captura la configuración de un contenedor Proxmox respaldado
// (pct config), para restaurarlo con pct restore con las mismas
// características (hostname, memoria, red, rootfs y mount points).
type LXC struct {
	VMID         int               `json:"vmid"`
	Name         string            `json:"name,omitempty"` // hostname
	Node         string            `json:"node,omitempty"`
	Unprivileged bool              `json:"unprivileged,omitempty"`
	Config       map[string]string `json:"config"` // líneas crudas de pct config
}

// Manifest es el documento completo de imagen.
type Manifest struct {
	SchemaVersion            int          `json:"schema_version"`
	ManifestID               string       `json:"manifest_id"`
	RunID                    string       `json:"run_id"`
	AgentID                  string       `json:"agent_id"`
	Host                     Host         `json:"host"`
	Kind                     string       `json:"kind"` // "image" | "vm" | "lxc"
	ImageMode                ImageMode    `json:"image_mode"`
	Consistency              string       `json:"consistency"` // crash_consistent | app_consistent | filesystem_consistent
	CapturedAt               string       `json:"captured_at"`
	Repository               Repository   `json:"repository"`
	Security                 Security     `json:"security"`
	Disks                    []Disk       `json:"disks"`
	Streams                  []Stream     `json:"streams"`
	UnallocatedBytesCaptured bool         `json:"unallocated_bytes_captured"`
	RestoreHints             RestoreHints `json:"restore_hints"`
	Tooling                  Tooling      `json:"tooling"`
	// VM se rellena cuando la captura es de una VM de Proxmox (Kind "vm").
	VM *VM `json:"vm,omitempty"`
	// LXC se rellena cuando la captura es de un contenedor (Kind "lxc"):
	// un único stream con el archivo vzdump (tar.zst) por segmentos.
	LXC *LXC `json:"lxc,omitempty"`
}

// Validate comprueba los invariantes básicos del manifiesto.
func (m *Manifest) Validate() error {
	if m.SchemaVersion != SchemaVersion {
		return fmt.Errorf("schema_version %d ≠ %d", m.SchemaVersion, SchemaVersion)
	}
	if m.Kind != "image" {
		return fmt.Errorf("kind debe ser \"image\", got %q", m.Kind)
	}
	if m.ManifestID == "" || m.RunID == "" || m.AgentID == "" {
		return fmt.Errorf("manifest_id, run_id y agent_id son obligatorios")
	}
	if len(m.Disks) == 0 {
		return fmt.Errorf("el manifiesto debe incluir al menos un disco")
	}
	if m.CapturedAt == "" {
		return fmt.Errorf("captured_at es obligatorio")
	}
	if _, err := time.Parse(time.RFC3339, m.CapturedAt); err != nil {
		return fmt.Errorf("captured_at no es RFC3339: %w", err)
	}
	switch m.Consistency {
	case "crash_consistent", "app_consistent", "filesystem_consistent":
	default:
		return fmt.Errorf("consistency inválido: %q", m.Consistency)
	}
	return nil
}

// Marshal serializa el manifiesto con indentación legible.
func (m *Manifest) Marshal() ([]byte, error) {
	return json.MarshalIndent(m, "", "  ")
}

// UnManifest parsea un manifiesto JSON y valida.
func UnManifest(data []byte) (*Manifest, error) {
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, err
	}
	if err := m.Validate(); err != nil {
		return nil, err
	}
	return &m, nil
}

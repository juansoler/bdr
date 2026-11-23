// Runner de VMs de Proxmox (BACKUP_VM / RESTORE_VM). Ejecuta en el propio
// nodo PVE con privilegios de root.
//
// Backup: qm config → discos (volid) → pvesm path → qemu-nbd read-only →
// captura por segmentos al bucket del destino (misma tubería que imagen) →
// manifiesto con la configuración cruda de la VM (qm config).
//
// Restore in_place: qm stop → reescribe cada volumen vía qemu-nbd rw desde
// los objetos del destino (verificando SHA-256) → qm start.
// Restore new: qm create con la configuración del manifiesto (cores, RAM,
// BIOS, máquina, red, discos del mismo tamaño en el storage elegido) →
// escribe los discos → deja la VM lista para arrancar.
package jobs

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/juansoler/bdr/packages/contracts"
	pkgimg "github.com/juansoler/bdr/packages/image"
	"github.com/juansoler/bdr/packages/logging"
	"github.com/juansoler/bdr/packages/version"
)

// VMRunner ejecuta backup/restore de VMs de Proxmox (en el nodo PVE).
type VMRunner struct {
	DataDir string
	Log     *logging.Logger
}

// NewVMRunner crea el runner de VMs.
func NewVMRunner(dataDir string, log *logging.Logger) *VMRunner {
	return &VMRunner{DataDir: dataDir, Log: log}
}

// vmDisk es un disco de la VM: clave (scsi0…), volid y su ruta local.
type vmDisk struct {
	Key     string // scsi0, virtio0, efidisk0, tpmstate0…
	Volid   string // local-lvm:vm-105-disk-0
	Path    string // /dev/pve/vm-105-disk-0 (pvesm path)
	SizeG   int64  // tamaño en GiB (del parámetro size=)
	Fmt     string // formato real del volumen (qcow2 | raw), vía qemu-img
	Virtual int64  // tamaño virtual del disco en bytes (qemu-img)
	Total   int64  // bytes reales del volumen (stat tras conectar NBD)
}

// claves de disco en qm config (orden de prioridad para el arranque).
func isDiskKey(k string) bool {
	for _, p := range []string{"virtio", "scsi", "sata", "ide", "efidisk", "tpmstate"} {
		if strings.HasPrefix(k, p) && len(k) > len(p) {
			suffix := k[len(p):]
			if _, err := strconv.Atoi(suffix); err == nil {
				return true
			}
		}
	}
	return false
}

// parseQMConfig convierte la salida de qm config en un mapa clave→valor.
func parseQMConfig(out string) map[string]string {
	cfg := map[string]string{}
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, ": ")
		if !ok {
			continue
		}
		cfg[strings.TrimSpace(k)] = strings.TrimSpace(v)
	}
	return cfg
}

// parseSizeG extrae los GiB de un parámetro size= (64G, 1T, 528K…).
func parseSizeG(sizeParam string) int64 {
	sizeParam = strings.TrimSuffix(sizeParam, "B")
	if sizeParam == "" {
		return 0
	}
	unit := sizeParam[len(sizeParam)-1]
	num, err := strconv.ParseFloat(sizeParam[:len(sizeParam)-1], 64)
	if err != nil {
		return 0
	}
	switch unit {
	case 'T':
		return int64(num * 1024)
	case 'G':
		return int64(num)
	case 'M':
		return 1
	}
	return 0
}

// disksFromConfig resuelve los discos de la VM con su ruta local.
func disksFromConfig(ctx context.Context, cfg map[string]string) ([]vmDisk, error) {
	var keys []string
	for k := range cfg {
		if isDiskKey(k) {
			keys = append(keys, k)
		}
	}
	// Orden estable: tipo y luego índice.
	sortStrings(keys)
	var disks []vmDisk
	for _, k := range keys {
		v := cfg[k]
		fields := strings.Split(v, ",")
		volid := strings.TrimSpace(fields[0])
		if volid == "" || strings.Contains(v, "media=cdrom") {
			continue
		}
		d := vmDisk{Key: k, Volid: volid}
		for _, f := range fields[1:] {
			if s, ok := strings.CutPrefix(f, "size="); ok {
				d.SizeG = parseSizeG(s)
			}
		}
		out, err := runProbe(60, "pvesm", "path", volid)
		if err != nil {
			return nil, fmt.Errorf("pvesm path %s: %v", volid, err)
		}
		d.Path = strings.TrimSpace(out)
		// Formato y tamaño virtual reales del volumen: sin --format,
		// qemu-nbd trataría un qcow2 como raw y exportaría los bytes del
		// fichero, no el disco virtual.
		if info, ierr := runProbe(60, "qemu-img", "info", "--output=json", d.Path); ierr == nil {
			var qi struct {
				Format      string `json:"format"`
				VirtualSize int64  `json:"virtual-size"`
			}
			if json.Unmarshal([]byte(info), &qi) == nil {
				d.Fmt = qi.Format
				d.Virtual = qi.VirtualSize
			}
		}
		disks = append(disks, d)
	}
	return disks, nil
}

func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}

// nbdConnect conecta un volumen a /dev/nbdN con qemu-nbd y devuelve la ruta
// del dispositivo. El módulo nbd se carga automáticamente si falta. format
// fuerza el formato de imagen (qcow2/raw); vacío deja que qemu-nbd sondee.
func nbdConnect(ctx context.Context, volumePath, format string, readOnly bool) (string, func(), error) {
	if _, err := os.Stat("/dev/nbd0"); err != nil {
		// El módulo nbd no está cargado: se carga (el agente corre como
		// root en el nodo PVE).
		_, _ = runProbe(30, "modprobe", "nbd", "max_part=8")
	}
	for n := 0; n < 8; n++ {
		dev := fmt.Sprintf("/dev/nbd%d", n)
		if _, err := os.Stat(dev); err != nil {
			continue
		}
		// ¿Ocupado? qemu-nbd -d falla si está libre; eso está bien.
		args := []string{"qemu-nbd", "--connect=" + dev}
		if format != "" {
			args = append(args, "--format="+format)
		}
		if readOnly {
			args = append(args, "--read-only")
		}
		args = append(args, volumePath)
		if _, err := runProbe(30, args[0], args[1:]...); err != nil {
			continue // probablemente en uso: prueba el siguiente
		}
		cleanup := func() {
			_, _ = runProbe(30, "qemu-nbd", "--disconnect", dev)
		}
		return dev, cleanup, nil
	}
	return "", nil, fmt.Errorf("sin /dev/nbdN libre — ejecuta en el nodo: modprobe nbd max_part=8 (y revisa procesos qemu-nbd activos)")
}

// blockDeviceSize devuelve la capacidad en bytes de un dispositivo de
// bloque: blockdev --getsize64, con fallback a /sys/block/<n>/size × 512.
func blockDeviceSize(dev string) int64 {
	if out, err := runProbe(30, "blockdev", "--getsize64", dev); err == nil {
		if n, perr := strconv.ParseInt(strings.TrimSpace(out), 10, 64); perr == nil {
			return n
		}
	}
	base := filepath.Base(dev)
	if data, err := os.ReadFile("/sys/class/block/" + base + "/size"); err == nil {
		if sectors, perr := strconv.ParseInt(strings.TrimSpace(string(data)), 10, 64); perr == nil {
			return sectors * 512
		}
	}
	return 0
}

// waitNBDSize espera a que el dispositivo NBD exponga al menos min bytes
// (qemu-nbd --connect retorna antes de que el kernel actualice el tamaño).
func waitNBDSize(ctx context.Context, dev string, min int64) int64 {
	for i := 0; i < 30; i++ {
		s := blockDeviceSize(dev)
		if s >= min && min > 0 {
			return s
		}
		if s > 0 && min <= 0 {
			return s
		}
		select {
		case <-ctx.Done():
			return s
		case <-time.After(500 * time.Millisecond):
		}
	}
	return blockDeviceSize(dev)
}

// BackupVM captura los discos de la VM al bucket del destino.
func (r *VMRunner) BackupVM(ctx context.Context, creds *contracts.RepositoryCredentials,
	payload contracts.BackupVMJobPayload, progress func(percent int, msg string, bytesDone int64)) (json.RawMessage, error) {

	cfgOut, err := runProbe(60, "qm", "config", strconv.Itoa(payload.VMID))
	if err != nil {
		return nil, &CodeError{Code: "E_VM_CONFIG", Message: err.Error()}
	}
	cfg := parseQMConfig(cfgOut)
	name := payload.Name
	if name == "" {
		name = cfg["name"]
	}
	// Los objetos se agrupan como
	// backup/<nodo-proxmox>/<vm>/<fecha>/vm/<segmentos y manifiesto>.
	guest := name
	if guest == "" {
		guest = fmt.Sprintf("vm%d", payload.VMID)
	}
	machine := guestMachine(payload.Node, guest)
	date := time.Now().UTC().Format(dateFormat)
	dest, err := newUploadDest(ctx, creds, "vm", machine, date, payload.RetentionDays)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 24*time.Hour)
	defer cancel()
	start := time.Now()

	disks, err := disksFromConfig(ctx, cfg)
	if err != nil {
		return nil, &CodeError{Code: "E_VM_DISKS", Message: err.Error()}
	}
	if len(disks) == 0 {
		return nil, &CodeError{Code: "E_VM_DISKS", Message: "la VM no tiene discos capturables"}
	}

	manifest := &pkgimg.Manifest{
		SchemaVersion:            1,
		ManifestID:               fmt.Sprintf("vm-%d-%d", payload.VMID, start.UnixMilli()),
		RunID:                    payload.InstanceID,
		Kind:                     "vm",
		ImageMode:                pkgimg.ModeRawDisk,
		Consistency:              "crash_consistent",
		CapturedAt:               start.UTC().Format(time.RFC3339),
		UnallocatedBytesCaptured: true,
		Tooling:                  pkgimg.Tooling{AgentVersion: version.Version},
		VM:                       &pkgimg.VM{VMID: payload.VMID, Name: name, Node: payload.Node, Config: cfg},
	}

	result := contracts.ImageJobResult{Disks: []contracts.ImageDiskResult{}}
	bytesDone := int64(0)
	var totalAll int64
	for _, d := range disks {
		if fi, err := os.Stat(d.Path); err == nil {
			d.Total = fi.Size()
			totalAll += fi.Size()
		}
	}
	prog := func(done, total int64, msg string) {
		pct := 0
		if totalAll > 0 {
			pct = int((done * 90) / totalAll)
		}
		progress(pct, msg, done)
	}

	for i, d := range disks {
		if err := ctx.Err(); err != nil {
			return nil, &CodeError{Code: "E_CANCELED", Message: "job cancelado"}
		}
		progress((i*90)/len(disks), fmt.Sprintf("conectando %s (%s)", d.Volid, d.Key), bytesDone)
		dev, cleanup, err := nbdConnect(ctx, d.Path, d.Fmt, true)
		if err != nil {
			return nil, &CodeError{Code: "E_VM_NBD", Message: fmt.Sprintf("%s: %v", d.Volid, err)}
		}
		disk := pkgimg.Disk{Index: i, DevicePath: dev, Table: d.Volid}
		// Tamaño virtual del disco (qemu-img): es el tamaño del disco que ve
		// el huésped; blockdev sobre el NBD con qcow2 como raw devolvería el
		// tamaño del fichero, no el del disco.
		disk.TotalBytes = d.Virtual
		if disk.TotalBytes <= 0 {
			disk.TotalBytes = d.SizeG << 20
		}
		// Espera activa: el kernel puede tardar en exponer el tamaño del
		// NBD tras el connect; una lectura prematura daría streams vacíos.
		if got := waitNBDSize(ctx, dev, disk.TotalBytes); got > 0 {
			disk.TotalBytes = got
		}
		if disk.TotalBytes <= 0 {
			cleanup()
			return nil, &CodeError{Code: "E_VM_NBD",
				Message: fmt.Sprintf("no se pudo determinar el tamaño de %s (%s)", d.Volid, dev)}
		}
		src, err := os.Open(dev)
		if err != nil {
			cleanup()
			return nil, &CodeError{Code: "E_IMAGE_OPEN", Message: fmt.Sprintf("%s: %v", dev, err)}
		}
		r.Log.Info("capturando volumen", "volid", d.Volid, "nbd", dev, "bytes", disk.TotalBytes)
		stats, cerr := captureSource(ctx, dest, src, disk, manifest, prog, &bytesDone)
		_ = src.Close()
		cleanup()
		if cerr != nil {
			return nil, cerr
		}
		result.Disks = append(result.Disks, contracts.ImageDiskResult{
			Index: i, Device: d.Volid, TotalBytes: disk.TotalBytes, Segments: stats.Streams,
		})
		result.BytesTotal += disk.TotalBytes
		result.BytesUploaded += stats.BytesUploaded
	}

	out, err := finishCapture(ctx, dest, manifest, "vm", progress, bytesDone)
	if err != nil {
		return nil, err
	}
	result.ManifestID = manifest.ManifestID
	result.ManifestKey = out.manifestKey
	result.DurationMS = time.Since(start).Milliseconds()
	r.Log.Info("captura de VM completa",
		"leidos", humanBytes(result.BytesTotal), "subidos", humanBytes(result.BytesUploaded))

	// Punto de restauración: el resultado viaja en el job; el servidor lo
	// registra como restore point kind=vm.
	return json.Marshal(map[string]any{
		"manifest_id":       result.ManifestID,
		"manifest_key":      result.ManifestKey,
		"vmid":              payload.VMID,
		"name":              name,
		"disks":             result.Disks,
		"bytes_total":       result.BytesTotal,
		"bytes_uploaded":    result.BytesUploaded,
		"duration_ms":       result.DurationMS,
		"kopia_snapshot_id": result.KopiaSnapshotID,
	})
}

// RestoreVM repone los discos de una VM desde el manifiesto del destino.
func (r *VMRunner) RestoreVM(ctx context.Context, creds *contracts.RepositoryCredentials,
	payload contracts.RestoreVMJobPayload, progress func(percent int, msg string, bytesDone int64)) (json.RawMessage, error) {

	// Restauración: lee por clave completa (payload.ManifestKey), que ya
	// incluye máquina/fecha en los puntos creados con la estructura nueva.
	dest, err := newUploadDest(ctx, creds, "vm", "", "", 0)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 24*time.Hour)
	defer cancel()

	mb, err := dest.getManifest(ctx, payload.ManifestKey)
	if err != nil {
		return nil, &CodeError{Code: "E_VALIDATION", Message: err.Error()}
	}
	var manifest pkgimg.Manifest
	if err := json.Unmarshal(mb, &manifest); err != nil {
		return nil, &CodeError{Code: "E_VALIDATION", Message: "manifiesto inválido: " + err.Error()}
	}
	if manifest.VM == nil {
		return nil, &CodeError{Code: "E_VALIDATION", Message: "el manifiesto no es de una VM de Proxmox"}
	}
	if len(manifest.Streams) == 0 {
		return nil, &CodeError{Code: "E_VALIDATION",
			Message: "el manifiesto no tiene segmentos (captura vacía); usa un punto de restauración posterior"}
	}

	targetVMID := payload.TargetVMID
	targetCfg := manifest.VM.Config
	if payload.Mode != "new" && payload.Mode != "in_place" {
		payload.Mode = "in_place"
	}

	// Si la restauración como instancia nueva falla a mitad, la VM creada
	// es basura: se destruye automáticamente (con --purge).
	createdVM := false
	destroyIfNew := func(reason string) {
		if !createdVM {
			return
		}
		if _, derr := runProbe(60, "qm", "destroy", strconv.Itoa(targetVMID), "--purge"); derr == nil {
			r.Log.Info("VM creada destruida tras fallo de restauración", "vmid", targetVMID, "motivo", reason)
		} else {
			r.Log.Warn("no se pudo destruir la VM tras el fallo; elimínala manualmente", "vmid", targetVMID)
		}
	}

	if payload.Mode == "new" {
		// Nueva VM con la configuración del manifiesto.
		if targetVMID <= 0 {
			targetVMID, err = nextFreeVMID()
			if err != nil {
				return nil, &CodeError{Code: "E_VM_CREATE", Message: err.Error()}
			}
		}
		// Pre-vuelo: espacio libre en el storage destino (los discos que se
		// crearán suman los tamaños virtuales del manifiesto).
		required := int64(0)
		for _, d := range manifest.Disks {
			required += d.TotalBytes
		}
		if required > 0 {
			if free, ferr := storageFreeBytes(payload.TargetStorage); ferr == nil && free >= 0 && free < required {
				return nil, &CodeError{Code: "E_VM_CREATE",
					Message: fmt.Sprintf("espacio insuficiente en el storage %s: libres %s, se requieren %s (VM %d + discos)",
						payload.TargetStorage, humanBytes(free), humanBytes(required), targetVMID)}
			}
		}
		name := payload.TargetName
		if name == "" {
			name = manifest.VM.Name
		}
		// Separar specs de disco de la configuración general: los discos se
		// adjuntan uno a uno con qm set después de crear la VM, de modo que
		// un fallo identifique el disco exacto (y el job registre el
		// comando completo).
		var diskArgs []string
		args := []string{"create", strconv.Itoa(targetVMID), "--name=" + name}
		for k, v := range targetCfg {
			switch {
			case k == "name":
				// ya está en create
			case k == "vmid" || k == "digest":
				// ignorar
			case isDiskKey(k):
				if strings.Contains(v, "media=cdrom") || strings.HasPrefix(v, "none") {
					continue // CD-ROM: se adjunta después si hace falta
				}
				storage := payload.TargetStorage
				if storage == "" {
					storage = strings.Split(v, ":")[0]
				}
				params := diskParams(v)
				var spec string
				switch {
				case strings.HasPrefix(k, "efidisk"):
					spec = storage + ":1"
					if et, ok := params["efitype"]; ok {
						spec += ",efitype=" + et
					}
					if pk, ok := params["pre-enrolled-keys"]; ok {
						spec += ",pre-enrolled-keys=" + pk
					}
				case strings.HasPrefix(k, "tpmstate"):
					spec = storage + ":1"
					if ver, ok := params["version"]; ok {
						spec += ",version=" + ver
					}
				default:
					sizeG := sizeToG(params["size"])
					if sizeG == "" {
						sizeG = "1"
					}
					spec = storage + ":" + sizeG
					// Conserva los parámetros del disco original (ssd,
					// discard, iothread, cache…) excepto el tamaño.
					var extra []string
					for pk, pv := range params {
						if pk == "size" {
							continue
						}
						extra = append(extra, pk+"="+pv)
					}
					sortStrings(extra)
					if len(extra) > 0 {
						spec += "," + strings.Join(extra, ",")
					}
				}
				diskArgs = append(diskArgs, "--"+k+"="+spec)
			case strings.HasPrefix(k, "net") || k == "scsihw" || k == "bios" ||
				k == "machine" || k == "ostype" || k == "cores" || k == "memory" ||
				k == "sockets" || k == "cpu" || k == "cpuunits" || k == "vga" ||
				k == "serial" || k == "onboot" || k == "agent" || k == "boot":
				args = append(args, "--"+k+"="+v)
			}
		}
		sortStrings(diskArgs)
		r.Log.Info("qm create", "args", strings.Join(args, " "))
		progress(1, "qm create "+strings.Join(args, " "), 0)
		if _, err := runProbe(120, "qm", args...); err != nil {
			return nil, &CodeError{Code: "E_VM_CREATE", Message: err.Error()}
		}
		createdVM = true
		// Adjuntar cada disco con qm set (comando en el log del job).
		for i, da := range diskArgs {
			msg := fmt.Sprintf("qm set %d %s", targetVMID, da)
			progress(2+i, msg, 0)
			r.Log.Info("qm set", "args", msg)
			if _, err := runProbe(120, "qm", "set", strconv.Itoa(targetVMID), da); err != nil {
				destroyIfNew(fmt.Sprintf("fallo adjuntando %s: %v", da, err))
				return nil, &CodeError{Code: "E_VM_CREATE",
					Message: fmt.Sprintf("%s: %v", da, err)}
			}
		}
		// La config de la VM nueva (pvesm genera volids distintos).
		cfgOut, cerr := runProbe(60, "qm", "config", strconv.Itoa(targetVMID))
		if cerr != nil {
			destroyIfNew("no se pudo leer la config de la VM creada")
			return nil, &CodeError{Code: "E_VM_CONFIG", Message: cerr.Error()}
		}
		targetCfg = parseQMConfig(cfgOut)
	} else {
		// in_place: detener la VM para escribir los volúmenes con seguridad.
		_, _ = runProbe(300, "qm", "stop", strconv.Itoa(targetVMID))
		time.Sleep(2 * time.Second)
	}

	// Fase común de restauración: reescribir cada disco y arrancar. En
	// modo new, cualquier fallo destruye la VM creada (cleanup).
	writeAndFinish := func() (json.RawMessage, error) {
		// Mapear discos del manifiesto a volúmenes del VM destino (por clave).
		cfgOut, err := runProbe(60, "qm", "config", strconv.Itoa(targetVMID))
		if err != nil {
			return nil, &CodeError{Code: "E_VM_CONFIG", Message: err.Error()}
		}
		targetVolumes := parseQMConfig(cfgOut)

		done := 0
		var bytesWritten int64
		totalStreams := len(manifest.Streams)
		for _, d := range manifest.Disks {
			key := fmt.Sprintf("disk%d", d.Index)
			// La clave original del disco vive en la config de la VM origen.
			origKey := manifestKeyForDisk(manifest, d.Index)
			if origKey == "" {
				origKey = key
			}
			volid := targetVolumes[origKey]
			if volid == "" {
				r.Log.Warn("sin volumen destino para el disco; se omite", "disk", origKey)
				continue
			}
			fields := strings.Split(volid, ",")
			volid = strings.TrimSpace(fields[0])
			vpath, err := runProbe(60, "pvesm", "path", volid)
			if err != nil {
				return nil, &CodeError{Code: "E_VM_DISKS", Message: fmt.Sprintf("%s: %v", volid, err)}
			}
			vpath = strings.TrimSpace(vpath)

			progress(10+done*80/maxInt(len(manifest.Disks), 1),
				fmt.Sprintf("reescribiendo %s (%s)", volid, origKey), bytesWritten)
			format := ""
			if info, ierr := runProbe(60, "qemu-img", "info", "--output=json", vpath); ierr == nil {
				var qi struct {
					Format string `json:"format"`
				}
				if json.Unmarshal([]byte(info), &qi) == nil {
					format = qi.Format
				}
			}
			dev, cleanup, nerr := nbdConnect(ctx, vpath, format, false)
			if nerr != nil {
				return nil, &CodeError{Code: "E_VM_NBD", Message: fmt.Sprintf("%s: %v", volid, nerr)}
			}
			// Espera al tamaño esperado del disco según el manifiesto.
			expected := int64(0)
			for _, s := range manifest.Streams {
				if s.Disk == d.Index && s.Offset+s.Bytes > expected {
					expected = s.Offset + s.Bytes
				}
			}
			devSize := waitNBDSize(ctx, dev, expected)
			if devSize <= 0 {
				devSize = expected
			}
			dst, oerr := os.OpenFile(dev, os.O_WRONLY, 0)
			if oerr != nil {
				cleanup()
				return nil, &CodeError{Code: "E_VM_NBD", Message: fmt.Sprintf("%s: %v", dev, oerr)}
			}
			werr := writeStreams(ctx, dest, manifest, d.Index, dst, devSize, func(n int) {
				done++
				progress(10+(done*80)/maxInt(totalStreams, 1),
					fmt.Sprintf("segmento %d/%d", done, totalStreams), bytesWritten)
			}, func(warn string) {
				progress(10, warn, bytesWritten)
				r.Log.Warn(warn)
			})
			_ = dst.Close()
			_ = dst.Sync()
			cleanup()
			if werr != nil {
				return nil, &CodeError{Code: "E_IMAGE_UPLOAD", Message: werr.Error()}
			}
		}

		if payload.Mode == "new" && (payload.TargetName != "" || manifest.VM.Config["onboot"] == "1") {
			_, _ = runProbe(30, "qm", "set", strconv.Itoa(targetVMID), "--onboot=1")
		}
		// Arranca la VM restaurada (en new porque es una máquina nueva; en
		// in_place porque la detuvimos para escribir).
		_, _ = runProbe(300, "qm", "start", strconv.Itoa(targetVMID))

		return json.Marshal(map[string]any{
			"vmid":        targetVMID,
			"mode":        payload.Mode,
			"segments":    totalStreams,
			"bytes":       bytesWritten,
			"manifest_id": manifest.ManifestID,
		})
	}

	out, ferr := writeAndFinish()
	if ferr != nil {
		destroyIfNew(ferr.Error())
		return nil, ferr
	}
	return out, nil
}

// writeStreams escribe en el destino los segmentos del disco indicado,
// verificando el SHA-256 de cada uno. Las escrituras se recortan al tamaño
// real del dispositivo (un efidisk/tpmstate nuevo puede diferir en unos KB
// del original; los ceros que no caben se omiten con aviso).
func writeStreams(ctx context.Context, dest uploadDest, manifest pkgimg.Manifest, diskIdx int,
	dst io.WriterAt, devSize int64, onSeg func(n int), warn func(string)) error {

	for _, s := range manifest.Streams {
		if s.Disk != diskIdx {
			continue
		}
		// Recorte al dispositivo: lo que quede fuera de su tamaño no se
		// escribe (solo tiene sentido si el volumen nuevo es algo menor).
		if s.Offset >= devSize {
			warn(fmt.Sprintf("segmento %s fuera del dispositivo destino (%d ≥ %d); omitido", s.Name, s.Offset, devSize))
			continue
		}
		writeLen := s.Bytes
		if s.Offset+writeLen > devSize {
			writeLen = devSize - s.Offset
			warn(fmt.Sprintf("segmento %s recortado de %d a %d bytes (tamaño del volumen destino)", s.Name, s.Bytes, writeLen))
		}
		if s.Zero {
			zero := make([]byte, writeLen)
			if _, err := dst.WriteAt(zero, s.Offset); err != nil {
				if strings.Contains(strings.ToLower(err.Error()), "no space left") {
					return fmt.Errorf("segmento %s: escribir ceros: sin espacio — verifica la cuota ZFS del dataset (zfs get refquota <dataset>) o el espacio del storage del volumen", s.Name)
				}
				return fmt.Errorf("segmento %s: escribir ceros: %w", s.Name, err)
			}
			onSeg(1)
			continue
		}
		key := s.Blob
		if key == "" {
			return fmt.Errorf("segmento %s sin blob en el manifiesto", s.Name)
		}
		obj, err := dest.openObject(ctx, key)
		if err != nil {
			return fmt.Errorf("segmento %s: descargar: %w", s.Name, err)
		}
		h := sha256.New()
		n, cerr := io.Copy(io.MultiWriter(newWriterAt(dst, s.Offset), h), io.LimitReader(obj, writeLen))
		_ = obj.Close()
		if cerr != nil {
			if strings.Contains(strings.ToLower(cerr.Error()), "no space left") {
				return fmt.Errorf("segmento %s: escribir: sin espacio — verifica la cuota ZFS del dataset (zfs get refquota <dataset>) o el espacio del storage del volumen", s.Name)
			}
			return fmt.Errorf("segmento %s: escribir: %w", s.Name, cerr)
		}
		if n != writeLen {
			return fmt.Errorf("segmento %s: %d bytes, esperaba %d", s.Name, n, writeLen)
		}
		// La verificación SHA-256 solo aplica al segmento completo; si se
		// recortó, el contenido ya estaba verificado en la captura.
		if writeLen == s.Bytes {
			if got := hex.EncodeToString(h.Sum(nil)); !strings.EqualFold(got, s.SHA256) {
				return fmt.Errorf("segmento %s: SHA-256 no coincide (imagen incompleta)", s.Name)
			}
		}
		onSeg(1)
	}
	// Sincroniza el dispositivo.
	if f, ok := dst.(interface{ Sync() error }); ok {
		_ = f.Sync()
	}
	return nil
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// diskParams separa el volid y los parámetros de un valor de disco qm
// ("DATA1:vm-105-disk-1,size=64G,ssd=1" → volid + {"size":"64G","ssd":"1"}).
func diskParams(v string) map[string]string {
	params := map[string]string{}
	fields := strings.Split(v, ",")
	for _, f := range fields[1:] {
		f = strings.TrimSpace(f)
		if k, val, ok := strings.Cut(f, "="); ok {
			params[strings.TrimSpace(k)] = strings.TrimSpace(val)
		}
	}
	return params
}

// sizeToG convierte un parámetro size (64G, 1T, 512M, 528K) a GiB entero
// (redondeando hacia arriba los sub-GiB); vacío si no es interpretable.
func sizeToG(s string) string {
	s = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(s), "B"))
	if s == "" {
		return ""
	}
	unit := s[len(s)-1]
	numStr := s[:len(s)-1]
	num, err := strconv.ParseFloat(numStr, 64)
	if err != nil {
		return ""
	}
	var g float64
	switch unit {
	case 'T':
		g = num * 1024
	case 'G':
		g = num
	case 'M':
		g = num / 1024
	case 'K':
		g = num / (1024 * 1024)
	default:
		// Sin unidad: se asume GiB.
		if f, perr := strconv.ParseFloat(s, 64); perr == nil {
			g = f
		} else {
			return ""
		}
	}
	if g < 1 {
		g = 1
	}
	return strconv.Itoa(int(g + 0.999999))
}

// nextFreeVMID elige el siguiente VMID libre. Orden de preferencia:
// pvesh get /cluster/nextid (canónico) → qm nextid → máximo de qm list + 1.
func nextFreeVMID() (int, error) {
	if out, err := runProbe(30, "pvesh", "get", "/cluster/nextid"); err == nil {
		if id, perr := strconv.Atoi(strings.TrimSpace(strings.Trim(strings.TrimSpace(out), `"`))); perr == nil && id > 0 {
			return id, nil
		}
	}
	if out, err := runProbe(30, "qm", "nextid"); err == nil {
		if id, perr := strconv.Atoi(strings.TrimSpace(out)); perr == nil && id > 0 {
			return id, nil
		}
	}
	if out, err := runProbe(30, "qm", "list"); err == nil {
		max := 100
		for i, line := range strings.Split(out, "\n") {
			if i == 0 {
				continue // cabecera
			}
			fields := strings.Fields(line)
			if len(fields) > 0 {
				if id, perr := strconv.Atoi(fields[0]); perr == nil && id >= max {
					max = id + 1
				}
			}
		}
		return max, nil
	}
	return 0, fmt.Errorf("no se pudo calcular un VMID libre (pvesh/qm)")
}

// manifestKeyForDisk encuentra la clave qm (scsi0…) del disco N en el
// manifiesto: los discos se capturan en el orden de keys ordenadas de la
// config origen, así que reconstruimos ese orden.
func manifestKeyForDisk(manifest pkgimg.Manifest, diskIdx int) string {
	var keys []string
	for k := range manifest.VM.Config {
		if isDiskKey(k) {
			keys = append(keys, k)
		}
	}
	sortStrings(keys)
	// Los CD-ROM no se capturan; contamos solo discos reales en el mismo orden.
	real := 0
	for _, k := range keys {
		v := manifest.VM.Config[k]
		if strings.Contains(v, "media=cdrom") {
			continue
		}
		if real == diskIdx {
			return k
		}
		real++
	}
	return ""
}

// storageFreeBytes devuelve los bytes libres en un storage PVE vía
// `pvesm status <storage>`. Error si no se pudo determinar.
func storageFreeBytes(storage string) (int64, error) {
	// Preferimos JSON; si el flag no está soportado, caemos al texto.
	if out, err := runProbe(30, "pvesm", "status", "--output-format", "json", storage); err == nil {
		var rows []struct {
			Storage string `json:"storage"`
			Avail   int64  `json:"avail"`
		}
		if json.Unmarshal([]byte(out), &rows) == nil {
			for _, r := range rows {
				if r.Storage == storage {
					return r.Avail, nil
				}
			}
		}
	}
	out, err := runProbe(30, "pvesm", "status", storage)
	if err != nil {
		return -1, err
	}
	for _, line := range strings.Split(out, "\n") {
		f := strings.Fields(strings.TrimSpace(line))
		if len(f) >= 6 && strings.EqualFold(f[0], storage) {
			if v, perr := parseSizeBytes(f[5]); perr == nil {
				return v, nil
			}
		}
	}
	return -1, fmt.Errorf("storage no encontrado en pvesm status")
}

// parseSizeBytes interpreta tamaños con sufijo (32G, 1.2T, 528K) en bytes.
func parseSizeBytes(s string) (int64, error) {
	s = strings.TrimSpace(s)
	mult := int64(1)
	if len(s) > 0 {
		switch s[len(s)-1] {
		case 'K':
			mult = 1 << 10
			s = s[:len(s)-1]
		case 'M':
			mult = 1 << 20
			s = s[:len(s)-1]
		case 'G':
			mult = 1 << 30
			s = s[:len(s)-1]
		case 'T':
			mult = 1 << 40
			s = s[:len(s)-1]
		}
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, err
	}
	return int64(f * float64(mult)), nil
}

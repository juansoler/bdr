// Runner de contenedores LXC de Proxmox (BACKUP_LXC / RESTORE_LXC). Ejecuta
// en el propio nodo PVE con privilegios de root.
//
// Backup: pct config → vzdump --stdout (snapshot; fallback stop si el
// storage no soporta snapshots) comprimido con zstd en vuelo y subido por
// segmentos al destino (misma tubería que imagen/VM: SHA-256) → manifiesto
// con la configuración cruda (pct config). Nada se escribe en el disco del
// nodo PVE: el reintento regenera el archivo desde cero (un stream tar no
// es reproducible byte a byte, así que una subida a medias no es
// reanudable).
//
// Restore: descarga los segmentos del destino verificando SHA-256, los
// descomprime en un archivo temporal y repone el contenedor con `pct
// restore` (in_place: stop → destroy → restore → start; new: pct restore
// como CT nuevo con la config del manifiesto).
package jobs

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/juansoler/bdr/packages/contracts"
	pkgimg "github.com/juansoler/bdr/packages/image"
	"github.com/juansoler/bdr/packages/version"

	"github.com/klauspost/compress/zstd"
)

// BackupLXC archiva el contenedor al destino de almacenamiento.
// attachCleanupPrefix adjunta el prefijo del destino a un error de backup
// para que el panel ofrezca limpiar los objetos incompletos.
func attachCleanupPrefix(err error, dest uploadDest) {
	if ce, ok := err.(*CodeError); ok && ce != nil && ce.CleanupPrefix == "" && dest != nil {
		ce.CleanupPrefix = dest.Prefix()
	}
}

func (r *VMRunner) BackupLXC(ctx context.Context, creds *contracts.RepositoryCredentials,
	payload contracts.BackupLXCJobPayload, progress func(percent int, msg string, bytesDone int64)) (res json.RawMessage, err error) {

	cfgOut, err := runProbe(60, "pct", "config", strconv.Itoa(payload.VMID))
	if err != nil {
		return nil, &CodeError{Code: "E_LXC_CONFIG", Message: err.Error()}
	}
	cfg := parseQMConfig(cfgOut)
	name := payload.Name
	if name == "" {
		name = cfg["hostname"]
	}
	// Los objetos se agrupan como
	// backup/<nodo-proxmox>/<ct>/<fecha>/lxc/<segmentos y manifiesto>.
	guest := name
	if guest == "" {
		guest = fmt.Sprintf("ct%d", payload.VMID)
	}
	machine := guestMachine(payload.Node, guest)
	date := time.Now().UTC().Format(dateFormat)
	dest, err := newUploadDest(ctx, creds, "lxc", machine, date, payload.RetentionDays)
	if err != nil {
		return nil, err
	}
	defer func() { attachCleanupPrefix(err, dest) }()
	ctx, cancel := context.WithTimeout(ctx, 24*time.Hour)
	defer cancel()
	start := time.Now()

	estimated := lxcEstimatedBytes(cfg)

	// Subida directa: vzdump --stdout → zstd en vuelo → segmentos al
	// destino. No se escribe nada en el disco del nodo: el staging local
	// (0.16.33–0.16.38) ocupaba en el PVE el tamaño comprimido del CT y un
	// intento fallido lo dejaba completo hasta el reintento o 48 h. Los
	// restos de esa época se depuran al empezar cada backup.
	r.removeLegacyStaging()

	manifest := &pkgimg.Manifest{
		SchemaVersion: 1,
		ManifestID:    fmt.Sprintf("lxc-%d-%d", payload.VMID, start.UnixMilli()),
		RunID:         payload.InstanceID,
		Kind:          "lxc",
		Consistency:   "filesystem_consistent",
		CapturedAt:    start.UTC().Format(time.RFC3339),
		Tooling:       pkgimg.Tooling{AgentVersion: version.Version},
		LXC: &pkgimg.LXC{
			VMID: payload.VMID, Name: name, Node: payload.Node,
			Unprivileged: cfg["unprivileged"] == "1",
			Config:       cfg,
		},
		Disks: []pkgimg.Disk{{Index: 0, TotalBytes: estimated}},
	}

	// vzdump a stdout: snapshot si el storage lo soporta; si falla sin
	// emitir datos, reintenta con stop (parada breve del contenedor).
	var mode string
	var segCount int
	var bytesDone int64
	var xerr error
	var lastErr error
	for _, m := range []string{"snapshot", "stop"} {
		manifest.Streams = nil // cada intento construye su propio manifiesto
		cmd, stdout, stderr, err := startVzdump(ctx, payload.VMID, m)
		if err != nil {
			lastErr = &CodeError{Code: "E_LXC_BACKUP", Message: err.Error()}
			continue // fallo al arrancar snapshot → prueba con stop
		}
		r.Log.Info("vzdump iniciado", "vmid", payload.VMID, "mode", m)
		segCount, bytesDone, xerr = streamArchive(ctx, dest, manifest, stdout, estimated, progress)
		if xerr != nil {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
			return nil, xerr
		}
		waitErr := cmd.Wait()
		if waitErr == nil {
			mode = m
			break
		}
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = waitErr.Error()
		}
		lastErr = &CodeError{Code: "E_LXC_BACKUP", Message: "vzdump: " + firstLines(msg, 2)}
		if ctx.Err() != nil {
			return nil, &CodeError{Code: "E_CANCELED", Message: "job cancelado"}
		}
		// El fallback a stop solo tiene sentido si snapshot no llegó a
		// emitir datos (el storage no soporta snapshots): sin segmentos
		// subidos no hubo datos; a mitad de archivo el error es real y no
		// se re-intenta parando el CT.
		if segCount > 0 || m == "stop" {
			break
		}
	}
	if mode == "" {
		if lastErr == nil {
			lastErr = &CodeError{Code: "E_LXC_BACKUP", Message: "vzdump: no se pudo generar el archivo"}
		}
		return nil, lastErr
	}

	out, err := finishCapture(ctx, dest, manifest, "lxc", progress, bytesDone)
	if err != nil {
		return nil, err
	}
	progress(100, fmt.Sprintf("archivo completo en el destino (%s)", out.manifestKey), bytesDone)
	return json.Marshal(map[string]any{
		"manifest_id":  manifest.ManifestID,
		"manifest_key": out.manifestKey,
		"vmid":         payload.VMID,
		"name":         name,
		"mode":         mode,
		"segments":     segCount,
		"bytes_total":  bytesDone,
		"duration_ms":  time.Since(start).Milliseconds(),
	})
}

// removeLegacyStaging elimina el directorio de staging de los agentes
// 0.16.33–0.16.38 (DataDir/lxc-staging): la subida vuelve a ser directa
// al destino y esos restos (archivos completos de intentos fallidos) solo
// ocuparían disco en el nodo PVE. Se invoca al empezar cada backup; sin
// efecto si no hay DataDir.
func (r *VMRunner) removeLegacyStaging() {
	if r.DataDir == "" {
		return
	}
	_ = os.RemoveAll(filepath.Join(r.DataDir, "lxc-staging"))
}

// startVzdump lanza `vzdump <vmid> --stdout --mode <mode>` y devuelve el
// comando (para Wait) y su stdout en streaming.
func startVzdump(ctx context.Context, vmid int, mode string) (*exec.Cmd, io.ReadCloser, *bytes.Buffer, error) {
	cmd := exec.CommandContext(ctx, "vzdump", strconv.Itoa(vmid), "--stdout", "--mode", mode)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, nil, nil, err
	}
	stderr := &bytes.Buffer{}
	cmd.Stderr = stderr
	if err := cmd.Start(); err != nil {
		return nil, nil, nil, err
	}
	return cmd, stdout, stderr, nil
}

// streamArchive lee el archivo de vzdump, lo comprime con zstd y lo sube por
// segmentos (reanudación incremental), registrando los streams del
// manifiesto. Devuelve (segmentos, bytes subidos, error).
func streamArchive(ctx context.Context, dest uploadDest, manifest *pkgimg.Manifest,
	src io.Reader, estimated int64, progress func(percent int, msg string, bytesDone int64)) (int, int64, error) {

	now := time.Now().UTC()
	vmID := 0
	if manifest.LXC != nil {
		vmID = manifest.LXC.VMID
	}
	buf := make([]byte, imageSegmentSize)
	var offset int64
	var bytesDone int64
	seg := 0

	// Compresión en vuelo: vzdump --stdout emite tar sin comprimir; el
	// agente comprime con zstd mientras segmenta (menos datos en el destino).
	pr, pw := io.Pipe()
	go func() {
		zw, zerr := zstd.NewWriter(pw, zstd.WithEncoderConcurrency(1))
		if zerr != nil {
			_ = pw.CloseWithError(zerr)
			return
		}
		if _, err := io.Copy(zw, src); err != nil {
			_ = zw.Close()
			_ = pw.CloseWithError(err)
			return
		}
		// Close vuelca el epílogo del frame zstd; después cierra el pipe (EOF).
		if err := zw.Close(); err != nil {
			_ = pw.CloseWithError(err)
			return
		}
		_ = pw.Close()
	}()

	for {
		if err := ctx.Err(); err != nil {
			_ = pr.Close()
			return seg, bytesDone, &CodeError{Code: "E_CANCELED", Message: "job cancelado"}
		}
		n, rerr := io.ReadFull(pr, buf)
		if n == 0 && rerr == io.EOF {
			break
		}
		if rerr != nil && rerr != io.EOF && rerr != io.ErrUnexpectedEOF {
			_ = pr.Close()
			return seg, bytesDone, &CodeError{Code: "E_LXC_BACKUP", Message: "leer vzdump: " + rerr.Error()}
		}
		chunk := buf[:n]
		sum := sha256Hex(chunk)
		name := fmt.Sprintf("ct%d-seg%04d", vmID, seg)
		key, err := dest.putSegment(ctx, dest.Prefix()+name+"-"+sum, chunk, now)
		if err != nil {
			_ = pr.Close()
			return seg, bytesDone, &CodeError{Code: "E_IMAGE_UPLOAD", Message: "subir segmento: " + err.Error()}
		}
		manifest.Streams = append(manifest.Streams, pkgimg.Stream{
			Disk: 0, Segment: seg, Name: name,
			Offset: offset, Bytes: int64(n), SHA256: sum,
			State: pkgimg.SegmentCompleted, Blob: key,
		})
		seg++
		offset += int64(n)
		bytesDone += int64(n)
		pct := 0
		if estimated > 0 {
			// Los bytes comprimidos son menores que el origen: el porcentaje
			// se calcula contra el origen estimado y se acota al 95 %.
			pct = int((bytesDone * 190) / estimated) // ratio ~2x de compresión
			if pct > 95 {
				pct = 95
			}
		}
		progress(pct, fmt.Sprintf("segmento %d (%s)", seg, humanBytes(int64(n))), bytesDone)
		if rerr == io.EOF || rerr == io.ErrUnexpectedEOF {
			break
		}
	}
	_ = pr.Close()
	return seg, bytesDone, nil
}

// lxcEstimatedBytes suma los tamaños declarados de rootfs y mount points
// (mpX) del pct config; sirve solo para el porcentaje de progreso.
func lxcEstimatedBytes(cfg map[string]string) int64 {
	total := int64(0)
	for k, v := range cfg {
		isVol := k == "rootfs" || (strings.HasPrefix(k, "mp") && len(k) > 2 && isNumeric(k[2:]))
		if !isVol {
			continue
		}
		for _, f := range strings.Split(v, ",") {
			if s, ok := strings.CutPrefix(f, "size="); ok {
				total += parseSizeG(s) * (1 << 30)
			}
		}
	}
	return total
}

func isNumeric(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

// RestoreLXC repone el contenedor desde el manifiesto del destino.
func (r *VMRunner) RestoreLXC(ctx context.Context, creds *contracts.RepositoryCredentials,
	payload contracts.RestoreLXCJobPayload, progress func(percent int, msg string, bytesDone int64)) (json.RawMessage, error) {

	// Restauración: lee por clave completa (payload.ManifestKey).
	dest, err := newUploadDest(ctx, creds, "lxc", "", "", 0)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 24*time.Hour)
	defer cancel()
	start := time.Now()

	mb, err := dest.getManifest(ctx, payload.ManifestKey)
	if err != nil {
		return nil, &CodeError{Code: "E_VALIDATION", Message: err.Error()}
	}
	var manifest pkgimg.Manifest
	if err := json.Unmarshal(mb, &manifest); err != nil {
		return nil, &CodeError{Code: "E_VALIDATION", Message: "manifiesto inválido: " + err.Error()}
	}
	if manifest.LXC == nil {
		return nil, &CodeError{Code: "E_VALIDATION", Message: "el manifiesto no es de un contenedor LXC de Proxmox"}
	}
	if len(manifest.Streams) == 0 {
		return nil, &CodeError{Code: "E_VALIDATION",
			Message: "el manifiesto no tiene segmentos (captura vacía); usa un punto de restauración posterior"}
	}

	mode := payload.Mode
	if mode != "new" && mode != "in_place" {
		mode = "in_place"
	}
	targetVMID := payload.TargetVMID
	if mode == "new" && targetVMID <= 0 {
		targetVMID, err = nextFreeVMID()
		if err != nil {
			return nil, &CodeError{Code: "E_LXC_RESTORE", Message: err.Error()}
		}
	}
	if mode == "in_place" && targetVMID <= 0 {
		targetVMID = manifest.LXC.VMID
	}

	// Storage destino del rootfs: el del payload o el del manifiesto.
	storage := payload.TargetStorage
	if storage == "" {
		storage = lxcRootfsStorage(manifest.LXC.Config)
	}

	// Fichero del dump para `pct restore` (necesita una ruta local). Con
	// DataDir el nombre es determinista por manifiesto y un reintento
	// reanuda la descarga: los segmentos ya verificados se re-comprueban
	// leyendo su rango del fichero local (sin re-descargar). Sin DataDir,
	// temporal aleatorio y limpieza al salir (comportamiento clásico).
	tmpDir := "/var/tmp"
	if r.DataDir != "" {
		tmpDir = filepath.Join(r.DataDir, "tmp")
	}
	if err := os.MkdirAll(tmpDir, 0o700); err != nil {
		return nil, &CodeError{Code: "E_LXC_RESTORE", Message: "no se pudo crear el directorio temporal: " + err.Error()}
	}
	tmpPath := filepath.Join(tmpDir, fmt.Sprintf("bdr-lxc-restore-%s.tar.zst",
		restoreFingerprint(payload.ManifestKey)))
	// Depura dumps de restauraciones abandonadas de más de 48 h (el
	// reintento solo reutiliza los recientes).
	if old, err := filepath.Glob(filepath.Join(tmpDir, "bdr-lxc-restore-*.tar.zst")); err == nil {
		for _, p := range old {
			if p == tmpPath {
				continue
			}
			if fi, ierr := os.Stat(p); ierr == nil && fi.ModTime().Before(time.Now().Add(-48*time.Hour)) {
				_ = os.Remove(p)
			}
		}
	}

	resume := loadRestoreState(r.DataDir, payload.ManifestKey)
	restoreFailed := true
	normalized := "" // dump re-escrito con una sola capa (backups legacy)
	defer func() {
		if restoreFailed {
			// Un reintento continúa desde aquí: conserva dump y estado.
			return
		}
		_ = os.Remove(tmpPath)
		if normalized != "" && normalized != tmpPath {
			_ = os.Remove(normalized)
		}
		resume.Clear()
	}()
	if len(resume.Done) > 0 {
		r.Log.Info("reanudando restauración de LXC", "segmentos_completados", len(resume.Done))
	}

	tmpFile, err := os.OpenFile(tmpPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, &CodeError{Code: "E_LXC_RESTORE", Message: "no se pudo crear el archivo temporal: " + err.Error()}
	}
	defer func() { _ = tmpFile.Close() }()

	// Descarga y verificación: escribe cada segmento en su offset del dump
	// concatenado y verifica el SHA-256 del formato comprimido.
	totalStreams := len(manifest.Streams)
	done := 0
	var bytesDone int64
	for _, s := range manifest.Streams {
		if err := ctx.Err(); err != nil {
			return nil, &CodeError{Code: "E_CANCELED", Message: "job cancelado"}
		}
		if s.Zero || s.Blob == "" {
			continue
		}
		if resume.Has(s.Name) {
			// Re-verifica el rango local del intento anterior.
			h := sha256.New()
			sec := io.NewSectionReader(tmpFile, s.Offset, s.Bytes)
			if nv, cerr := io.Copy(h, sec); cerr == nil && nv == s.Bytes &&
				strings.EqualFold(hex.EncodeToString(h.Sum(nil)), s.SHA256) {
				bytesDone += s.Bytes
				done++
				continue
			}
			// Corrupto o a medias: se descarga de nuevo.
		}
		progress(5+(done*55)/maxInt(totalStreams, 1),
			fmt.Sprintf("descargando %s (%d/%d)", s.Name, done+1, totalStreams), bytesDone)
		obj, err := dest.openObject(ctx, s.Blob)
		if err != nil {
			return nil, &CodeError{Code: "E_IMAGE_UPLOAD", Message: "descargar " + s.Name + ": " + err.Error()}
		}
		h := sha256.New()
		n, cerr := io.Copy(io.MultiWriter(newWriterAt(tmpFile, s.Offset), h), obj)
		_ = obj.Close()
		if cerr != nil {
			return nil, &CodeError{Code: "E_IMAGE_UPLOAD", Message: "descargar " + s.Name + ": " + cerr.Error()}
		}
		if n != s.Bytes {
			return nil, &CodeError{Code: "E_IMAGE_UPLOAD",
				Message: fmt.Sprintf("segmento %s: %d bytes, esperaba %d (archivo incompleto en el destino)", s.Name, n, s.Bytes)}
		}
		if got := hex.EncodeToString(h.Sum(nil)); !strings.EqualFold(got, s.SHA256) {
			return nil, &CodeError{Code: "E_IMAGE_UPLOAD",
				Message: fmt.Sprintf("segmento %s: SHA-256 no coincide (destino corrupto o captura a medias)", s.Name)}
		}
		resume.Mark(s.Name)
		bytesDone += n
		done++
	}
	if err := tmpFile.Sync(); err != nil {
		return nil, &CodeError{Code: "E_LXC_RESTORE", Message: "no se pudo cerrar el archivo temporal: " + err.Error()}
	}
	_ = tmpFile.Close()

	// pct restore solo descomprime una capa: si el dump llegó
	// doble-comprimido (backups de agentes 0.16.33–0.16.38), se re-escribe
	// con una sola capa de zstd antes de restaurar.
	dumpPath, nerr := normalizeLXCDump(tmpPath)
	if nerr != nil {
		return nil, &CodeError{Code: "E_LXC_RESTORE", Message: "normalizar el dump: " + nerr.Error()}
	}
	normalized = dumpPath

	out, err := r.finishLXCRestore(ctx, payload, &manifest, mode, targetVMID, storage, hostnameForRestore(payload, &manifest, mode), dumpPath, start, bytesDone, totalStreams, progress)
	if err != nil {
		return nil, err
	}
	// Restauración completa: el dump y el estado ya no hacen falta.
	restoreFailed = false
	return out, nil
}

// hostnameForRestore calcula el hostname destino del CT restaurado.
func hostnameForRestore(payload contracts.RestoreLXCJobPayload, manifest *pkgimg.Manifest, mode string) string {
	if mode != "new" {
		return ""
	}
	if payload.TargetName != "" {
		return payload.TargetName
	}
	return manifest.LXC.Name
}

// finishLXCRestore ejecuta la fase de pct del restore (stop/destroy en
// in_place, restore y start) y serializa el resultado del job.
func (r *VMRunner) finishLXCRestore(ctx context.Context, payload contracts.RestoreLXCJobPayload,
	manifest *pkgimg.Manifest, mode string, targetVMID int, storage, hostname, tmpPath string,
	start time.Time, bytesDone int64, totalStreams int,
	progress func(percent int, msg string, bytesDone int64)) (json.RawMessage, error) {

	if mode == "in_place" {
		// Restaura sobre el propio CT: parada, destrucción y restore con la
		// config del archivo. Destructivo: si pct restore falla, el CT
		// queda destruido (el archivo sigue en el destino para reintentar).
		progress(62, "pct stop "+strconv.Itoa(targetVMID), bytesDone)
		if _, serr := runProbe(600, "pct", "stop", strconv.Itoa(targetVMID)); serr != nil {
			r.Log.Warn("pct stop falló (¿ya parado o no existe?)", "vmid", targetVMID, "error", serr.Error())
		}
		if _, derr := runProbe(300, "pct", "destroy", strconv.Itoa(targetVMID), "--purge"); derr != nil {
			if strings.Contains(strings.ToLower(derr.Error()), "does not exist") ||
				strings.Contains(strings.ToLower(derr.Error()), "no existe") {
				r.Log.Info("el CT origen no existe; se restaura como nuevo", "vmid", targetVMID)
			} else {
				return nil, &CodeError{Code: "E_LXC_RESTORE",
					Message: fmt.Sprintf("pct destroy %d: %v (la restauración en sitio requiere eliminar el CT; usa modo «nuevo» para conservarlo)", targetVMID, derr)}
			}
		}
	}

	progress(65, "pct restore "+strconv.Itoa(targetVMID)+" (storage "+storage+")", bytesDone)
	args := []string{"restore", strconv.Itoa(targetVMID), tmpPath, "--storage", storage}
	if hostname != "" {
		args = append(args, "--hostname", hostname)
	}
	if _, rerr := runProbe(1800, "pct", args...); rerr != nil {
		return nil, &CodeError{Code: "E_LXC_RESTORE", Message: "pct restore: " + rerr.Error()}
	}
	if mode == "in_place" || manifest.LXC.Config["onboot"] == "1" || payload.TargetVMID > 0 && mode == "new" {
		progress(95, "pct start "+strconv.Itoa(targetVMID), bytesDone)
		if _, serr := runProbe(300, "pct", "start", strconv.Itoa(targetVMID)); serr != nil {
			r.Log.Warn("pct start falló; el CT queda parado", "vmid", targetVMID, "error", serr.Error())
		}
	}

	bytesTotal := int64(0)
	for _, s := range manifest.Streams {
		bytesTotal += s.Bytes
	}
	progress(100, "contenedor restaurado", bytesDone)
	return json.Marshal(map[string]any{
		"vmid":        targetVMID,
		"mode":        mode,
		"segments":    totalStreams,
		"bytes":       bytesTotal,
		"manifest_id": manifest.ManifestID,
		"duration_ms": time.Since(start).Milliseconds(),
	})
}

// zstdMagic es la firma de un frame zstd (0xFD2FB528 little-endian).
var zstdMagic = []byte{0x28, 0xB5, 0x2F, 0xFD}

// normalizeLXCDump garantiza que el dump descargado contenga una sola capa
// de zstd sobre el tar. Los backups de los agentes 0.16.33–0.16.38 llegaron
// doble-comprimidos (el archivo de staging ya era zstd y la subida lo volvió
// a comprimir) y `pct restore` solo descomprime una: si tras descomprimir la
// primera capa aparece otro frame zstd, re-escribe el dump con una sola capa
// (coste temporal en disco solo para esa ventana de versiones) y devuelve su
// ruta; si ya es correcto, devuelve la ruta tal cual.
func normalizeLXCDump(tmpPath string) (string, error) {
	f, err := os.Open(tmpPath)
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()
	head := make([]byte, len(zstdMagic))
	if _, err := io.ReadFull(f, head); err != nil {
		return "", fmt.Errorf("dump vacío o truncado: %w", err)
	}
	if !bytes.Equal(head, zstdMagic) {
		return "", fmt.Errorf("el dump no es un archivo zstd")
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return "", err
	}
	zr, err := zstd.NewReader(f)
	if err != nil {
		return "", err
	}
	defer zr.Close()
	inner := make([]byte, len(zstdMagic))
	if _, err := io.ReadFull(zr, inner); err != nil {
		return "", fmt.Errorf("leer el dump: %w", err)
	}
	if !bytes.Equal(inner, zstdMagic) {
		return tmpPath, nil // una sola capa: correcto
	}
	// Doble comprimido (legado): descomprime la capa extra a un fichero nuevo.
	normPath := strings.TrimSuffix(tmpPath, ".tar.zst") + ".norm.tar.zst"
	src, err := os.Open(tmpPath)
	if err != nil {
		return "", err
	}
	defer func() { _ = src.Close() }()
	zr2, err := zstd.NewReader(src)
	if err != nil {
		return "", err
	}
	defer zr2.Close()
	out, err := os.Create(normPath)
	if err != nil {
		return "", err
	}
	if _, err := io.Copy(out, zr2); err != nil {
		_ = out.Close()
		_ = os.Remove(normPath)
		return "", fmt.Errorf("re-escribir el dump con una sola capa: %w", err)
	}
	if err := out.Close(); err != nil {
		_ = os.Remove(normPath)
		return "", err
	}
	return normPath, nil
}

// lxcRootfsStorage extrae el storage del rootfs original
// ("local-lvm:subvol-100-disk-0,size=8G" → "local-lvm").
func lxcRootfsStorage(cfg map[string]string) string {
	v := strings.Split(cfg["rootfs"], ",")[0]
	if i := strings.Index(v, ":"); i > 0 {
		return v[:i]
	}
	return ""
}

// firstLines recorta un error a las primeras n líneas (para mensajes de API).
func firstLines(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	lines := strings.SplitN(s, "\n", n+1)
	if len(lines) > n {
		lines = lines[:n]
	}
	out := strings.TrimSpace(strings.Join(lines, " "))
	if len(out) > 400 {
		out = out[:400]
	}
	return out
}

// Runner de contenedores LXC de Proxmox (BACKUP_LXC / RESTORE_LXC). Ejecuta
// en el propio nodo PVE con privilegios de root.
//
// Backup: pct config → vzdump --stdout (snapshot; fallback stop si el
// storage no soporta snapshots) comprimido con zstd en el propio agente y
// subido por segmentos al destino (misma tubería que imagen/VM: SHA-256,
// reanudación incremental) → manifiesto con la configuración cruda (pct
// config).
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
	"sort"
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

	// Archivo en staging local: se genera UNA VEZ (vzdump+zstd) y las
	// subidas que fallan a mitad reanudan desde él — el reintento no
	// regenera el archivo ni re-sube los segmentos ya en el destino
	// (los segmentos son direccionados por contenido: la clave lleva el
	// SHA-256, así que el destino ya no los reenvía). Se elimina al tener
	// el manifiesto subido; los restos de ejecuciones abandonadas se
	// depuran a las 48 h.
	stagingPath := r.lxcStagingPath(payload.VMID, date, cfg)
	r.cleanStaleStaging()
	var staged *os.File
	if stagingPath != "" {
		if f, err := os.Open(stagingPath); err == nil {
			if info, ierr := f.Stat(); ierr == nil && info.Size() > 0 {
				staged = f
				r.Log.Info("reanudando subida desde el archivo local del intento anterior",
					"staging", stagingPath, "bytes", info.Size())
				progress(0, "reanudando subida (el archivo ya está generado en el agente)", 0)
			} else {
				_ = f.Close()
			}
		}
	}
	mode := "reanudado"
	if staged == nil {
		var berr error
		mode, berr = r.buildLXCStaging(ctx, payload.VMID, stagingPath, estimated, progress)
		if berr != nil {
			return nil, berr
		}
		staged, err = os.Open(stagingPath)
		if err != nil {
			return nil, &CodeError{Code: "E_LXC_BACKUP",
				Message: "abrir el archivo local de staging: " + err.Error()}
		}
	}

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

	segCount, bytesDone, xerr := streamArchive(ctx, dest, manifest, staged, estimated, progress)
	if xerr != nil {
		return nil, xerr
	}

	out, err := finishCapture(ctx, dest, manifest, "lxc", progress, bytesDone)
	if err != nil {
		return nil, err
	}
	// El punto de restauración está completo: el archivo local ya no hace
	// falta (una nueva ejecución del mismo día genera uno nuevo con los
	// datos actuales del CT).
	if stagingPath != "" {
		_ = staged.Close()
		_ = os.Remove(stagingPath)
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

// lxcStagingDir devuelve el directorio de staging de archivos LXC del
// agente (DataDir/lxc-staging) o cadena vacía si no hay DataDir.
func (r *VMRunner) lxcStagingDir() string {
	if r.DataDir == "" {
		return ""
	}
	return filepath.Join(r.DataDir, "lxc-staging")
}

// lxcStagingPath construye la ruta del archivo en staging para un CT y día:
// DataDir/lxc-staging/ct<vmid>-<huella cfg>-<fecha>.zst. La huella del
// config evita reutilizar el archivo de un CT destruido y recreado con el
// mismo VMID el mismo día.
func (r *VMRunner) lxcStagingPath(vmid int, date string, cfg map[string]string) string {
	dir := r.lxcStagingDir()
	if dir == "" {
		return ""
	}
	keys := make([]string, 0, len(cfg))
	for k := range cfg {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	h := sha256.New()
	for _, k := range keys {
		fmt.Fprintf(h, "%s=%s\n", k, cfg[k])
	}
	return filepath.Join(dir, fmt.Sprintf("ct%d-%s-%s.zst",
		vmid, hex.EncodeToString(h.Sum(nil))[:8], date))
}

// cleanStaleStaging depura restos de staging: temporales de ejecuciones
// interrumpidas y archivos completos de hace más de 48 h (ningún job
// reanuda algo tan viejo). Sin efecto si no hay directorio.
func (r *VMRunner) cleanStaleStaging() {
	dir := r.lxcStagingDir()
	if dir == "" {
		return
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		path := filepath.Join(dir, e.Name())
		if strings.HasSuffix(e.Name(), ".bdr-tmp") {
			_ = os.Remove(path) // sin job concurrente: ningún intento lo usa
			continue
		}
		if info, ierr := e.Info(); ierr == nil && info.ModTime().
			Before(time.Now().Add(-48*time.Hour)) {
			_ = os.Remove(path)
		}
	}
}

// buildLXCStaging genera el archivo zstd del CT en stagingPath ejecutando
// vzdump a stdout y comprimiendo en vuelo hacia un temporal que se renombra
// al terminar (atómico: solo existe en staging si el archivo está completo).
// Devuelve el modo vzdump usado. Si el modo snapshot falla sin emitir
// bytes, reintenta con stop (parada breve del contenedor); un fallo a mitad
// de archivo es error (nunca se renombra un archivo truncado).
func (r *VMRunner) buildLXCStaging(ctx context.Context, vmid int, stagingPath string,
	estimated int64, progress func(percent int, msg string, bytesDone int64)) (string, error) {
	tmpPath := stagingPath + ".bdr-tmp"
	var lastErr error
	for _, mode := range []string{"snapshot", "stop"} {
		cmd, stdout, stderr, err := startVzdump(ctx, vmid, mode)
		if err != nil {
			lastErr = &CodeError{Code: "E_LXC_BACKUP", Message: err.Error()}
			continue // fallo al arrancar snapshot → prueba con stop
		}
		r.Log.Info("vzdump iniciado (staging)", "vmid", vmid, "mode", mode)
		raw, berr := r.dumpToStaging(ctx, cmd, stdout, stderr, tmpPath, mode, estimated, progress)
		if berr == nil {
			if rerr := os.Rename(tmpPath, stagingPath); rerr != nil {
				return "", &CodeError{Code: "E_LXC_BACKUP",
					Message: "guardar el archivo en staging: " + rerr.Error()}
			}
			return mode, nil
		}
		_ = os.Remove(tmpPath)
		if ctx.Err() != nil {
			return "", &CodeError{Code: "E_CANCELED", Message: "job cancelado"}
		}
		lastErr = berr
		// El fallback a stop solo tiene sentido si snapshot no llegó a
		// emitir datos (el storage no soporta snapshots); a mitad de
		// archivo el error es real y no se re-intenta parando el CT.
		if raw > 0 || mode == "stop" {
			break
		}
	}
	if lastErr == nil {
		lastErr = &CodeError{Code: "E_LXC_BACKUP", Message: "vzdump: no se pudo generar el archivo"}
	}
	return "", lastErr
}

// dumpToStaging vuelca vzdump --stdout comprimido con zstd al temporal,
// contando los bytes crudos leídos, y espera a que vzdump termine con
// éxito. Devuelve (bytes crudos leídos, error). Si hay error, el temporal
// se elimina: nunca se renombra un archivo truncado.
func (r *VMRunner) dumpToStaging(ctx context.Context, cmd *exec.Cmd, stdout io.ReadCloser,
	stderr *bytes.Buffer, tmpPath, mode string, estimated int64,
	progress func(percent int, msg string, bytesDone int64)) (int64, error) {
	f, err := os.Create(tmpPath)
	if err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return 0, &CodeError{Code: "E_LXC_BACKUP",
			Message: "crear el archivo local de staging: " + err.Error()}
	}
	defer func() {
		_ = f.Close()
		_ = os.Remove(tmpPath) // no-op si se renombró antes (el rename lo mueve)
	}()

	zw, zerr := zstd.NewWriter(f, zstd.WithEncoderConcurrency(1))
	if zerr != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return 0, &CodeError{Code: "E_LXC_BACKUP", Message: "comprimir el archivo: " + zerr.Error()}
	}
	buf := make([]byte, 1<<20)
	var raw int64
	var rerr error
	for rerr == nil {
		if ctx.Err() != nil {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
			return raw, &CodeError{Code: "E_CANCELED", Message: "job cancelado"}
		}
		var n int
		n, rerr = stdout.Read(buf)
		if n > 0 {
			raw += int64(n)
			if estimated > 0 {
				pct := int(raw * 95 / estimated)
				if pct > 95 {
					pct = 95
				}
				progress(pct, fmt.Sprintf("generando archivo (vzdump %s)", mode), raw)
			}
			if _, werr := zw.Write(buf[:n]); werr != nil {
				_ = cmd.Process.Kill()
				_ = cmd.Wait()
				return raw, &CodeError{Code: "E_LXC_BACKUP",
					Message: "comprimir al archivo de staging: " + werr.Error()}
			}
		}
	}
	if rerr != nil && rerr != io.EOF {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return raw, &CodeError{Code: "E_LXC_BACKUP", Message: "leer vzdump: " + rerr.Error()}
	}
	// Cierra el epílogo zstd antes de esperar a vzdump: si vzdump falla,
	// el error manda y el temporal se elimina (nunca se renombra truncado).
	if err := zw.Close(); err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return raw, &CodeError{Code: "E_LXC_BACKUP", Message: "comprimir el archivo: " + err.Error()}
	}
	if err := cmd.Wait(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return raw, &CodeError{Code: "E_LXC_BACKUP", Message: "vzdump: " + firstLines(msg, 2)}
	}
	return raw, nil
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
	defer func() {
		if restoreFailed {
			// Un reintento continúa desde aquí: conserva dump y estado.
			return
		}
		_ = os.Remove(tmpPath)
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

	out, err := r.finishLXCRestore(ctx, payload, &manifest, mode, targetVMID, storage, hostnameForRestore(payload, &manifest, mode), tmpPath, start, bytesDone, totalStreams, progress)
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

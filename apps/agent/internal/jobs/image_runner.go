// Captura de imagen completa del equipo (bare-metal) para BACKUP_IMAGE.
//
// Flujo: enumera los discos fijos y sube cada segmento DIRECTAMENTE al
// bucket del destino (minio-go) sin staging en disco — el espacio local que
// usa es el journal/manifest, no los datos. Los objetos se nombran con el
// SHA-256 del segmento, lo que da reanudación incremental y salto de
// segmentos enteramente a cero.
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
	"runtime"
	"strings"
	"time"

	"github.com/juansoler/bdr/packages/contracts"
	pkgimg "github.com/juansoler/bdr/packages/image"
	"github.com/juansoler/bdr/packages/logging"
	"github.com/juansoler/bdr/packages/version"
)

// imageSegmentSize es el tamaño de segmento para capturas de disco.
const imageSegmentSize = int64(64 << 20) // 64 MiB

// ImageRunner captura imágenes completas de disco (equipos físicos).
type ImageRunner struct {
	DataDir     string
	Log         *logging.Logger
	KopiaBinary string
	KopiaConfig string // modo heredado; vacío = conexión del agente
}

// NewImageRunner crea el runner de imágenes.
func NewImageRunner(dataDir string, log *logging.Logger, kopiaBinary, kopiaConfig string) *ImageRunner {
	return &ImageRunner{DataDir: dataDir, Log: log, KopiaBinary: kopiaBinary, KopiaConfig: kopiaConfig}
}

// Capture ejecuta el job de imagen sobre los discos físicos del equipo.
func (r *ImageRunner) Capture(ctx context.Context, creds *contracts.RepositoryCredentials,
	payload contracts.ImageJobPayload, progress func(percent int, msg string, bytesDone int64)) (json.RawMessage, error) {

	dest, err := newUploadDest(ctx, creds, "image", payload.RetentionDays)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 24*time.Hour)
	defer cancel()

	start := time.Now()
	disks, err := r.enumerateDisks()
	if err != nil {
		return nil, &CodeError{Code: "E_IMAGE_ENUM", Message: err.Error()}
	}
	if len(disks) == 0 {
		return nil, &CodeError{Code: "E_IMAGE_ENUM", Message: "sin discos fijos que capturar"}
	}
	disks, err = filterImageDisks(disks, payload.Disks)
	if err != nil {
		return nil, err
	}

	manifest := &pkgimg.Manifest{
		SchemaVersion:            1,
		ManifestID:               fmt.Sprintf("img-%s-%d", payload.PlanID, start.UnixMilli()),
		RunID:                    payload.PlanID,
		Kind:                     "image",
		ImageMode:                pkgimg.ModeRawDisk,
		Consistency:              "crash_consistent",
		CapturedAt:               start.UTC().Format(time.RFC3339),
		Security:                 pkgimg.Security{BootMode: "uefi"},
		UnallocatedBytesCaptured: true,
		Tooling:                  pkgimg.Tooling{AgentVersion: version.Version},
		RestoreHints:             pkgimg.RestoreHints{ExpectedFirmware: "uefi", SystemDisk: disks[0].Index},
	}

	result := contracts.ImageJobResult{Disks: []contracts.ImageDiskResult{}}
	prog := func(done, total int64, msg string) {
		pct := 0
		if total > 0 {
			pct = int((done * 90) / total)
		}
		progress(pct, msg, done)
	}
	for i, d := range disks {
		if err := ctx.Err(); err != nil {
			return nil, &CodeError{Code: "E_CANCELED", Message: "job cancelado"}
		}
		progress((i*90)/len(disks), fmt.Sprintf("capturando disco %d (%s) de %d", d.Index, d.DevicePath, len(disks)), result.BytesTotal)

		src, err := os.Open(d.DevicePath)
		if err != nil {
			return nil, &CodeError{Code: "E_IMAGE_OPEN", Message: fmt.Sprintf("disco %d (%s): %v", d.Index, d.DevicePath, err)}
		}
		segCount, err := captureSource(ctx, dest, src, d, manifest, prog, &result.BytesTotal)
		_ = src.Close()
		if err != nil {
			return nil, err
		}
		result.Disks = append(result.Disks, contracts.ImageDiskResult{
			Index: d.Index, Device: d.DevicePath, Table: d.Table,
			TotalBytes: d.TotalBytes, Segments: segCount,
		})
		result.BytesTotal += d.TotalBytes
	}

	out, err := finishCapture(ctx, dest, manifest, "image", progress, result.BytesTotal)
	if err != nil {
		return nil, err
	}
	result.ManifestID = manifest.ManifestID
	result.ManifestKey = out.manifestKey
	result.DurationMS = time.Since(start).Milliseconds()
	return out.result, nil
}

// filterImageDisks aplica el filtro de índices del payload de imagen
// (vacío = todos los discos). No muta el slice de entrada. Falla con
// E_VALIDATION si ningún índice pedido coincide con un disco real.
func filterImageDisks(disks []pkgimg.Disk, want []int) ([]pkgimg.Disk, error) {
	if len(want) == 0 {
		return disks, nil
	}
	filtered := make([]pkgimg.Disk, 0, len(disks))
	for _, d := range disks {
		for _, w := range want {
			if d.Index == w {
				filtered = append(filtered, d)
				break
			}
		}
	}
	if len(filtered) == 0 {
		return nil, &CodeError{Code: "E_VALIDATION", Message: "el filtro de discos no coincide con ningún disco"}
	}
	return filtered, nil
}

// captureSource captura un origen de disco (dispositivo físico o volumen
// NBD) en segmentos subidos al destino. Reutilizable por imagen y VMs.
// Añade el disco al manifiesto.
func captureSource(ctx context.Context, dest uploadDest, src io.ReaderAt, disk pkgimg.Disk,
	manifest *pkgimg.Manifest,
	progress func(done, total int64, msg string), bytesDone *int64) (int, error) {

	manifest.Disks = append(manifest.Disks, disk)
	total := disk.TotalBytes
	idx := disk.Index
	buf := make([]byte, imageSegmentSize)
	segCount := 0
	now := time.Now().UTC()
	for seg := 0; ; seg++ {
		if err := ctx.Err(); err != nil {
			return segCount, &CodeError{Code: "E_CANCELED", Message: "job cancelado"}
		}
		offset := int64(seg) * imageSegmentSize
		if offset >= total {
			break
		}
		n, rerr := src.ReadAt(buf, offset)
		// Lectura de 0 bytes sin error con el disco pendiente: el
		// dispositivo NBD aún no expone el tamaño tras qemu-nbd --connect.
		// Se reintenta hasta ~10 s antes de fallar.
		if n == 0 && (rerr == nil || rerr == io.EOF) && offset < total {
			for retry := 0; retry < 20 && n == 0; retry++ {
				time.Sleep(500 * time.Millisecond)
				n, rerr = src.ReadAt(buf, offset)
			}
		}
		if n == 0 && rerr != nil && rerr != io.EOF {
			// Cola ilegible (discos con tamaño redondeado en Windows):
			// recorta al último offset legible y termina el disco. Un fallo
			// en el primer segmento es un disco realmente ilegible.
			if seg > 0 {
				disk.TotalBytes = offset
				progress(*bytesDone, 0, fmt.Sprintf(
					"aviso: disco %d: cola ilegible desde el offset %d; se recorta al final legible", idx, offset))
				break
			}
			return segCount, &CodeError{Code: "E_IMAGE_CAPTURE",
				Message: fmt.Sprintf("disco %d seg %d: %v", idx, seg, rerr)}
		}
		if n == 0 && offset < total {
			return segCount, &CodeError{Code: "E_IMAGE_CAPTURE",
				Message: fmt.Sprintf("disco %d seg %d: lectura corta en offset %d de %d (¿dispositivo no listo?)", idx, seg, offset, total)}
		}
		chunk := buf[:n]
		sum := sha256Hex(chunk)
		name := fmt.Sprintf("disk%d-seg%04d", idx, seg)
		st := pkgimg.Stream{
			Disk: idx, Segment: seg, Name: name,
			Offset: offset, Bytes: int64(n), SHA256: sum,
			State: pkgimg.SegmentCompleted,
		}
		switch {
		case allZero(chunk):
			st.Zero = true
		default:
			key, err := dest.putSegment(ctx, dest.Prefix()+name+"-"+sum, chunk, now)
			if err != nil {
				return segCount, &CodeError{Code: "E_IMAGE_UPLOAD",
					Message: fmt.Sprintf("disco %d seg %d: %v", idx, seg, err)}
			}
			st.Blob = key
		}
		manifest.Streams = append(manifest.Streams, st)
		segCount++
		*bytesDone += int64(n)
		if segCount%20 == 0 || n < int(imageSegmentSize) {
			progress(*bytesDone, 0, fmt.Sprintf("disco %d: segmento %d (%s)", idx, seg, humanBytes(*bytesDone)))
		}
		if n < int(imageSegmentSize) {
			break
		}
	}
	return segCount, nil
}

type captureResult struct {
	manifestKey string
	result      []byte
}

// finishCapture sube el manifiesto como objeto y serializa el resultado.
func finishCapture(ctx context.Context, dest uploadDest, manifest *pkgimg.Manifest,
	ns string, progress func(percent int, msg string, bytesDone int64), bytesDone int64) (captureResult, error) {

	mb, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return captureResult{}, &CodeError{Code: "E_INTERNAL", Message: "manifiesto: " + err.Error()}
	}
	manifestKey := fmt.Sprintf("%smanifest-%s.json", dest.Prefix(), manifest.ManifestID)
	if err := dest.putRaw(ctx, manifestKey, mb, time.Now().UTC()); err != nil {
		return captureResult{}, &CodeError{Code: "E_IMAGE_UPLOAD", Message: "manifiesto: " + err.Error()}
	}
	progress(100, fmt.Sprintf("captura completa en el destino (%s)", manifestKey), bytesDone)
	return captureResult{manifestKey: manifestKey, result: mb}, nil
}

func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// allZero indica si el buffer es enteramente cero.
func allZero(b []byte) bool {
	for _, v := range b {
		if v != 0 {
			return false
		}
	}
	return true
}

func humanBytes(n int64) string {
	switch {
	case n >= 1<<40:
		return fmt.Sprintf("%.1f TiB", float64(n)/(1<<40))
	case n >= 1<<30:
		return fmt.Sprintf("%.1f GiB", float64(n)/(1<<30))
	default:
		return fmt.Sprintf("%.0f MiB", float64(n)/(1<<20))
	}
}

// enumerateDisks lista los discos fijos con su tabla de particiones.
func (r *ImageRunner) enumerateDisks() ([]pkgimg.Disk, error) {
	switch runtime.GOOS {
	case "linux":
		return enumerateDisksLinux()
	case "windows":
		return enumerateDisksWindows()
	default:
		return nil, fmt.Errorf("captura de imagen no soportada en %s", runtime.GOOS)
	}
}

// runProbe ejecuta un comando de sondeo y devuelve su stdout como texto.
func runProbe(timeoutSeconds int, name string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeoutSeconds)*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	cmd.SysProcAttr = probeSysProcAttr()
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return "", fmt.Errorf("%s: %s", name, msg)
	}
	return stdout.String(), nil
}

// Escritura de imagen para despliegue bare-metal: reconstruye el contenido
// de un disco desde los segmentos del snapshot (directorios de staging
// restaurados con kopia restore) verificando el SHA-256 de cada segmento.
//
// Uso típico (equipo nuevo, arranque desde un live):
//
//	kopia restore <snapshot-id> /mnt/staging
//	bdr-agent image-write --segments-dir /mnt/staging --target /dev/sda --yes
package image

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	pkgimg "github.com/juansoler/bdr/packages/image"
)

// WriteOptions configura una escritura de imagen.
type WriteOptions struct {
	SegmentsDir string // directorio con manifest.json y diskN-segNNNN
	Target      string // dispositivo o fichero destino
	Verify      bool   // verifica SHA-256 de cada segmento (por defecto true)
	Progress    func(segment, total int, bytesDone int64)
}

// WriteImage escribe la imagen completa en el destino, en orden de offset,
// verificando cada segmento contra el manifiesto. Requiere --yes del
// llamante (aquí se asume); la verificación es del manifiesto original.
func WriteImage(opts WriteOptions) (pkgimg.Manifest, error) {
	var manifest pkgimg.Manifest
	mb, err := os.ReadFile(filepath.Join(opts.SegmentsDir, "manifest.json"))
	if err != nil {
		return manifest, fmt.Errorf("manifiesto: %w", err)
	}
	if err := json.Unmarshal(mb, &manifest); err != nil {
		return manifest, fmt.Errorf("manifiesto inválido: %w", err)
	}
	if len(manifest.Streams) == 0 {
		return manifest, fmt.Errorf("el manifiesto no tiene segmentos")
	}

	// Orden determinista: disco, luego offset.
	streams := append([]pkgimg.Stream(nil), manifest.Streams...)
	sort.Slice(streams, func(i, j int) bool {
		if streams[i].Disk != streams[j].Disk {
			return streams[i].Disk < streams[j].Disk
		}
		return streams[i].Offset < streams[j].Offset
	})

	dst, err := os.OpenFile(opts.Target, os.O_WRONLY|os.O_CREATE, 0o644)
	if err != nil {
		return manifest, fmt.Errorf("abrir destino %s: %w", opts.Target, err)
	}
	defer dst.Close()

	for i, s := range streams {
		if s.Zero {
			// Región toda a ceros: no hay blob que abrir; se escriben
			// ceros en su offset (en bloques, para no reservar un buffer
			// del tamaño del stream).
			const zeroChunk = 4 << 20
			buf := make([]byte, zeroChunk)
			for off, remaining := s.Offset, s.Bytes; remaining > 0; {
				n := remaining
				if n > zeroChunk {
					n = zeroChunk
				}
				if _, err := dst.WriteAt(buf[:n], off); err != nil {
					return manifest, fmt.Errorf("segmento %s: escribir ceros: %w", s.Name, err)
				}
				off += n
				remaining -= n
			}
			if opts.Progress != nil {
				opts.Progress(i+1, len(streams), s.Offset+s.Bytes)
			}
			continue
		}
		name := s.Blob
		if name == "" {
			name = s.Name
		}
		path := filepath.Join(opts.SegmentsDir, name)
		f, err := os.Open(path)
		if err != nil {
			return manifest, fmt.Errorf("segmento %s: %w", name, err)
		}
		if _, err := dst.Seek(s.Offset, io.SeekStart); err != nil {
			_ = f.Close()
			return manifest, fmt.Errorf("segmento %s: seek: %w", s.Name, err)
		}
		if opts.Verify {
			// Verifica mientras escribe: el hasher ve los mismos bytes que
			// van al destino.
			h := sha256.New()
			n, err := io.Copy(dst, io.TeeReader(f, h))
			if err != nil {
				_ = f.Close()
				return manifest, fmt.Errorf("segmento %s: escribir: %w", s.Name, err)
			}
			got := hex.EncodeToString(h.Sum(nil))
			if !strings.EqualFold(got, s.SHA256) {
				_ = f.Close()
				return manifest, fmt.Errorf("segmento %s: SHA-256 no coincide (imagen incompleta o destino defectuoso)", s.Name)
			}
			if n != s.Bytes {
				_ = f.Close()
				return manifest, fmt.Errorf("segmento %s: %d bytes, esperaba %d", s.Name, n, s.Bytes)
			}
		} else {
			if _, err := io.Copy(dst, f); err != nil {
				_ = f.Close()
				return manifest, fmt.Errorf("segmento %s: escribir: %w", s.Name, err)
			}
		}
		_ = f.Close()
		if opts.Progress != nil {
			opts.Progress(i+1, len(streams), s.Offset+s.Bytes)
		}
	}
	// Sincroniza antes de devolver el control (el destino suele ser un disco).
	if err := dst.Sync(); err != nil {
		return manifest, err
	}
	return manifest, nil
}

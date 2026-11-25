// Helpers de restauración: descarga de segmentos con reintento y estado de
// reanudación.
package jobs

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash"
	"io"
	"os"
	"path/filepath"
	"time"

	pkgimg "github.com/juansoler/bdr/packages/image"
)

// Reintentos de un segmento cuya descarga falla (esperas de 5, 10… s: ~2 min en total).
const restoreCopyRetries = 7

// restoreRetryDelay es la espera base entre reintentos (crece con el intento).
var restoreRetryDelay = 5 * time.Second

// copyStream vuelca un segmento del destino en el volumen desde su offset y
// suma al hash lo escrito. El error lleva el lado que falló («descargar:» o «escribir:»).
func copyStream(ctx context.Context, dest uploadDest, s pkgimg.Stream, dst io.WriterAt,
	limit int64, h hash.Hash, warn func(string)) (int64, error) {
	for intento := 0; ; intento++ {
		n, err := copyStreamOnce(ctx, dest, s, dst, limit, h)
		// Reintentar la descarga es seguro: el segmento ocupa un rango fijo y el
		// llamante verifica su SHA-256.
		if err == nil || intento >= restoreCopyRetries || !retryableDownload(err) {
			return n, err
		}
		warn(fmt.Sprintf("segmento %s: %v; reintento %d/%d", s.Name, err, intento+1, restoreCopyRetries))
		select {
		case <-ctx.Done():
			return 0, ctx.Err()
		case <-time.After(time.Duration(intento+1) * restoreRetryDelay):
		}
	}
}

// copyStreamOnce separa lectura y escritura para atribuir el fallo al lado que lo produjo.
func copyStreamOnce(ctx context.Context, dest uploadDest, s pkgimg.Stream, dst io.WriterAt,
	limit int64, h hash.Hash) (int64, error) {
	obj, err := dest.openObject(ctx, s.Blob)
	if err != nil {
		return 0, &errorDescarga{err}
	}
	defer obj.Close()
	h.Reset()
	buf := make([]byte, 32<<10)
	var n int64
	for n < limit {
		want := limit - n
		if want > int64(len(buf)) {
			want = int64(len(buf))
		}
		r, rerr := obj.Read(buf[:want])
		if r > 0 {
			wn, werr := dst.WriteAt(buf[:r], s.Offset+n)
			n += int64(wn)
			if werr != nil {
				return n, fmt.Errorf("escribir: %w", werr)
			}
			if wn < r {
				return n, fmt.Errorf("escribir: %w", io.ErrShortWrite)
			}
			h.Write(buf[:r])
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			return n, &errorDescarga{rerr}
		}
	}
	return n, nil
}

// errorDescarga marca un fallo al leer el objeto del destino: el dispositivo
// NBD no se recupera solo tras un error de E/S, así que solo este lado se reintenta.
type errorDescarga struct{ err error }

func (e *errorDescarga) Error() string { return "descargar: " + e.err.Error() }
func (e *errorDescarga) Unwrap() error { return e.err }

// retryableDownload: fallo de transporte o 5xx del backend; no un 4xx, un objeto ausente ni la cancelación del job.
func retryableDownload(err error) bool {
	var descarga *errorDescarga
	if !errors.As(err, &descarga) || errors.Is(err, context.Canceled) ||
		errors.Is(err, context.DeadlineExceeded) || errors.Is(err, os.ErrNotExist) {
		return false
	}
	return esRetryableS3(err)
}

// restoreState es el estado de reanudación de una restauración: registra
// los streams ya escritos y verificados para que un reintento del job
// continúe donde quedó (el fichero vive en DataDir/restore-state y se
// elimina cuando la restauración termina bien).
type restoreState struct {
	ManifestKey string   `json:"manifest_key"`
	Scope       string   `json:"scope,omitempty"` // destino de la restauración: separa estados de un mismo manifiesto
	Done        []string `json:"done"`

	dataDir string `json:"-"` // ubicación del fichero de estado
}

// restoreFingerprint calcula la huella corta de una clave de manifiesto:
// identifica de forma determinista el fichero de estado y el dump temporal
// de una restauración.
func restoreFingerprint(manifestKey string) string {
	sum := sha256.Sum256([]byte(manifestKey))
	return hex.EncodeToString(sum[:])[:16]
}

// restoreStatePath calcula la ruta del fichero de estado para una
// restauración: DataDir/restore-state/<huella del manifiesto y su ámbito>.json.
func restoreStatePath(dataDir, manifestKey, scope string) string {
	if dataDir == "" {
		return ""
	}
	if scope != "" {
		manifestKey += "\n" + scope
	}
	return filepath.Join(dataDir, "restore-state",
		restoreFingerprint(manifestKey)+".json")
}

// loadRestoreState carga el estado de reanudación (o uno vacío).
func loadRestoreState(dataDir, manifestKey, scope string) *restoreState {
	s := &restoreState{ManifestKey: manifestKey, Scope: scope, dataDir: dataDir}
	if path := restoreStatePath(dataDir, manifestKey, scope); path != "" {
		if data, err := os.ReadFile(path); err == nil {
			var saved restoreState
			if json.Unmarshal(data, &saved) == nil && saved.ManifestKey == manifestKey && saved.Scope == scope {
				s.Done = saved.Done
			}
		}
	}
	return s
}

// Has indica si un stream ya está completado en un intento anterior.
func (s *restoreState) Has(name string) bool {
	for _, d := range s.Done {
		if d == name {
			return true
		}
	}
	return false
}

// Mark registra un stream completado y persiste el estado de forma atómica.
// Sin DataDir (restore-state desactivado) solo mantiene la lista en memoria.
func (s *restoreState) Mark(name string) {
	if s.Has(name) {
		return
	}
	s.Done = append(s.Done, name)
	path := restoreStatePath(s.dataDir, s.ManifestKey, s.Scope)
	if path == "" {
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return
	}
	data, err := json.Marshal(s)
	if err != nil {
		return
	}
	tmp := path + ".bdr-tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return
	}
	_ = os.Rename(tmp, path)
}

// Clear elimina el estado (restauración completada).
func (s *restoreState) Clear() {
	if path := restoreStatePath(s.dataDir, s.ManifestKey, s.Scope); path != "" {
		_ = os.Remove(path)
	}
}

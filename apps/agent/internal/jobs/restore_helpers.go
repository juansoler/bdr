// Helpers de escritura por offset para restaurar segmentos en volúmenes.
package jobs

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
)

// offsetWriter adapta un io.WriterAt a io.Writer avanzando la posición.
type offsetWriter struct {
	dst io.WriterAt
	pos int64
}

func newWriterAt(dst io.WriterAt, base int64) io.Writer {
	return &offsetWriter{dst: dst, pos: base}
}

func (w *offsetWriter) Write(p []byte) (int, error) {
	n, err := w.dst.WriteAt(p, w.pos)
	w.pos += int64(n)
	return n, err
}

// restoreState es el estado de reanudación de una restauración: registra
// los streams ya escritos y verificados para que un reintento del job
// continúe donde quedó (el fichero vive en DataDir/restore-state y se
// elimina cuando la restauración termina bien).
type restoreState struct {
	ManifestKey string   `json:"manifest_key"`
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
// restauración: DataDir/restore-state/<huella del manifiesto>.json.
func restoreStatePath(dataDir, manifestKey string) string {
	if dataDir == "" {
		return ""
	}
	return filepath.Join(dataDir, "restore-state",
		restoreFingerprint(manifestKey)+".json")
}

// loadRestoreState carga el estado de reanudación (o uno vacío).
func loadRestoreState(dataDir, manifestKey string) *restoreState {
	s := &restoreState{ManifestKey: manifestKey, dataDir: dataDir}
	if path := restoreStatePath(dataDir, manifestKey); path != "" {
		if data, err := os.ReadFile(path); err == nil {
			var saved restoreState
			if json.Unmarshal(data, &saved) == nil && saved.ManifestKey == manifestKey {
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
	path := restoreStatePath(s.dataDir, s.ManifestKey)
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
	if path := restoreStatePath(s.dataDir, s.ManifestKey); path != "" {
		_ = os.Remove(path)
	}
}


// Navegación avanzada del snapshot: búsqueda recursiva y descarga de
// directorios en ZIP. Ambas operaciones van por la conexión directa del
// plano de control a Kopia y quedan auditadas.
package control

import (
	"archive/zip"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/juansoler/bdr/apps/server/internal/store"
	"github.com/juansoler/bdr/packages/contracts"
)

// dirLister abstrae el acceso de lectura al snapshot: ListDir para recorrer
// y OpenObject para leer ficheros. *KopiaControl lo implementa; los tests
// sustituyen Server.DirLister por un fake.
type DirLister interface {
	ListDir(ctx context.Context, objPath string) ([]contracts.FileEntry, error)
	OpenObject(ctx context.Context, objPath string) (io.ReadCloser, error)
}

// Límites de los recorridos: protegen al plano de control de snapshots
// enormes y de rutas patológicas. Los recorridos se cortan (no fallan) y
// el resultado indica si quedó truncado.
const (
	searchMaxDirs    = 500              // directorios visitados por búsqueda
	searchMaxDepth   = 8                // profundidad máxima desde la base
	searchMaxResults = 100              // resultados devueltos
	zipMaxFiles      = 2000             // ficheros por ZIP
	zipMaxBytes      = int64(512 << 20) // bytes totales por ZIP
)

// walkEntry es un resultado de búsqueda con su ruta completa.
type walkEntry struct {
	entry contracts.FileEntry
}

// walkDir recorre en anchura desde objRoot visitando directorios hasta los
// límites. fn recibe cada entrada con su ruta RELATIVA A LA BASE (los
// resultados de búsqueda le añaden el prefijo; el ZIP la usa tal cual para
// las entradas del archivo). Devuelve si el recorrido se truncó.
func walkDir(ctx context.Context, dl DirLister, objRoot, _ string, depth int, fn func(contracts.FileEntry) bool) (truncated bool) {
	type dir struct {
		objPath string
		relPath string
		depth   int
	}
	queue := []dir{{objPath: objRoot, depth: 0}}
	visited := 0
	for len(queue) > 0 {
		d := queue[0]
		queue = queue[1:]
		visited++
		if visited > searchMaxDirs || d.depth > depth {
			return true
		}
		entries, err := dl.ListDir(ctx, d.objPath)
		if err != nil {
			// Un directorio ilegible no aborta el recorrido.
			continue
		}
		for _, e := range entries {
			e.Path = joinRel(d.relPath, e.Name)
			if !fn(e) {
				return true
			}
			if e.Type == "dir" {
				queue = append(queue, dir{objPath: d.objPath + "/" + e.Name, relPath: e.Path, depth: d.depth + 1})
			}
		}
	}
	return false
}

func joinRel(base, name string) string {
	if base == "" {
		return name
	}
	return base + "/" + name
}

// restorePointFilesSearch busca entradas por nombre (subcadena, sin distinguir
// mayúsculas) desde la base indicada.
// GET /admin/restore-points/{id}/files/search?q=&path=&limit=
func (s *Server) restorePointFilesSearch(w http.ResponseWriter, r *http.Request, id string) {
	if !allowMethod(w, r, http.MethodGet) {
		return
	}
	r, a, ok := s.authorize(w, r, "read")
	if !ok {
		return
	}
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if q == "" {
		s.problem(w, r, http.StatusBadRequest, "E_VALIDATION", "Falta el parámetro q")
		return
	}
	dl := s.fileLister()
	if dl == nil {
		s.problem(w, r, http.StatusConflict, "E_REPO_UNCONFIGURED",
			"El plano de control no tiene acceso directo al repositorio; la operación no está disponible")
		return
	}
	rawPath, _, objRoot, ok := s.resolveSnapshotRoot(w, r, id)
	if !ok {
		return
	}
	limit := searchMaxResults
	if v := atoiClamp(r.URL.Query().Get("limit"), 1, searchMaxResults); v > 0 {
		limit = v
	}
	fold := strings.ToLower(q)
	var items []contracts.FileEntry
	truncated := walkDir(r.Context(), dl, objRoot, rawPath, searchMaxDepth, func(e contracts.FileEntry) bool {
		if strings.Contains(strings.ToLower(e.Name), fold) {
			if len(items) >= limit {
				return false
			}
			// Los resultados se expresan respecto a la raíz del snapshot.
			e.Path = joinRel(rawPath, e.Path)
			items = append(items, e)
		}
		return true
	})
	s.audit(r, a, "restore_point.searched", "restore_point", id, "ok",
		map[string]string{"q": q, "path": rawPath, "results": fmt.Sprint(len(items))})
	writeJSON(w, http.StatusOK, contracts.FileSearchResult{
		RestorePointID: id, Path: rawPath, Query: q, Truncated: truncated, Items: items,
	})
}

// restorePointFilesDownloadZip descarga un directorio (o la raíz) como ZIP en
// streaming, con límites de ficheros y bytes.
// GET /admin/restore-points/{id}/files/download?path=
func (s *Server) restorePointFilesDownloadZip(w http.ResponseWriter, r *http.Request, id string) {
	if !allowMethod(w, r, http.MethodGet) {
		return
	}
	r, a, ok := s.authorize(w, r, "read")
	if !ok {
		return
	}
	dl := s.fileLister()
	if dl == nil {
		s.problem(w, r, http.StatusConflict, "E_REPO_UNCONFIGURED",
			"El plano de control no tiene acceso directo al repositorio; la operación no está disponible")
		return
	}
	rawPath, _, objRoot, ok := s.resolveSnapshotRoot(w, r, id)
	if !ok {
		return
	}
	baseName := "snapshot-" + shortID(id)
	if rawPath != "" {
		baseName += "-" + strings.ReplaceAll(rawPath, "/", "-")
	}
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", baseName+".zip"))
	w.Header().Set("X-Content-Type-Options", "nosniff")

	var files, dirs int
	var bytesWritten int64
	truncated := false
	zw := zip.NewWriter(w)
	stopped := walkDir(r.Context(), dl, objRoot, rawPath, searchMaxDepth, func(e contracts.FileEntry) bool {
		if e.Type == "dir" {
			dirs++
			return true
		}
		if files >= zipMaxFiles || bytesWritten >= zipMaxBytes {
			truncated = true
			return false
		}
		objPath := objRoot
		if e.Path != "" {
			objPath += "/" + e.Path
		}
		rc, err := dl.OpenObject(r.Context(), objPath)
		if err != nil {
			return true // fichero ilegible: se salta
		}
		defer rc.Close()
		f, err := zw.Create(e.Path)
		if err != nil {
			return false
		}
		n, _ := io.Copy(f, rc)
		files++
		bytesWritten += n
		return bytesWritten < zipMaxBytes
	})
	truncated = truncated || stopped
	if err := zw.Close(); err != nil {
		s.Logger.Warn("zip incompleto", "rp", id, "error", err.Error())
	}
	s.audit(r, a, "restore_point.dir_downloaded", "restore_point", id, "ok", map[string]string{
		"path": rawPath, "files": fmt.Sprint(files), "bytes": fmt.Sprint(bytesWritten),
		"truncated": fmt.Sprint(truncated),
	})
	_ = dirs
}

// resolveSnapshotRoot valida la ruta pedida y devuelve (ruta relativa, punto
// de recuperación, objeto raíz del recorrido).
func (s *Server) resolveSnapshotRoot(w http.ResponseWriter, r *http.Request, id string) (string, *contracts.RestorePointView, string, bool) {
	rawPath := strings.Trim(r.URL.Query().Get("path"), "/")
	if !validRelPath(rawPath) {
		s.problem(w, r, http.StatusBadRequest, "E_VALIDATION", "Ruta no válida")
		return "", nil, "", false
	}
	var rp *contracts.RestorePointView
	err := s.Store.View(r.Context(), func(st *store.State) error {
		if x := st.RestorePoints[id]; x != nil {
			v := s.restorePointView(st, x)
			rp = &v
		}
		return nil
	})
	if err != nil || rp == nil {
		s.problem(w, r, http.StatusNotFound, "E_NOT_FOUND", "Punto de recuperación no encontrado")
		return "", nil, "", false
	}
	objRoot := rp.RootObjectID
	if objRoot == "" {
		objRoot = rp.SnapshotID
	}
	if rawPath != "" {
		objRoot += "/" + rawPath
	}
	return rawPath, rp, objRoot, true
}

func (s *Server) requireServerSideRepo(w http.ResponseWriter, r *http.Request) bool {
	if s.Kopia == nil || !s.Kopia.ServerSide() {
		s.problem(w, r, http.StatusConflict, "E_REPO_UNCONFIGURED",
			"El plano de control no tiene acceso directo al repositorio; la operación no está disponible")
		return false
	}
	return true
}

// shortID recorta un ID para nombres de fichero sin riesgo de rango.
func shortID(id string) string {
	if len(id) > 8 {
		return id[:8]
	}
	return id
}

// fileLister devuelve el acceso de lectura al snapshot: el sustituto de tests
// si está puesto, o la conexión directa a Kopia si es usable.
func (s *Server) fileLister() DirLister {
	if s.DirLister != nil {
		return s.DirLister
	}
	if s.Kopia != nil && s.Kopia.ServerSide() {
		return s.Kopia
	}
	return nil
}

func atoiClamp(s string, min, max int) int {
	v := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0
		}
		v = v*10 + int(c-'0')
		if v > max {
			return max
		}
	}
	if v < min {
		return min
	}
	return v
}

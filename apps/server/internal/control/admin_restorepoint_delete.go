// Eliminación de puntos de restauración (backups) en el almacenamiento S3 o
// físico: borra el snapshot de Kopia (file) o los objetos del manifiesto en
// el destino (image/vm) y retira el registro del catálogo. Con Object Lock
// GOVERNANCE el borrado usa bypass de governance; si las credenciales del
// destino no lo permiten, la llamada S3 falla y el punto se conserva.
package control

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/juansoler/bdr/apps/server/internal/store"
	"github.com/juansoler/bdr/packages/contracts"
	pkgimg "github.com/juansoler/bdr/packages/image"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// restorePointDelete procesa DELETE /api/v1/admin/restore-points/{id}.
// Requiere rol admin: borra datos de backup de forma irreversible.
func (s *Server) restorePointDelete(w http.ResponseWriter, r *http.Request, id string) {
	if !allowMethod(w, r, http.MethodDelete) {
		return
	}
	r, a, ok := s.authorize(w, r, "admin")
	if !ok {
		return
	}
	// Copia del punto y del destino fuera de la transacción: el borrado
	// toca repositorios Kopia y almacenamiento externo.
	var rp store.RestorePoint
	var target *store.StorageTarget // nil si el punto vive en el repo central
	err := s.Store.View(r.Context(), func(st *store.State) error {
		x := st.RestorePoints[id]
		if x == nil {
			return store.ErrNotFound
		}
		rp = *x
		if rp.TargetID != "" {
			t := st.Storage[rp.TargetID]
			if t == nil {
				return fmt.Errorf("el destino de almacenamiento del punto (%s) ya no existe: borra los objetos a mano o registra el destino con el mismo nombre", rp.TargetID)
			}
			c := *t
			target = &c
		}
		return nil
	})
	if err != nil {
		if err == store.ErrNotFound {
			s.problem(w, r, http.StatusNotFound, "E_NOT_FOUND", "Punto de restauración no encontrado")
		} else {
			s.problem(w, r, http.StatusNotFound, "E_NOT_FOUND", err.Error())
		}
		return
	}

	var detail string
	switch rp.Kind {
	case "file":
		detail, err = s.deleteFileRestorePoint(r.Context(), &rp, target)
	case "image", "vm", "lxc":
		detail, err = s.deleteObjectsRestorePoint(r.Context(), target, rp.SnapshotID)
	default:
		s.problem(w, r, http.StatusBadRequest, "E_VALIDATION",
			"tipo de punto de restauración no soportado: "+rp.Kind)
		return
	}
	if err != nil {
		s.problem(w, r, http.StatusBadGateway, "E_DELETE_FAILED", err.Error())
		return
	}

	// Retira el registro del catálogo (la auditoría conserva la acción).
	if err := s.Store.Update(r.Context(), func(st *store.State) error {
		delete(st.RestorePoints, id)
		return nil
	}); err != nil {
		s.internalError(w, r, err)
		return
	}
	s.audit(r, a, "restore_point.deleted", "restore_point", id, "ok",
		map[string]string{"kind": rp.Kind, "detail": detail})
	writeJSON(w, http.StatusOK, map[string]any{"id": id, "kind": rp.Kind, "detail": detail})
}

// deleteFileRestorePoint borra el snapshot de Kopia de un punto "file" en el
// repositorio central (conexión directa del plano de control) o en el repo
// del destino (config dedicada por destino).
func (s *Server) deleteFileRestorePoint(ctx context.Context, rp *store.RestorePoint, target *store.StorageTarget) (string, error) {
	if s.Kopia == nil || !s.Kopia.ServerSide() {
		return "", fmt.Errorf("borrar puntos del repositorio requiere acceso directo del plano de control al repositorio")
	}
	if target == nil {
		if _, err := s.Kopia.RunRaw(ctx, 5*time.Minute,
			"snapshot", "delete", rp.SnapshotID, "--delete"); err != nil {
			return "", fmt.Errorf("borrar snapshot %s: %s", rp.SnapshotID, firstLines(err.Error(), 2))
		}
		return "snapshot " + rp.SnapshotID + " borrado del repositorio central", nil
	}
	// Repo del destino: aprovisiona (idempotente) para asegurar la config
	// local y borra el snapshot con esa config.
	pw, err := s.storageEnsureRepo(ctx, target)
	if err != nil {
		return "", fmt.Errorf("preparar el destino %s: %w", target.Name, err)
	}
	cfgFile := filepath.Join(filepath.Dir(s.Kopia.ConfigFile), "target-"+target.ID+".config")
	if _, err := s.Kopia.RunRepoOp(ctx, 5*time.Minute, cfgFile, pw,
		"snapshot", "delete", rp.SnapshotID, "--delete"); err != nil {
		return "", fmt.Errorf("borrar snapshot %s en el destino %s: %s",
			rp.SnapshotID, target.Name, firstLines(err.Error(), 2))
	}
	return "snapshot " + rp.SnapshotID + " borrado del destino " + target.Name, nil
}

// deleteObjectsRestorePoint borra los objetos (manifiesto + segmentos) de un
// punto image/vm/lxc en el destino S3, filesystem o SMB. Los objetos bajo
// Object Lock GOVERNANCE se borran con bypass si las credenciales lo permiten.
func (s *Server) deleteObjectsRestorePoint(ctx context.Context, target *store.StorageTarget, manifestKey string) (string, error) {
	if target == nil {
		return "", fmt.Errorf("el punto no tiene destino de almacenamiento asociado")
	}
	if !validObjectKey(manifestKey) {
		return "", fmt.Errorf("clave del manifiesto inválida: %q", manifestKey)
	}
	keys := []string{manifestKey}
	// Segmentos referenciados por el manifiesto (best-effort: si el
	// manifiesto no se puede leer, se borra solo el propio manifiesto).
	if data, err := s.readTargetObject(ctx, target, manifestKey); err == nil {
		var m pkgimg.Manifest
		if json.Unmarshal(data, &m) == nil {
			for _, st := range m.Streams {
				if validObjectKey(st.Blob) && st.Blob != manifestKey {
					keys = append(keys, st.Blob)
				}
			}
		}
	}

	var deleted int
	var failures []string
	switch {
	case target.IsSMB():
		deleted, failures = s.smbDeleteObjects(ctx, target, keys)
	case target.IsFilesystem():
		root := target.RepoPath()
		for _, k := range keys {
			if err := os.Remove(filepath.Join(root, k)); err != nil && !os.IsNotExist(err) {
				failures = append(failures, k+": "+err.Error())
				continue
			}
			deleted++
		}
	default:
		sk, err := s.Box.Open(target.SecretKeyEnc)
		if err != nil {
			return "", fmt.Errorf("no se pudo descifrar el secreto del destino")
		}
		mc, err := minioClientFor(ctx, target, sk)
		if err != nil {
			return "", err
		}
		ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
		defer cancel()
		for _, k := range keys {
			if err := mc.RemoveObject(ctx, target.Bucket, k, minio.RemoveObjectOptions{
				GovernanceBypass: target.ObjectLock,
			}); err != nil {
				failures = append(failures, k+": "+firstLines(err.Error(), 1))
				continue
			}
			deleted++
		}
	}
	if len(failures) > 0 {
		return "", fmt.Errorf("no se pudieron borrar %d de %d objetos del destino %s%s",
			len(failures), len(keys), target.Name, lockHint(failures))
	}
	return fmt.Sprintf("%d objetos borrados del destino %s", deleted, target.Name), nil
}

// readTargetObject lee un objeto del destino (S3, filesystem o SMB).
func (s *Server) readTargetObject(ctx context.Context, t *store.StorageTarget, key string) ([]byte, error) {
	if !validObjectKey(key) {
		return nil, fmt.Errorf("clave inválida: %q", key)
	}
	if t.IsSMB() {
		return s.smbReadObject(ctx, t, key)
	}
	if t.IsFilesystem() {
		return os.ReadFile(filepath.Join(t.RepoPath(), key))
	}
	sk, err := s.Box.Open(t.SecretKeyEnc)
	if err != nil {
		return nil, fmt.Errorf("no se pudo descifrar el secreto del destino")
	}
	region := t.Region
	if region == "" {
		region = contracts.DetectS3Region(t.Endpoint)
	}
	mc, err := minio.New(t.Endpoint, &minio.Options{
		Creds:        credentials.NewStaticV4(t.AccessKey, sk, ""),
		Secure:       !t.NoTLS,
		Region:       region,
		BucketLookup: minio.BucketLookupPath,
	})
	if err != nil {
		return nil, fmt.Errorf("endpoint del destino inválido")
	}
	obj, err := mc.GetObject(ctx, t.Bucket, key, minio.GetObjectOptions{})
	if err != nil {
		return nil, err
	}
	defer obj.Close()
	return io.ReadAll(obj)
}

// validObjectKey evita recorridos de ruta (../) en claves de objetos de
// destinos filesystem; en S3 es defensiva (las claves vienen del catálogo).
func validObjectKey(key string) bool {
	if key == "" || strings.HasPrefix(key, "/") || strings.ContainsRune(key, 0) {
		return false
	}
	for _, part := range strings.Split(key, "/") {
		if part == "" || part == "." || part == ".." {
			return false
		}
	}
	return true
}

// minioClientFor construye el cliente S3 de un destino con el secreto ya
// descifrado (región autodetectada del endpoint si no viene).
func minioClientFor(ctx context.Context, t *store.StorageTarget, secret string) (*minio.Client, error) {
	region := t.Region
	if region == "" {
		region = contracts.DetectS3Region(t.Endpoint)
	}
	mc, err := minio.New(t.Endpoint, &minio.Options{
		Creds:        credentials.NewStaticV4(t.AccessKey, secret, ""),
		Secure:       !t.NoTLS,
		Region:       region,
		BucketLookup: minio.BucketLookupPath,
	})
	if err != nil {
		return nil, fmt.Errorf("endpoint del destino inválido")
	}
	return mc, nil
}

// lockHint añade contexto si los fallos parecen debidos a Object Lock.
func lockHint(failures []string) string {
	for _, f := range failures {
		if strings.Contains(strings.ToLower(f), "retention") ||
			strings.Contains(strings.ToLower(f), "governance") ||
			strings.Contains(strings.ToLower(f), "object lock") ||
			strings.Contains(strings.ToLower(f), "locked") {
			return "; los objetos están retenidos por Object Lock hasta que expire la retención (o usa credenciales con permiso de bypass)"
		}
	}
	return ""
}

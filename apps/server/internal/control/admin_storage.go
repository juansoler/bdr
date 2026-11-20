// Destinos de almacenamiento para la réplica de los backups (docs/15):
// registro persistente de remotes S3-compatible o de rutas físicas, con
// credenciales cifradas en S3, verificación de conexión/Object Lock y
// réplica del repositorio del plano de control vía `kopia repository
// sync-to` (manual o programada).
package control

import (
	"context"
	"net/http"
	"net/url"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/juansoler/bdr/apps/server/internal/store"
	"github.com/juansoler/bdr/packages/contracts"
)

func (s *Server) handleStorageTargets(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/api/v1/admin/storage/targets")
	if rest == "" || rest == "/" {
		switch r.Method {
		case http.MethodGet:
			s.storageList(w, r)
		case http.MethodPost:
			s.storageCreate(w, r)
		default:
			s.problem(w, r, http.StatusMethodNotAllowed, "E_METHOD", "Método no permitido")
		}
		return
	}
	parts := strings.Split(strings.Trim(rest, "/"), "/")
	id := parts[0]
	switch {
	case len(parts) == 1 && r.Method == http.MethodPut:
		s.storageUpdate(w, r, id)
	case len(parts) == 1 && r.Method == http.MethodDelete:
		s.storageDelete(w, r, id)
	case len(parts) == 2 && parts[1] == "verify":
		s.storageVerify(w, r, id)
	case len(parts) == 2 && parts[1] == "sync":
		s.storageSync(w, r, id)
	default:
		s.problem(w, r, http.StatusNotFound, "E_NOT_FOUND", "Ruta no encontrada")
	}
}

func storageView(t *store.StorageTarget) contracts.StorageTargetView {
	return contracts.StorageTargetView{
		ID: t.ID, Name: t.Name, Kind: t.KindName(),
		Endpoint: t.Endpoint, Bucket: t.Bucket,
		Prefix: t.Prefix, Path: t.Path, AccessKey: t.AccessKey, NoTLS: t.NoTLS,
		SMB: t.IsSMB(), SMBUser: t.SMBUser, SMBDomain: t.SMBDomain,
		ObjectLock: t.ObjectLock, RetentionDays: t.RetentionDays,
		SyncIntervalMinutes: t.SyncIntervalMinutes, Enabled: t.Enabled,
		CreatedAt: t.CreatedAt, LastSyncAt: t.LastSyncAt,
		LastSyncStatus: t.LastSyncStatus, LastVerifyAt: t.LastVerifyAt,
		LastVerifyStatus: t.LastVerifyStatus,
	}
}

func (s *Server) storageList(w http.ResponseWriter, r *http.Request) {
	if !allowMethod(w, r, http.MethodGet) {
		return
	}
	r, _, ok := s.authorize(w, r, "read")
	if !ok {
		return
	}
	items := []contracts.StorageTargetView{}
	err := s.Store.View(r.Context(), func(st *store.State) error {
		for _, t := range st.Storage {
			items = append(items, storageView(t))
		}
		return nil
	})
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Name < items[j].Name })
	writeJSON(w, http.StatusOK, contracts.StorageTargetList{Items: items})
}

// parseEndpoint separa el esquema del endpoint: devuelve host:puerto y TLS.
func parseEndpoint(endpoint string) (string, bool) {
	e := strings.TrimSpace(endpoint)
	if strings.Contains(e, "://") {
		u, err := url.Parse(e)
		if err != nil {
			return strings.TrimPrefix(e, "https://"), false
		}
		return u.Host, u.Scheme == "https"
	}
	return e, true
}

func (s *Server) storageCreate(w http.ResponseWriter, r *http.Request) {
	if !allowMethod(w, r, http.MethodPost) {
		return
	}
	r, a, ok := s.authorize(w, r, "admin")
	if !ok {
		return
	}
	id, _, status, _ := s.storageUpsert(w, r, "", a)
	if status != 0 {
		return
	}
	s.audit(r, a, "storage_target.created", "storage_target", id, "ok", nil)
	writeJSON(w, http.StatusCreated, map[string]string{"id": id})
}

func (s *Server) storageUpdate(w http.ResponseWriter, r *http.Request, id string) {
	if !allowMethod(w, r, http.MethodPut) {
		return
	}
	r, a, ok := s.authorize(w, r, "admin")
	if !ok {
		return
	}
	_, _, status, _ := s.storageUpsert(w, r, id, a)
	if status != 0 {
		return
	}
	s.audit(r, a, "storage_target.updated", "storage_target", id, "ok", nil)
	w.WriteHeader(http.StatusNoContent)
}

// storageUpsert valida y persiste la creación/actualización de un destino
// S3 o de ruta física. Devuelve (id, record, httpStatusDeError, mensaje).
func (s *Server) storageUpsert(w http.ResponseWriter, r *http.Request, id string, a *actor) (string, *store.StorageTarget, int, string) {
	if s.Box == nil {
		s.problem(w, r, http.StatusServiceUnavailable, "E_SECRETS_UNCONFIGURED",
			"Sin caja de secretos no se pueden guardar credenciales de almacenamiento")
		return "", nil, http.StatusServiceUnavailable, ""
	}
	var req contracts.StorageTargetRequest
	if !decodeJSON(w, r, &req, maxBodyAdmin) {
		return "", nil, http.StatusBadRequest, ""
	}
	req.Name = strings.TrimSpace(req.Name)
	req.Bucket = strings.TrimSpace(req.Bucket)
	req.Prefix = strings.Trim(req.Prefix, "/")
	req.Path = strings.TrimSpace(req.Path)
	req.AccessKey = strings.TrimSpace(req.AccessKey)
	kindExplicit := req.Kind != ""
	if req.Kind == "" {
		req.Kind = contracts.RepoKindS3
	}
	// PUT sin kind explícito: conserva el tipo del destino existente
	// (compatibilidad con clientes anteriores a 0.8).
	if !kindExplicit && id != "" {
		_ = s.Store.View(r.Context(), func(st *store.State) error {
			if t := st.Storage[id]; t != nil {
				req.Kind = t.KindName()
			}
			return nil
		})
	}
	if req.Kind != contracts.RepoKindS3 && req.Kind != contracts.RepoKindFilesystem {
		s.problem(w, r, http.StatusBadRequest, "E_VALIDATION",
			"kind debe ser \"s3\" o \"filesystem\"")
		return "", nil, http.StatusBadRequest, ""
	}
	enc := ""
	smbEnc := ""
	if req.Kind == contracts.RepoKindFilesystem {
		// Ruta física: requiere nombre y ruta absoluta; sin credenciales S3.
		if req.Name == "" || req.Path == "" {
			s.problem(w, r, http.StatusBadRequest, "E_VALIDATION",
				"Se requieren name y path para un destino de ruta física")
			return "", nil, http.StatusBadRequest, ""
		}
		// Intención SMB: prefijo "//" o "smb://" exige recurso compartido.
		isSMB := false
		if strings.HasPrefix(req.Path, "//") || strings.HasPrefix(strings.ToLower(req.Path), "smb://") {
			info, ok := contracts.ParseSMBPath(req.Path)
			if !ok || info.Share == "" {
				s.problem(w, r, http.StatusBadRequest, "E_VALIDATION",
					"la ruta SMB debe incluir el recurso compartido (p. ej. //servidor/recurso o smb://servidor/recurso/datos)")
				return "", nil, http.StatusBadRequest, ""
			}
			isSMB = true
		} else if !filepath.IsAbs(req.Path) {
			s.problem(w, r, http.StatusBadRequest, "E_VALIDATION",
				"path debe ser una ruta absoluta (p. ej. /mnt/backups) o una compartida SMB (//servidor/recurso)")
			return "", nil, http.StatusBadRequest, ""
		}
		if isSMB {
			req.Path = contracts.NormalizeSMBPath(req.Path)
			if req.SMBPassword != "" {
				e, err := s.Box.Seal(req.SMBPassword)
				if err != nil {
					s.internalError(w, r, err)
					return "", nil, http.StatusInternalServerError, ""
				}
				smbEnc = e
			}
		}
		req.ObjectLock, req.RetentionDays = false, 0 // sin Object Lock en filesystem
	} else {
		host, _ := parseEndpoint(req.Endpoint)
		if req.Name == "" || host == "" || req.Bucket == "" || req.AccessKey == "" ||
			(id == "" && req.SecretKey == "") {
			s.problem(w, r, http.StatusBadRequest, "E_VALIDATION",
				"Se requieren name, endpoint, bucket, access_key y secret_key")
			return "", nil, http.StatusBadRequest, ""
		}
		if req.SecretKey != "" {
			e, err := s.Box.Seal(req.SecretKey)
			if err != nil {
				s.internalError(w, r, err)
				return "", nil, http.StatusInternalServerError, ""
			}
			enc = e
		}
		// Nombre de bucket S3 válido (RFC 1123): sin "/" — en S3 no hay
		// carpetas y el prefijo es parte del nombre del objeto. Un "/" en el
		// bucket llega al proveedor como nombre inválido (InvalidRequest) y
		// la prueba de escritura falla sin pista.
		if strings.Contains(req.Bucket, "/") || strings.Contains(req.Bucket, " ") {
			s.problem(w, r, http.StatusBadRequest, "E_VALIDATION",
				"el bucket no puede contener \"/\" ni espacios (usa a-z, 0-9 y \"-\"); para separar datos usa buckets distintos o el campo prefix")
			return "", nil, http.StatusBadRequest, ""
		}
	}
	now := s.Now().UTC()
	idOut := id
	if err := s.Store.Update(r.Context(), func(st *store.State) error {
		var t *store.StorageTarget
		if id != "" {
			t = st.Storage[id]
			if t == nil {
				return store.ErrNotFound
			}
		} else {
			nid, err := newID()
			if err != nil {
				return err
			}
			idOut = nid
			t = &store.StorageTarget{ID: nid, CreatedAt: now}
		}
		// Si cambia el tipo o la ubicación de datos, el repo previo del
		// destino ya no sirve: se re-aprovisiona en el próximo uso.
		prevKind, prevPrefix, prevPath := t.Kind, t.Prefix, t.Path
		t.Name = req.Name
		t.Kind = req.Kind
		t.Prefix = req.Prefix
		t.SyncIntervalMinutes = req.SyncIntervalMinutes
		t.Enabled = req.Enabled
		if req.Kind == contracts.RepoKindFilesystem {
			t.Path = req.Path
			t.Endpoint, t.Bucket, t.Region = "", "", ""
			t.AccessKey, t.SecretKeyEnc = "", ""
			t.NoTLS, t.ObjectLock, t.RetentionDays = false, false, 0
			if t.IsSMB() {
				t.SMBUser = strings.TrimSpace(req.SMBUser)
				t.SMBDomain = strings.TrimSpace(req.SMBDomain)
				if smbEnc != "" {
					t.SMBPasswordEnc = smbEnc
				}
			} else {
				t.SMBUser, t.SMBDomain, t.SMBPasswordEnc = "", "", ""
			}
		} else {
			t.Path = ""
			t.SMBUser, t.SMBDomain, t.SMBPasswordEnc = "", "", ""
			t.Endpoint = parseEndpointHost(req.Endpoint)
			// Si el endpoint explicita esquema, manda; si no, manda no_tls de la
			// petición (por defecto TLS).
			t.NoTLS = !parseEndpointTLS(req.Endpoint)
			if !strings.Contains(req.Endpoint, "://") {
				t.NoTLS = req.NoTLS
			}
			t.Bucket = req.Bucket
			t.Region = strings.TrimSpace(req.Region)
			t.AccessKey = req.AccessKey
			if enc != "" {
				t.SecretKeyEnc = enc
			}
			t.ObjectLock = req.ObjectLock
			t.RetentionDays = req.RetentionDays
		}
		if id != "" && (t.Kind != prevKind || t.Prefix != prevPrefix || t.Path != prevPath) {
			t.RepoReady = false
		}
		st.Storage[t.ID] = t
		return nil
	}); err != nil {
		if err == store.ErrNotFound {
			s.problem(w, r, http.StatusNotFound, "E_NOT_FOUND", "Destino no encontrado")
			return "", nil, http.StatusNotFound, ""
		}
		s.internalError(w, r, err)
		return "", nil, http.StatusInternalServerError, ""
	}
	return idOut, nil, 0, ""
}

// parseEndpointTLS: true si el endpoint explicita https (o no explicita esquema).
func parseEndpointTLS(endpoint string) bool {
	if strings.HasPrefix(strings.TrimSpace(endpoint), "http://") {
		return false
	}
	return true
}

// parseEndpointHost devuelve solo el host:puerto del endpoint.
func parseEndpointHost(endpoint string) string {
	h, _ := parseEndpoint(endpoint)
	return h
}

func (s *Server) storageDelete(w http.ResponseWriter, r *http.Request, id string) {
	if !allowMethod(w, r, http.MethodDelete) {
		return
	}
	r, a, ok := s.authorize(w, r, "admin")
	if !ok {
		return
	}
	err := s.Store.Update(r.Context(), func(st *store.State) error {
		if st.Storage[id] == nil {
			return store.ErrNotFound
		}
		delete(st.Storage, id)
		return nil
	})
	if err != nil {
		if err == store.ErrNotFound {
			s.problem(w, r, http.StatusNotFound, "E_NOT_FOUND", "Destino no encontrado")
			return
		}
		s.internalError(w, r, err)
		return
	}
	s.audit(r, a, "storage_target.deleted", "storage_target", id, "ok", nil)
	w.WriteHeader(http.StatusNoContent)
}

// storageVerify comprueba credenciales/bucket y la disponibilidad de Object
// Lock (sube un objeto de prueba con retención GOVERNANCE 1 h si aplica).
func (s *Server) storageVerify(w http.ResponseWriter, r *http.Request, id string) {
	if !allowMethod(w, r, http.MethodPost) {
		return
	}
	r, a, ok := s.authorize(w, r, "operate")
	if !ok {
		return
	}
	var t *store.StorageTarget
	err := s.Store.View(r.Context(), func(st *store.State) error {
		if x := st.Storage[id]; x != nil {
			t = &store.StorageTarget{}
			*t = *x
		}
		return nil
	})
	if err != nil || t == nil {
		s.problem(w, r, http.StatusNotFound, "E_NOT_FOUND", "Destino no encontrado")
		return
	}
	result := s.storageCheck(r.Context(), t)
	now := s.Now().UTC()
	_ = s.Store.Update(r.Context(), func(st *store.State) error {
		if x := st.Storage[id]; x != nil {
			x.LastVerifyAt = &now
			x.LastVerifyStatus = result
		}
		return nil
	})
	s.audit(r, a, "storage_target.verified", "storage_target", id, "ok", map[string]string{"result": result})
	writeJSON(w, http.StatusOK, map[string]any{"id": id, "result": result, "checked_at": now})
}

// storageSync ejecuta la réplica del repositorio del plano de control hacia
// el destino con `kopia repository sync-to s3`.
func (s *Server) storageSync(w http.ResponseWriter, r *http.Request, id string) {
	if !allowMethod(w, r, http.MethodPost) {
		return
	}
	r, a, ok := s.authorize(w, r, "operate")
	if !ok {
		return
	}
	var t *store.StorageTarget
	err := s.Store.View(r.Context(), func(st *store.State) error {
		if x := st.Storage[id]; x != nil {
			t = &store.StorageTarget{}
			*t = *x
		}
		return nil
	})
	if err != nil || t == nil {
		s.problem(w, r, http.StatusNotFound, "E_NOT_FOUND", "Destino no encontrado")
		return
	}
	msg, ok := s.storageRunSync(r.Context(), t)
	now := s.Now().UTC()
	_ = s.Store.Update(r.Context(), func(st *store.State) error {
		if x := st.Storage[id]; x != nil {
			x.LastSyncAt = &now
			x.LastSyncStatus = msg
		}
		return nil
	})
	if !ok {
		s.problem(w, r, http.StatusBadGateway, "E_SYNC_FAILED", "Réplica fallida: "+msg)
		return
	}
	s.audit(r, a, "storage_target.synced", "storage_target", id, "ok", nil)
	writeJSON(w, http.StatusOK, map[string]any{"id": id, "result": msg, "synced_at": now})
}

// syncDueStorage lanza las réplicas programadas vencidas.
func (s *Server) syncDueStorage(ctx context.Context) {
	now := s.Now().UTC()
	var due []*store.StorageTarget
	_ = s.Store.View(ctx, func(st *store.State) error {
		for _, t := range st.Storage {
			if !t.Enabled || t.SyncIntervalMinutes <= 0 {
				continue
			}
			if t.LastSyncAt == nil || now.Sub(*t.LastSyncAt) >= time.Duration(t.SyncIntervalMinutes)*time.Minute {
				c := *t
				due = append(due, &c)
			}
		}
		return nil
	})
	for _, t := range due {
		msg, _ := s.storageRunSync(ctx, t)
		_ = s.Store.Update(ctx, func(st *store.State) error {
			if x := st.Storage[t.ID]; x != nil {
				n := s.Now().UTC()
				x.LastSyncAt = &n
				x.LastSyncStatus = msg
			}
			return nil
		})
	}
}

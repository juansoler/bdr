// Gestión de instancias de Proxmox: registro con token de API (secreto
// cifrado en reposo) e inventario de VMs vía la API de PVE. La ejecución de
// backup/restore de VMs requiere el agente en el nodo PVE (slice posterior).
package control

import (
	"strconv"

	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/juansoler/bdr/apps/server/internal/store"
	"github.com/juansoler/bdr/packages/contracts"
)

func (s *Server) handleProxmoxInstances(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/api/v1/admin/proxmox/instances")
	if rest == "" || rest == "/" {
		switch r.Method {
		case http.MethodGet:
			s.pveInstancesList(w, r)
		case http.MethodPost:
			s.pveInstancesCreate(w, r)
		default:
			s.problem(w, r, http.StatusMethodNotAllowed, "E_METHOD", "Método no permitido")
		}
		return
	}
	parts := strings.Split(strings.Trim(rest, "/"), "/")
	id := parts[0]
	switch {
	case len(parts) == 1 && r.Method == http.MethodDelete:
		s.pveInstancesDelete(w, r, id)
	case len(parts) == 1 && r.Method == http.MethodPut:
		s.pveInstancesUpdate(w, r, id)
	case len(parts) == 2 && parts[1] == "vms" && r.Method == http.MethodGet:
		s.pveInstancesVMs(w, r, id)
	case len(parts) == 4 && parts[1] == "vms" && parts[3] == "backup" && r.Method == http.MethodPost:
		vmid, _ := strconv.Atoi(parts[2])
		if vmid <= 0 {
			s.problem(w, r, http.StatusBadRequest, "E_VALIDATION", "VMID inválido")
			return
		}
		s.pveVMBackup(w, r, id, vmid)
	case len(parts) == 4 && parts[1] == "vms" && parts[3] == "restore-points" && r.Method == http.MethodGet:
		vmid, _ := strconv.Atoi(parts[2])
		if vmid <= 0 {
			s.problem(w, r, http.StatusBadRequest, "E_VALIDATION", "VMID inválido")
			return
		}
		// El namespace de VMID es compartido entre qemu y lxc: filtra ambos
		// tipos de punto para el mismo VMID.
		s.pveVMRestorePoints(w, r, id, strconv.Itoa(vmid))
	case len(parts) == 4 && parts[1] == "lxc" && parts[3] == "backup" && r.Method == http.MethodPost:
		vmid, _ := strconv.Atoi(parts[2])
		if vmid <= 0 {
			s.problem(w, r, http.StatusBadRequest, "E_VALIDATION", "VMID inválido")
			return
		}
		s.pveLXCBackup(w, r, id, vmid)
	case len(parts) == 4 && parts[1] == "lxc" && parts[3] == "restore-points" && r.Method == http.MethodGet:
		vmid, _ := strconv.Atoi(parts[2])
		if vmid <= 0 {
			s.problem(w, r, http.StatusBadRequest, "E_VALIDATION", "VMID inválido")
			return
		}
		s.pveVMRestorePoints(w, r, id, strconv.Itoa(vmid))
	case len(parts) == 2 && parts[1] == "storage" && r.Method == http.MethodGet:
		s.handleProxmoxStorage(w, r, id)
	case len(parts) == 2 && parts[1] == "manifest" && r.Method == http.MethodGet:
		s.handleProxmoxManifest(w, r, id)
	default:
		s.problem(w, r, http.StatusNotFound, "E_NOT_FOUND", "Ruta no encontrada")
	}
}

func (s *Server) pveInstancesList(w http.ResponseWriter, r *http.Request) {
	if !allowMethod(w, r, http.MethodGet) {
		return
	}
	r, _, ok := s.authorize(w, r, "read")
	if !ok {
		return
	}
	var out []contracts.PVEVMList
	err := s.Store.View(r.Context(), func(st *store.State) error {
		for _, p := range st.PVE {
			out = append(out, contracts.PVEVMList{InstanceID: p.ID, Name: p.Name, URL: p.URL, LastCheck: p.LastCheck})
		}
		sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
		return nil
	})
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	if out == nil {
		out = []contracts.PVEVMList{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": out})
}

func (s *Server) pveInstancesCreate(w http.ResponseWriter, r *http.Request) {
	if !allowMethod(w, r, http.MethodPost) {
		return
	}
	r, a, ok := s.authorize(w, r, "admin")
	if !ok {
		return
	}
	if s.Box == nil {
		s.problem(w, r, http.StatusServiceUnavailable, "E_SECRETS_UNCONFIGURED", "Sin caja de secretos no se pueden guardar credenciales de Proxmox")
		return
	}
	var req contracts.PVEInstanceRequest
	if !decodeJSON(w, r, &req, maxBodyAdmin) {
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	req.URL = strings.TrimRight(strings.TrimSpace(req.URL), "/")
	req.TokenUser = strings.TrimSpace(req.TokenUser)
	req.TokenID = strings.TrimSpace(req.TokenID)
	// La API de Proxmox siempre es HTTPS; se permite http solo para
	// hosts de bucle local (desarrollo y tests).
	httpOK := false
	if u, perr := url.Parse(req.URL); perr == nil {
		httpOK = u.Hostname() == "127.0.0.1" || u.Hostname() == "localhost" || strings.HasSuffix(u.Hostname(), ".localhost")
	}
	if req.Name == "" || req.URL == "" || !strings.HasPrefix(req.URL, "https://") && !httpOK ||
		req.TokenUser == "" || req.TokenID == "" || req.TokenSecret == "" {
		s.problem(w, r, http.StatusBadRequest, "E_VALIDATION",
			"Se requieren name, url (https), token_user (usuario@realm), token_id y token_secret")
		return
	}
	if s.Box == nil {
		return
	}
	enc, err := s.Box.Seal(req.TokenSecret)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	id, err := newID()
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	now := s.Now().UTC()
	rec := &store.PVEInstance{
		ID: id, Name: req.Name, URL: req.URL,
		TokenUser: req.TokenUser, TokenID: req.TokenID,
		TokenSecretEnc: enc, VerifyTLS: req.VerifyTLS,
		CreatedAt: now, LastCheck: "sin verificar",
	}
	if err := s.Store.Update(r.Context(), func(st *store.State) error {
		st.PVE[id] = rec
		return nil
	}); err != nil {
		s.internalError(w, r, err)
		return
	}
	s.audit(r, a, "proxmox_instance.created", "proxmox_instance", id, "ok", map[string]string{"name": req.Name})
	writeJSON(w, http.StatusCreated, contracts.PVEVMList{InstanceID: id, Name: req.Name, URL: req.URL, LastCheck: rec.LastCheck})
}

func (s *Server) pveInstancesDelete(w http.ResponseWriter, r *http.Request, id string) {
	if !allowMethod(w, r, http.MethodDelete) {
		return
	}
	r, a, ok := s.authorize(w, r, "admin")
	if !ok {
		return
	}
	err := s.Store.Update(r.Context(), func(st *store.State) error {
		if st.PVE[id] == nil {
			return store.ErrNotFound
		}
		delete(st.PVE, id)
		return nil
	})
	if err != nil {
		if err == store.ErrNotFound {
			s.problem(w, r, http.StatusNotFound, "E_NOT_FOUND", "Instancia no encontrada")
			return
		}
		s.internalError(w, r, err)
		return
	}
	s.audit(r, a, "proxmox_instance.deleted", "proxmox_instance", id, "ok", nil)
	w.WriteHeader(http.StatusNoContent)
}

// pveClient construye la petición autenticada contra la API de Proxmox.
func (s *Server) pveClient(ctx context.Context, p *store.PVEInstance, path string) ([]byte, int, error) {
	secret, err := s.Box.Open(p.TokenSecretEnc)
	if err != nil {
		return nil, 0, fmt.Errorf("no se pudo descifrar el secreto del token: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.URL+path, nil)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Authorization", "PVEAPIToken="+p.TokenUser+"!"+p.TokenID+"="+secret)
	client := &http.Client{Timeout: 20 * time.Second}
	if !p.VerifyTLS {
		// Los certificados de Proxmox son autofirmados por defecto; el
		// operador activa VerifyTLS si instala la CA del nodo.
		client.Transport = &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	return b, resp.StatusCode, err
}

// pveInstancesVMs devuelve el inventario de VMs/LXC de la instancia.
// GET /admin/proxmox/instances/{id}/vms
func (s *Server) pveInstancesVMs(w http.ResponseWriter, r *http.Request, id string) {
	if !allowMethod(w, r, http.MethodGet) {
		return
	}
	r, a, ok := s.authorize(w, r, "read")
	if !ok {
		return
	}
	var p *store.PVEInstance
	err := s.Store.View(r.Context(), func(st *store.State) error {
		if x := st.PVE[id]; x != nil {
			p = &store.PVEInstance{}
			*p = *x
		}
		return nil
	})
	if err != nil || p == nil {
		s.problem(w, r, http.StatusNotFound, "E_NOT_FOUND", "Instancia de Proxmox no encontrada")
		return
	}
	body, status, err := s.pveClient(r.Context(), p, "/api2/json/cluster/resources?type=vm")
	now := s.Now().UTC()
	_ = s.Store.Update(context.Background(), func(st *store.State) error {
		if x := st.PVE[id]; x != nil {
			x.LastVerifiedAt = &now
			msg := "ok"
			if err != nil {
				msg = "error: " + firstLines(err.Error(), 1)
			} else if status != 200 {
				msg = fmt.Sprintf("error: HTTP %d", status)
			}
			x.LastCheck = msg
		}
		return nil
	})
	if err != nil {
		s.problem(w, r, http.StatusBadGateway, "E_PVE_UNREACHABLE",
			"La API de Proxmox no respondió correctamente (HTTP "+fmt.Sprint(status)+"): "+firstLines(err.Error(), 1))
		return
	}
	var parsed struct {
		Data []struct {
			VMID    int    `json:"vmid"`
			Name    string `json:"name"`
			Node    string `json:"node"`
			Status  string `json:"status"`
			Type    string `json:"type"`
			MaxMem  int64  `json:"maxmem"`
			MaxDisk int64  `json:"maxdisk"`
			Cores   int    `json:"maxcpu"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		s.problem(w, r, http.StatusBadGateway, "E_PVE_PARSE", "Respuesta de Proxmox no reconocida")
		return
	}
	items := make([]contracts.PVEVM, 0, len(parsed.Data))
	for _, v := range parsed.Data {
		items = append(items, contracts.PVEVM{
			VMID: v.VMID, Name: v.Name, Node: v.Node, Status: v.Status, Type: v.Type,
			MemMiB: v.MaxMem / (1024 * 1024), DiskGiB: v.MaxDisk / (1024 * 1024 * 1024), Cores: v.Cores,
		})
	}
	sort.Slice(items, func(i, j int) bool { return items[i].VMID < items[j].VMID })
	s.audit(r, a, "proxmox.vms_listed", "proxmox_instance", id, "ok", map[string]string{"vms": fmt.Sprint(len(items))})
	writeJSON(w, http.StatusOK, contracts.PVEVMList{
		InstanceID: id, Name: p.Name, URL: p.URL, Items: items,
		LastCheck: now.Format(time.RFC3339),
	})
}

// pveInstancesUpdate actualiza una instancia (nombre, credenciales, agente
// del nodo). El secreto vacío conserva el actual.
func (s *Server) pveInstancesUpdate(w http.ResponseWriter, r *http.Request, id string) {
	r, a, ok := s.authorize(w, r, "admin")
	if !ok {
		return
	}
	if s.Box == nil {
		s.problem(w, r, http.StatusServiceUnavailable, "E_SECRETS_UNCONFIGURED", "Sin caja de secretos no se pueden guardar credenciales de Proxmox")
		return
	}
	var req contracts.PVEInstanceRequest
	if !decodeJSON(w, r, &req, maxBodyAdmin) {
		return
	}
	var out *store.PVEInstance
	err := s.Store.Update(r.Context(), func(st *store.State) error {
		p := st.PVE[id]
		if p == nil {
			return store.ErrNotFound
		}
		if req.Name != "" {
			p.Name = req.Name
		}
		if req.URL != "" {
			p.URL = req.URL
		}
		if req.TokenUser != "" {
			p.TokenUser = req.TokenUser
		}
		if req.TokenID != "" {
			p.TokenID = req.TokenID
		}
		if req.TokenSecret != "" {
			p.TokenSecretEnc = s.Box.MustSeal(req.TokenSecret)
		}
		if req.AgentID != "" {
			a := st.Agents[req.AgentID]
			if a == nil || a.RevokedAt != nil {
				return fmt.Errorf("el agente %s no existe o está revocado", req.AgentID)
			}
			p.AgentID = req.AgentID
		}
		p.LastVerifiedAt = nil
		out = p
		return nil
	})
	if err == store.ErrNotFound {
		s.problem(w, r, http.StatusNotFound, "E_NOT_FOUND", "Instancia no encontrada")
		return
	}
	if err != nil {
		s.problem(w, r, http.StatusBadRequest, "E_VALIDATION", err.Error())
		return
	}
	s.audit(r, a, "proxmox_instance.updated", "proxmox_instance", id, "ok", map[string]string{"agent": out.AgentID})
	writeJSON(w, http.StatusOK, contracts.PVEVMList{
		InstanceID: out.ID, Name: out.Name, URL: out.URL,
		LastCheck: out.LastCheck, AgentID: out.AgentID,
	})
}

// pveInstanceAgent exige que la instancia tenga agente vinculado.
func (s *Server) pveInstanceAgent(st *store.State, id string) (*store.PVEInstance, store.Agent, error) {
	p := st.PVE[id]
	if p == nil {
		return nil, store.Agent{}, store.ErrNotFound
	}
	if p.AgentID == "" {
		return nil, store.Agent{}, fmt.Errorf("la instancia no tiene agente en el nodo (edítala y vincula uno)")
	}
	a := st.Agents[p.AgentID]
	if a == nil || a.RevokedAt != nil {
		return nil, store.Agent{}, fmt.Errorf("el agente vinculado no está disponible")
	}
	return p, *a, nil
}

// pveVMBackup dispara un BACKUP_VM en el agente del nodo.
func (s *Server) pveVMBackup(w http.ResponseWriter, r *http.Request, instanceID string, vmid int) {
	var req contracts.BackupVMRequest
	if !decodeJSON(w, r, &req, maxBodyAdmin) {
		return
	}
	if req.StorageTargetID == "" {
		s.problem(w, r, http.StatusBadRequest, "E_VALIDATION", "El backup de VM requiere un destino de almacenamiento")
		return
	}
	r, a, ok := s.authorize(w, r, "operate")
	if !ok {
		return
	}
	var jobID string
	err := s.Store.Update(r.Context(), func(st *store.State) error {
		p, agent, err := s.pveInstanceAgent(st, instanceID)
		if err != nil {
			return err
		}
		tg := st.Storage[req.StorageTargetID]
		if tg == nil || !tg.Enabled {
			return fmt.Errorf("el destino de almacenamiento no existe o está deshabilitado")
		}
		// Validación fail-fast: solo VMs qemu en esta ruta (los LXC usan
		// /lxc/{vmid}/backup); obtenemos nodo y nombre del inventario.
		vmName, node, verr := s.pveGuestInfo(r.Context(), p, vmid, "qemu")
		if verr != nil {
			return verr
		}
		now := s.Now().UTC()
		payload, _ := json.Marshal(contracts.BackupVMJobPayload{
			InstanceID: p.ID, VMID: vmid, Name: vmName, Node: node,
			StorageTargetID: tg.ID, RetentionDays: tg.RetentionDays,
		})
		id, _ := newID()
		j := &store.Job{
			ID: id, Type: contracts.JobTypeBackupVM, State: store.JobPending,
			AgentID: agent.ID, Trigger: "manual",
			Payload: payload, MaxAttempts: 3, CreatedBy: a.auditName(),
			CreatedAt: now, UpdatedAt: now,
		}
		st.Jobs[id] = j
		st.AppendJobEvent(contracts.JobEvent{JobID: id, Kind: "state", State: store.JobPending, At: now})
		jobID = id
		return nil
	})
	if err != nil {
		s.problem(w, r, http.StatusBadRequest, "E_VALIDATION", err.Error())
		return
	}
	s.audit(r, a, "proxmox.vm.backup", "proxmox_vm", fmt.Sprintf("%s/%d", instanceID, vmid), "ok", map[string]string{"job": jobID})
	writeJSON(w, http.StatusAccepted, contracts.BackupResponse{JobID: jobID, State: store.JobPending})
}

// pveLXCBackup dispara un BACKUP_LXC en el agente del nodo.
// POST /api/v1/admin/proxmox/instances/{id}/lxc/{vmid}/backup
func (s *Server) pveLXCBackup(w http.ResponseWriter, r *http.Request, instanceID string, vmid int) {
	var req contracts.BackupVMRequest
	if !decodeJSON(w, r, &req, maxBodyAdmin) {
		return
	}
	if req.StorageTargetID == "" {
		s.problem(w, r, http.StatusBadRequest, "E_VALIDATION", "El backup de LXC requiere un destino de almacenamiento")
		return
	}
	r, a, ok := s.authorize(w, r, "operate")
	if !ok {
		return
	}
	var jobID string
	err := s.Store.Update(r.Context(), func(st *store.State) error {
		p, agent, err := s.pveInstanceAgent(st, instanceID)
		if err != nil {
			return err
		}
		tg := st.Storage[req.StorageTargetID]
		if tg == nil || !tg.Enabled {
			return fmt.Errorf("el destino de almacenamiento no existe o está deshabilitado")
		}
		ctName, node, verr := s.pveGuestInfo(r.Context(), p, vmid, "lxc")
		if verr != nil {
			return verr
		}
		now := s.Now().UTC()
		payload, _ := json.Marshal(contracts.BackupLXCJobPayload{
			InstanceID: p.ID, VMID: vmid, Name: ctName, Node: node,
			StorageTargetID: tg.ID, RetentionDays: tg.RetentionDays,
		})
		id, _ := newID()
		j := &store.Job{
			ID: id, Type: contracts.JobTypeBackupLXC, State: store.JobPending,
			AgentID: agent.ID, Trigger: "manual",
			Payload: payload, MaxAttempts: 3, CreatedBy: a.auditName(),
			CreatedAt: now, UpdatedAt: now,
		}
		st.Jobs[id] = j
		st.AppendJobEvent(contracts.JobEvent{JobID: id, Kind: "state", State: store.JobPending, At: now})
		jobID = id
		return nil
	})
	if err != nil {
		s.problem(w, r, http.StatusBadRequest, "E_VALIDATION", err.Error())
		return
	}
	s.audit(r, a, "proxmox.lxc.backup", "proxmox_lxc", fmt.Sprintf("%s/%d", instanceID, vmid), "ok", map[string]string{"job": jobID})
	writeJSON(w, http.StatusAccepted, contracts.BackupResponse{JobID: jobID, State: store.JobPending})
}

// pveVMRestorePoints lista los puntos de restauración de imagen de una VM o
// contenedor (el namespace de VMID es compartido en Proxmox).
func (s *Server) pveVMRestorePoints(w http.ResponseWriter, r *http.Request, instanceID, vmid string) {
	r, _, ok := s.authorize(w, r, "read")
	if !ok {
		return
	}
	suffix := "pve/" + instanceID + "/" + vmid
	items := []contracts.RestorePointView{}
	_ = s.Store.View(r.Context(), func(st *store.State) error {
		for _, rp := range st.RestorePoints {
			if rp.Kind != "vm" && rp.Kind != "lxc" {
				continue
			}
			if rp.SourcePath != suffix {
				continue
			}
			items = append(items, s.restorePointView(st, rp))
		}
		return nil
	})
	sort.Slice(items, func(i, j int) bool { return items[i].CreatedAt.After(items[j].CreatedAt) })
	writeJSON(w, http.StatusOK, contracts.VMRestorePointList{Items: items})
}

// handleProxmoxRestore restaura una VM o un contenedor LXC desde un punto
// de restauración. POST /api/v1/admin/proxmox/restore
func (s *Server) handleProxmoxRestore(w http.ResponseWriter, r *http.Request) {
	if !allowMethod(w, r, http.MethodPost) {
		return
	}
	var req contracts.RestoreVMRequest
	if !decodeJSON(w, r, &req, maxBodyAdmin) {
		return
	}
	if req.RestorePointID == "" || req.TargetInstanceID == "" {
		s.problem(w, r, http.StatusBadRequest, "E_VALIDATION", "restore_point_id y target_instance_id son obligatorios")
		return
	}
	r, a, ok := s.authorize(w, r, "operate")
	if !ok {
		return
	}
	var jobID string
	var jobType string
	err := s.Store.Update(r.Context(), func(st *store.State) error {
		rp := st.RestorePoints[req.RestorePointID]
		if rp == nil || rp.SnapshotID == "" {
			return fmt.Errorf("punto de restauración no encontrado")
		}
		if rp.TargetID == "" {
			return fmt.Errorf("el punto de restauración no tiene destino de almacenamiento asociado")
		}
		p, agent, err := s.pveInstanceAgent(st, req.TargetInstanceID)
		if err != nil {
			return err
		}
		mode := req.Mode
		if mode == "" {
			mode = "in_place"
		}
		if mode == "in_place" && req.TargetVMID <= 0 {
			return fmt.Errorf("restauración en sitio requiere target_vmid")
		}
		now := s.Now().UTC()
		var payload []byte
		switch rp.Kind {
		case "vm":
			jobType = contracts.JobTypeRestoreVM
			payload, _ = json.Marshal(contracts.RestoreVMJobPayload{
				InstanceID: p.ID, ManifestKey: rp.SnapshotID,
				StorageTargetID: rp.TargetID,
				Mode:            mode, TargetVMID: req.TargetVMID,
				TargetName: req.TargetName, TargetStorage: req.TargetStorage,
			})
		case "lxc":
			jobType = contracts.JobTypeRestoreLXC
			payload, _ = json.Marshal(contracts.RestoreLXCJobPayload{
				InstanceID: p.ID, ManifestKey: rp.SnapshotID,
				StorageTargetID: rp.TargetID,
				Mode:            mode, TargetVMID: req.TargetVMID,
				TargetName: req.TargetName, TargetStorage: req.TargetStorage,
			})
		default:
			return fmt.Errorf("el punto de restauración no es de una VM o contenedor de Proxmox")
		}
		id, _ := newID()
		j := &store.Job{
			ID: id, Type: jobType, State: store.JobPending,
			AgentID: agent.ID, Trigger: "manual",
			Payload: payload, MaxAttempts: 3, CreatedBy: a.auditName(),
			CreatedAt: now, UpdatedAt: now,
		}
		st.Jobs[id] = j
		st.AppendJobEvent(contracts.JobEvent{JobID: id, Kind: "state", State: store.JobPending, At: now})
		jobID = id
		return nil
	})
	if err != nil {
		s.problem(w, r, http.StatusBadRequest, "E_VALIDATION", err.Error())
		return
	}
	auditAction := "proxmox.vm.restore"
	if jobType == contracts.JobTypeRestoreLXC {
		auditAction = "proxmox.lxc.restore"
	}
	s.audit(r, a, auditAction, "proxmox_guest", req.TargetInstanceID, "ok", map[string]string{"job": jobID})
	writeJSON(w, http.StatusAccepted, contracts.BackupResponse{JobID: jobID, State: store.JobPending})
}

// pveGuestInfo valida que el VMID sea un guest del tipo pedido (qemu o lxc)
// y devuelve nombre y nodo. Consulta el inventario de la API de Proxmox
// (cluster/resources).
func (s *Server) pveGuestInfo(ctx context.Context, p *store.PVEInstance, vmid int, wantType string) (name, node string, err error) {
	body, status, cerr := s.pveClient(ctx, p, "/api2/json/cluster/resources?type=vm")
	if cerr != nil || status != 200 {
		return "", "", fmt.Errorf("no se pudo consultar Proxmox (HTTP %d)", status)
	}
	var parsed struct {
		Data []struct {
			VMID int    `json:"vmid"`
			Name string `json:"name"`
			Node string `json:"node"`
			Type string `json:"type"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return "", "", fmt.Errorf("inventario de Proxmox ilegible")
	}
	for _, v := range parsed.Data {
		if v.VMID == vmid {
			if v.Type != wantType {
				return "", "", fmt.Errorf("la VMID %d es un guest de tipo %q; se esperaba %q", vmid, v.Type, wantType)
			}
			return v.Name, v.Node, nil
		}
	}
	return "", "", fmt.Errorf("la VMID %d no existe en esta instancia de Proxmox", vmid)
}

// handleProxmoxStorage diagnostica los storages de la instancia vía la API
// de PVE: uso real por storage y (para tipo dir) la ruta del directorio de
// contenidos en el nodo — imprescindible para depurar ENOSPC.
// GET /api/v1/admin/proxmox/instances/{id}/storage[?node=pve]
func (s *Server) handleProxmoxStorage(w http.ResponseWriter, r *http.Request, id string) {
	r, _, ok := s.authorize(w, r, "read")
	if !ok {
		return
	}
	var p *store.PVEInstance
	err := s.Store.View(r.Context(), func(st *store.State) error {
		if x := st.PVE[id]; x != nil {
			p = &store.PVEInstance{}
			*p = *x
		}
		return nil
	})
	if err != nil || p == nil {
		s.problem(w, r, http.StatusNotFound, "E_NOT_FOUND", "Instancia de Proxmox no encontrada")
		return
	}
	node := strings.TrimSpace(r.URL.Query().Get("node"))
	if node == "" {
		// Primer nodo del cluster (instancia single-node en este slice).
		body, status, cerr := s.pveClient(r.Context(), p, "/api2/json/nodes")
		if cerr != nil || status != 200 {
			s.problem(w, r, http.StatusBadGateway, "E_PVE_UNREACHABLE", "No se pudo listar los nodos de Proxmox")
			return
		}
		var nodes struct {
			Data []struct {
				Node string `json:"node"`
			} `json:"data"`
		}
		if json.Unmarshal(body, &nodes) != nil || len(nodes.Data) == 0 {
			s.problem(w, r, http.StatusBadGateway, "E_PVE_UNREACHABLE", "Sin nodos en Proxmox")
			return
		}
		node = nodes.Data[0].Node
	}
	body, status, cerr := s.pveClient(r.Context(), p,
		"/api2/json/nodes/"+url.PathEscape(node)+"/storage")
	if cerr != nil || status != 200 {
		s.problem(w, r, http.StatusBadGateway, "E_PVE_UNREACHABLE",
			fmt.Sprintf("No se pudo consultar el storage del nodo (HTTP %d): %s", status, firstLines(cerr.Error(), 1)))
		return
	}
	var parsed struct {
		Data []struct {
			Storage string `json:"storage"`
			Type    string `json:"type"`
			Total   int64  `json:"total"`
			Used    int64  `json:"used"`
			Avail   int64  `json:"avail"`
			Content string `json:"content"`
			Active  int    `json:"active"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		s.internalError(w, r, err)
		return
	}
	type row struct {
		Storage string `json:"storage"`
		Type    string `json:"type"`
		Total   int64  `json:"total"`
		Used    int64  `json:"used"`
		Avail   int64  `json:"avail"`
		Content string `json:"content"`
		Path    string `json:"path,omitempty"`
	}
	items := []row{}
	for _, st := range parsed.Data {
		it := row{Storage: st.Storage, Type: st.Type, Total: st.Total,
			Used: st.Used, Avail: st.Avail, Content: st.Content}
		// Para storages tipo dir, la ruta del directorio de contenidos en el
		// nodo: ahí es donde viven los volúmenes y donde ENOSPC muerde.
		if st.Type == "dir" {
			if sub, sst, serr := s.pveClient(r.Context(), p,
				"/api2/json/nodes/"+url.PathEscape(node)+"/storage/"+url.PathEscape(st.Storage)); serr == nil && sst == 200 {
				var one struct {
					Data struct {
						Path string `json:"path"`
					} `json:"data"`
				}
				if json.Unmarshal(sub, &one) == nil {
					it.Path = one.Data.Path
				}
			}
		}
		items = append(items, it)
	}
	writeJSON(w, http.StatusOK, map[string]any{"node": node, "items": items})
}

// handleProxmoxManifestRoute extrae el ID del punto de restauración de la
// ruta y sirve el manifiesto.
func (s *Server) handleProxmoxManifestRoute(w http.ResponseWriter, r *http.Request) {
	rpID := strings.TrimPrefix(r.URL.Path, "/api/v1/admin/proxmox/manifest/")
	if rpID == "" || strings.Contains(rpID, "/") {
		s.problem(w, r, http.StatusNotFound, "E_NOT_FOUND", "Ruta no encontrada")
		return
	}
	s.handleProxmoxManifest(w, r, rpID)
}

// handleProxmoxManifest devuelve el manifiesto de imagen/VM asociado a un
// punto de restauración (lee el objeto del bucket S3 o de la ruta física
// del destino). GET /api/v1/admin/proxmox/manifest/{rp_id}
func (s *Server) handleProxmoxManifest(w http.ResponseWriter, r *http.Request, rpID string) {
	r, _, ok := s.authorize(w, r, "read")
	if !ok {
		return
	}
	var rp *store.RestorePoint
	var target *store.StorageTarget
	err := s.Store.View(r.Context(), func(st *store.State) error {
		rp = st.RestorePoints[rpID]
		if rp == nil || rp.Kind != "vm" && rp.Kind != "image" && rp.Kind != "lxc" {
			return fmt.Errorf("el punto de restauración no es de imagen/VM/LXC")
		}
		target = st.Storage[rp.TargetID]
		if target == nil {
			return fmt.Errorf("el punto no tiene destino de almacenamiento asociado")
		}
		return nil
	})
	if err != nil {
		s.problem(w, r, http.StatusNotFound, "E_NOT_FOUND", err.Error())
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()
	data, err := s.readTargetObject(ctx, target, rp.SnapshotID)
	if err != nil {
		s.problem(w, r, http.StatusBadGateway, "E_MANIFEST", "No se pudo leer el manifiesto: "+firstLines(err.Error(), 1))
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(data)
}

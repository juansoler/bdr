// Endpoints de administración del motor SQL Server (RF-40/A-11).
//
// El patrón es el de los jobs ad-hoc de VM/LXC: el administrador pide una
// operación sobre una base registrada de un agente, el control plane crea
// un job con payload SQL_* y el agente del host SQL lo ejecuta con SSPI.
// Los resultados de SQL_BACKUP se ingieren como RestorePoints Kind "sql"
// (agent_api.recordSQLRestorePoint) y alimentan el catálogo del panel §23.
package control

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"

	"github.com/juansoler/bdr/apps/server/internal/store"
	"github.com/juansoler/bdr/packages/contracts"
)

// sqlJobRequest es la petición común de los endpoints SQL: agente destino,
// conexión a la instancia y (en backup/restore/verify) destino de storage.
type sqlJobRequest struct {
	AgentID         string `json:"agent_id"`
	Server          string `json:"server"`             // host o host\instancia
	Instance        string `json:"instance,omitempty"` // instancia con nombre
	Database        string `json:"database,omitempty"`
	StorageTargetID string `json:"storage_target_id,omitempty"`
}

// handleSQLBackup crea un job SQL_BACKUP.
// POST /api/v1/admin/sql/backup
func (s *Server) handleSQLBackup(w http.ResponseWriter, r *http.Request) {
	if !allowMethod(w, r, http.MethodPost) {
		return
	}
	r, a, ok := s.authorize(w, r, "operate")
	if !ok {
		return
	}
	var req struct {
		sqlJobRequest
		Type          string `json:"type"` // FULL | DIFF | LOG | COPY_ONLY (se normaliza)
		RetentionDays int    `json:"retention_days,omitempty"`
		Verify        *bool  `json:"verify,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.problem(w, r, http.StatusBadRequest, "E_VALIDATION", "cuerpo inválido")
		return
	}
	if req.AgentID == "" || req.Server == "" || req.Database == "" {
		s.problem(w, r, http.StatusBadRequest, "E_VALIDATION", "agent_id, server y database son obligatorios")
		return
	}
	if req.StorageTargetID == "" {
		s.problem(w, r, http.StatusBadRequest, "E_VALIDATION", "el backup SQL requiere un destino de almacenamiento")
		return
	}
	// Normalización igual que el CLI: "copy-only" → COPY_ONLY.
	backupType := strings.ReplaceAll(strings.ToUpper(strings.TrimSpace(req.Type)), "-", "_")
	switch backupType {
	case "", "FULL":
		backupType = "FULL"
	case "DIFF", "DIFFERENTIAL":
		backupType = "DIFFERENTIAL"
	case "LOG", "COPY_ONLY":
	default:
		s.problem(w, r, http.StatusBadRequest, "E_VALIDATION", "tipo no soportado (full|diff|log|copy-only)")
		return
	}

	var jobID string
	err := s.Store.Update(r.Context(), func(st *store.State) error {
		agent := st.Agents[req.AgentID]
		if agent == nil {
			return store.ErrNotFound
		}
		tg := st.Storage[req.StorageTargetID]
		if tg == nil || !tg.Enabled {
			return fmt.Errorf("el destino de almacenamiento no existe o está deshabilitado")
		}
		now := s.Now().UTC()
		payload, _ := json.Marshal(contracts.SQLBackupJobPayload{
			Server: req.Server, Instance: req.Instance, Database: req.Database,
			Type: backupType, StorageTargetID: tg.ID,
			RetentionDays: req.RetentionDays, Verify: req.Verify,
		})
		id, _ := newID()
		st.Jobs[id] = &store.Job{
			ID: id, Type: contracts.JobTypeSQLBackup, State: store.JobPending,
			AgentID: agent.ID, Trigger: "manual",
			Payload: payload, MaxAttempts: 3, CreatedBy: a.auditName(),
			CreatedAt: now, UpdatedAt: now,
		}
		st.AppendJobEvent(contracts.JobEvent{JobID: id, Kind: "state", State: store.JobPending, At: now})
		jobID = id
		return nil
	})
	if err != nil {
		s.problem(w, r, http.StatusBadRequest, "E_VALIDATION", err.Error())
		return
	}
	s.audit(r, a, "sql.backup", "agent", req.AgentID, "ok",
		map[string]string{"database": req.Database, "type": backupType, "job": jobID})
	writeJSON(w, http.StatusAccepted, contracts.BackupResponse{JobID: jobID, State: store.JobPending})
}

// handleSQLRestore crea un job SQL_RESTORE desde una cadena del catálogo.
// POST /api/v1/admin/sql/restore
func (s *Server) handleSQLRestore(w http.ResponseWriter, r *http.Request) {
	if !allowMethod(w, r, http.MethodPost) {
		return
	}
	r, a, ok := s.authorize(w, r, "operate")
	if !ok {
		return
	}
	var req struct {
		sqlJobRequest
		Files    []string `json:"files"` // URIs de repositorio en orden
		Mode     string   `json:"mode,omitempty"`
		StopAt   string   `json:"stop_at,omitempty"`
		MoveData string   `json:"move_data,omitempty"`
		MoveLog  string   `json:"move_log,omitempty"`
		Confirm  bool     `json:"confirm"` // destructivo: obligatorio
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.problem(w, r, http.StatusBadRequest, "E_VALIDATION", "cuerpo inválido")
		return
	}
	if req.AgentID == "" || req.Server == "" || req.Database == "" {
		s.problem(w, r, http.StatusBadRequest, "E_VALIDATION", "agent_id, server y database son obligatorios")
		return
	}
	if len(req.Files) == 0 {
		s.problem(w, r, http.StatusBadRequest, "E_VALIDATION", "falta la cadena de ficheros (files)")
		return
	}
	if !req.Confirm {
		s.problem(w, r, http.StatusBadRequest, "E_VALIDATION", "la restauración SQL es destructiva: confirma con confirm=true")
		return
	}
	switch strings.ToUpper(strings.TrimSpace(req.Mode)) {
	case "", "NORECOVERY", "RECOVERY":
	default:
		s.problem(w, r, http.StatusBadRequest, "E_VALIDATION", "mode debe ser vacío, NORECOVERY o RECOVERY")
		return
	}

	var jobID string
	err := s.Store.Update(r.Context(), func(st *store.State) error {
		agent := st.Agents[req.AgentID]
		if agent == nil {
			return store.ErrNotFound
		}
		now := s.Now().UTC()
		payload, _ := json.Marshal(contracts.SQLRestoreJobPayload{
			Server: req.Server, Instance: req.Instance, Database: req.Database,
			Files: req.Files, Mode: strings.ToUpper(strings.TrimSpace(req.Mode)),
			StopAt: req.StopAt, MoveData: req.MoveData, MoveLog: req.MoveLog,
			StorageTargetID: req.StorageTargetID,
		})
		id, _ := newID()
		st.Jobs[id] = &store.Job{
			ID: id, Type: contracts.JobTypeSQLRestore, State: store.JobPending,
			AgentID: agent.ID, Trigger: "manual",
			Payload: payload, MaxAttempts: 1, CreatedBy: a.auditName(),
			CreatedAt: now, UpdatedAt: now,
		}
		st.AppendJobEvent(contracts.JobEvent{JobID: id, Kind: "state", State: store.JobPending, At: now})
		jobID = id
		return nil
	})
	if err != nil {
		s.problem(w, r, http.StatusBadRequest, "E_VALIDATION", err.Error())
		return
	}
	s.audit(r, a, "sql.restore", "agent", req.AgentID, "ok",
		map[string]string{"database": req.Database, "job": jobID, "files": fmt.Sprint(len(req.Files))})
	writeJSON(w, http.StatusAccepted, contracts.BackupResponse{JobID: jobID, State: store.JobPending})
}

// handleSQLVerify crea un job SQL_VERIFY (RF-44: verificación de un fichero
// del destino con RESTORE VERIFYONLY).
// POST /api/v1/admin/sql/verify
func (s *Server) handleSQLVerify(w http.ResponseWriter, r *http.Request) {
	if !allowMethod(w, r, http.MethodPost) {
		return
	}
	r, a, ok := s.authorize(w, r, "operate")
	if !ok {
		return
	}
	var req struct {
		sqlJobRequest
		RepoURI string `json:"repo_uri"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.problem(w, r, http.StatusBadRequest, "E_VALIDATION", "cuerpo inválido")
		return
	}
	if req.AgentID == "" || req.Server == "" || req.RepoURI == "" {
		s.problem(w, r, http.StatusBadRequest, "E_VALIDATION", "agent_id, server y repo_uri son obligatorios")
		return
	}

	var jobID string
	err := s.Store.Update(r.Context(), func(st *store.State) error {
		agent := st.Agents[req.AgentID]
		if agent == nil {
			return store.ErrNotFound
		}
		now := s.Now().UTC()
		payload, _ := json.Marshal(contracts.SQLVerifyJobPayload{
			Server: req.Server, Instance: req.Instance,
			RepoURI: req.RepoURI, StorageTargetID: req.StorageTargetID,
		})
		id, _ := newID()
		st.Jobs[id] = &store.Job{
			ID: id, Type: contracts.JobTypeSQLVerify, State: store.JobPending,
			AgentID: agent.ID, Trigger: "manual",
			Payload: payload, MaxAttempts: 3, CreatedBy: a.auditName(),
			CreatedAt: now, UpdatedAt: now,
		}
		st.AppendJobEvent(contracts.JobEvent{JobID: id, Kind: "state", State: store.JobPending, At: now})
		jobID = id
		return nil
	})
	if err != nil {
		s.problem(w, r, http.StatusBadRequest, "E_VALIDATION", err.Error())
		return
	}
	s.audit(r, a, "sql.verify", "agent", req.AgentID, "ok",
		map[string]string{"repo_uri": req.RepoURI, "job": jobID})
	writeJSON(w, http.StatusAccepted, contracts.BackupResponse{JobID: jobID, State: store.JobPending})
}

// handleSQLInventory crea un job SQL_INVENTORY (inventario de bases).
// POST /api/v1/admin/sql/inventory
func (s *Server) handleSQLInventory(w http.ResponseWriter, r *http.Request) {
	if !allowMethod(w, r, http.MethodPost) {
		return
	}
	r, a, ok := s.authorize(w, r, "operate")
	if !ok {
		return
	}
	var req sqlJobRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.problem(w, r, http.StatusBadRequest, "E_VALIDATION", "cuerpo inválido")
		return
	}
	if req.AgentID == "" || req.Server == "" {
		s.problem(w, r, http.StatusBadRequest, "E_VALIDATION", "agent_id y server son obligatorios")
		return
	}

	var jobID string
	err := s.Store.Update(r.Context(), func(st *store.State) error {
		agent := st.Agents[req.AgentID]
		if agent == nil {
			return store.ErrNotFound
		}
		now := s.Now().UTC()
		payload, _ := json.Marshal(contracts.SQLInventoryJobPayload{
			Server: req.Server, Instance: req.Instance,
		})
		id, _ := newID()
		st.Jobs[id] = &store.Job{
			ID: id, Type: contracts.JobTypeSQLInventory, State: store.JobPending,
			AgentID: agent.ID, Trigger: "manual",
			Payload: payload, MaxAttempts: 3, CreatedBy: a.auditName(),
			CreatedAt: now, UpdatedAt: now,
		}
		st.AppendJobEvent(contracts.JobEvent{JobID: id, Kind: "state", State: store.JobPending, At: now})
		jobID = id
		return nil
	})
	if err != nil {
		s.problem(w, r, http.StatusBadRequest, "E_VALIDATION", err.Error())
		return
	}
	s.audit(r, a, "sql.inventory", "agent", req.AgentID, "ok", map[string]string{"job": jobID})
	writeJSON(w, http.StatusAccepted, contracts.BackupResponse{JobID: jobID, State: store.JobPending})
}

// handleSQLRestorePoints lista los puntos de restauración SQL (Kind "sql"),
// más recientes primero, filtrando por agente y/o base.
// GET /api/v1/admin/sql/restore-points?agent_id=&database=
func (s *Server) handleSQLRestorePoints(w http.ResponseWriter, r *http.Request) {
	if !allowMethod(w, r, http.MethodGet) {
		return
	}
	r, _, ok := s.authorize(w, r, "read")
	if !ok {
		return
	}
	agentID := r.URL.Query().Get("agent_id")
	database := r.URL.Query().Get("database")

	items := []contracts.RestorePointView{}
	_ = s.Store.View(r.Context(), func(st *store.State) error {
		for _, rp := range st.RestorePoints {
			if rp.Kind != "sql" || rp.SQL == nil {
				continue
			}
			if agentID != "" && rp.AgentID != agentID {
				continue
			}
			if database != "" && rp.SQL.Database != database {
				continue
			}
			items = append(items, s.restorePointView(st, rp))
		}
		return nil
	})
	sort.Slice(items, func(i, j int) bool { return items[i].CreatedAt.After(items[j].CreatedAt) })
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

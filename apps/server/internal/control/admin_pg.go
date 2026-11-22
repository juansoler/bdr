// Endpoints de administración del motor PostgreSQL (pg_dump/pg_restore).
// El patrón es el de los endpoints SQL: el control crea jobs PG_* y el
// agente del host PostgreSQL los ejecuta; el resultado de PG_DUMP se
// ingiere como RestorePoint Kind "pg".
package control

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"

	"github.com/juansoler/bdr/apps/server/internal/store"
	"github.com/juansoler/bdr/packages/contracts"
)

// pgConnRequest es la petición común de los endpoints PG: agente destino
// y conexión a la instancia (sin contraseñas: BDR_PG_PASSWORD del agente).
type pgConnRequest struct {
	AgentID string `json:"agent_id"`
	Host    string `json:"host,omitempty"` // vacío = socket local
	Port    string `json:"port,omitempty"` // vacío = 5432
	User    string `json:"user,omitempty"` // vacío = usuario del proceso
}

// handlePGDump crea un job PG_DUMP.
// POST /api/v1/admin/pg/dump
func (s *Server) handlePGDump(w http.ResponseWriter, r *http.Request) {
	if !allowMethod(w, r, http.MethodPost) {
		return
	}
	r, a, ok := s.authorize(w, r, "operate")
	if !ok {
		return
	}
	var req struct {
		pgConnRequest
		Database        string `json:"database"`
		StorageTargetID string `json:"storage_target_id"`
		RetentionDays   int    `json:"retention_days,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.problem(w, r, http.StatusBadRequest, "E_VALIDATION", "cuerpo inválido")
		return
	}
	if req.AgentID == "" || req.Database == "" || req.StorageTargetID == "" {
		s.problem(w, r, http.StatusBadRequest, "E_VALIDATION", "agent_id, database y storage_target_id son obligatorios")
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
		payload, _ := json.Marshal(contracts.PGDumpJobPayload{
			Host: req.Host, Port: req.Port, User: req.User,
			Database: req.Database, StorageTargetID: tg.ID,
			RetentionDays: req.RetentionDays,
		})
		id, _ := newID()
		st.Jobs[id] = &store.Job{
			ID: id, Type: contracts.JobTypePGDump, State: store.JobPending,
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
	s.audit(r, a, "pg.dump", "agent", req.AgentID, "ok",
		map[string]string{"database": req.Database, "job": jobID})
	writeJSON(w, http.StatusAccepted, contracts.BackupResponse{JobID: jobID, State: store.JobPending})
}

// handlePGRestore crea un job PG_RESTORE.
// POST /api/v1/admin/pg/restore
func (s *Server) handlePGRestore(w http.ResponseWriter, r *http.Request) {
	if !allowMethod(w, r, http.MethodPost) {
		return
	}
	r, a, ok := s.authorize(w, r, "operate")
	if !ok {
		return
	}
	var req struct {
		pgConnRequest
		Database      string `json:"database"`
		CreateDB      bool   `json:"create_db,omitempty"`
		DropExisting  bool   `json:"drop_existing,omitempty"`
		RepoURI       string `json:"repo_uri"`
		StorageTarget string `json:"storage_target_id,omitempty"`
		Confirm       bool   `json:"confirm"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.problem(w, r, http.StatusBadRequest, "E_VALIDATION", "cuerpo inválido")
		return
	}
	if req.AgentID == "" || req.Database == "" || req.RepoURI == "" {
		s.problem(w, r, http.StatusBadRequest, "E_VALIDATION", "agent_id, database y repo_uri son obligatorios")
		return
	}
	if !req.Confirm {
		s.problem(w, r, http.StatusBadRequest, "E_VALIDATION",
			"la restauración PG es destructiva (o crea la base): confirma con confirm=true")
		return
	}

	var jobID string
	err := s.Store.Update(r.Context(), func(st *store.State) error {
		agent := st.Agents[req.AgentID]
		if agent == nil {
			return store.ErrNotFound
		}
		// Destino de la cadena si no viene: el punto cuyo URI coincide.
		targetID := req.StorageTarget
		if targetID == "" {
			for _, rp := range st.RestorePoints {
				if rp.PG != nil && rp.SnapshotID == req.RepoURI {
					targetID = rp.TargetID
					break
				}
			}
		}
		now := s.Now().UTC()
		payload, _ := json.Marshal(contracts.PGRestoreJobPayload{
			Host: req.Host, Port: req.Port, User: req.User,
			Database: req.Database, CreateDB: req.CreateDB, DropExisting: req.DropExisting,
			RepoURI: req.RepoURI, StorageTargetID: targetID,
		})
		id, _ := newID()
		st.Jobs[id] = &store.Job{
			ID: id, Type: contracts.JobTypePGRestore, State: store.JobPending,
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
	s.audit(r, a, "pg.restore", "agent", req.AgentID, "ok",
		map[string]string{"database": req.Database, "job": jobID})
	writeJSON(w, http.StatusAccepted, contracts.BackupResponse{JobID: jobID, State: store.JobPending})
}

// handlePGVerify crea un job PG_VERIFY (pg_restore --list).
// POST /api/v1/admin/pg/verify
func (s *Server) handlePGVerify(w http.ResponseWriter, r *http.Request) {
	if !allowMethod(w, r, http.MethodPost) {
		return
	}
	r, a, ok := s.authorize(w, r, "operate")
	if !ok {
		return
	}
	var req struct {
		RepoURI string `json:"repo_uri"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.problem(w, r, http.StatusBadRequest, "E_VALIDATION", "cuerpo inválido")
		return
	}
	if req.RepoURI == "" {
		s.problem(w, r, http.StatusBadRequest, "E_VALIDATION", "repo_uri es obligatorio")
		return
	}
	// El agente que verifica es el que tiene el punto (su destino).
	agentID, targetID := "", ""
	_ = s.Store.View(r.Context(), func(st *store.State) error {
		for _, rp := range st.RestorePoints {
			if rp.PG != nil && rp.SnapshotID == req.RepoURI {
				agentID, targetID = rp.AgentID, rp.TargetID
				return nil
			}
		}
		return nil
	})
	if agentID == "" {
		s.problem(w, r, http.StatusNotFound, "E_NOT_FOUND", "punto PG no encontrado para ese repo_uri")
		return
	}

	var jobID string
	err := s.Store.Update(r.Context(), func(st *store.State) error {
		agent := st.Agents[agentID]
		if agent == nil {
			return store.ErrNotFound
		}
		now := s.Now().UTC()
		payload, _ := json.Marshal(contracts.PGVerifyJobPayload{
			RepoURI: req.RepoURI, StorageTargetID: targetID,
		})
		id, _ := newID()
		st.Jobs[id] = &store.Job{
			ID: id, Type: contracts.JobTypePGVerify, State: store.JobPending,
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
	s.audit(r, a, "pg.verify", "agent", agentID, "ok",
		map[string]string{"repo_uri": req.RepoURI, "job": jobID})
	writeJSON(w, http.StatusAccepted, contracts.BackupResponse{JobID: jobID, State: store.JobPending})
}

// handlePGInventory crea un job PG_INVENTORY.
// POST /api/v1/admin/pg/inventory
func (s *Server) handlePGInventory(w http.ResponseWriter, r *http.Request) {
	if !allowMethod(w, r, http.MethodPost) {
		return
	}
	r, a, ok := s.authorize(w, r, "operate")
	if !ok {
		return
	}
	var req pgConnRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.problem(w, r, http.StatusBadRequest, "E_VALIDATION", "cuerpo inválido")
		return
	}
	if req.AgentID == "" {
		s.problem(w, r, http.StatusBadRequest, "E_VALIDATION", "agent_id es obligatorio")
		return
	}

	var jobID string
	err := s.Store.Update(r.Context(), func(st *store.State) error {
		agent := st.Agents[req.AgentID]
		if agent == nil {
			return store.ErrNotFound
		}
		now := s.Now().UTC()
		payload, _ := json.Marshal(contracts.PGInventoryJobPayload{
			Host: req.Host, Port: req.Port, User: req.User,
		})
		id, _ := newID()
		st.Jobs[id] = &store.Job{
			ID: id, Type: contracts.JobTypePGInventory, State: store.JobPending,
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
	s.audit(r, a, "pg.inventory", "agent", req.AgentID, "ok", map[string]string{"job": jobID})
	writeJSON(w, http.StatusAccepted, contracts.BackupResponse{JobID: jobID, State: store.JobPending})
}

// handlePGRestorePoints lista los puntos de restauración PostgreSQL
// (Kind "pg"), más recientes primero, filtrando por agente y/o base.
// GET /api/v1/admin/pg/restore-points
func (s *Server) handlePGRestorePoints(w http.ResponseWriter, r *http.Request) {
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
			if rp.Kind != "pg" || rp.PG == nil {
				continue
			}
			if agentID != "" && rp.AgentID != agentID {
				continue
			}
			if database != "" && rp.PG.Database != database {
				continue
			}
			items = append(items, s.restorePointView(st, rp))
		}
		return nil
	})
	sort.Slice(items, func(i, j int) bool { return items[i].CreatedAt.After(items[j].CreatedAt) })
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

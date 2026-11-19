package control

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/juansoler/bdr/apps/server/internal/auth"
	"github.com/juansoler/bdr/apps/server/internal/pki"
	"github.com/juansoler/bdr/apps/server/internal/store"
	"github.com/juansoler/bdr/packages/contracts"
)

// handleAgentEnroll registra un agente con un token de un solo uso.
// Con csr_pem y la CA activa emite además un certificado de cliente mTLS.
func (s *Server) handleAgentEnroll(w http.ResponseWriter, r *http.Request) {
	if !allowMethod(w, r, http.MethodPost) {
		return
	}
	var req contracts.EnrollRequest
	if !decodeJSON(w, r, &req, maxBodyAdmin) {
		return
	}
	req.Token = strings.TrimSpace(req.Token)
	req.Hostname = strings.TrimSpace(req.Hostname)
	if req.Token == "" || req.Hostname == "" {
		s.problem(w, r, http.StatusBadRequest, "E_VALIDATION", "token y hostname son obligatorios")
		return
	}
	// windows y linux son los sistemas soportados; darwin se acepta para
	// desarrollo (el smoke y el panel corren en el equipo del operador).
	switch req.OSFamily {
	case "windows", "linux", "darwin":
	default:
		s.problem(w, r, http.StatusBadRequest, "E_VALIDATION", "os_family debe ser windows o linux")
		return
	}

	now := s.Now().UTC()
	var consumed store.EnrollmentToken
	err := s.Store.Update(r.Context(), func(st *store.State) error {
		t := st.TokenByHash(auth.Hash(req.Token))
		if t == nil {
			return store.ErrNotFound
		}
		if t.RevokedAt != nil {
			return store.ErrTokenRevoked
		}
		if t.UsedAt != nil {
			return store.ErrTokenUsed
		}
		if now.After(t.ExpiresAt) {
			return store.ErrTokenExpired
		}
		if t.ExpectedHostname != "" && !strings.EqualFold(strings.TrimSpace(t.ExpectedHostname), req.Hostname) {
			return store.ErrTokenHostname
		}
		used := now
		t.UsedAt = &used
		consumed = *t
		return nil
	})
	if err != nil {
		switch err {
		case store.ErrTokenUsed:
			s.problem(w, r, http.StatusGone, "E_ENROLL_USED", "El token ya se ha utilizado")
		case store.ErrTokenExpired:
			s.problem(w, r, http.StatusGone, "E_ENROLL_EXPIRED", "El token ha caducado")
		case store.ErrTokenHostname:
			s.problem(w, r, http.StatusForbidden, "E_ENROLL_HOSTNAME", "El hostname no coincide con el token")
		case store.ErrTokenRevoked:
			s.problem(w, r, http.StatusGone, "E_ENROLL_REVOKED", "El token fue revocado")
		default:
			s.problem(w, r, http.StatusUnauthorized, "E_ENROLL_INVALID", "Token de enrolamiento inválido")
		}
		return
	}

	agentID, err := newID()
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	apiKey, err := auth.NewSecret(32)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	agent := store.Agent{
		ID: agentID, Hostname: req.Hostname, OSFamily: req.OSFamily,
		OSVersion: req.OSVersion, Arch: req.Arch, AgentVersion: req.AgentVersion,
		EnrolledAt: now, APIKeyHash: auth.Hash(apiKey),
	}
	// Aplica grupo/plan del token automáticamente.
	autoAssign := func(st *store.State) {
		if consumed.GroupID != "" && st.Groups[consumed.GroupID] != nil {
			g := st.Groups[consumed.GroupID]
			g.MemberIDs = append(g.MemberIDs, agentID)
			g.UpdatedAt = now
		}
		if consumed.PlanID != "" && st.Plans[consumed.PlanID] != nil {
			id, _ := newID()
			a := &store.Assignment{ID: id, PlanID: consumed.PlanID,
				TargetType: contracts.TargetAgent, TargetID: agentID, AgentID: agentID,
				Enabled: true, CreatedAt: now}
			st.Assignments[id] = a
		}
	}
	err = s.Store.Update(r.Context(), func(st *store.State) error {
		if st.AgentByAPIKeyHash(agent.APIKeyHash) != nil {
			return store.ErrConflict
		}
		st.Agents[agent.ID] = &agent
		autoAssign(st)
		st.BumpConfig()
		return nil
	})
	if err != nil {
		s.internalError(w, r, err)
		return
	}

	out := contracts.EnrollResponse{
		AgentID: agentID, APIKey: apiKey, HeartbeatSeconds: s.HeartbeatSeconds,
		AgentURL: s.AgentPublicURL, ServerFingerprint: s.CertFingerprint,
	}
	// Emisión de certificado si el agente envió CSR y hay CA.
	if req.CSRPEM != "" && s.CA != nil {
		if issued, err := s.CA.SignAgentCSR(req.CSRPEM, agentID, now); err == nil {
			out.CertificatePEM = issued.PEM
			exp := issued.NotAfter
			out.CertificateExpiry = &exp
			out.CAPEM = string(s.CA.CertPEM)
			if out.AgentURL != "" {
				out.AgentURL = s.AgentPublicURL
			}
			_ = s.Store.Update(r.Context(), func(st *store.State) error {
				if a := st.Agents[agentID]; a != nil {
					a.CertSerial = issued.Serial
					a.CertNotAfter = &exp
				}
				return nil
			})
		} else {
			s.Logger.Warn("csr inválido en enrolamiento", "agent", agentID, "error", err.Error())
		}
	}
	s.Logger.Info("agent enrolled", "agent_id", agentID, "hostname", req.Hostname, "token_id", consumed.ID)
	writeJSON(w, http.StatusCreated, out)
}

// handleAgentRenewCert renueva el certificado mTLS del agente autenticado.
func (s *Server) handleAgentRenewCert(w http.ResponseWriter, r *http.Request) {
	if !allowMethod(w, r, http.MethodPost) {
		return
	}
	agent, ok := s.requireAgent(w, r)
	if !ok {
		return
	}
	if s.CA == nil {
		s.problem(w, r, http.StatusConflict, "E_NO_CA", "El servidor no emite certificados")
		return
	}
	var req contracts.RenewCertRequest
	if !decodeJSON(w, r, &req, maxBodyAdmin) {
		return
	}
	now := s.Now().UTC()
	issued, err := s.CA.SignAgentCSR(req.CSRPEM, agent.ID, now)
	if err != nil {
		s.problem(w, r, http.StatusBadRequest, "E_VALIDATION", err.Error())
		return
	}
	_ = s.Store.Update(r.Context(), func(st *store.State) error {
		if a := st.Agents[agent.ID]; a != nil {
			a.CertSerial = issued.Serial
			a.CertNotAfter = &issued.NotAfter
		}
		return nil
	})
	s.audit(r, &actor{kind: "agent", id: agent.ID, name: agent.Hostname}, "agent.cert_renewed", "agent", agent.ID, "ok", nil)
	writeJSON(w, http.StatusOK, contracts.RenewCertResponse{
		CertificatePEM: issued.PEM, CertificateExpiry: issued.NotAfter, CAPEM: string(s.CA.CertPEM),
	})
}

func (s *Server) handleAgentHeartbeat(w http.ResponseWriter, r *http.Request) {
	if !allowMethod(w, r, http.MethodPost) {
		return
	}
	agent, ok := s.requireAgent(w, r)
	if !ok {
		return
	}
	var req contracts.HeartbeatRequest
	if !decodeJSON(w, r, &req, maxBodyAgent) {
		return
	}
	now := s.Now().UTC()
	var resp contracts.HeartbeatResponse
	resp.ServerTime = now
	resp.PendingJobs = []string{}
	resp.CancelRequested = []string{}
	err := s.Store.UpdateLazy(r.Context(), func(st *store.State) error {
		a := st.Agents[agent.ID]
		if a == nil {
			return store.ErrNotFound
		}
		a.LastSeenAt = &now
		a.AgentVersion = req.AgentVersion
		a.ReportedState = req.State
		a.CurrentJobID = req.CurrentJobID
		resp.ConfigVersion = st.Meta.ConfigVersion
		resp.HeartbeatSeconds = s.HeartbeatSeconds
		// Jobs pendientes y cancelaciones solicitadas.
		for _, j := range st.Jobs {
			if j.AgentID != agent.ID {
				continue
			}
			if j.State == store.JobPending {
				resp.PendingJobs = append(resp.PendingJobs, j.ID)
			}
			if j.CancelRequestedAt != nil && (j.State == store.JobLeased || j.State == store.JobRunning) {
				resp.CancelRequested = append(resp.CancelRequested, j.ID)
			}
		}
		if a.CertNotAfter != nil && now.Add(14*24*time.Hour).After(*a.CertNotAfter) {
			t := now.Add(7 * 24 * time.Hour)
			resp.CertRenewAfter = &t
		}
		return nil
	})
	if err != nil {
		s.problem(w, r, http.StatusUnauthorized, "E_AUTH_TOKEN", "API key de agente inválida")
		return
	}
	// Oferta de autoactualización si el agente reporta otra versión y hay
	// binario publicado con su SHA-256.
	resp.Upgrade = s.agentUpgradeOffer(agent.OSFamily, agent.Arch, req.AgentVersion)
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleAgentInventory(w http.ResponseWriter, r *http.Request) {
	if !allowMethod(w, r, http.MethodPut) {
		return
	}
	agent, ok := s.requireAgent(w, r)
	if !ok {
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBodyAgent))
	if err != nil {
		s.problem(w, r, http.StatusRequestEntityTooLarge, "E_BODY_TOO_LARGE", "El inventario supera el tamaño permitido")
		return
	}
	if !json.Valid(body) {
		s.problem(w, r, http.StatusBadRequest, "E_VALIDATION", "El inventario no es JSON válido")
		return
	}
	hash := auth.Hash(string(body))
	now := s.Now().UTC()
	changed := false
	err = s.Store.Update(r.Context(), func(st *store.State) error {
		a := st.Agents[agent.ID]
		if a == nil {
			return store.ErrNotFound
		}
		a.LastSeenAt = &now
		if a.InventoryHash != hash {
			changed = true
			a.Inventory = append([]byte(nil), body...)
			a.InventoryHash = hash
			rec := store.InventoryRecord{Hash: hash, CollectedAt: now, Data: append([]byte(nil), body...)}
			hist := append(st.Inventory[agent.ID], rec)
			if len(hist) > store.MaxInventoryHistory {
				hist = append([]store.InventoryRecord(nil), hist[len(hist)-store.MaxInventoryHistory:]...)
			}
			st.Inventory[agent.ID] = hist
		}
		return nil
	})
	if err != nil {
		s.problem(w, r, http.StatusUnauthorized, "E_AUTH_TOKEN", "API key de agente inválida")
		return
	}
	if changed {
		s.Logger.Debug("inventario actualizado", "agent_id", agent.ID)
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleAgentMe(w http.ResponseWriter, r *http.Request) {
	if !allowMethod(w, r, http.MethodGet) {
		return
	}
	agent, ok := s.requireAgent(w, r)
	if !ok {
		return
	}
	now := s.Now().UTC()
	var v contracts.AgentView
	_ = s.Store.View(r.Context(), func(st *store.State) error {
		if a := st.Agents[agent.ID]; a != nil {
			v = s.agentView(st, a, now)
		}
		return nil
	})
	writeJSON(w, http.StatusOK, v)
}

// handleAgentPlans devuelve los planes efectivos en formato agente.
func (s *Server) handleAgentPlans(w http.ResponseWriter, r *http.Request) {
	if !allowMethod(w, r, http.MethodGet) {
		return
	}
	agent, ok := s.requireAgent(w, r)
	if !ok {
		return
	}
	now := s.Now().UTC()
	items := []contracts.PlanView{}
	_ = s.Store.View(r.Context(), func(st *store.State) error {
		eff, err := s.effectivePlans(st, agent.ID, now)
		if err != nil {
			return nil
		}
		for _, e := range eff {
			items = append(items, e.Plan)
		}
		return nil
	})
	writeJSON(w, http.StatusOK, contracts.AgentPlansResponse{Items: items})
}

func (s *Server) handleAgentLease(w http.ResponseWriter, r *http.Request) {
	if !allowMethod(w, r, http.MethodPost) {
		return
	}
	agent, ok := s.requireAgent(w, r)
	if !ok {
		return
	}
	var req struct {
		WaitSeconds int `json:"wait_seconds"`
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 4096))
	if err != nil {
		s.problem(w, r, http.StatusBadRequest, "E_VALIDATION", "Cuerpo inválido")
		return
	}
	if strings.TrimSpace(string(body)) != "" {
		if err := json.Unmarshal(body, &req); err != nil {
			s.problem(w, r, http.StatusBadRequest, "E_VALIDATION", "Cuerpo inválido")
			return
		}
	}
	wait := req.WaitSeconds
	if wait < 0 {
		wait = 0
	}
	if wait > 25 {
		wait = 25
	}
	deadline := time.Now().Add(time.Duration(wait) * time.Second)

	for {
		var out *contracts.JobView
		err := s.Store.Update(r.Context(), func(st *store.State) error {
			now := s.Now().UTC()
			job := st.LeaseJobFor(agent.ID, now, leaseDuration, leaseGrace)
			if job == nil {
				return store.ErrNotFound
			}
			v := jobView(job)
			out = &v
			st.AppendJobEvent(contracts.JobEvent{JobID: job.ID, Kind: "state", State: job.State, At: now})
			return nil
		})
		if err != nil && !errors.Is(err, store.ErrNotFound) {
			s.internalError(w, r, err)
			return
		}
		if err == nil && out != nil {
			writeJSON(w, http.StatusOK, contracts.JobLeaseResponse{Job: out})
			return
		}
		if wait == 0 || time.Now().After(deadline) {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		select {
		case <-r.Context().Done():
			return
		case <-time.After(500 * time.Millisecond):
		}
	}
}

func (s *Server) handleAgentJobAction(w http.ResponseWriter, r *http.Request) {
	if !allowMethod(w, r, http.MethodPost, http.MethodGet) {
		return
	}
	agent, ok := s.requireAgent(w, r)
	if !ok {
		return
	}
	rest := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/v1/agent/jobs/"), "/")
	parts := strings.Split(rest, "/")
	if len(parts) < 1 || parts[0] == "" {
		s.problem(w, r, http.StatusNotFound, "E_NOT_FOUND", "Ruta no encontrada")
		return
	}
	jobID := parts[0]
	action := ""
	if len(parts) == 2 {
		action = parts[1]
	}
	now := s.Now().UTC()

	switch {
	case action == "progress" && r.Method == http.MethodPost:
		var req contracts.JobProgressRequest
		if !decodeJSON(w, r, &req, maxBodyAgent) {
			return
		}
		cancelRequested := false
		err := s.Store.Update(r.Context(), func(st *store.State) error {
			j := st.Jobs[jobID]
			if j == nil || j.AgentID != agent.ID {
				return store.ErrNotFound
			}
			if j.State != store.JobLeased && j.State != store.JobRunning {
				return store.ErrConflict
			}
			if req.Progress < 0 {
				req.Progress = 0
			}
			if req.Progress > 100 {
				req.Progress = 100
			}
			j.State = store.JobRunning
			if j.StartedAt == nil {
				t := now
				j.StartedAt = &t
				j.Attempts++
				st.AppendJobEvent(contracts.JobEvent{JobID: jobID, Kind: "state", State: store.JobRunning, At: now})
			}
			if req.Progress > j.Progress {
				j.Progress = req.Progress
			}
			j.Message = req.Message
			j.BytesDone = req.BytesDone
			j.BytesTotal = req.BytesTotal
			j.UpdatedAt = now
			until := now.Add(leaseDuration)
			j.LeaseUntil = &until
			st.AppendJobEvent(contracts.JobEvent{JobID: jobID, Kind: "progress", Percent: j.Progress, Message: req.Message, At: now})
			cancelRequested = j.CancelRequestedAt != nil
			return nil
		})
		if err != nil {
			s.jobAgentErr(w, r, err)
			return
		}
		writeJSON(w, http.StatusOK, contracts.JobProgressResponse{CancelRequested: cancelRequested})
	case action == "log" && r.Method == http.MethodPost:
		var req contracts.JobLogRequest
		if !decodeJSON(w, r, &req, maxBodyAgent) {
			return
		}
		if len(req.Lines) > 500 {
			req.Lines = req.Lines[:500]
		}
		err := s.Store.Update(r.Context(), func(st *store.State) error {
			j := st.Jobs[jobID]
			if j == nil || j.AgentID != agent.ID {
				return store.ErrNotFound
			}
			for _, l := range req.Lines {
				if l.At.IsZero() {
					l.At = now
				}
				st.AppendJobEvent(contracts.JobEvent{
					JobID: jobID, Kind: "log", Level: orDefault(l.Level, "info"),
					Message: truncate(l.Message, 500), At: l.At,
				})
			}
			return nil
		})
		if err != nil {
			s.jobAgentErr(w, r, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	case action == "cancel" && r.Method == http.MethodGet:
		var status contracts.JobCancelStatus
		err := s.Store.View(r.Context(), func(st *store.State) error {
			j := st.Jobs[jobID]
			if j == nil || j.AgentID != agent.ID {
				return store.ErrNotFound
			}
			status.CancelRequested = j.CancelRequestedAt != nil
			return nil
		})
		if err != nil {
			s.jobAgentErr(w, r, err)
			return
		}
		writeJSON(w, http.StatusOK, status)
	case action == "complete" && r.Method == http.MethodPost:
		var req contracts.JobCompleteRequest
		if !decodeJSON(w, r, &req, maxBodyAgent) {
			return
		}
		if req.State != store.JobSucceeded && req.State != store.JobFailed && req.State != store.JobCanceled {
			s.problem(w, r, http.StatusBadRequest, "E_VALIDATION", "state debe ser succeeded, failed o canceled")
			return
		}
		var finished *store.Job
		err := s.Store.Update(r.Context(), func(st *store.State) error {
			j := st.Jobs[jobID]
			if j == nil || j.AgentID != agent.ID {
				return store.ErrNotFound
			}
			if j.State == store.JobSucceeded || j.State == store.JobFailed || j.State == store.JobCanceled {
				return store.ErrConflict
			}
			t := now
			j.State = req.State
			j.ErrorCode = req.ErrorCode
			j.ErrorMessage = truncate(req.ErrorMessage, 1000)
			j.Result = append([]byte(nil), req.Result...)
			if len(j.Result) > 512*1024 {
				j.Result = j.Result[:512*1024]
			}
			j.LeaseUntil = nil
			j.FinishedAt = &t
			j.UpdatedAt = t
			if req.State == store.JobSucceeded {
				j.Progress = 100
			}
			st.AppendJobEvent(contracts.JobEvent{JobID: jobID, Kind: "state", State: req.State, At: now})
			if req.State == store.JobFailed {
				s.alertFailedJob(j, st)
			}
			// Restore points de un backup exitoso.
			if req.State == store.JobSucceeded && j.Type == contracts.JobTypeBackupFile {
				s.recordRestorePoints(st, j, now)
			}
			if req.State == store.JobSucceeded && j.Type == contracts.JobTypeBackupVM {
				s.recordVMRestorePoint(st, j, now)
			}
			if req.State == store.JobSucceeded && j.Type == contracts.JobTypeBackupLXC {
				s.recordLXCRestorePoint(st, j, now)
			}
			cj := *j
			finished = &cj
			return nil
		})
		if err != nil {
			s.jobAgentErr(w, r, err)
			return
		}
		if finished != nil && finished.Type == contracts.JobTypeRestoreFile && req.State == store.JobSucceeded {
			s.Logger.Info("restauración completada", "job_id", jobID, "agent_id", agent.ID)
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		s.problem(w, r, http.StatusNotFound, "E_NOT_FOUND", "Acción no encontrada")
	}
}

func (s *Server) jobAgentErr(w http.ResponseWriter, r *http.Request, err error) {
	switch err {
	case store.ErrNotFound:
		s.problem(w, r, http.StatusNotFound, "E_JOB_NOT_FOUND", "Job no encontrado para este agente")
	case store.ErrConflict:
		s.problem(w, r, http.StatusConflict, "E_JOB_STATE", "El job no está en un estado válido")
	default:
		s.internalError(w, r, err)
	}
}

// recordRestorePoints crea un punto de recuperación por cada snapshot del resultado.
func (s *Server) recordRestorePoints(st *store.State, j *store.Job, now time.Time) {
	var result contracts.BackupJobResult
	if err := json.Unmarshal(j.Result, &result); err != nil {
		s.Logger.Warn("resultado de backup no interpretable", "job_id", j.ID, "error", err.Error())
		return
	}
	// Destino de almacenamiento del plan (repo Kopia del destino): permite
	// borrar/verificar el snapshot en el repositorio correcto.
	tgID := ""
	if p := st.Plans[j.PlanID]; p != nil {
		tgID = p.StorageTargetID
	}
	for _, sn := range result.Snapshots {
		if sn.ID == "" {
			continue
		}
		id, _ := newID()
		rp := &store.RestorePoint{
			ID: id, AgentID: j.AgentID, PlanID: j.PlanID, JobID: j.ID,
			Kind: "file", SnapshotID: sn.ID, RootObjectID: sn.RootObjectID,
			SourcePath: sn.Path, SourceHost: sn.SourceHost, SourceUser: sn.SourceUser,
			CreatedAt: now, StartTime: sn.StartTime, EndTime: sn.EndTime,
			SizeBytes: sn.SizeBytes, Files: sn.Files, Dirs: sn.Dirs, ErrorCount: sn.ErrorCount,
			VerifyState: contracts.VerifyNone,
			TargetID:    tgID,
		}
		st.RestorePoints[id] = rp
	}
}

// recordVMRestorePoint registra el punto de restauración de un backup de VM
// de Proxmox: SnapshotID = clave del objeto manifiesto en el destino.
func (s *Server) recordVMRestorePoint(st *store.State, j *store.Job, now time.Time) {
	var payload contracts.BackupVMJobPayload
	if err := json.Unmarshal(j.Payload, &payload); err != nil {
		s.Logger.Warn("payload de BACKUP_VM no interpretable", "job_id", j.ID, "error", err.Error())
		return
	}
	var result struct {
		ManifestID  string `json:"manifest_id"`
		ManifestKey string `json:"manifest_key"`
		VMID        int    `json:"vmid"`
		Name        string `json:"name"`
		BytesTotal  int64  `json:"bytes_total"`
	}
	if err := json.Unmarshal(j.Result, &result); err != nil {
		s.Logger.Warn("resultado de BACKUP_VM no interpretable", "job_id", j.ID, "error", err.Error())
		return
	}
	if result.ManifestKey == "" {
		s.Logger.Warn("BACKUP_VM sin manifiesto", "job_id", j.ID)
		return
	}
	id, _ := newID()
	rp := &store.RestorePoint{
		ID: id, AgentID: j.AgentID, JobID: j.ID,
		Kind:       "vm",
		SnapshotID: result.ManifestKey, TargetID: payload.StorageTargetID,
		SourcePath: fmt.Sprintf("pve/%s/%d", payload.InstanceID, result.VMID),
		CreatedAt:  now, StartTime: now, EndTime: now,
		SizeBytes:   result.BytesTotal,
		VerifyState: contracts.VerifyNone,
	}
	st.RestorePoints[id] = rp
	s.Logger.Info("punto de restauración de VM registrado", "job_id", j.ID,
		"vmid", result.VMID, "manifest", result.ManifestKey)
}

// recordLXCRestorePoint registra el punto de restauración de un backup de
// contenedor LXC de Proxmox: SnapshotID = clave del manifiesto en el destino.
func (s *Server) recordLXCRestorePoint(st *store.State, j *store.Job, now time.Time) {
	var payload contracts.BackupLXCJobPayload
	if err := json.Unmarshal(j.Payload, &payload); err != nil {
		s.Logger.Warn("payload de BACKUP_LXC no interpretable", "job_id", j.ID, "error", err.Error())
		return
	}
	var result struct {
		ManifestID  string `json:"manifest_id"`
		ManifestKey string `json:"manifest_key"`
		VMID        int    `json:"vmid"`
		Name        string `json:"name"`
		BytesTotal  int64  `json:"bytes_total"`
	}
	if err := json.Unmarshal(j.Result, &result); err != nil {
		s.Logger.Warn("resultado de BACKUP_LXC no interpretable", "job_id", j.ID, "error", err.Error())
		return
	}
	if result.ManifestKey == "" {
		s.Logger.Warn("BACKUP_LXC sin manifiesto", "job_id", j.ID)
		return
	}
	id, _ := newID()
	rp := &store.RestorePoint{
		ID: id, AgentID: j.AgentID, JobID: j.ID,
		Kind:       "lxc",
		SnapshotID: result.ManifestKey, TargetID: payload.StorageTargetID,
		SourcePath: fmt.Sprintf("pve/%s/%d", payload.InstanceID, result.VMID),
		CreatedAt:  now, StartTime: now, EndTime: now,
		SizeBytes:   result.BytesTotal,
		VerifyState: contracts.VerifyNone,
	}
	st.RestorePoints[id] = rp
	s.Logger.Info("punto de restauración de LXC registrado", "job_id", j.ID,
		"vmid", result.VMID, "manifest", result.ManifestKey)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

// handleRepoCredentials entrega la credencial Kopia del agente.
// Con ?target=<id> entrega las credenciales del repositorio de ese destino
// de almacenamiento (solo si el agente tiene un plan que lo use).
func (s *Server) handleRepoCredentials(w http.ResponseWriter, r *http.Request) {
	if !allowMethod(w, r, http.MethodGet) {
		return
	}
	agent, ok := s.requireAgent(w, r)
	if !ok {
		return
	}
	targetID := strings.TrimSpace(r.URL.Query().Get("target"))
	if targetID != "" {
		s.handleTargetRepoCredentials(w, r, agent, targetID)
		return
	}
	var repoCfg struct {
		Kind        string
		URL         string
		Fingerprint string
		Path        string
	}
	if s.Kopia != nil {
		repoCfg.Kind = s.Kopia.Kind()
		repoCfg.URL = s.Kopia.ServerURL
		repoCfg.Fingerprint = s.Kopia.ServerFingerprint
		repoCfg.Path = s.Kopia.RepoPath
	}
	if repoCfg.Kind == "" {
		s.problem(w, r, http.StatusNotFound, "E_REPO_UNCONFIGURED", "No hay repositorio configurado")
		return
	}
	ttl := time.Hour
	now := s.Now().UTC()
	var out contracts.RepositoryCredentials
	var rc *store.RepoCredential

	err := s.Store.Update(r.Context(), func(st *store.State) error {
		// Filesystem: todos los agentes comparten la contraseña del repositorio
		// (el aislamiento por agente exige un Kopia Repository Server con ACL).
		if repoCfg.Kind == "filesystem" {
			out = contracts.RepositoryCredentials{
				RepositoryID: "primary", Kind: repoCfg.Kind,
				Path: repoCfg.Path, Password: s.Kopia.Password,
				Version: 1, IssuedAt: now, ExpiresAt: now.Add(ttl),
			}
			return nil
		}
		rc = st.RepoCreds[agent.ID]
		if rc == nil {
			pw, err := auth.NewSecret(24)
			if err != nil {
				return err
			}
			rc = &store.RepoCredential{
				AgentID: agent.ID, Username: "agent-" + agent.ID[:8], Hostname: strings.ToLower(agent.Hostname),
				PasswordEnc: s.Box.MustSeal(pw), Version: 1, CreatedAt: now, RotatedAt: now,
			}
			st.RepoCreds[agent.ID] = rc
		} else if rc.Hostname != strings.ToLower(rc.Hostname) {
			// Auto-curación: usuarios anteriores a 0.5 con hostname en mayúsculas.
			rc.Hostname = strings.ToLower(rc.Hostname)
		}
		return nil
	})
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	if repoCfg.Kind == "server" {
		// El usuario debe existir en el repositorio antes de entregar la
		// credencial. Es idempotente: si hay deriva (rotación fallida,
		// usuario borrado a mano), aquí converge.
		pw, err := s.Box.Open(rc.PasswordEnc)
		if err != nil {
			s.problem(w, r, http.StatusInternalServerError, "E_SECRETS", "No se pudo descifrar la credencial del repositorio")
			return
		}
		if err := s.Kopia.ProvisionRepoUser(r.Context(), rc.Username, rc.Hostname, pw); err != nil {
			s.Logger.Warn("no se pudo aprovisionar el usuario del repositorio", "agent_id", agent.ID, "error", err.Error())
			s.problem(w, r, http.StatusBadGateway, "E_REPO_PROVISION", "El repositorio no aceptó el usuario del agente; reintenta")
			return
		}
		out = contracts.RepositoryCredentials{
			RepositoryID: "primary", Kind: repoCfg.Kind,
			URL: repoCfg.URL, Fingerprint: repoCfg.Fingerprint,
			Username: rc.Username, Hostname: rc.Hostname, Path: repoCfg.Path,
			Password: pw, Version: rc.Version, IssuedAt: now, ExpiresAt: now.Add(ttl),
		}
	}
	_ = ttl
	writeJSON(w, http.StatusOK, out)
}

// handleTargetRepoCredentials entrega las credenciales del repositorio Kopia
// de un destino de almacenamiento: solo a agentes con un plan asignado
// (directo o por grupo) que use ese destino, y aprovisiona el repo si hace
// falta (connect o create sobre el bucket S3 o la ruta física).
func (s *Server) handleTargetRepoCredentials(w http.ResponseWriter, r *http.Request, agent store.Agent, targetID string) {
	now := s.Now().UTC()
	var target *store.StorageTarget
	var pw string
	err := s.Store.Update(r.Context(), func(st *store.State) error {
		target = st.Storage[targetID]
		if target == nil || !target.Enabled {
			return errAbort
		}
		authorized := false
		plans, perr := s.effectivePlans(st, agent.ID, now)
		if perr != nil {
			return perr
		}
		for _, ep := range plans {
			if ep.Plan.StorageTargetID == targetID {
				authorized = true
				break
			}
		}
		// Los jobs ad-hoc de VM/LXC (backup/restore desde Proxmox) no llevan
		// plan: el agente del nodo recibe credenciales mientras tenga un
		// job activo que use ese destino.
		if !authorized {
			for _, j := range st.Jobs {
				if j.AgentID != agent.ID || j.Payload == nil {
					continue
				}
				if j.Type != contracts.JobTypeBackupVM && j.Type != contracts.JobTypeRestoreVM &&
					j.Type != contracts.JobTypeBackupLXC && j.Type != contracts.JobTypeRestoreLXC {
					continue
				}
				if j.State != store.JobPending && j.State != store.JobLeased && j.State != store.JobRunning {
					continue
				}
				var p struct {
					StorageTargetID string `json:"storage_target_id"`
				}
				if json.Unmarshal(j.Payload, &p) == nil && p.StorageTargetID == targetID {
					authorized = true
					break
				}
			}
		}
		if !authorized {
			return errAbort
		}
		// Destinos SMB: sin repositorio Kopia (kopia no escribe en
		// compartidas sin montar). Las credenciales sirven para el upload
		// directo de imagen/VM/LXC; no se aprovisiona nada.
		if target.IsSMB() {
			if target.RepoPasswordEnc != "" {
				if _, oerr := s.Box.Open(target.RepoPasswordEnc); oerr != nil {
					return fmt.Errorf("contraseña del repo: %w", oerr)
				}
			}
			return nil
		}
		var eerr error
		pw, eerr = s.storageEnsureRepo(r.Context(), target)
		return eerr
	})
	if err == errAbort {
		s.problem(w, r, http.StatusForbidden, "E_FORBIDDEN",
			"El destino no existe o el agente no tiene ningún plan asignado que lo use")
		return
	}
	if err != nil {
		s.problem(w, r, http.StatusBadGateway, "E_REPO_PROVISION", err.Error())
		return
	}
	// Contraseña SMB descifrada para las credenciales del agente.
	smbPass := ""
	if target.IsSMB() && target.SMBPasswordEnc != "" {
		var oerr error
		if smbPass, oerr = s.Box.Open(target.SMBPasswordEnc); oerr != nil {
			s.problem(w, r, http.StatusInternalServerError, "E_SECRETS",
				"No se pudo descifrar la contraseña SMB del destino")
			return
		}
	}
	// Ruta física: credenciales de repo filesystem (sin secretos S3).
	if target.IsFilesystem() {
		credPath := target.RepoPath()
		if target.IsSMB() {
			// La ruta SMB no puede pasar por filepath.Join (destruiría el
			// //servidor/recurso); el prefijo va como subdirectorio del URI.
			credPath = contracts.NormalizeSMBPath(target.Path + "/" + strings.Trim(target.Prefix, "/"))
		}
		writeJSON(w, http.StatusOK, contracts.RepositoryCredentials{
			RepositoryID: targetID, Kind: contracts.RepoKindFilesystem,
			Path:    credPath,
			SMBUser: target.SMBUser, SMBPassword: smbPass, SMBDomain: target.SMBDomain,
			Password: pw, Version: 1,
			IssuedAt: now, ExpiresAt: now.Add(time.Hour),
		})
		return
	}
	sk, err := s.Box.Open(target.SecretKeyEnc)
	if err != nil {
		s.problem(w, r, http.StatusInternalServerError, "E_SECRETS",
			"No se pudo descifrar la clave del destino")
		return
	}
	writeJSON(w, http.StatusOK, contracts.RepositoryCredentials{
		RepositoryID: targetID, Kind: contracts.RepoKindS3,
		Endpoint: target.Endpoint, Bucket: target.Bucket,
		Prefix: strings.Trim(target.Prefix, "/"), NoTLS: target.NoTLS,
		AccessKey: target.AccessKey, SecretKey: sk,
		Password: pw, Version: 1,
		IssuedAt: now, ExpiresAt: now.Add(time.Hour),
	})
}

// keep pki import used when CA nil-checking
var _ = pki.AgentURIPrefix

package control

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/juansoler/bdr/apps/server/internal/auth"
	"github.com/juansoler/bdr/apps/server/internal/store"
	"github.com/juansoler/bdr/packages/contracts"
)

func jobView(j *store.Job) contracts.JobView {
	v := contracts.JobView{
		ID: j.ID, Type: j.Type, State: j.State, AgentID: j.AgentID, PlanID: j.PlanID,
		RestorePointID: j.RestorePointID, Trigger: j.Trigger,
		Progress: j.Progress, Message: j.Message,
		BytesDone: j.BytesDone, BytesTotal: j.BytesTotal,
		ErrorCode: j.ErrorCode, ErrorMessage: j.ErrorMessage,
		Attempts: j.Attempts, MaxAttempts: j.MaxAttempts, RetryOf: j.RetryOf,
		CreatedBy: j.CreatedBy,
		CreatedAt: j.CreatedAt, UpdatedAt: j.UpdatedAt,
		StartedAt: j.StartedAt, FinishedAt: j.FinishedAt,
		LeaseUntil: j.LeaseUntil, CancelRequestedAt: j.CancelRequestedAt,
	}
	if len(j.Payload) > 0 {
		v.Payload = json.RawMessage(j.Payload)
	}
	if len(j.Result) > 0 {
		v.Result = json.RawMessage(j.Result)
	}
	return v
}

// --- Lista y detalle ---

func (s *Server) handleAdminJobs(w http.ResponseWriter, r *http.Request) {
	if !allowMethod(w, r, http.MethodGet) {
		return
	}
	r, _, ok := s.authorize(w, r, "read")
	if !ok {
		return
	}
	q := r.URL.Query()
	agentID := q.Get("agent_id")
	state := q.Get("state")
	planID := q.Get("plan_id")
	limit := atoiDefault(q.Get("limit"), 100)
	if limit > 500 {
		limit = 500
	}
	cursor := q.Get("cursor")
	var before time.Time
	if cursor != "" {
		if t, err := time.Parse(time.RFC3339Nano, cursor); err == nil {
			before = t
		}
	}
	items := []contracts.JobView{}
	var next string
	err := s.Store.View(r.Context(), func(st *store.State) error {
		var jobs []*store.Job
		for _, j := range st.Jobs {
			if agentID != "" && j.AgentID != agentID {
				continue
			}
			if state != "" && j.State != state {
				continue
			}
			if planID != "" && j.PlanID != planID {
				continue
			}
			if !before.IsZero() && !j.CreatedAt.Before(before) {
				continue
			}
			jobs = append(jobs, j)
		}
		sort.Slice(jobs, func(i, j int) bool { return jobs[i].CreatedAt.After(jobs[j].CreatedAt) })
		for i, j := range jobs {
			if i >= limit {
				next = jobs[i].CreatedAt.UTC().Format(time.RFC3339Nano)
				break
			}
			items = append(items, jobView(j))
		}
		return nil
	})
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, contracts.JobList{Items: items, NextCursor: next})
}

func (s *Server) handleAdminJob(w http.ResponseWriter, r *http.Request) {
	rest := pathID("/api/v1/admin/jobs/", r.URL.Path)
	parts := strings.Split(rest, "/")
	if len(parts) == 0 || parts[0] == "" {
		s.problem(w, r, http.StatusNotFound, "E_NOT_FOUND", "Job no encontrado")
		return
	}
	id := parts[0]
	switch {
	case len(parts) == 1:
		s.jobDetail(w, r, id)
	case len(parts) == 2 && parts[1] == "events":
		s.jobEvents(w, r, id)
	case len(parts) == 2 && parts[1] == "cancel":
		s.jobCancel(w, r, id)
	case len(parts) == 2 && parts[1] == "retry":
		s.jobRetry(w, r, id)
	default:
		s.problem(w, r, http.StatusNotFound, "E_NOT_FOUND", "Ruta no encontrada")
	}
}

func (s *Server) jobDetail(w http.ResponseWriter, r *http.Request, id string) {
	if !allowMethod(w, r, http.MethodGet) {
		return
	}
	r, _, ok := s.authorize(w, r, "read")
	if !ok {
		return
	}
	var out *contracts.JobView
	err := s.Store.View(r.Context(), func(st *store.State) error {
		j := st.Jobs[id]
		if j == nil {
			return store.ErrNotFound
		}
		v := jobView(j)
		out = &v
		return nil
	})
	if err != nil {
		s.jobAdminErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, *out)
}

func (s *Server) jobAdminErr(w http.ResponseWriter, r *http.Request, err error) {
	if err == store.ErrNotFound {
		s.problem(w, r, http.StatusNotFound, "E_JOB_NOT_FOUND", "Job no encontrado")
		return
	}
	s.internalError(w, r, err)
}

// --- Eventos (JSON paginado y SSE) ---

func (s *Server) jobEvents(w http.ResponseWriter, r *http.Request, id string) {
	wantsSSE := strings.Contains(r.Header.Get("Accept"), "text/event-stream")
	if !allowMethod(w, r, http.MethodGet) {
		return
	}
	r, _, ok := s.authorize(w, r, "read")
	if !ok {
		return
	}
	after := atoiDefault(r.URL.Query().Get("after"), 0)

	if !wantsSSE {
		var all []contracts.JobEvent
		err := s.Store.View(r.Context(), func(st *store.State) error {
			if st.Jobs[id] == nil {
				return store.ErrNotFound
			}
			for _, ev := range st.JobEvents[id] {
				if ev.Seq > after {
					all = append(all, ev)
				}
			}
			return nil
		})
		if err != nil {
			s.jobAdminErr(w, r, err)
			return
		}
		writeJSON(w, http.StatusOK, contracts.JobEventList{Items: all})
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	rc := http.NewResponseController(w)
	flush := func() { _ = rc.Flush() }
	flush()

	ctx := r.Context()
	lastSeq := after
	deadline := time.Now().Add(15 * time.Second)
	for {
		var events []contracts.JobEvent
		var jobState string
		exists := false
		_ = s.Store.View(ctx, func(st *store.State) error {
			j := st.Jobs[id]
			if j == nil {
				return store.ErrNotFound
			}
			exists = true
			jobState = j.State
			for _, ev := range st.JobEvents[id] {
				if ev.Seq > lastSeq {
					events = append(events, ev)
					lastSeq = ev.Seq
				}
			}
			return nil
		})
		if !exists {
			return
		}
		for _, ev := range events {
			fmt.Fprintf(w, "event: %s\ndata: %s\n\n", ev.Kind, marshalCompact(ev))
		}
		if len(events) > 0 {
			flush()
		}
		if contracts.JobStateFinal(jobState) && len(events) == 0 {
			fmt.Fprintf(w, "event: done\ndata: {\"job_id\":%q,\"state\":%q}\n\n", id, jobState)
			flush()
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(500 * time.Millisecond):
		}
		if time.Now().After(deadline) {
			fmt.Fprint(w, ": keepalive\n\n")
			flush()
			deadline = time.Now().Add(15 * time.Second)
		}
	}
}

func marshalCompact(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

// --- Backup bajo demanda ---

func (s *Server) agentBackupNow(w http.ResponseWriter, r *http.Request, agentID string) {
	if !allowMethod(w, r, http.MethodPost) {
		return
	}
	r, a, ok := s.authorize(w, r, "operate")
	if !ok {
		return
	}
	s.withIdempotency(w, r, a, func() (int, any) {
		var req contracts.BackupRequest
		if !decodeJSON(w, r, &req, maxBodyAdmin) {
			return 0, nil
		}
		now := s.Now().UTC()
		var job *store.Job
		status, msg := http.StatusInternalServerError, ""
		err := s.Store.Update(r.Context(), func(st *store.State) error {
			ag := st.Agents[agentID]
			if ag == nil {
				status, msg = http.StatusNotFound, "Agente no encontrado"
				return errAbort
			}
			if ag.RevokedAt != nil {
				status, msg = http.StatusConflict, "El agente está revocado"
				return errAbort
			}
			var plan *store.Plan
			if req.PlanID != "" {
				plan = st.Plans[req.PlanID]
				if plan == nil {
					status, msg = http.StatusNotFound, "Plan no encontrado"
					return errAbort
				}
			} else {
				eff, _ := s.effectivePlans(st, agentID, now)
				for _, e := range eff {
					if e.Plan.Enabled {
						plan = st.Plans[e.Plan.ID]
						break
					}
				}
			}
			if plan == nil || !plan.Enabled {
				status, msg = http.StatusBadRequest, "El agente no tiene un plan habilitado"
				return errAbort
			}
			if planHasPendingJob(st, agentID, plan.ID) {
				status, msg = http.StatusConflict, "Ya hay un job en curso o en cola para este plan"
				return errAbort
			}
			var jtype string
			var payload []byte
			var retentionDays int
			if tg := st.Storage[plan.StorageTargetID]; tg != nil {
				retentionDays = tg.RetentionDays
			}
			if plan.Kind == "image" {
				jtype = contracts.JobTypeBackupImage
				payload, _ = json.Marshal(contracts.ImageJobPayload{
					PlanID: plan.ID, PlanName: plan.Name,
					Disks:           plan.Disks,
					StorageTargetID: plan.StorageTargetID,
					RetentionDays:   retentionDays,
				})
			} else {
				jtype = contracts.JobTypeBackupFile
				payload, _ = json.Marshal(contracts.BackupJobPayload{
					PlanID: plan.ID, PlanName: plan.Name,
					Paths: plan.Paths, Excludes: plan.Excludes,
					Retention:       &plan.Retention,
					StorageTargetID: plan.StorageTargetID,
				})
			}
			id, _ := newID()
			j := &store.Job{
				ID: id, Type: jtype, State: store.JobPending,
				AgentID: agentID, PlanID: plan.ID, Trigger: "manual",
				Payload: payload, MaxAttempts: 3, CreatedBy: a.auditName(),
				CreatedAt: now, UpdatedAt: now,
			}
			st.Jobs[id] = j
			st.AppendJobEvent(contracts.JobEvent{JobID: id, Kind: "state", State: store.JobPending, At: now})
			job = j
			return nil
		})
		if err == errAbort {
			s.problem(w, r, status, codeForStatus(status), msg)
			return 0, nil
		}
		if err != nil {
			return 0, errInternal{err}
		}
		s.audit(r, a, "backup.started", "agent", agentID, "ok", map[string]string{"job": job.ID, "plan": job.PlanID})
		s.Logger.Info("backup job created", "job_id", job.ID, "agent_id", agentID, "plan_id", job.PlanID)
		return http.StatusAccepted, contracts.BackupResponse{JobID: job.ID, State: job.State}
	})
}

var errAbort = &codedError{"abort"}

func planHasPendingJob(st *store.State, agentID, planID string) bool {
	for _, j := range st.Jobs {
		if j.AgentID == agentID && j.PlanID == planID &&
			(j.State == store.JobPending || j.State == store.JobLeased || j.State == store.JobRunning) {
			return true
		}
	}
	return false
}

// --- Cancel y retry ---

func (s *Server) jobCancel(w http.ResponseWriter, r *http.Request, id string) {
	if !allowMethod(w, r, http.MethodPost) {
		return
	}
	r, a, ok := s.authorize(w, r, "operate")
	if !ok {
		return
	}
	now := s.Now().UTC()
	err := s.Store.Update(r.Context(), func(st *store.State) error {
		j := st.Jobs[id]
		if j == nil {
			return store.ErrNotFound
		}
		switch j.State {
		case store.JobPending:
			j.State = store.JobCanceled
			j.ErrorCode = "E_CANCELED"
			j.ErrorMessage = "Cancelado por el usuario"
			t := now
			j.FinishedAt = &t
			j.UpdatedAt = now
			st.AppendJobEvent(contracts.JobEvent{JobID: id, Kind: "state", State: store.JobCanceled, At: now})
		case store.JobLeased, store.JobRunning:
			if j.CancelRequestedAt == nil {
				j.CancelRequestedAt = &now
				st.AppendJobEvent(contracts.JobEvent{JobID: id, Kind: "state", State: "cancel_requested", At: now})
			}
			// Un job leased que el agente nunca ha empezado a ejecutar
			// (sin StartedAt) no puede abortar por sí mismo: se cancela
			// en el servidor al momento. Los en marcha esperan al agente,
			// que los aborta vía el cancel del heartbeat.
			if j.State == store.JobLeased && j.StartedAt == nil {
				j.State = store.JobCanceled
				j.ErrorCode = "E_CANCELED"
				j.ErrorMessage = "Cancelado en cola por el usuario"
				j.FinishedAt = &now
				j.UpdatedAt = now
				if j.LeaseUntil != nil {
					j.LeaseUntil = nil
				}
				st.AppendJobEvent(contracts.JobEvent{JobID: id, Kind: "state", State: store.JobCanceled, At: now})
			}
		default:
			return store.ErrConflict
		}
		return nil
	})
	if err != nil {
		if err == store.ErrNotFound {
			s.problem(w, r, http.StatusNotFound, "E_JOB_NOT_FOUND", "Job no encontrado")
			return
		}
		if err == store.ErrConflict {
			s.problem(w, r, http.StatusConflict, "E_JOB_STATE", "El job ya está terminado")
			return
		}
		s.internalError(w, r, err)
		return
	}
	s.audit(r, a, "job.cancel_requested", "job", id, "ok", nil)
	w.WriteHeader(http.StatusAccepted)
}

func (s *Server) jobRetry(w http.ResponseWriter, r *http.Request, id string) {
	if !allowMethod(w, r, http.MethodPost) {
		return
	}
	r, a, ok := s.authorize(w, r, "operate")
	if !ok {
		return
	}
	s.withIdempotency(w, r, a, func() (int, any) {
		now := s.Now().UTC()
		var newJob *store.Job
		status, msg := http.StatusInternalServerError, ""
		err := s.Store.Update(r.Context(), func(st *store.State) error {
			j := st.Jobs[id]
			if j == nil {
				status, msg = http.StatusNotFound, "Job no encontrado"
				return errAbort
			}
			if j.State != store.JobFailed && j.State != store.JobExpired && j.State != store.JobCanceled {
				status, msg = http.StatusConflict, "Solo se reintentan jobs fallidos, expirados o cancelados"
				return errAbort
			}
			for _, other := range st.Jobs {
				if other.RetryOf == j.ID && (other.State == store.JobPending || other.State == store.JobLeased || other.State == store.JobRunning) {
					status, msg = http.StatusConflict, "Ya existe un reintento en curso"
					return errAbort
				}
			}
			if planHasPendingJob(st, j.AgentID, j.PlanID) && j.Type == contracts.JobTypeBackupFile {
				status, msg = http.StatusConflict, "Ya hay un job en curso para este plan"
				return errAbort
			}
			nid, _ := newID()
			nj := &store.Job{
				ID: nid, Type: j.Type, State: store.JobPending,
				AgentID: j.AgentID, PlanID: j.PlanID, RestorePointID: j.RestorePointID,
				Trigger: "retry", Payload: append([]byte(nil), j.Payload...),
				Attempts: j.Attempts, MaxAttempts: j.MaxAttempts, RetryOf: j.ID,
				CreatedBy: a.auditName(), CreatedAt: now, UpdatedAt: now,
			}
			st.Jobs[nid] = nj
			st.AppendJobEvent(contracts.JobEvent{JobID: nid, Kind: "state", State: store.JobPending, At: now})
			newJob = nj
			return nil
		})
		if err == errAbort {
			s.problem(w, r, status, codeForStatus(status), msg)
			return 0, nil
		}
		if err != nil {
			return 0, errInternal{err}
		}
		s.audit(r, a, "job.retried", "job", id, "ok", map[string]string{"new_job": newJob.ID})
		return http.StatusAccepted, contracts.BackupResponse{JobID: newJob.ID, State: newJob.State}
	})
}

// --- Restore points ---

func (s *Server) restorePointView(st *store.State, rp *store.RestorePoint) contracts.RestorePointView {
	v := contracts.RestorePointView{
		ID: rp.ID, AgentID: rp.AgentID, PlanID: rp.PlanID, JobID: rp.JobID,
		Kind: rp.Kind, SnapshotID: rp.SnapshotID, TargetID: rp.TargetID, RootObjectID: rp.RootObjectID,
		SourcePath: rp.SourcePath, SourceHost: rp.SourceHost, SourceUser: rp.SourceUser,
		CreatedAt: rp.CreatedAt, StartTime: rp.StartTime, EndTime: rp.EndTime,
		SizeBytes: rp.SizeBytes, Files: rp.Files, Dirs: rp.Dirs, ErrorCount: rp.ErrorCount,
		Consistency: "crash", Encrypted: true,
		VerifyState: rp.VerifyState, VerifiedAt: rp.VerifiedAt, VerifyDetail: rp.VerifyDetail,
		ExpiredAt: rp.ExpiredAt,
	}
	if p := st.Plans[rp.PlanID]; p != nil {
		v.PlanName = p.Name
	}
	if rp.SQL != nil {
		v.SQL = &contracts.RestorePointSQLView{
			Instance:    rp.SQL.Instance,
			Database:    rp.SQL.Database,
			BackupType:  rp.SQL.BackupType,
			DatabaseLSN: rp.SQL.DatabaseLSN,
			FirstLSN:    rp.SQL.FirstLSN,
			LastLSN:     rp.SQL.LastLSN,
			Checksum:    rp.SQL.Checksum,
			Compression: rp.SQL.Compression,
			Verified:    rp.SQL.Verified,
		}
	}
	if a := st.Agents[rp.AgentID]; a != nil {
		v.Hostname = a.Hostname
	}
	v.Destination = "NAS"
	v.Browsable = s.Kopia != nil && s.Kopia.ServerSide() && rp.Kind == "file"
	if v.VerifyState == "" {
		v.VerifyState = contracts.VerifyNone
	}
	return v
}

func (s *Server) handleRestorePoints(w http.ResponseWriter, r *http.Request) {
	if !allowMethod(w, r, http.MethodGet) {
		return
	}
	r, _, ok := s.authorize(w, r, "read")
	if !ok {
		return
	}
	q := r.URL.Query()
	agentID, planID := q.Get("agent_id"), q.Get("plan_id")
	limit := atoiDefault(q.Get("limit"), 50)
	if limit > 200 {
		limit = 200
	}
	var before time.Time
	if c := q.Get("cursor"); c != "" {
		t, _ := time.Parse(time.RFC3339Nano, c)
		before = t
	}
	items := []contracts.RestorePointView{}
	var next string
	err := s.Store.View(r.Context(), func(st *store.State) error {
		var rps []*store.RestorePoint
		for _, rp := range st.RestorePoints {
			if rp.ExpiredAt != nil {
				continue
			}
			if agentID != "" && rp.AgentID != agentID {
				continue
			}
			if planID != "" && rp.PlanID != planID {
				continue
			}
			if !before.IsZero() && !rp.CreatedAt.Before(before) {
				continue
			}
			rps = append(rps, rp)
		}
		sort.Slice(rps, func(i, j int) bool { return rps[i].CreatedAt.After(rps[j].CreatedAt) })
		for i, rp := range rps {
			if i >= limit {
				next = rp.CreatedAt.UTC().Format(time.RFC3339Nano)
				break
			}
			items = append(items, s.restorePointView(st, rp))
		}
		return nil
	})
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, contracts.RestorePointList{Items: items, NextCursor: next})
}

func (s *Server) handleRestorePointAction(w http.ResponseWriter, r *http.Request) {
	rest := pathID("/api/v1/admin/restore-points/", r.URL.Path)
	parts := strings.Split(rest, "/")
	if len(parts) == 0 || parts[0] == "" {
		s.problem(w, r, http.StatusNotFound, "E_NOT_FOUND", "Ruta no encontrada")
		return
	}
	id := parts[0]
	switch {
	case len(parts) == 1:
		if r.Method == http.MethodDelete {
			s.restorePointDelete(w, r, id)
		} else {
			s.restorePointDetail(w, r, id)
		}
	case len(parts) == 2 && parts[1] == "verify":
		s.restorePointVerify(w, r, id)
	case len(parts) == 3 && parts[1] == "files" && parts[2] == "content":
		s.restorePointFiles(w, r, id, "content")
	case len(parts) == 3 && parts[1] == "files" && parts[2] == "search":
		s.restorePointFilesSearch(w, r, id)
	case len(parts) == 3 && parts[1] == "files" && parts[2] == "download":
		s.restorePointFilesDownloadZip(w, r, id)
	case len(parts) == 2 && parts[1] == "files":
		s.restorePointFiles(w, r, id, "list")
	default:
		s.problem(w, r, http.StatusNotFound, "E_NOT_FOUND", "Ruta no encontrada")
	}
}

func (s *Server) restorePointDetail(w http.ResponseWriter, r *http.Request, id string) {
	if !allowMethod(w, r, http.MethodGet) {
		return
	}
	r, _, ok := s.authorize(w, r, "read")
	if !ok {
		return
	}
	var out *contracts.RestorePointView
	err := s.Store.View(r.Context(), func(st *store.State) error {
		rp := st.RestorePoints[id]
		if rp == nil {
			return store.ErrNotFound
		}
		v := s.restorePointView(st, rp)
		out = &v
		return nil
	})
	if err != nil {
		s.jobAdminErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, *out)
}

// restorePointVerify lanza la verificación de Kopia en segundo plano.
func (s *Server) restorePointVerify(w http.ResponseWriter, r *http.Request, id string) {
	if !allowMethod(w, r, http.MethodPost) {
		return
	}
	r, a, ok := s.authorize(w, r, "operate")
	if !ok {
		return
	}
	var req contracts.VerifyRequest
	if !decodeJSON(w, r, &req, maxBodyAdmin) {
		return
	}
	if req.FilesPercent < 0 || req.FilesPercent > 100 {
		s.problem(w, r, http.StatusBadRequest, "E_VALIDATION", "files_percent debe estar entre 0 y 100")
		return
	}
	var rp *store.RestorePoint
	_ = s.Store.View(r.Context(), func(st *store.State) error {
		if x := st.RestorePoints[id]; x != nil {
			c := *x
			rp = &c
		}
		return nil
	})
	if rp == nil {
		s.problem(w, r, http.StatusNotFound, "E_NOT_FOUND", "Punto de recuperación no encontrado")
		return
	}
	if s.Kopia == nil || !s.Kopia.ServerSide() {
		s.problem(w, r, http.StatusConflict, "E_REPO_UNCONFIGURED",
			"El plano de control no tiene acceso directo al repositorio; la verificación no está disponible")
		return
	}
	_ = s.Store.Update(r.Context(), func(st *store.State) error {
		if x := st.RestorePoints[id]; x != nil {
			x.VerifyState = contracts.VerifyRunning
		}
		return nil
	})
	s.audit(r, a, "restore_point.verify_started", "restore_point", id, "ok",
		map[string]string{"files_percent": fmt.Sprint(req.FilesPercent)})
	go s.verifyRestorePoint(rp, req.FilesPercent)
	w.WriteHeader(http.StatusAccepted)
}

// verifyRestorePoint ejecuta kopia snapshot verify y guarda el resultado.
func (s *Server) verifyRestorePoint(rp *store.RestorePoint, filesPercent int) {
	ctx, cancel := contextWithTimeout(15 * time.Minute)
	defer cancel()
	detail, err := s.Kopia.VerifySnapshot(ctx, rp.SnapshotID, filesPercent)
	now := s.Now().UTC()
	state := contracts.VerifyOK
	if filesPercent > 0 {
		state = contracts.VerifyContent
	}
	if err != nil {
		state = contracts.VerifyFailed
		if detail == "" {
			detail = err.Error()
		}
	}
	_ = s.Store.Update(context.Background(), func(st *store.State) error {
		if x := st.RestorePoints[rp.ID]; x != nil {
			x.VerifyState = state
			x.VerifiedAt = &now
			x.VerifyDetail = detail
		}
		return nil
	})
	s.Logger.Info("verificación de restore point", "rp", rp.ID, "state", state, "detail", detail)
	if state == contracts.VerifyFailed {
		s.raiseAlert(notifPayload{
			Key: "verify:" + rp.ID, Kind: "verify_failed", Severity: contracts.SeverityHigh,
			Title: "Verificación fallida", Detail: detail, AgentID: rp.AgentID, PlanID: rp.PlanID,
		})
	}
}

// restorePointFiles navega un directorio del snapshot (streaming desde Kopia).
func (s *Server) restorePointFiles(w http.ResponseWriter, r *http.Request, id, action string) {
	if !allowMethod(w, r, http.MethodGet) {
		return
	}
	r, a, ok := s.authorize(w, r, "read")
	if !ok {
		return
	}
	if s.Kopia == nil || !s.Kopia.ServerSide() {
		s.problem(w, r, http.StatusConflict, "E_REPO_UNCONFIGURED",
			"El plano de control no tiene acceso directo al repositorio; la navegación no está disponible")
		return
	}
	q := r.URL.Query()
	rawPath := strings.Trim(q.Get("path"), "/")
	var rp *store.RestorePoint
	err := s.Store.View(r.Context(), func(st *store.State) error {
		if x := st.RestorePoints[id]; x != nil {
			c := *x
			rp = &c
		}
		return nil
	})
	if err != nil || rp == nil {
		s.problem(w, r, http.StatusNotFound, "E_NOT_FOUND", "Punto de recuperación no encontrado")
		return
	}
	objPath := rp.RootObjectID
	if objPath == "" {
		objPath = rp.SnapshotID
	}
	if rawPath != "" {
		if !validRelPath(rawPath) {
			s.problem(w, r, http.StatusBadRequest, "E_VALIDATION", "Ruta no válida")
			return
		}
		objPath += "/" + rawPath
	}

	if action == "content" {
		s.audit(r, a, "restore_point.file_downloaded", "restore_point", id, "ok", map[string]string{"path": rawPath})
		s.Kopia.StreamObject(w, r, objPath)
		return
	}
	s.audit(r, a, "restore_point.browsed", "restore_point", id, "ok", map[string]string{"path": rawPath})
	entries, err := s.Kopia.ListDir(r.Context(), objPath)
	if err != nil {
		s.Logger.Warn("navegación fallida", "rp", id, "path", rawPath, "error", err.Error())
		s.problem(w, r, http.StatusNotFound, "E_NOT_FOUND", "No se pudo leer el directorio del snapshot")
		return
	}
	// La ruta completa de cada entrada la completa el plano de control: el
	// explorador del panel la usa para navegar y descargar.
	for i := range entries {
		entries[i].Path = joinRel(rawPath, entries[i].Name)
	}
	writeJSON(w, http.StatusOK, contracts.FileListing{RestorePointID: id, Path: rawPath, Entries: entries})
}

func validRelPath(p string) bool {
	if p == "" {
		return true
	}
	if strings.Contains(p, "\x00") {
		return false
	}
	for _, part := range strings.Split(p, "/") {
		if part == "" || part == "." || part == ".." {
			return false
		}
	}
	return true
}

// --- Restauración de archivos ---

func (s *Server) handleRestoreFiles(w http.ResponseWriter, r *http.Request) {
	if !allowMethod(w, r, http.MethodPost) {
		return
	}
	r, a, ok := s.authorize(w, r, "operate")
	if !ok {
		return
	}
	s.withIdempotency(w, r, a, func() (int, any) {
		var req contracts.FileRestoreRequest
		if !decodeJSON(w, r, &req, maxBodyAdmin) {
			return 0, nil
		}
		for _, it := range req.Items {
			if !validRelPath(it) {
				return 0, errProblem{Status: http.StatusBadRequest, Code: "E_VALIDATION", Detail: "Ruta de item no válida: " + it}
			}
		}
		if !req.InPlace && req.Destination == "" && req.TargetAgentID == "" {
			return 0, errProblem{Status: http.StatusBadRequest, Code: "E_VALIDATION",
				Detail: "Indica destination (ruta absoluta en el equipo destino) o in_place=true"}
		}
		if !req.InPlace && req.Destination != "" && !isAbsPath(req.Destination) {
			return 0, errProblem{Status: http.StatusBadRequest, Code: "E_VALIDATION",
				Detail: "destination debe ser una ruta absoluta del equipo destino"}
		}
		now := s.Now().UTC()
		var job *store.Job
		status, msg := http.StatusInternalServerError, ""
		err := s.Store.Update(r.Context(), func(st *store.State) error {
			rp := st.RestorePoints[req.RestorePointID]
			if rp == nil || rp.ExpiredAt != nil {
				status, msg = http.StatusNotFound, "Punto de recuperación no encontrado"
				return errAbort
			}
			ag := st.Agents[rp.AgentID]
			if ag == nil {
				status, msg = http.StatusNotFound, "El equipo de origen ya no existe"
				return errAbort
			}
			if ag.RevokedAt != nil {
				status, msg = http.StatusConflict, "El equipo de origen está revocado"
				return errAbort
			}
			target := rp.AgentID
			if req.TargetAgentID != "" {
				t := st.Agents[req.TargetAgentID]
				if t == nil {
					status, msg = http.StatusNotFound, "Equipo destino no encontrado"
					return errAbort
				}
				if t.RevokedAt != nil {
					status, msg = http.StatusConflict, "El equipo destino está revocado"
					return errAbort
				}
				if t.OSFamily != ag.OSFamily {
					status, msg = http.StatusBadRequest, "El destino debe ser el mismo sistema operativo que el origen"
					return errAbort
				}
				target = req.TargetAgentID
			}
			if planHasPendingJobType(st, target, contracts.JobTypeRestoreFile) {
				status, msg = http.StatusConflict, "Ya hay una restauración en curso en ese equipo"
				return errAbort
			}
			payload, _ := json.Marshal(contracts.RestoreJobPayload{
				RestorePointID: rp.ID, SnapshotID: rp.SnapshotID, RootObjectID: rp.RootObjectID,
				SourcePath: rp.SourcePath, Items: req.Items,
				Destination: req.Destination, InPlace: req.InPlace, Overwrite: req.Overwrite,
			})
			id, _ := newID()
			j := &store.Job{
				ID: id, Type: contracts.JobTypeRestoreFile, State: store.JobPending,
				AgentID: target, RestorePointID: rp.ID, Trigger: "manual",
				Payload: payload, MaxAttempts: 1, CreatedBy: a.auditName(),
				CreatedAt: now, UpdatedAt: now,
			}
			st.Jobs[id] = j
			st.AppendJobEvent(contracts.JobEvent{JobID: id, Kind: "state", State: store.JobPending, At: now})
			job = j
			return nil
		})
		if err == errAbort {
			s.problem(w, r, status, codeForStatus(status), msg)
			return 0, nil
		}
		if err != nil {
			return 0, errInternal{err}
		}
		s.audit(r, a, "restore.files_started", "restore_point", req.RestorePointID, "ok",
			map[string]string{"job": job.ID, "agent": job.AgentID})
		return http.StatusAccepted, contracts.RestoreResponse{RestoreID: job.ID, JobID: job.ID, State: job.State}
	})
}

func isAbsPath(p string) bool {
	if p == "" {
		return false
	}
	if strings.HasPrefix(p, "/") {
		return true
	}
	// Windows: C:\ o \\servidor\recurso
	if len(p) >= 3 && p[1] == ':' && (p[2] == '\\' || p[2] == '/') {
		return true
	}
	return strings.HasPrefix(p, `\\`)
}

func planHasPendingJobType(st *store.State, agentID, jobType string) bool {
	for _, j := range st.Jobs {
		if j.AgentID == agentID && j.Type == jobType &&
			(j.State == store.JobPending || j.State == store.JobLeased || j.State == store.JobRunning) {
			return true
		}
	}
	return false
}

func atoiDefault(s string, def int) int {
	if s == "" {
		return def
	}
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			return def
		}
		n = n*10 + int(c-'0')
		if n > 1<<30 {
			return def
		}
	}
	return n
}

// keep: referencia para futuras ampliaciones (hash de cuerpo).
var _ = auth.Hash

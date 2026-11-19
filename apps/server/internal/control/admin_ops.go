package control

import (
	"encoding/json"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/juansoler/bdr/apps/server/internal/store"
	"github.com/juansoler/bdr/packages/contracts"
)

// --- Alertas ---

func (s *Server) handleAlerts(w http.ResponseWriter, r *http.Request) {
	if !allowMethod(w, r, http.MethodGet) {
		return
	}
	r, _, ok := s.authorize(w, r, "read")
	if !ok {
		return
	}
	state := r.URL.Query().Get("state")
	items := []contracts.AlertView{}
	_ = s.Store.View(r.Context(), func(st *store.State) error {
		for _, a := range st.Alerts {
			if state != "" && a.State != state {
				continue
			}
			items = append(items, alertView(a))
		}
		return nil
	})
	sort.Slice(items, func(i, j int) bool {
		pi, pj := severityRank(items[i].Severity), severityRank(items[j].Severity)
		if pi != pj {
			return pi > pj
		}
		return items[i].LastAt.After(items[j].LastAt)
	})
	writeJSON(w, http.StatusOK, contracts.AlertList{Items: items})
}

func (s *Server) handleAlertAction(w http.ResponseWriter, r *http.Request) {
	if !allowMethod(w, r, http.MethodPost) {
		return
	}
	r, a, ok := s.authorize(w, r, "operate")
	if !ok {
		return
	}
	rest := pathID("/api/v1/admin/alerts/", r.URL.Path)
	parts := strings.Split(rest, "/")
	if len(parts) != 2 {
		s.problem(w, r, http.StatusNotFound, "E_NOT_FOUND", "Ruta no encontrada")
		return
	}
	id, action := parts[0], parts[1]
	now := s.Now().UTC()
	err := s.Store.Update(r.Context(), func(st *store.State) error {
		al := st.Alerts[id]
		if al == nil {
			return store.ErrNotFound
		}
		switch action {
		case "ack":
			if al.State == contracts.AlertResolved {
				return store.ErrConflict
			}
			al.State = contracts.AlertAcknowledged
			al.AckBy = a.auditName()
			al.AckAt = &now
		case "resolve":
			al.State = contracts.AlertResolved
			al.ResolvedBy = a.auditName()
			al.ResolvedAt = &now
		default:
			return store.ErrNotFound
		}
		return nil
	})
	if err != nil {
		if err == store.ErrNotFound {
			s.problem(w, r, http.StatusNotFound, "E_NOT_FOUND", "Alerta o acción no encontrada")
			return
		}
		if err == store.ErrConflict {
			s.problem(w, r, http.StatusConflict, "E_CONFLICT", "La alerta ya está resuelta")
			return
		}
		s.internalError(w, r, err)
		return
	}
	s.audit(r, a, "alert."+action, "alert", id, "ok", nil)
	w.WriteHeader(http.StatusNoContent)
}

// handleAlertRules expone los umbrales configurables (por ahora en Settings).
func (s *Server) handleAlertRules(w http.ResponseWriter, r *http.Request) {
	if !allowMethod(w, r, http.MethodGet) {
		return
	}
	r, _, ok := s.authorize(w, r, "read")
	if !ok {
		return
	}
	var st2 store.Settings
	_ = s.Store.View(r.Context(), func(st *store.State) error {
		st2 = st.Settings
		return nil
	})
	writeJSON(w, http.StatusOK, map[string]any{
		"agent_offline_minutes": st2.AgentOfflineMinutes,
		"min_severity":          st2.AlertMinSeverity,
		"renotify_hours":        st2.AlertRenotifyHours,
	})
}

// --- Auditoría ---

func (s *Server) handleAudit(w http.ResponseWriter, r *http.Request) {
	if !allowMethod(w, r, http.MethodGet) {
		return
	}
	r, _, ok := s.authorize(w, r, "admin")
	if !ok {
		return
	}
	q := r.URL.Query()
	var before int64
	if c := q.Get("cursor"); c != "" {
		before = int64(atoiDefault(c, 0))
	}
	filter := store.AuditFilter{
		ActorID: q.Get("actor_id"), Action: q.Get("action"),
		TargetID: q.Get("target_id"), Result: q.Get("result"),
		BeforeSeq: before, Limit: atoiDefault(q.Get("limit"), 100),
	}
	items, nextBefore := s.Store.Audit().List(filter)
	next := ""
	if nextBefore > 0 {
		next = itoa(nextBefore)
	}
	writeJSON(w, http.StatusOK, contracts.AuditList{Items: items, NextCursor: next})
}

func itoa(n int64) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}

func (s *Server) handleAuditVerify(w http.ResponseWriter, r *http.Request) {
	if !allowMethod(w, r, http.MethodGet) {
		return
	}
	r, _, ok := s.authorize(w, r, "admin")
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, s.Store.Audit().Verify())
}

func (s *Server) handleAuditExport(w http.ResponseWriter, r *http.Request) {
	if !allowMethod(w, r, http.MethodGet) {
		return
	}
	r, a, ok := s.authorize(w, r, "admin")
	if !ok {
		return
	}
	s.audit(r, a, "audit.exported", "audit", "", "ok", nil)
	w.Header().Set("Content-Type", "application/x-ndjson")
	w.Header().Set("Content-Disposition", `attachment; filename="bdr-audit.ndjson"`)
	w.WriteHeader(http.StatusOK)
	_ = s.Store.Audit().Export(func(ev contracts.AuditEvent) error {
		b, _ := json.Marshal(ev)
		_, err := w.Write(append(b, '\n'))
		return err
	})
}

// --- Ajustes ---

func (s *Server) handleSettings(w http.ResponseWriter, r *http.Request) {
	if !allowMethod(w, r, http.MethodGet, http.MethodPut) {
		return
	}
	r, a, ok := s.authorize(w, r, "admin")
	if !ok {
		return
	}
	switch r.Method {
	case http.MethodGet:
		var st2 store.Settings
		_ = s.Store.View(r.Context(), func(st *store.State) error {
			st2 = st.Settings
			return nil
		})
		out := contracts.Settings{
			EnvironmentName: st2.EnvironmentName, AgentOfflineMinutes: st2.AgentOfflineMinutes,
			AlertMinSeverity: st2.AlertMinSeverity, AlertRenotifyHours: st2.AlertRenotifyHours,
			WebhookURL: st2.WebhookURL, WebhookConfigured: st2.WebhookSecretEnc != "",
			SMTPHost: st2.SMTPHost, SMTPPort: st2.SMTPPort, SMTPUsername: st2.SMTPUsername,
			SMTPConfigured: st2.SMTPPasswordEnc != "", SMTPFrom: st2.SMTPFrom,
			SMTPTo: st2.SMTPTo, SMTPStartTLS: st2.SMTPStartTLS, SessionHours: st2.SessionHours,
		}
		if out.AlertMinSeverity == "" {
			out.AlertMinSeverity = contracts.SeverityMedium
		}
		writeJSON(w, http.StatusOK, out)
	case http.MethodPut:
		var req contracts.Settings
		if !decodeJSON(w, r, &req, maxBodyAdmin) {
			return
		}
		if req.AlertMinSeverity != "" && severityRank(req.AlertMinSeverity) == 0 && req.AlertMinSeverity != contracts.SeverityLow {
			s.problem(w, r, http.StatusBadRequest, "E_VALIDATION", "alert_min_severity inválida")
			return
		}
		err := s.Store.Update(r.Context(), func(st *store.State) error {
			cfg := st.Settings
			cfg.EnvironmentName = orDefault(req.EnvironmentName, cfg.EnvironmentName)
			if req.AgentOfflineMinutes > 0 {
				cfg.AgentOfflineMinutes = req.AgentOfflineMinutes
			}
			if req.AlertMinSeverity != "" {
				cfg.AlertMinSeverity = req.AlertMinSeverity
			}
			if req.AlertRenotifyHours > 0 {
				cfg.AlertRenotifyHours = req.AlertRenotifyHours
			}
			cfg.WebhookURL = req.WebhookURL
			if req.WebhookSecret != "" {
				cfg.WebhookSecretEnc = s.Box.MustSeal(req.WebhookSecret)
			} else if req.WebhookURL == "" {
				cfg.WebhookSecretEnc = ""
			}
			cfg.SMTPHost = req.SMTPHost
			if req.SMTPPort > 0 {
				cfg.SMTPPort = req.SMTPPort
			}
			cfg.SMTPUsername = req.SMTPUsername
			if req.SMTPPassword != "" {
				cfg.SMTPPasswordEnc = s.Box.MustSeal(req.SMTPPassword)
			} else if req.SMTPHost == "" {
				cfg.SMTPPasswordEnc = ""
			}
			cfg.SMTPFrom = req.SMTPFrom
			cfg.SMTPTo = req.SMTPTo
			cfg.SMTPStartTLS = req.SMTPStartTLS
			if req.SessionHours > 0 {
				cfg.SessionHours = req.SessionHours
			}
			st.Settings = cfg
			st.BumpConfig()
			return nil
		})
		if err != nil {
			s.internalError(w, r, err)
			return
		}
		s.audit(r, a, "settings.updated", "settings", "", "ok", nil)
		w.WriteHeader(http.StatusNoContent)
	}
}

func (s *Server) handleTestNotification(w http.ResponseWriter, r *http.Request) {
	if !allowMethod(w, r, http.MethodPost) {
		return
	}
	r, a, ok := s.authorize(w, r, "admin")
	if !ok {
		return
	}
	out := s.notify.Test(r.Context())
	s.audit(r, a, "settings.notification_test", "settings", "", "ok", nil)
	writeJSON(w, http.StatusOK, out)
}

// --- Repositorios ---

func (s *Server) handleRepositories(w http.ResponseWriter, r *http.Request) {
	if !allowMethod(w, r, http.MethodGet) {
		return
	}
	r, _, ok := s.authorize(w, r, "read")
	if !ok {
		return
	}
	view := contracts.RepositoryView{ID: "primary", Name: "Repositorio principal", Destination: "NAS"}
	if s.Kopia == nil || s.Kopia.KindName == "" {
		view.Status = "unconfigured"
		view.StatusDetail = "Configura el repositorio con el CLI de Kopia usando el kopia.config del servidor"
		writeJSON(w, http.StatusOK, contracts.RepositoryList{Items: []contracts.RepositoryView{view}})
		return
	}
	view.Kind = s.Kopia.KindName
	view.URL = s.Kopia.ServerURL
	view.Path = s.Kopia.RepoPath
	view.Fingerprint = s.Kopia.ServerFingerprint
	view.ServerSide = s.Kopia.ServerSide()
	view.Status = "unknown"
	checked := s.Now().UTC()
	view.CheckedAt = &checked
	if view.ServerSide {
		ok2, detail, count, logical, stored := s.Kopia.Status(r.Context())
		view.StatusDetail = detail
		view.SnapshotCount = count
		view.LogicalBytes = logical
		view.StoredBytes = stored
		if ok2 {
			view.Status = "ok"
		} else {
			view.Status = "error"
		}
	} else {
		view.StatusDetail = "El plano de control no tiene conexión directa; navegar/verificar no disponible"
	}
	creds := 0
	_ = s.Store.View(r.Context(), func(st *store.State) error {
		creds = len(st.RepoCreds)
		return nil
	})
	view.AgentsWithCreds = creds
	writeJSON(w, http.StatusOK, contracts.RepositoryList{Items: []contracts.RepositoryView{view}})
}

// --- Resumen y sistema ---

func (s *Server) handleSummary(w http.ResponseWriter, r *http.Request) {
	if !allowMethod(w, r, http.MethodGet) {
		return
	}
	r, _, ok := s.authorize(w, r, "read")
	if !ok {
		return
	}
	now := s.Now().UTC()
	var out contracts.Summary
	out.GeneratedAt = now
	out.AlertsOpen = map[string]int{}
	err := s.Store.View(r.Context(), func(st *store.State) error {
		out.AgentsTotal = len(st.Agents)
		offline := s.offlineThreshold(st)
		rpoOK := map[string]bool{}
		for _, a := range st.Agents {
			switch {
			case a.RevokedAt != nil:
				out.AgentsRevoked++
			case a.LastSeenAt != nil && now.Sub(*a.LastSeenAt) <= offline:
				out.AgentsOnline++
			}
		}
		out.PlansTotal = len(st.Plans)
		for _, j := range st.Jobs {
			switch j.State {
			case store.JobPending, store.JobLeased, store.JobRunning:
				out.JobsActive++
			case store.JobSucceeded, store.JobFailed:
				age := now.Sub(j.CreatedAt)
				if j.State == store.JobSucceeded {
					if age <= 24*time.Hour {
						out.Success24h.Succeeded++
					}
					if age <= 7*24*time.Hour {
						out.Success7d.Succeeded++
					}
				} else {
					if age <= 24*time.Hour {
						out.Success24h.Failed++
					}
					if age <= 7*24*time.Hour {
						out.Success7d.Failed++
					}
				}
				if j.Type == contracts.JobTypeBackupFile {
					day := j.CreatedAt.UTC().Format("2006-01-02")
					found := false
					for i := range out.BackupsPerDay {
						if out.BackupsPerDay[i].Day == day {
							found = true
							if j.State == store.JobSucceeded {
								out.BackupsPerDay[i].Succeeded++
							} else {
								out.BackupsPerDay[i].Failed++
							}
						}
					}
					if !found && len(out.BackupsPerDay) < 30 {
						dc := contracts.DayCount{Day: day}
						if j.State == store.JobSucceeded {
							dc.Succeeded = 1
						} else {
							dc.Failed = 1
						}
						out.BackupsPerDay = append(out.BackupsPerDay, dc)
					}
				}
			}
		}
		out.Success24h.Percent = pct(out.Success24h)
		out.Success7d.Percent = pct(out.Success7d)
		sort.Slice(out.BackupsPerDay, func(i, j int) bool { return out.BackupsPerDay[i].Day < out.BackupsPerDay[j].Day })

		for _, rp := range st.RestorePoints {
			if rp.ExpiredAt != nil {
				continue
			}
			out.RestorePoints++
			out.LogicalBytes += rp.SizeBytes
			if rp.ErrorCount == 0 {
				// Protegido: RP correcto. El RPO se valida con el plan.
				rpoOK[rp.AgentID] = true
			}
		}
		for _, a := range st.Agents {
			if a.RevokedAt == nil && rpoOK[a.ID] {
				out.AgentsProtected++
			}
		}
		for _, al := range st.Alerts {
			if al.State != contracts.AlertResolved {
				out.AlertsOpen[al.Severity]++
			}
		}
		// Próximas ejecuciones.
		type up struct {
			t time.Time
			u contracts.UpcomingRun
		}
		var ups []up
		for _, se := range st.Schedule {
			if se.NextRunAt.IsZero() || se.NextRunAt.Before(now) {
				continue
			}
			p := st.Plans[se.PlanID]
			a := st.Agents[se.AgentID]
			if p == nil || a == nil {
				continue
			}
			ups = append(ups, up{se.NextRunAt, contracts.UpcomingRun{
				At: se.NextRunAt, PlanID: p.ID, PlanName: p.Name, AgentID: a.ID, Hostname: a.Hostname,
			}})
		}
		sort.Slice(ups, func(i, j int) bool { return ups[i].t.Before(ups[j].t) })
		for i, u := range ups {
			if i >= 5 {
				break
			}
			out.Upcoming = append(out.Upcoming, u.u)
		}
		// Últimas restauraciones.
		var restores []*store.Job
		for _, j := range st.Jobs {
			if j.Type == contracts.JobTypeRestoreFile {
				restores = append(restores, j)
			}
		}
		sort.Slice(restores, func(i, j int) bool { return restores[i].CreatedAt.After(restores[j].CreatedAt) })
		for i, j := range restores {
			if i >= 5 {
				break
			}
			out.RecentRestores = append(out.RecentRestores, jobView(j))
		}
		out.RepositoryStatus = "unconfigured"
		if s.Kopia != nil && s.Kopia.KindName != "" {
			if s.Kopia.ServerSide() {
				out.RepositoryStatus = "ok"
			} else {
				out.RepositoryStatus = "limited"
			}
		}
		s.mu.Lock()
		if !s.lastTick.IsZero() {
			t := s.lastTick
			out.LastSchedulerTick = &t
		}
		s.mu.Unlock()
		return nil
	})
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func pct(x contracts.Ratio) float64 {
	if x.Succeeded+x.Failed == 0 {
		return -1
	}
	return float64(x.Succeeded) * 100 / float64(x.Succeeded+x.Failed)
}

func (s *Server) handleSystemHealth(w http.ResponseWriter, r *http.Request) {
	if !allowMethod(w, r, http.MethodGet) {
		return
	}
	r, _, ok := s.authorize(w, r, "read")
	if !ok {
		return
	}
	comps := map[string]string{"store": "ok"}
	status := "ok"
	if err := s.Store.Ping(r.Context()); err != nil {
		comps["store"] = "error: " + err.Error()
		status = "degraded"
	}
	if s.Kopia != nil && s.Kopia.KindName != "" {
		if s.Kopia.ServerSide() {
			comps["repository"] = "ok"
		} else {
			comps["repository"] = "limited"
		}
	} else {
		comps["repository"] = "unconfigured"
	}
	if s.CA != nil {
		comps["pki"] = "ok"
	}
	av := s.Store.Audit().Verify()
	if !av.OK {
		comps["audit"] = "CORRUPTA en seq " + itoa(av.BrokenAt)
		status = "degraded"
	} else {
		comps["audit"] = "ok"
	}
	out := contracts.SystemHealth{
		Status: status, Version: s.Version, Components: comps,
		StartedAt: s.StartedAt, AgentTLS: s.AgentPublicURL != "",
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleSystemVersion(w http.ResponseWriter, r *http.Request) {
	if !allowMethod(w, r, http.MethodGet) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"version": s.Version})
}

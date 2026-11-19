package control

import (
	"encoding/json"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/juansoler/bdr/apps/server/internal/auth"
	"github.com/juansoler/bdr/apps/server/internal/store"
	"github.com/juansoler/bdr/packages/contracts"
)

// --- Agentes ---

func (s *Server) handleAdminAgents(w http.ResponseWriter, r *http.Request) {
	if !allowMethod(w, r, http.MethodGet) {
		return
	}
	r, _, ok := s.authorize(w, r, "read")
	if !ok {
		return
	}
	now := s.Now().UTC()
	views := []contracts.AgentView{}
	err := s.Store.View(r.Context(), func(st *store.State) error {
		views = s.agentViews(st, now)
		return nil
	})
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, contracts.AgentList{Items: views})
}

// agentViews calcula las vistas de todos los agentes (dentro de View).
func (s *Server) agentViews(st *store.State, now time.Time) []contracts.AgentView {
	out := make([]contracts.AgentView, 0, len(st.Agents))
	for _, a := range st.Agents {
		out = append(out, s.agentView(st, a, now))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Hostname < out[j].Hostname })
	return out
}

func (s *Server) offlineThreshold(st *store.State) time.Duration {
	mins := st.Settings.AgentOfflineMinutes
	if mins <= 0 {
		mins = 15
	}
	return time.Duration(mins) * time.Minute
}

// agentView convierte un agente a su vista con estado calculado.
func (s *Server) agentView(st *store.State, a *store.Agent, now time.Time) contracts.AgentView {
	onlineWindow := onlineWindowBase
	if s.HeartbeatSeconds > 0 {
		onlineWindow = time.Duration(3*s.HeartbeatSeconds) * time.Second
	}
	if w := s.offlineThreshold(st); w < onlineWindow {
		onlineWindow = w
	}
	status := contracts.AgentOffline
	switch {
	case a.RevokedAt != nil:
		status = contracts.AgentRevoked
	case a.LastSeenAt == nil:
		status = contracts.AgentPending
	case now.Sub(*a.LastSeenAt) <= onlineWindow:
		status = contracts.AgentOnline
	}

	v := contracts.AgentView{
		ID:            a.ID,
		Hostname:      a.Hostname,
		OSFamily:      a.OSFamily,
		OSVersion:     a.OSVersion,
		Arch:          a.Arch,
		AgentVersion:  a.AgentVersion,
		Status:        status,
		ReportedState: a.ReportedState,
		CurrentJobID:  a.CurrentJobID,
		LastSeenAt:    a.LastSeenAt,
		EnrolledAt:    a.EnrolledAt,
		RevokedAt:     a.RevokedAt,
		RevokedReason: a.RevokedReason,
		CertExpiresAt: a.CertNotAfter,
	}
	if len(a.Inventory) > 0 {
		v.Inventory = json.RawMessage(a.Inventory)
	}
	// Grupos.
	for _, g := range st.Groups {
		for _, id := range g.MemberIDs {
			if id == a.ID {
				v.Groups = append(v.Groups, contracts.GroupRef{ID: g.ID, Name: g.Name})
				break
			}
		}
	}
	// Credencial de repositorio.
	if rc := st.RepoCreds[a.ID]; rc != nil {
		v.RepoUsername = rc.Username
	}
	// Último backup y próximo.
	for _, rp := range st.RestorePoints {
		if rp.AgentID != a.ID || rp.ExpiredAt != nil {
			continue
		}
		if v.LastBackupAt == nil || rp.CreatedAt.After(*v.LastBackupAt) {
			t := rp.CreatedAt
			v.LastBackupAt = &t
			v.LastBackupState = "ok"
			if rp.ErrorCount > 0 {
				v.LastBackupState = "errors"
			}
		}
	}
	// Próxima ejecución programada.
	for _, se := range st.Schedule {
		if se.AgentID == a.ID && !se.NextRunAt.IsZero() {
			if v.NextBackupAt == nil || se.NextRunAt.Before(*v.NextBackupAt) {
				t := se.NextRunAt
				v.NextBackupAt = &t
			}
		}
	}
	return v
}

func decodeInventory(b []byte) (*contracts.Inventory, error) {
	var inv contracts.Inventory
	if err := json.Unmarshal(b, &inv); err != nil {
		return nil, err
	}
	return &inv, nil
}

// handleAgentAction reparte /api/v1/admin/agents/{id}/...
func (s *Server) handleAgentAction(w http.ResponseWriter, r *http.Request) {
	rest := pathID("/api/v1/admin/agents/", r.URL.Path)
	parts := strings.Split(rest, "/")
	if len(parts) == 0 || parts[0] == "" {
		s.problem(w, r, http.StatusNotFound, "E_NOT_FOUND", "Ruta no encontrada")
		return
	}
	agentID := parts[0]
	switch {
	case len(parts) == 1:
		s.agentDetail(w, r, agentID)
	case len(parts) == 2 && parts[1] == "backup":
		s.agentBackupNow(w, r, agentID)
	case len(parts) == 2 && parts[1] == "revoke":
		s.agentRevoke(w, r, agentID)
	case len(parts) == 2 && parts[1] == "delete":
		s.agentDelete(w, r, agentID)
	case len(parts) == 2 && parts[1] == "inventory":
		s.agentInventoryHistory(w, r, agentID)
	case len(parts) == 2 && parts[1] == "effective-plans":
		s.agentEffectivePlans(w, r, agentID)
	case len(parts) == 2 && parts[1] == "repo-credentials":
		s.agentRepoCredsView(w, r, agentID)
	case len(parts) == 3 && parts[1] == "repo-credentials" && parts[2] == "rotate":
		s.agentRepoCredsRotate(w, r, agentID)
	default:
		s.problem(w, r, http.StatusNotFound, "E_NOT_FOUND", "Ruta no encontrada")
	}
}

func (s *Server) agentDetail(w http.ResponseWriter, r *http.Request, agentID string) {
	if !allowMethod(w, r, http.MethodGet) {
		return
	}
	r, _, ok := s.authorize(w, r, "read")
	if !ok {
		return
	}
	now := s.Now().UTC()
	var detail contracts.AgentDetail
	err := s.Store.View(r.Context(), func(st *store.State) error {
		a := st.Agents[agentID]
		if a == nil {
			return store.ErrNotFound
		}
		detail.Agent = s.agentView(st, a, now)
		eff, _ := s.effectivePlans(st, agentID, now)
		for _, e := range eff {
			detail.EffectivePlans = append(detail.EffectivePlans, e)
		}
		if detail.EffectivePlans == nil {
			detail.EffectivePlans = []contracts.EffectivePlan{}
		}
		// Últimos 10 jobs.
		var jobs []*store.Job
		for _, j := range st.Jobs {
			if j.AgentID == agentID {
				jobs = append(jobs, j)
			}
		}
		sort.Slice(jobs, func(i, j int) bool { return jobs[i].CreatedAt.After(jobs[j].CreatedAt) })
		for i, j := range jobs {
			if i >= 10 {
				break
			}
			detail.RecentJobs = append(detail.RecentJobs, jobView(j))
		}
		if detail.RecentJobs == nil {
			detail.RecentJobs = []contracts.JobView{}
		}
		// Últimos 10 restore points.
		var rps []*store.RestorePoint
		for _, rp := range st.RestorePoints {
			if rp.AgentID == agentID && rp.ExpiredAt == nil {
				rps = append(rps, rp)
			}
		}
		sort.Slice(rps, func(i, j int) bool { return rps[i].CreatedAt.After(rps[j].CreatedAt) })
		for i, rp := range rps {
			if i >= 10 {
				break
			}
			detail.RestorePoints = append(detail.RestorePoints, s.restorePointView(st, rp))
		}
		if detail.RestorePoints == nil {
			detail.RestorePoints = []contracts.RestorePointView{}
		}
		for _, al := range st.Alerts {
			if al.AgentID == agentID && al.State != contracts.AlertResolved {
				detail.OpenAlerts = append(detail.OpenAlerts, alertView(al))
			}
		}
		if detail.OpenAlerts == nil {
			detail.OpenAlerts = []contracts.AlertView{}
		}
		return nil
	})
	if err != nil {
		if err == store.ErrNotFound {
			s.problem(w, r, http.StatusNotFound, "E_AGENT_NOT_FOUND", "Agente no encontrado")
			return
		}
		s.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, detail)
}

func (s *Server) agentRevoke(w http.ResponseWriter, r *http.Request, agentID string) {
	if !allowMethod(w, r, http.MethodPost) {
		return
	}
	r, a, ok := s.authorize(w, r, "admin")
	if !ok {
		return
	}
	var req contracts.RevokeRequest
	_ = decodeJSON(w, r, &req, maxBodyAdmin) // cuerpo opcional
	now := s.Now().UTC()
	err := s.Store.Update(r.Context(), func(st *store.State) error {
		ag := st.Agents[agentID]
		if ag == nil {
			return store.ErrNotFound
		}
		ag.RevokedAt = &now
		ag.RevokedReason = strings.TrimSpace(req.Reason)
		return nil
	})
	if err != nil {
		if err == store.ErrNotFound {
			s.problem(w, r, http.StatusNotFound, "E_AGENT_NOT_FOUND", "Agente no encontrado")
			return
		}
		s.internalError(w, r, err)
		return
	}
	s.audit(r, a, "agent.revoked", "agent", agentID, "ok", map[string]string{"reason": req.Reason})

	// En repositorio server, eliminar también el usuario del agente: sin
	// esta limpieza, un agente revocado podría seguir leyendo sus snapshots.
	// Es best-effort: el fallo queda en el log y en la alerta de operación.
	if s.Kopia != nil && s.Kopia.Kind() == "server" {
		var rc *store.RepoCredential
		_ = s.Store.View(r.Context(), func(st *store.State) error {
			rc = st.RepoCreds[agentID]
			return nil
		})
		if rc != nil {
			if err := s.Kopia.DeleteRepoUser(r.Context(), rc.Username, rc.Hostname); err != nil {
				s.Logger.Warn("no se pudo eliminar el usuario del repositorio del agente revocado",
					"agent_id", agentID, "error", err.Error())
			}
		}
	}
	w.WriteHeader(http.StatusNoContent)
}

// agentRepoCredsView devuelve el estado de la credencial de repositorio del
// agente (sin la contraseña).
// agentDelete elimina definitivamente el agente del inventario. Requiere que
// esté revocado: así no se borra por error un equipo aún en producción. Limpia
// sus dependencias (credencial de repositorio, inventario, programación,
// pertenencia a grupos y alertas abiertas); los jobs y restore points se
// conservan como rastro de auditoría (hostname queda vacío en sus vistas).
func (s *Server) agentDelete(w http.ResponseWriter, r *http.Request, agentID string) {
	if !allowMethod(w, r, http.MethodDelete) {
		return
	}
	r, a, ok := s.authorize(w, r, "admin")
	if !ok {
		return
	}
	var hostname string
	err := s.Store.Update(r.Context(), func(st *store.State) error {
		ag := st.Agents[agentID]
		if ag == nil {
			return store.ErrNotFound
		}
		if ag.RevokedAt == nil {
			return errAgentNotRevoked
		}
		hostname = ag.Hostname
		delete(st.Agents, agentID)
		delete(st.RepoCreds, agentID)
		delete(st.Inventory, agentID)
		for id, se := range st.Schedule {
			if se.AgentID == agentID {
				delete(st.Schedule, id)
			}
		}
		for gid, g := range st.Groups {
			rest := make([]string, 0, len(g.MemberIDs))
			for _, m := range g.MemberIDs {
				if m != agentID {
					rest = append(rest, m)
				}
			}
			if len(rest) != len(g.MemberIDs) {
				g.MemberIDs = rest
				g.UpdatedAt = s.Now().UTC()
				st.Groups[gid] = g
			}
		}
		for id, al := range st.Alerts {
			if al.AgentID == agentID && al.State != contracts.AlertResolved {
				delete(st.Alerts, id)
			}
		}
		st.BumpConfig()
		return nil
	})
	if err != nil {
		switch err {
		case store.ErrNotFound:
			s.problem(w, r, http.StatusNotFound, "E_AGENT_NOT_FOUND", "Agente no encontrado")
		case errAgentNotRevoked:
			s.problem(w, r, http.StatusConflict, "E_AGENT_NOT_REVOKED",
				"Revoca el acceso del equipo antes de eliminarlo")
		default:
			s.internalError(w, r, err)
		}
		return
	}
	s.audit(r, a, "agent.deleted", "agent", agentID, "ok", map[string]string{"hostname": hostname})
	w.WriteHeader(http.StatusNoContent)
}

var errAgentNotRevoked = &codedError{"agent not revoked"}

func (s *Server) agentRepoCredsView(w http.ResponseWriter, r *http.Request, agentID string) {
	if !allowMethod(w, r, http.MethodGet) {
		return
	}
	r, _, ok := s.authorize(w, r, "read")
	if !ok {
		return
	}
	var out *contracts.RepoCredsView
	err := s.Store.View(r.Context(), func(st *store.State) error {
		if st.Agents[agentID] == nil {
			return store.ErrNotFound
		}
		if rc := st.RepoCreds[agentID]; rc != nil {
			out = &contracts.RepoCredsView{
				AgentID: agentID, Username: rc.Username, Hostname: rc.Hostname,
				Version: rc.Version, RotatedAt: rc.RotatedAt,
			}
		}
		return nil
	})
	if err != nil {
		if err == store.ErrNotFound {
			s.problem(w, r, http.StatusNotFound, "E_AGENT_NOT_FOUND", "Agente no encontrado")
			return
		}
		s.internalError(w, r, err)
		return
	}
	if out == nil {
		s.problem(w, r, http.StatusNotFound, "E_REPO_CRED_MISSING", "El agente aún no tiene credencial de repositorio")
		return
	}
	writeJSON(w, http.StatusOK, out)
}

// agentRepoCredsRotate genera una nueva contraseña de repositorio para el
// agente y la aplica en el repositorio. La entrega al agente se produce en su
// siguiente GET /agent/repository-credentials; el aprovisionamiento de esa
// ruta es idempotente y converge si esta llamada no pudo aplicarse.
func (s *Server) agentRepoCredsRotate(w http.ResponseWriter, r *http.Request, agentID string) {
	if !allowMethod(w, r, http.MethodPost) {
		return
	}
	r, a, ok := s.authorize(w, r, "admin")
	if !ok {
		return
	}
	if s.Box == nil {
		s.problem(w, r, http.StatusServiceUnavailable, "E_SECRETS_UNCONFIGURED", "Sin caja de secretos no hay credenciales de repositorio por agente")
		return
	}
	if s.Kopia == nil || s.Kopia.Kind() != "server" {
		s.problem(w, r, http.StatusConflict, "E_REPO_KIND", "La rotación exige repositorio de tipo server")
		return
	}
	pw, err := auth.NewSecret(24)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	now := s.Now().UTC()
	var rc *store.RepoCredential
	err = s.Store.Update(r.Context(), func(st *store.State) error {
		ag := st.Agents[agentID]
		if ag == nil {
			return store.ErrNotFound
		}
		rc = st.RepoCreds[agentID]
		if rc == nil {
			rc = &store.RepoCredential{
				AgentID: agentID, Username: "agent-" + ag.ID[:8], Hostname: strings.ToLower(ag.Hostname),
				CreatedAt: now,
			}
		}
		rc.PasswordEnc = s.Box.MustSeal(pw)
		if rc.Version == 0 {
			rc.Version = 1
		} else {
			rc.Version++
		}
		rc.RotatedAt = now
		st.RepoCreds[agentID] = rc
		return nil
	})
	if err != nil {
		if err == store.ErrNotFound {
			s.problem(w, r, http.StatusNotFound, "E_AGENT_NOT_FOUND", "Agente no encontrado")
			return
		}
		s.internalError(w, r, err)
		return
	}
	out := contracts.RepoCredsView{
		AgentID: agentID, Username: rc.Username, Hostname: rc.Hostname,
		Version: rc.Version, RotatedAt: rc.RotatedAt,
	}
	// Aplicar la contraseña en el repositorio. Si falla, la siguiente
	// entrega de credenciales reintentará (convergencia).
	if err := s.Kopia.ProvisionRepoUser(r.Context(), rc.Username, rc.Hostname, pw); err != nil {
		s.Logger.Warn("rotación aplicada en el estado pero no en el repositorio", "agent_id", agentID, "error", err.Error())
		out.PendingApply = true
	}
	s.audit(r, a, "agent.repo_creds_rotated", "agent", agentID, "ok",
		map[string]string{"version": strconv.Itoa(rc.Version)})
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) agentInventoryHistory(w http.ResponseWriter, r *http.Request, agentID string) {
	if !allowMethod(w, r, http.MethodGet) {
		return
	}
	r, _, ok := s.authorize(w, r, "read")
	if !ok {
		return
	}
	var hist []store.InventoryRecord
	err := s.Store.View(r.Context(), func(st *store.State) error {
		if st.Agents[agentID] == nil {
			return store.ErrNotFound
		}
		hist = append(hist, st.Inventory[agentID]...)
		return nil
	})
	if err != nil {
		if err == store.ErrNotFound {
			s.problem(w, r, http.StatusNotFound, "E_AGENT_NOT_FOUND", "Agente no encontrado")
			return
		}
		s.internalError(w, r, err)
		return
	}
	sort.Slice(hist, func(i, j int) bool { return hist[i].CollectedAt.After(hist[j].CollectedAt) })
	items := make([]contracts.InventoryRecord, 0, len(hist))
	for _, h := range hist {
		items = append(items, contracts.InventoryRecord{Hash: h.Hash, CollectedAt: h.CollectedAt, Inventory: json.RawMessage(h.Data)})
	}
	writeJSON(w, http.StatusOK, contracts.InventoryHistory{Items: items})
}

// --- Tokens de enrolamiento ---

func (s *Server) handleEnrollmentTokens(w http.ResponseWriter, r *http.Request) {
	if !allowMethod(w, r, http.MethodGet, http.MethodPost) {
		return
	}
	r, a, ok := s.authorize(w, r, "operate")
	if !ok {
		return
	}
	switch r.Method {
	case http.MethodGet:
		var toks []store.EnrollmentToken
		_ = s.Store.View(r.Context(), func(st *store.State) error {
			for _, t := range st.Tokens {
				toks = append(toks, *t)
			}
			return nil
		})
		sort.Slice(toks, func(i, j int) bool { return toks[i].CreatedAt.After(toks[j].CreatedAt) })
		now := s.Now().UTC()
		items := make([]contracts.EnrollmentTokenView, 0, len(toks))
		for _, t := range toks {
			items = append(items, contracts.EnrollmentTokenView{
				ID: t.ID, CreatedAt: t.CreatedAt, ExpiresAt: t.ExpiresAt,
				UsedAt: t.UsedAt, UsedByAgentID: t.UsedByAgentID,
				ExpectedHostname: t.ExpectedHostname, Expired: now.After(t.ExpiresAt),
				RevokedAt: t.RevokedAt, GroupID: t.GroupID, PlanID: t.PlanID,
				Note: t.Note, CreatedBy: t.CreatedBy,
			})
		}
		writeJSON(w, http.StatusOK, contracts.EnrollmentTokenList{Items: items})
	case http.MethodPost:
		s.withIdempotency(w, r, a, func() (int, any) {
			var req contracts.EnrollmentTokenRequest
			if !decodeJSON(w, r, &req, maxBodyAdmin) {
				return 0, nil
			}
			ttl := time.Hour
			if req.ExpiresInSeconds > 0 {
				ttl = time.Duration(req.ExpiresInSeconds) * time.Second
			}
			if ttl > maxTokenTTL {
				ttl = maxTokenTTL
			}
			if ttl < time.Minute {
				ttl = time.Minute
			}
			token, err := auth.NewSecret(32)
			if err != nil {
				return 0, errInternal{err}
			}
			id, err := newID()
			if err != nil {
				return 0, errInternal{err}
			}
			now := s.Now().UTC()
			rec := store.EnrollmentToken{
				ID: id, TokenHash: auth.Hash(token),
				ExpectedHostname: strings.TrimSpace(req.ExpectedHostname),
				CreatedAt:        now, ExpiresAt: now.Add(ttl),
				GroupID: req.GroupID, PlanID: req.PlanID,
				Note: req.Note, CreatedBy: a.auditName(),
			}
			if err := s.Store.Update(r.Context(), func(st *store.State) error {
				st.Tokens[id] = &rec
				return nil
			}); err != nil {
				return 0, errInternal{err}
			}
			s.audit(r, a, "enrollment_token.created", "enrollment_token", id, "ok",
				map[string]string{"hostname": rec.ExpectedHostname, "plan": rec.PlanID})
			s.Logger.Info("enrollment token created", "token_id", id, "expires_at", rec.ExpiresAt)
			return http.StatusCreated, contracts.EnrollmentTokenResponse{
				ID: id, Token: token, ExpiresAt: rec.ExpiresAt,
				ServerURL: s.PublicURL, AgentURL: s.AgentPublicURL, ServerFingerprint: s.CertFingerprint,
			}
		})
	}
}

func (s *Server) handleEnrollmentTokenAction(w http.ResponseWriter, r *http.Request) {
	if !allowMethod(w, r, http.MethodDelete) {
		return
	}
	r, a, ok := s.authorize(w, r, "operate")
	if !ok {
		return
	}
	id := pathID("/api/v1/admin/enrollment-tokens/", r.URL.Path)
	now := s.Now().UTC()
	err := s.Store.Update(r.Context(), func(st *store.State) error {
		t := st.Tokens[id]
		if t == nil {
			return store.ErrNotFound
		}
		t.RevokedAt = &now
		return nil
	})
	if err != nil {
		if err == store.ErrNotFound {
			s.problem(w, r, http.StatusNotFound, "E_NOT_FOUND", "Token no encontrado")
			return
		}
		s.internalError(w, r, err)
		return
	}
	s.audit(r, a, "enrollment_token.revoked", "enrollment_token", id, "ok", nil)
	w.WriteHeader(http.StatusNoContent)
}

// --- Grupos ---

func (s *Server) handleGroups(w http.ResponseWriter, r *http.Request) {
	if !allowMethod(w, r, http.MethodGet, http.MethodPost) {
		return
	}
	r, a, ok := s.authorize(w, r, "operate")
	if !ok {
		return
	}
	switch r.Method {
	case http.MethodGet:
		groups := []contracts.GroupView{}
		_ = s.Store.View(r.Context(), func(st *store.State) error {
			for _, g := range st.Groups {
				groups = append(groups, s.groupView(st, g))
			}
			return nil
		})
		sort.Slice(groups, func(i, j int) bool { return groups[i].Name < groups[j].Name })
		writeJSON(w, http.StatusOK, contracts.GroupList{Items: groups})
	case http.MethodPost:
		var req contracts.GroupRequest
		if !decodeJSON(w, r, &req, maxBodyAdmin) {
			return
		}
		if req.Name == nil || strings.TrimSpace(*req.Name) == "" {
			s.problem(w, r, http.StatusBadRequest, "E_VALIDATION", "name es obligatorio")
			return
		}
		id, err := newID()
		if err != nil {
			s.internalError(w, r, err)
			return
		}
		now := s.Now().UTC()
		g := store.Group{ID: id, Name: strings.TrimSpace(*req.Name),
			Description: orDefaultPtr(req.Description, ""), ParentID: orDefaultPtr(req.ParentID, ""),
			MemberIDs: []string{}, CreatedAt: now, UpdatedAt: now}
		if err := s.Store.Update(r.Context(), func(st *store.State) error {
			if req.ParentID != nil && *req.ParentID != "" && st.Groups[*req.ParentID] == nil {
				return store.ErrNotFound
			}
			if groupCycle(st, g.ParentID, id) {
				return errGroupCycle
			}
			st.Groups[g.ID] = &g
			st.BumpConfig()
			return nil
		}); err != nil {
			switch err {
			case store.ErrNotFound:
				s.problem(w, r, http.StatusBadRequest, "E_VALIDATION", "El grupo padre no existe")
			case errGroupCycle:
				s.problem(w, r, http.StatusBadRequest, "E_GROUP_CYCLE", "La jerarquía de grupos sería cíclica")
			default:
				s.internalError(w, r, err)
			}
			return
		}
		s.audit(r, a, "group.created", "group", id, "ok", map[string]string{"name": g.Name})
		var out *contracts.GroupView
		_ = s.Store.View(r.Context(), func(st *store.State) error {
			if gg := st.Groups[id]; gg != nil {
				v := s.groupView(st, gg)
				out = &v
			}
			return nil
		})
		if out == nil {
			s.internalError(w, r, errGroupCycle)
			return
		}
		writeJSON(w, http.StatusCreated, *out)
	}
}

var errGroupCycle = &codedError{"group cycle"}

// groupCycle indica si hacer que parent sea ancestro de childID crearía un ciclo.
func groupCycle(st *store.State, parent, childID string) bool {
	if parent == childID {
		return true
	}
	for parent != "" {
		g := st.Groups[parent]
		if g == nil {
			return false
		}
		if g.ParentID == childID {
			return true
		}
		parent = g.ParentID
	}
	return false
}

func (s *Server) groupView(st *store.State, g *store.Group) contracts.GroupView {
	v := contracts.GroupView{
		ID: g.ID, Name: g.Name, Description: g.Description, ParentID: g.ParentID,
		MemberIDs: append([]string(nil), g.MemberIDs...),
		CreatedAt: g.CreatedAt, UpdatedAt: g.UpdatedAt,
	}
	for _, a := range st.Assignments {
		if a.TargetType == contracts.TargetGroup && a.TargetID == g.ID && a.Enabled {
			v.PlanIDs = append(v.PlanIDs, a.PlanID)
		}
	}
	return v
}

func (s *Server) handleGroupAction(w http.ResponseWriter, r *http.Request) {
	rest := pathID("/api/v1/admin/groups/", r.URL.Path)
	parts := strings.Split(rest, "/")
	if len(parts) == 0 || parts[0] == "" {
		s.problem(w, r, http.StatusNotFound, "E_NOT_FOUND", "Ruta no encontrada")
		return
	}
	id := parts[0]
	if len(parts) == 1 {
		s.groupItem(w, r, id)
		return
	}
	if len(parts) == 2 && parts[1] == "members" {
		s.groupMembers(w, r, id)
		return
	}
	if len(parts) == 3 && parts[1] == "members" {
		s.groupMemberDelete(w, r, id, parts[2])
		return
	}
	s.problem(w, r, http.StatusNotFound, "E_NOT_FOUND", "Ruta no encontrada")
}

func (s *Server) groupItem(w http.ResponseWriter, r *http.Request, id string) {
	if !allowMethod(w, r, http.MethodGet, http.MethodPatch, http.MethodDelete) {
		return
	}
	r, a, ok := s.authorize(w, r, "operate")
	if !ok {
		return
	}
	switch r.Method {
	case http.MethodGet:
		var out *contracts.GroupView
		err := s.Store.View(r.Context(), func(st *store.State) error {
			g := st.Groups[id]
			if g == nil {
				return store.ErrNotFound
			}
			v := s.groupView(st, g)
			out = &v
			return nil
		})
		if err != nil {
			s.groupErr(w, r, err)
			return
		}
		writeJSON(w, http.StatusOK, *out)
	case http.MethodPatch:
		var req contracts.GroupRequest
		if !decodeJSON(w, r, &req, maxBodyAdmin) {
			return
		}
		now := s.Now().UTC()
		err := s.Store.Update(r.Context(), func(st *store.State) error {
			g := st.Groups[id]
			if g == nil {
				return store.ErrNotFound
			}
			if req.Name != nil && strings.TrimSpace(*req.Name) != "" {
				g.Name = strings.TrimSpace(*req.Name)
			}
			if req.Description != nil {
				g.Description = *req.Description
			}
			if req.ParentID != nil {
				parent := strings.TrimSpace(*req.ParentID)
				if parent != "" && st.Groups[parent] == nil {
					return store.ErrNotFound
				}
				if parent == g.ID || groupCycle(st, parent, g.ID) {
					return errGroupCycle
				}
				g.ParentID = parent
			}
			g.UpdatedAt = now
			st.BumpConfig()
			return nil
		})
		if err != nil {
			switch err {
			case store.ErrNotFound:
				s.problem(w, r, http.StatusNotFound, "E_NOT_FOUND", "Grupo o padre no encontrado")
			case errGroupCycle:
				s.problem(w, r, http.StatusBadRequest, "E_GROUP_CYCLE", "La jerarquía de grupos sería cíclica")
			default:
				s.internalError(w, r, err)
			}
			return
		}
		s.audit(r, a, "group.updated", "group", id, "ok", nil)
		var out *contracts.GroupView
		_ = s.Store.View(r.Context(), func(st *store.State) error {
			if g := st.Groups[id]; g != nil {
				v := s.groupView(st, g)
				out = &v
			}
			return nil
		})
		writeJSON(w, http.StatusOK, *out)
	case http.MethodDelete:
		err := s.Store.Update(r.Context(), func(st *store.State) error {
			g := st.Groups[id]
			if g == nil {
				return store.ErrNotFound
			}
			for _, other := range st.Groups {
				if other.ParentID == id {
					other.ParentID = g.ParentID
				}
			}
			delete(st.Groups, id)
			for aid, a2 := range st.Assignments {
				if a2.TargetType == contracts.TargetGroup && a2.TargetID == id {
					delete(st.Assignments, aid)
				}
			}
			st.BumpConfig()
			return nil
		})
		if err != nil {
			s.groupErr(w, r, err)
			return
		}
		s.audit(r, a, "group.deleted", "group", id, "ok", nil)
		w.WriteHeader(http.StatusNoContent)
	}
}

func (s *Server) groupErr(w http.ResponseWriter, r *http.Request, err error) {
	if err == store.ErrNotFound {
		s.problem(w, r, http.StatusNotFound, "E_NOT_FOUND", "Grupo no encontrado")
		return
	}
	s.internalError(w, r, err)
}

func (s *Server) groupMembers(w http.ResponseWriter, r *http.Request, id string) {
	if !allowMethod(w, r, http.MethodGet, http.MethodPost) {
		return
	}
	r, a, ok := s.authorize(w, r, "operate")
	if !ok {
		return
	}
	switch r.Method {
	case http.MethodGet:
		members := []contracts.AgentView{}
		_ = s.Store.View(r.Context(), func(st *store.State) error {
			g := st.Groups[id]
			if g == nil {
				return store.ErrNotFound
			}
			now := s.Now().UTC()
			for _, mid := range g.MemberIDs {
				if ag := st.Agents[mid]; ag != nil {
					members = append(members, s.agentView(st, ag, now))
				}
			}
			return nil
		})
		writeJSON(w, http.StatusOK, contracts.AgentList{Items: members})
	case http.MethodPost:
		var req contracts.GroupMemberRequest
		if !decodeJSON(w, r, &req, maxBodyAdmin) {
			return
		}
		if req.AgentID == "" {
			s.problem(w, r, http.StatusBadRequest, "E_VALIDATION", "agent_id es obligatorio")
			return
		}
		err := s.Store.Update(r.Context(), func(st *store.State) error {
			g := st.Groups[id]
			if g == nil {
				return store.ErrNotFound
			}
			if st.Agents[req.AgentID] == nil {
				return store.ErrNotFound
			}
			for _, m := range g.MemberIDs {
				if m == req.AgentID {
					return nil // idempotente
				}
			}
			g.MemberIDs = append(g.MemberIDs, req.AgentID)
			g.UpdatedAt = s.Now().UTC()
			st.BumpConfig()
			return nil
		})
		if err != nil {
			s.groupErr(w, r, err)
			return
		}
		s.audit(r, a, "group.member_added", "group", id, "ok", map[string]string{"agent": req.AgentID})
		w.WriteHeader(http.StatusNoContent)
	}
}

func (s *Server) groupMemberDelete(w http.ResponseWriter, r *http.Request, id, agentID string) {
	if !allowMethod(w, r, http.MethodDelete) {
		return
	}
	r, a, ok := s.authorize(w, r, "operate")
	if !ok {
		return
	}
	err := s.Store.Update(r.Context(), func(st *store.State) error {
		g := st.Groups[id]
		if g == nil {
			return store.ErrNotFound
		}
		rest := make([]string, 0, len(g.MemberIDs))
		for _, m := range g.MemberIDs {
			if m != agentID {
				rest = append(rest, m)
			}
		}
		g.MemberIDs = rest
		g.UpdatedAt = s.Now().UTC()
		st.BumpConfig()
		return nil
	})
	if err != nil {
		s.groupErr(w, r, err)
		return
	}
	s.audit(r, a, "group.member_removed", "group", id, "ok", map[string]string{"agent": agentID})
	w.WriteHeader(http.StatusNoContent)
}

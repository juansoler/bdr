package control

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/juansoler/bdr/apps/server/internal/cron"
	"github.com/juansoler/bdr/apps/server/internal/store"
	"github.com/juansoler/bdr/packages/contracts"
)

// --- Planes ---

func (s *Server) handlePlans(w http.ResponseWriter, r *http.Request) {
	if !allowMethod(w, r, http.MethodGet, http.MethodPost) {
		return
	}
	r, a, ok := s.authorize(w, r, "operate")
	if !ok {
		return
	}
	switch r.Method {
	case http.MethodGet:
		plans := []contracts.PlanView{}
		now := s.Now().UTC()
		_ = s.Store.View(r.Context(), func(st *store.State) error {
			for _, p := range st.Plans {
				plans = append(plans, s.planView(st, p, now))
			}
			return nil
		})
		sort.Slice(plans, func(i, j int) bool { return plans[i].CreatedAt.After(plans[j].CreatedAt) })
		writeJSON(w, http.StatusOK, contracts.PlanList{Items: plans})
	case http.MethodPost:
		s.withIdempotency(w, r, a, func() (int, any) {
			var req contracts.PlanRequest
			if !decodeJSON(w, r, &req, maxBodyAdmin) {
				return 0, nil
			}
			p, verr := s.validatePlanRequest(nil, &req)
			if verr != "" {
				return 0, errProblem{Status: http.StatusBadRequest, Code: "E_VALIDATION", Detail: verr}
			}
			id, err := newID()
			if err != nil {
				return 0, errInternal{err}
			}
			now := s.Now().UTC()
			p.ID = id
			p.CreatedAt = now
			p.UpdatedAt = now
			if err := s.Store.Update(r.Context(), func(st *store.State) error {
				st.Plans[p.ID] = p
				st.BumpConfig()
				return nil
			}); err != nil {
				return 0, errInternal{err}
			}
			s.audit(r, a, "plan.created", "plan", p.ID, "ok", map[string]string{"name": p.Name})
			var out contracts.PlanView
			_ = s.Store.View(r.Context(), func(st *store.State) error {
				out = s.planView(st, st.Plans[p.ID], now)
				return nil
			})
			return http.StatusCreated, out
		})
	}
}

type errProblem struct {
	Status int
	Code   string
	Detail string
}

func (e errProblem) Error() string { return e.Detail }

// validatePlanRequest aplica las reglas de un plan (crear o actualizar).
func (s *Server) validatePlanRequest(cur *store.Plan, req *contracts.PlanRequest) (*store.Plan, string) {
	p := store.Plan{}
	if cur != nil {
		p = *cur
		p.Paths = append([]string(nil), cur.Paths...)
		p.Excludes = append([]string(nil), cur.Excludes...)
		p.Disks = append([]int(nil), cur.Disks...)
	}
	if req.Name != nil {
		p.Name = strings.TrimSpace(*req.Name)
	} else if cur == nil {
		return nil, "name es obligatorio"
	}
	if cur == nil && p.Name == "" {
		return nil, "name es obligatorio"
	}
	if req.Description != nil {
		p.Description = *req.Description
	}
	if req.Paths != nil {
		p.Paths = trimNonEmpty(req.Paths)
	} else if cur == nil && req.Kind != "image" {
		return nil, "paths es obligatorio"
	}
	if req.Excludes != nil {
		p.Excludes = trimNonEmpty(req.Excludes)
	}
	if req.Disks != nil {
		disks, derr := normalizeDisks(req.Disks)
		if derr != "" {
			return nil, derr
		}
		p.Disks = disks
	}
	switch req.Kind {
	case "", "file":
		if cur == nil {
			p.Kind = "file"
		}
		if cur == nil && len(p.Paths) == 0 {
			return nil, "el plan necesita al menos una ruta"
		}
	case "image":
		// Imagen completa del equipo: sin rutas; exige destino de
		// almacenamiento propio (S3 o ruta física; los segmentos se suben
		// en streaming al destino).
		if cur == nil {
			p.Kind = "image"
			p.Paths = nil
		} else if p.Kind != "image" && cur.Kind != "image" {
			return nil, "el kind de un plan no se puede cambiar; crea otro plan"
		}
		if p.StorageTargetID == "" && (cur == nil || cur.StorageTargetID == "") &&
			(req.StorageTargetID == nil || strings.TrimSpace(*req.StorageTargetID) == "") {
			return nil, "los planes de imagen requieren un destino de almacenamiento (S3 o ruta física)"
		}
	default:
		return nil, "kind no soportado: usa file o image"
	}
	// La selección de discos solo tiene sentido en imagen. Se comprueba
	// sobre el kind final resuelto (en PATCH req.Kind puede venir vacío).
	if p.Kind == "file" && len(p.Disks) > 0 {
		return nil, "disks solo aplica a planes de imagen"
	}
	if cur == nil {
		if req.Enabled == nil || *req.Enabled {
			p.Enabled = true
		}
	} else if req.Enabled != nil {
		p.Enabled = *req.Enabled
	}
	if req.Schedule != nil {
		sched := strings.TrimSpace(*req.Schedule)
		if sched != "" {
			if _, err := cron.Parse(sched); err != nil {
				return nil, "schedule inválido: " + err.Error()
			}
		}
		p.Schedule = sched
	}
	if req.Timezone != nil {
		tz := strings.TrimSpace(*req.Timezone)
		if tz == "" {
			tz = "UTC"
		}
		if _, err := time.LoadLocation(tz); err != nil {
			return nil, "timezone desconocida: " + tz
		}
		p.Timezone = tz
	} else if cur == nil {
		p.Timezone = "UTC"
	}
	if req.Window != nil {
		if _, err := cron.ParseClock(req.Window.Start); err != nil {
			return nil, "window.start debe ser HH:MM"
		}
		if _, err := cron.ParseClock(req.Window.End); err != nil {
			return nil, "window.end debe ser HH:MM"
		}
		p.Window = req.Window
	}
	if req.Retention != nil {
		if err := validateRetention(req.Retention); err != "" {
			return nil, err
		}
		p.Retention = *req.Retention
	} else if cur == nil {
		p.Retention = contracts.Retention{KeepLatest: 7, KeepDaily: 7, KeepWeekly: 4, KeepMonthly: 6}
	}
	if req.RPOHours != nil {
		if *req.RPOHours < 0 || *req.RPOHours > 24*365 {
			return nil, "rpo_hours fuera de rango"
		}
		p.RPOHours = *req.RPOHours
	}
	// Destino de almacenamiento: debe existir y estar habilitado. Los
	// planes de archivo no admiten destinos SMB (kopia no escribe en
	// compartidas sin montar); imagen sí (upload directo del agente).
	if req.StorageTargetID != nil {
		id := strings.TrimSpace(*req.StorageTargetID)
		if id != "" {
			var found bool
			var isSMBFilePlan bool
			_ = s.Store.View(context.Background(), func(st *store.State) error {
				t := st.Storage[id]
				if t != nil && t.Enabled {
					found = true
					isSMBFilePlan = t.IsSMB() && p.Kind != "image"
				}
				return nil
			})
			if !found {
				return nil, "el destino de almacenamiento no existe o está deshabilitado"
			}
			if isSMBFilePlan {
				return nil, "los planes de archivo no admiten destinos SMB: usa S3 o una ruta local/NFS (los SMB valen para imagen)"
			}
		}
		p.StorageTargetID = id
	}
	return &p, ""
}

func validateRetention(r *contracts.Retention) string {
	vals := []int{r.KeepLatest, r.KeepHourly, r.KeepDaily, r.KeepWeekly, r.KeepMonthly, r.KeepAnnual}
	for _, v := range vals {
		if v < 0 || v > 9999 {
			return "retención fuera de rango"
		}
	}
	total := 0
	for _, v := range vals {
		total += v
	}
	if total == 0 {
		return "la retención debe conservar al menos un snapshot"
	}
	return ""
}

func trimNonEmpty(in []string) []string {
	out := make([]string, 0, len(in))
	for _, s := range in {
		if t := strings.TrimSpace(s); t != "" {
			out = append(out, t)
		}
	}
	return out
}

// diskListStr formatea índices de disco para mensajes: "0, 1, 2".
func diskListStr(disks []int) string {
	parts := make([]string, len(disks))
	for i, d := range disks {
		parts[i] = strconv.Itoa(d)
	}
	return strings.Join(parts, ", ")
}

// normalizeDisks valida la selección de discos de un plan de imagen:
// índices 0..255, sin duplicados y en orden. Vacío (o todo ceros de
// longitud cero) devuelve nil = capturar todos los discos fijos.
func normalizeDisks(in []int) ([]int, string) {
	seen := make(map[int]bool, len(in))
	out := make([]int, 0, len(in))
	for _, d := range in {
		if d < 0 || d > 255 {
			return nil, "disks: índice fuera de rango (0-255)"
		}
		if seen[d] {
			continue
		}
		seen[d] = true
		out = append(out, d)
	}
	if len(out) == 0 {
		return nil, ""
	}
	sort.Ints(out)
	return out, ""
}

func (s *Server) planView(st *store.State, p *store.Plan, now time.Time) contracts.PlanView {
	v := contracts.PlanView{
		ID: p.ID, Name: p.Name, Description: p.Description, Kind: p.Kind,
		Paths: append([]string(nil), p.Paths...), Excludes: append([]string(nil), p.Excludes...),
		Disks:   append([]int(nil), p.Disks...),
		Enabled: p.Enabled, Schedule: p.Schedule, Timezone: p.Timezone,
		Window: p.Window, Retention: p.Retention, RPOHours: p.RPOHours,
		StorageTargetID: p.StorageTargetID,
		CreatedAt:       p.CreatedAt, UpdatedAt: p.UpdatedAt,
	}
	if p.Schedule != "" {
		if sch, err := cron.Parse(p.Schedule); err == nil {
			loc := timeZone(p.Timezone)
			v.NextRuns = sch.NextN(now.In(loc), 5)
		}
	}
	for _, a := range st.Assignments {
		if a.PlanID == p.ID {
			v.Assignments = append(v.Assignments, s.assignmentView(st, a))
		}
	}
	return v
}

func timeZone(name string) *time.Location {
	if loc, err := time.LoadLocation(name); err == nil {
		return loc
	}
	return time.UTC
}

// handlePlanAction reparte /api/v1/admin/backup-plans/{id}/...
func (s *Server) handlePlanAction(w http.ResponseWriter, r *http.Request) {
	rest := pathID("/api/v1/admin/backup-plans/", r.URL.Path)
	parts := strings.Split(rest, "/")
	if len(parts) == 0 || parts[0] == "" {
		s.problem(w, r, http.StatusNotFound, "E_NOT_FOUND", "Ruta no encontrada")
		return
	}
	id := parts[0]
	switch {
	case len(parts) == 1:
		s.planItem(w, r, id)
	case len(parts) == 2 && parts[1] == "assign":
		s.planAssignCompat(w, r, id)
	case len(parts) == 2 && parts[1] == "assignments":
		s.planAssignments(w, r, id)
	case len(parts) == 2 && parts[1] == "validate":
		s.planValidate(w, r, id)
	default:
		s.problem(w, r, http.StatusNotFound, "E_NOT_FOUND", "Ruta no encontrada")
	}
}

func (s *Server) planItem(w http.ResponseWriter, r *http.Request, id string) {
	if !allowMethod(w, r, http.MethodGet, http.MethodPatch, http.MethodDelete) {
		return
	}
	cap := "operate"
	if r.Method == http.MethodDelete {
		cap = "admin"
	}
	r, a, ok := s.authorize(w, r, cap)
	if !ok {
		return
	}
	switch r.Method {
	case http.MethodGet:
		now := s.Now().UTC()
		var out *contracts.PlanView
		err := s.Store.View(r.Context(), func(st *store.State) error {
			p := st.Plans[id]
			if p == nil {
				return store.ErrNotFound
			}
			v := s.planView(st, p, now)
			out = &v
			return nil
		})
		if err != nil {
			s.planErr(w, r, err)
			return
		}
		writeJSON(w, http.StatusOK, *out)
	case http.MethodPatch:
		var req contracts.PlanRequest
		if !decodeJSON(w, r, &req, maxBodyAdmin) {
			return
		}
		now := s.Now().UTC()
		err := s.Store.Update(r.Context(), func(st *store.State) error {
			p := st.Plans[id]
			if p == nil {
				return store.ErrNotFound
			}
			updated, verr := s.validatePlanRequest(p, &req)
			if verr != "" {
				return errValidation(verr)
			}
			updated.UpdatedAt = now
			st.Plans[id] = updated
			st.BumpConfig()
			return nil
		})
		if err != nil {
			if ve, ok2 := err.(errValidation); ok2 {
				s.problem(w, r, http.StatusBadRequest, "E_VALIDATION", string(ve))
				return
			}
			s.planErr(w, r, err)
			return
		}
		s.audit(r, a, "plan.updated", "plan", id, "ok", nil)
		var out *contracts.PlanView
		_ = s.Store.View(r.Context(), func(st *store.State) error {
			if p := st.Plans[id]; p != nil {
				v := s.planView(st, p, now)
				out = &v
			}
			return nil
		})
		writeJSON(w, http.StatusOK, *out)
	case http.MethodDelete:
		err := s.Store.Update(r.Context(), func(st *store.State) error {
			p := st.Plans[id]
			if p == nil {
				return store.ErrNotFound
			}
			delete(st.Plans, id)
			for aid, as := range st.Assignments {
				if as.PlanID == id {
					delete(st.Assignments, aid)
				}
			}
			for k, se := range st.Schedule {
				if se.PlanID == id {
					delete(st.Schedule, k)
				}
			}
			st.BumpConfig()
			return nil
		})
		if err != nil {
			s.planErr(w, r, err)
			return
		}
		s.audit(r, a, "plan.deleted", "plan", id, "ok", nil)
		w.WriteHeader(http.StatusNoContent)
	}
}

type errValidation string

func (e errValidation) Error() string { return string(e) }

func (s *Server) planErr(w http.ResponseWriter, r *http.Request, err error) {
	if err == store.ErrNotFound {
		s.problem(w, r, http.StatusNotFound, "E_PLAN_NOT_FOUND", "Plan no encontrado")
		return
	}
	s.internalError(w, r, err)
}

func (s *Server) planValidate(w http.ResponseWriter, r *http.Request, id string) {
	if !allowMethod(w, r, http.MethodPost) {
		return
	}
	r, _, ok := s.authorize(w, r, "operate")
	if !ok {
		return
	}
	now := s.Now().UTC()
	var out contracts.PlanValidation
	err := s.Store.View(r.Context(), func(st *store.State) error {
		p := st.Plans[id]
		if p == nil {
			return store.ErrNotFound
		}
		out.Valid = true
		out.Errors = []string{}
		out.Warnings = []string{}
		isImage := p.Kind == "image"
		if !isImage && len(p.Paths) == 0 {
			out.Valid = false
			out.Errors = append(out.Errors, "sin rutas")
		}
		if p.Schedule != "" {
			sch, err := cron.Parse(p.Schedule)
			if err != nil {
				out.Valid = false
				out.Errors = append(out.Errors, err.Error())
			} else {
				out.NextRuns = sch.NextN(now.In(timeZone(p.Timezone)), 5)
			}
		} else {
			out.Warnings = append(out.Warnings, "sin programación: solo se ejecutará manualmente")
		}
		if validateRetention(&p.Retention) != "" {
			out.Valid = false
			out.Errors = append(out.Errors, "retención inválida")
		}
		if isImage {
			if len(p.Disks) == 0 {
				out.Warnings = append(out.Warnings, "sin selección de discos: se capturarán todos los discos fijos del equipo")
			} else {
				out.Warnings = append(out.Warnings, fmt.Sprintf("solo se capturarán los discos %s; el resto quedará fuera de la imagen", diskListStr(p.Disks)))
			}
		} else if len(p.Excludes) == 0 {
			out.Warnings = append(out.Warnings, "sin exclusiones: se copiará todo el contenido de las rutas")
		}
		assigned := false
		for _, as := range st.Assignments {
			if as.PlanID == id {
				assigned = true
				break
			}
		}
		if !assigned {
			out.Warnings = append(out.Warnings, "el plan no está asignado a ningún equipo o grupo")
		}
		return nil
	})
	if err != nil {
		s.planErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

// --- Asignaciones ---

func (s *Server) assignmentView(st *store.State, a *store.Assignment) contracts.AssignmentView {
	v := contracts.AssignmentView{
		ID: a.ID, PlanID: a.PlanID, TargetType: a.TargetType, TargetID: a.TargetID,
		Enabled: a.Enabled, CreatedAt: a.CreatedAt,
	}
	switch a.TargetType {
	case contracts.TargetAgent:
		v.AgentID = a.TargetID
		if ag := st.Agents[a.TargetID]; ag != nil {
			v.TargetName = ag.Hostname
		}
	case contracts.TargetGroup:
		if g := st.Groups[a.TargetID]; g != nil {
			v.TargetName = g.Name
		}
	}
	return v
}

func (s *Server) planAssignCompat(w http.ResponseWriter, r *http.Request, planID string) {
	// Compatibilidad 0.2/0.3: POST {agent_id} equivale a target agent.
	if !allowMethod(w, r, http.MethodPost) {
		return
	}
	var req contracts.AssignmentRequest
	if !decodeJSON(w, r, &req, maxBodyAdmin) {
		return
	}
	req.TargetType = contracts.TargetAgent
	req.TargetID = req.AgentID
	s.createAssignment(w, r, planID, &req)
}

func (s *Server) planAssignments(w http.ResponseWriter, r *http.Request, planID string) {
	if !allowMethod(w, r, http.MethodGet, http.MethodPost) {
		return
	}
	r, a, ok := s.authorize(w, r, "operate")
	if !ok {
		return
	}
	switch r.Method {
	case http.MethodGet:
		items := []contracts.AssignmentView{}
		_ = s.Store.View(r.Context(), func(st *store.State) error {
			if st.Plans[planID] == nil {
				return store.ErrNotFound
			}
			for _, as := range st.Assignments {
				if as.PlanID == planID {
					items = append(items, s.assignmentView(st, as))
				}
			}
			return nil
		})
		if items == nil {
			writeJSON(w, http.StatusNotFound, contracts.Problem{Type: "about:blank", Title: "Not Found", Status: 404, Code: "E_PLAN_NOT_FOUND", Detail: "Plan no encontrado"})
			return
		}
		sort.Slice(items, func(i, j int) bool { return items[i].CreatedAt.Before(items[j].CreatedAt) })
		writeJSON(w, http.StatusOK, contracts.AssignmentList{Items: items})
	case http.MethodPost:
		s.withIdempotency(w, r, a, func() (int, any) {
			var req contracts.AssignmentRequest
			if !decodeJSON(w, r, &req, maxBodyAdmin) {
				return 0, nil
			}
			status, out := s.createAssignmentInner(r, a, planID, &req)
			if status != http.StatusCreated {
				return 0, errProblem{Status: status, Code: codeForStatus(status), Detail: out.(string)}
			}
			return status, out
		})
	}
}

func codeForStatus(status int) string {
	switch status {
	case http.StatusBadRequest:
		return "E_VALIDATION"
	case http.StatusNotFound:
		return "E_NOT_FOUND"
	case http.StatusConflict:
		return "E_CONFLICT"
	}
	return "E_INTERNAL"
}

func (s *Server) createAssignment(w http.ResponseWriter, r *http.Request, planID string, req *contracts.AssignmentRequest) {
	r, a, ok := s.authorize(w, r, "operate")
	if !ok {
		return
	}
	status, out := s.createAssignmentInner(r, a, planID, req)
	if status != http.StatusCreated {
		s.problem(w, r, status, codeForStatus(status), out.(string))
		return
	}
	writeJSON(w, status, out)
}

// createAssignmentInner valida y crea (o devuelve) una asignación.
func (s *Server) createAssignmentInner(r *http.Request, a *actor, planID string, req *contracts.AssignmentRequest) (int, any) {
	tt, tid := req.TargetType, req.TargetID
	if tt == "" && req.AgentID != "" {
		tt, tid = contracts.TargetAgent, req.AgentID
	}
	if tt == "" || tid == "" {
		return http.StatusBadRequest, "target_type y target_id (o agent_id) son obligatorios"
	}
	if tt != contracts.TargetAgent && tt != contracts.TargetGroup {
		return http.StatusBadRequest, "target_type debe ser agent o group"
	}
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	var created store.Assignment
	id, err := newID()
	if err != nil {
		return http.StatusInternalServerError, err.Error()
	}
	now := s.Now().UTC()
	err = s.Store.Update(r.Context(), func(st *store.State) error {
		if st.Plans[planID] == nil {
			return store.ErrNotFound
		}
		if tt == contracts.TargetAgent && st.Agents[tid] == nil {
			return store.ErrNotFound
		}
		if tt == contracts.TargetGroup && st.Groups[tid] == nil {
			return store.ErrNotFound
		}
		for _, as := range st.Assignments {
			if as.PlanID == planID && as.TargetType == tt && as.TargetID == tid {
				as.Enabled = enabled
				created = *as
				return nil
			}
		}
		created = store.Assignment{ID: id, PlanID: planID, TargetType: tt, TargetID: tid, Enabled: enabled, CreatedAt: now}
		st.Assignments[id] = &created
		st.BumpConfig()
		return nil
	})
	if err != nil {
		if err == store.ErrNotFound {
			return http.StatusNotFound, "Plan, equipo o grupo no encontrado"
		}
		return http.StatusInternalServerError, err.Error()
	}
	s.audit(r, a, "plan.assigned", "plan", planID, "ok", map[string]string{"target": tt + "/" + tid})
	var out contracts.AssignmentView
	_ = s.Store.View(r.Context(), func(st *store.State) error {
		out = s.assignmentView(st, &created)
		return nil
	})
	return http.StatusCreated, out
}

// --- Planes efectivos ---

// groupDescendants devuelve los IDs de todos los descendientes de groupID (incluido).
func groupDescendants(st *store.State, groupID string) map[string]bool {
	out := map[string]bool{groupID: true}
	queue := []string{groupID}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		for _, g := range st.Groups {
			if g.ParentID == cur && !out[g.ID] {
				out[g.ID] = true
				queue = append(queue, g.ID)
			}
		}
	}
	return out
}

// membership calcula los grupos efectivos de un agente: sus grupos directos
// más todos sus ancestros. Así, la asignación a un grupo padre cubre a los
// miembros de los subgrupos (los hijos heredan los planes del padre).
func membership(st *store.State, agentID string) map[string]bool {
	out := map[string]bool{}
	for _, g := range st.Groups {
		member := false
		for _, m := range g.MemberIDs {
			if m == agentID {
				member = true
				break
			}
		}
		if !member {
			continue
		}
		cur := g.ID
		for cur != "" && !out[cur] {
			out[cur] = true
			parent := st.Groups[cur]
			if parent == nil {
				break
			}
			cur = parent.ParentID
		}
	}
	return out
}

// effectivePlans resuelve los planes de un agente: la asignación directa
// prevalece sobre la de grupo para el mismo plan.
func (s *Server) effectivePlans(st *store.State, agentID string, now time.Time) ([]contracts.EffectivePlan, error) {
	agent := st.Agents[agentID]
	if agent == nil {
		return nil, store.ErrNotFound
	}
	groups := membership(st, agentID)

	direct := map[string]*store.Assignment{}
	viaGroup := map[string]*store.Assignment{}
	viaGroupName := map[string]string{}
	for _, as := range st.Assignments {
		if !as.Enabled || st.Plans[as.PlanID] == nil {
			continue
		}
		switch as.TargetType {
		case contracts.TargetAgent:
			if as.TargetID == agentID {
				direct[as.PlanID] = as
			}
		case contracts.TargetGroup:
			if groups[as.TargetID] {
				if _, seen := viaGroup[as.PlanID]; !seen {
					viaGroup[as.PlanID] = as
					viaGroupName[as.PlanID] = st.Groups[as.TargetID].Name
				}
			}
		}
	}

	seen := map[string]bool{}
	out := []contracts.EffectivePlan{}
	// Orden estable: por fecha de creación del plan más antiguo.
	var planIDs []string
	for pid := range direct {
		planIDs = append(planIDs, pid)
	}
	for pid := range viaGroup {
		if _, ok := direct[pid]; !ok {
			planIDs = append(planIDs, pid)
		}
	}
	sort.Slice(planIDs, func(i, j int) bool {
		return st.Plans[planIDs[i]].CreatedAt.Before(st.Plans[planIDs[j]].CreatedAt)
	})
	for _, pid := range planIDs {
		if seen[pid] {
			continue
		}
		seen[pid] = true
		p := st.Plans[pid]
		if !p.Enabled {
			continue
		}
		eff := contracts.EffectivePlan{Plan: s.planView(st, p, now)}
		if as, ok := direct[pid]; ok {
			eff.Via = contracts.TargetAgent
			eff.AssignmentID = as.ID
			var shadow []string
			if g, ok := viaGroup[pid]; ok {
				shadow = append(shadow, g.ID)
			}
			eff.ShadowedBy = shadow
		} else {
			as := viaGroup[pid]
			eff.Via = contracts.TargetGroup
			eff.AssignmentID = as.ID
			eff.GroupID = as.TargetID
			eff.GroupName = viaGroupName[pid]
		}
		if se := st.Schedule[pid+"|"+agentID]; se != nil {
			if !se.NextRunAt.IsZero() {
				t := se.NextRunAt
				eff.NextRunAt = &t
			}
			if se.LastRunAt != nil {
				t := *se.LastRunAt
				eff.LastRunAt = &t
			}
		}
		out = append(out, eff)
	}
	return out, nil
}

func (s *Server) agentEffectivePlans(w http.ResponseWriter, r *http.Request, agentID string) {
	if !allowMethod(w, r, http.MethodGet) {
		return
	}
	r, _, ok := s.authorize(w, r, "read")
	if !ok {
		return
	}
	now := s.Now().UTC()
	var items []contracts.EffectivePlan
	err := s.Store.View(r.Context(), func(st *store.State) error {
		var err error
		items, err = s.effectivePlans(st, agentID, now)
		return err
	})
	if err != nil {
		if err == store.ErrNotFound {
			s.problem(w, r, http.StatusNotFound, "E_AGENT_NOT_FOUND", "Agente no encontrado")
			return
		}
		s.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, contracts.EffectivePlanList{Items: items})
}

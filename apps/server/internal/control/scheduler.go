package control

import (
	"context"
	"encoding/json"
	"sort"
	"time"

	"github.com/juansoler/bdr/apps/server/internal/cron"
	"github.com/juansoler/bdr/apps/server/internal/store"
	"github.com/juansoler/bdr/packages/contracts"
)

// RunScheduler ejecuta el bucle del programador hasta que se cancela ctx.
// Cada tick: recalcula próximas ejecuciones, lanza jobs vencidos y detecta
// agentes sin contacto y jobs con lease perdido.
func (s *Server) RunScheduler(ctx context.Context) {
	interval := s.SchedulerEvery
	if interval <= 0 {
		interval = time.Minute
	}
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			s.runSchedulerTick(ctx)
		}
	}
}

func (s *Server) runSchedulerTick(ctx context.Context) {
	defer func() {
		if rec := recover(); rec != nil {
			s.Logger.Error("panic en el programador", "panic", rec)
		}
	}()
	now := s.Now().UTC()
	s.mu.Lock()
	s.lastTick = now
	s.mu.Unlock()

	type launch struct{ planID, agentID string }
	var due []launch
	var sqlDue []string
	var sqlVerifyDue []string
	var offlineAlerts []notifPayload
	var expiredJobs []string

	err := s.Store.View(ctx, func(st *store.State) error {
		offline := s.offlineThreshold(st)

		// 1. Jobs con lease caducado y agente sin contacto: volver a pending
		// o marcar expirados si se agotaron los intentos.
		for _, j := range st.Jobs {
			if j.State == store.JobLeased || j.State == store.JobRunning {
				if j.LeaseUntil != nil && now.After(*j.LeaseUntil) {
					if j.Attempts >= j.MaxAttempts {
						expiredJobs = append(expiredJobs, j.ID)
					}
				}
			}
		}

		// 2. Agentes sin contacto → alerta.
		for _, a := range st.Agents {
			if a.RevokedAt != nil || a.LastSeenAt == nil {
				continue
			}
			if now.Sub(*a.LastSeenAt) > offline && now.Sub(*a.LastSeenAt) < offline*2 {
				// Solo si no tiene ya la alerta abierta (raise deduplica).
				offlineAlerts = append(offlineAlerts, notifPayload{
					Key:      "agent-offline:" + a.ID,
					Kind:     "agent_offline",
					Severity: contracts.SeverityHigh,
					Title:    "Equipo sin contacto: " + a.Hostname,
					Detail:   "Sin heartbeat durante más de " + offline.String(),
					AgentID:  a.ID,
				})
			}
		}

		// 3. Jobs programados vencidos.
		entries := make([]*store.ScheduleEntry, 0, len(st.Schedule))
		for _, se := range st.Schedule {
			entries = append(entries, se)
		}
		sort.Slice(entries, func(i, j int) bool { return entries[i].NextRunAt.Before(entries[j].NextRunAt) })
		for _, se := range entries {
			if !planEnabled(st, se.PlanID) || !agentActive(st, se.AgentID) {
				continue
			}
			if se.NextRunAt.IsZero() || se.NextRunAt.After(now) {
				continue
			}
			p := st.Plans[se.PlanID]
			if p == nil {
				continue
			}
			// Ventana horaria.
			if p.Window != nil {
				loc := timeZone(p.Timezone)
				inWin, err := cron.InWindow(now.In(loc), p.Window.Start, p.Window.End)
				if err != nil || !inWin {
					next := se.NextRunAt
					if sch, err2 := cron.Parse(p.Schedule); err2 == nil {
						next = sch.Next(now.In(loc))
					}
					se.NextRunAt = next
					continue
				}
			}
			if planHasPendingJob(st, se.AgentID, se.PlanID) || agentBusy(st, se.AgentID) {
				continue
			}
			due = append(due, launch{planID: se.PlanID, agentID: se.AgentID})
		}

		// 4. Planes SQL vencidos: modelo plano (NextRunAt en el propio plan,
		// binding directo agente+instancia+base, sin asignaciones).
		for _, p := range st.SQLPlans {
			if !p.Enabled || !agentActive(st, p.AgentID) {
				continue
			}
			if p.NextRunAt.IsZero() || p.NextRunAt.After(now) {
				continue
			}
			if tg := st.Storage[p.StorageTargetID]; tg == nil || !tg.Enabled {
				continue // destino inválido: no lanzar ni avanzar
			}
			if planHasPendingJob(st, p.AgentID, p.ID) || agentBusy(st, p.AgentID) {
				continue // reintenta en el próximo tick sin avanzar
			}
			sqlDue = append(sqlDue, p.ID)
		}

		// 5. Verificaciones programadas de planes SQL (RF-44): VERIFYONLY
		// periódico del último backup del plan contra el destino.
		for _, p := range st.SQLPlans {
			if !p.Enabled || p.VerifyCron == "" || !agentActive(st, p.AgentID) {
				continue
			}
			if p.NextVerifyAt.IsZero() || p.NextVerifyAt.After(now) {
				continue
			}
			if tg := st.Storage[p.StorageTargetID]; tg == nil || !tg.Enabled {
				continue
			}
			if planHasPendingJob(st, p.AgentID, p.ID) || agentBusy(st, p.AgentID) {
				continue
			}
			sqlVerifyDue = append(sqlVerifyDue, p.ID)
		}
		return nil
	})
	if err != nil {
		s.Logger.Error("tick del programador falló", "error", err.Error())
		return
	}

	// Fuera del View: marcar expirados.
	for _, id := range expiredJobs {
		_ = s.Store.Update(ctx, func(st *store.State) error {
			j := st.Jobs[id]
			if j == nil || (j.State != store.JobLeased && j.State != store.JobRunning) {
				return nil
			}
			if j.LeaseUntil == nil || !s.Now().UTC().After(*j.LeaseUntil) {
				return nil
			}
			j.State = store.JobExpired
			j.ErrorCode = "E_LEASE_LOST"
			j.ErrorMessage = "El agente perdió el lease"
			t := s.Now().UTC()
			j.FinishedAt = &t
			j.UpdatedAt = t
			st.AppendJobEvent(contracts.JobEvent{JobID: id, Kind: "state", State: store.JobExpired, At: t})
			return nil
		})
		s.raiseAlert(notifPayload{
			Key:      "job-expired:" + id,
			Kind:     "job_expired",
			Severity: contracts.SeverityHigh,
			Title:    "Job expirado",
			Detail:   "El job " + id + " perdió el lease del agente",
			JobID:    id,
		})
		// Reintento automático si quedan intentos y el fallo es transitorio.
		s.maybeAutoRetry(ctx, id)
	}

	// Lanzar los vencidos (uno a uno, cada uno en su Update).
	for _, d := range due {
		s.launchScheduled(ctx, d.planID, d.agentID, now)
	}

	// Planes SQL vencidos.
	for _, id := range sqlDue {
		s.launchSQLScheduled(ctx, id, now)
	}

	// Verificaciones programadas de planes SQL.
	for _, id := range sqlVerifyDue {
		s.launchSQLVerifyScheduled(ctx, id, now)
	}

	// Alertas de offline.
	for _, p := range offlineAlerts {
		s.raiseAlert(p)
	}

	// Resolver alertas de offline de agentes que volvieron.
	s.resolveOfflineAlerts(ctx, now)
}

func planEnabled(st *store.State, planID string) bool {
	p := st.Plans[planID]
	return p != nil && p.Enabled
}

func agentActive(st *store.State, agentID string) bool {
	a := st.Agents[agentID]
	return a != nil && a.RevokedAt == nil
}

func agentBusy(st *store.State, agentID string) bool {
	for _, j := range st.Jobs {
		if j.AgentID == agentID && (j.State == store.JobLeased || j.State == store.JobRunning) {
			return true
		}
	}
	return false
}

// launchScheduled crea el job de una ejecución programada y avanza la entrada.
func (s *Server) launchScheduled(ctx context.Context, planID, agentID string, now time.Time) {
	err := s.Store.Update(ctx, func(st *store.State) error {
		p := st.Plans[planID]
		a := st.Agents[agentID]
		if p == nil || a == nil || !p.Enabled || a.RevokedAt != nil {
			return errAbort
		}
		se := st.Schedule[planID+"|"+agentID]
		if se == nil {
			return errAbort
		}
		if planHasPendingJob(st, agentID, planID) || agentBusy(st, agentID) {
			// Reintenta en el próximo tick; no avanza la programación.
			return errAbort
		}
		var jtype string
		var payload []byte
		var retentionDays int
		if tg := st.Storage[p.StorageTargetID]; tg != nil {
			retentionDays = tg.RetentionDays
		}
		if p.Kind == "image" {
			jtype = contracts.JobTypeBackupImage
			payload, _ = json.Marshal(contracts.ImageJobPayload{
				PlanID: p.ID, PlanName: p.Name,
				Disks:           p.Disks,
				StorageTargetID: p.StorageTargetID,
				RetentionDays:   retentionDays,
			})
		} else {
			jtype = contracts.JobTypeBackupFile
			payload, _ = json.Marshal(contracts.BackupJobPayload{
				PlanID: p.ID, PlanName: p.Name, Paths: p.Paths, Excludes: p.Excludes,
				Retention: &p.Retention, StorageTargetID: p.StorageTargetID,
			})
		}
		id, _ := newID()
		j := &store.Job{
			ID: id, Type: jtype, State: store.JobPending,
			AgentID: agentID, PlanID: planID, Trigger: "schedule",
			Payload: payload, MaxAttempts: 3, CreatedAt: now, UpdatedAt: now,
		}
		st.Jobs[id] = j
		st.AppendJobEvent(contracts.JobEvent{JobID: id, Kind: "state", State: store.JobPending, At: now})
		se.LastRunAt = &now
		se.LastJobID = id
		if p.Schedule != "" {
			if sch, err := cron.Parse(p.Schedule); err == nil {
				se.NextRunAt = sch.Next(now.In(timeZone(p.Timezone)))
			}
		}
		return nil
	})
	if err == nil {
		s.Logger.Info("job programado creado", "plan_id", planID, "agent_id", agentID)
	}
}

// launchSQLScheduled crea el job SQL_BACKUP de un plan SQL vencido y avanza
// su NextRunAt. Espeja launchScheduled con el modelo plano de SQLPlan.
func (s *Server) launchSQLScheduled(ctx context.Context, planID string, now time.Time) {
	err := s.Store.Update(ctx, func(st *store.State) error {
		p := st.SQLPlans[planID]
		if p == nil || !p.Enabled || !agentActive(st, p.AgentID) {
			return errAbort
		}
		if planHasPendingJob(st, p.AgentID, p.ID) || agentBusy(st, p.AgentID) {
			// Reintenta en el próximo tick; no avanza la programación.
			return errAbort
		}
		tg := st.Storage[p.StorageTargetID]
		if tg == nil || !tg.Enabled {
			return errAbort
		}
		retentionDays := p.RetentionDays
		if retentionDays == 0 {
			retentionDays = tg.RetentionDays
		}
		// RF-45: cifrado del plan por certificado custodiado.
		var enc *contracts.SQLEncryption
		if p.EncryptCert != "" {
			enc = &contracts.SQLEncryption{Algorithm: "AES_256", CertName: p.EncryptCert}
		}
		payload, _ := json.Marshal(contracts.SQLBackupJobPayload{
			Server: p.Server, Instance: p.Instance, Database: p.Database,
			Type: p.Type, StorageTargetID: tg.ID,
			RetentionDays: retentionDays, Verify: p.Verify,
			Encryption: enc,
		})
		id, _ := newID()
		j := &store.Job{
			ID: id, Type: contracts.JobTypeSQLBackup, State: store.JobPending,
			AgentID: p.AgentID, PlanID: p.ID, Trigger: "schedule",
			Payload: payload, MaxAttempts: 3, CreatedAt: now, UpdatedAt: now,
		}
		st.Jobs[id] = j
		st.AppendJobEvent(contracts.JobEvent{JobID: id, Kind: "state", State: store.JobPending, At: now})
		p.LastJobID = id
		p.UpdatedAt = now
		if sch, err := cron.Parse(p.Schedule); err == nil {
			p.NextRunAt = sch.Next(now.In(timeZone(p.Timezone)))
		}
		return nil
	})
	if err == nil {
		s.Logger.Info("job SQL programado creado", "plan_id", planID)
	}
}

// launchSQLVerifyScheduled crea un job SQL_VERIFY sobre el último backup
// del plan (RF-44: verificación programada del repositorio). Si el plan
// aún no tiene backups, no avanza la programación.
func (s *Server) launchSQLVerifyScheduled(ctx context.Context, planID string, now time.Time) {
	err := s.Store.Update(ctx, func(st *store.State) error {
		p := st.SQLPlans[planID]
		if p == nil || !p.Enabled || p.VerifyCron == "" || !agentActive(st, p.AgentID) {
			return errAbort
		}
		if planHasPendingJob(st, p.AgentID, p.ID) || agentBusy(st, p.AgentID) {
			return errAbort
		}
		// Último punto de restauración del plan con URI de repositorio.
		var latest *store.RestorePoint
		for _, rp := range st.RestorePoints {
			if rp.Kind != "sql" || rp.PlanID != planID || rp.SnapshotID == "" || rp.SQL == nil {
				continue
			}
			if latest == nil || rp.CreatedAt.After(latest.CreatedAt) {
				latest = rp
			}
		}
		if latest == nil {
			return errAbort // sin backups aún: reintenta el próximo tick
		}
		payload, _ := json.Marshal(contracts.SQLVerifyJobPayload{
			Server: p.Server, Instance: p.Instance,
			RepoURI: latest.SnapshotID, StorageTargetID: p.StorageTargetID,
		})
		id, _ := newID()
		j := &store.Job{
			ID: id, Type: contracts.JobTypeSQLVerify, State: store.JobPending,
			AgentID: p.AgentID, PlanID: p.ID, Trigger: "schedule",
			Payload: payload, MaxAttempts: 3, CreatedAt: now, UpdatedAt: now,
		}
		st.Jobs[id] = j
		st.AppendJobEvent(contracts.JobEvent{JobID: id, Kind: "state", State: store.JobPending, At: now})
		p.UpdatedAt = now
		if sch, err := cron.Parse(p.VerifyCron); err == nil {
			p.NextVerifyAt = sch.Next(now.In(timeZone(p.Timezone)))
		}
		return nil
	})
	if err == nil {
		s.Logger.Info("verificación SQL programada creada", "plan_id", planID)
	}
}

// maybeAutoRetry reencola un job expirado si le quedan intentos.
func (s *Server) maybeAutoRetry(ctx context.Context, jobID string) {
	_ = s.Store.Update(ctx, func(st *store.State) error {
		j := st.Jobs[jobID]
		if j == nil || j.State != store.JobExpired {
			return nil
		}
		if j.Attempts >= j.MaxAttempts {
			return nil
		}
		if planHasPendingJob(st, j.AgentID, j.PlanID) || agentBusy(st, j.AgentID) {
			return nil
		}
		j.State = store.JobPending
		j.ErrorCode = ""
		j.ErrorMessage = ""
		j.LeaseUntil = nil
		j.UpdatedAt = s.Now().UTC()
		st.AppendJobEvent(contracts.JobEvent{JobID: jobID, Kind: "state", State: store.JobPending, At: j.UpdatedAt})
		return nil
	})
}

// resolveOfflineAlerts resuelve las alertas de agentes que tienen contacto reciente.
func (s *Server) resolveOfflineAlerts(ctx context.Context, now time.Time) {
	type res struct{ key, id string }
	var toResolve []res
	_ = s.Store.View(ctx, func(st *store.State) error {
		offline := s.offlineThreshold(st)
		for _, al := range st.Alerts {
			if al.Kind != "agent_offline" || al.State == contracts.AlertResolved {
				continue
			}
			if a := st.Agents[al.AgentID]; a != nil && a.LastSeenAt != nil && now.Sub(*a.LastSeenAt) < offline {
				toResolve = append(toResolve, res{al.Key, al.ID})
			}
		}
		return nil
	})
	for _, r := range toResolve {
		_ = s.Store.Update(ctx, func(st *store.State) error {
			if al := st.Alerts[r.id]; al != nil && al.State != contracts.AlertResolved {
				al.State = contracts.AlertResolved
				al.ResolvedBy = "system"
				t := now
				al.ResolvedAt = &t
			}
			return nil
		})
	}
}

// recomputeSchedule reconstruye las entradas de programación tras cambios en
// planes o asignaciones. Lo llama el programador en cada tick (barato a esta
// escala) para no complicar los handlers.
func (s *Server) recomputeSchedule(ctx context.Context, now time.Time) {
	_ = s.Store.Update(ctx, func(st *store.State) error {
		seen := map[string]bool{}
		for _, a := range st.Agents {
			if a.RevokedAt != nil {
				continue
			}
			eff, err := s.effectivePlans(st, a.ID, now)
			if err != nil {
				continue
			}
			for _, e := range eff {
				if e.Plan.Schedule == "" {
					continue
				}
				key := e.Plan.ID + "|" + a.ID
				seen[key] = true
				sch, err := cron.Parse(e.Plan.Schedule)
				if err != nil {
					continue
				}
				se := st.Schedule[key]
				if se == nil {
					next := sch.Next(now.In(timeZone(e.Plan.Timezone)))
					st.Schedule[key] = &store.ScheduleEntry{
						PlanID: e.Plan.ID, AgentID: a.ID, Schedule: e.Plan.Schedule, NextRunAt: next,
					}
				} else if se.Schedule != e.Plan.Schedule {
					se.Schedule = e.Plan.Schedule
					se.NextRunAt = sch.Next(now.In(timeZone(e.Plan.Timezone)))
				}
			}
		}
		for k := range st.Schedule {
			if !seen[k] {
				delete(st.Schedule, k)
			}
		}
		return nil
	})
}

// notifPayload describe una alerta y su notificación.
type notifPayload struct {
	Key      string
	Kind     string
	Severity string
	Title    string
	Detail   string
	AgentID  string
	PlanID   string
	JobID    string
}

// raiseAlert crea o deduplica una alerta y notifica (con re-notificación).
func (s *Server) raiseAlert(p notifPayload) {
	now := s.Now().UTC()
	var ev contracts.AlertView
	err := s.Store.Update(context.Background(), func(st *store.State) error {
		if existing := st.AlertByKey(p.Key); existing != nil {
			existing.Count++
			existing.LastAt = now
			existing.Detail = p.Detail
			// Re-notifica si pasó el periodo.
			renotify := time.Duration(st.Settings.AlertRenotifyHours) * time.Hour
			if renotify <= 0 {
				renotify = 6 * time.Hour
			}
			if existing.NotifiedAt == nil || now.Sub(*existing.NotifiedAt) >= renotify {
				existing.NotifiedAt = &now
				ev = alertView(existing)
			}
			return nil
		}
		id, _ := newID()
		al := &store.Alert{
			ID: id, Key: p.Key, Kind: p.Kind, Severity: p.Severity,
			State: contracts.AlertOpen, Title: p.Title, Detail: p.Detail,
			AgentID: p.AgentID, PlanID: p.PlanID, JobID: p.JobID,
			Count: 1, FirstAt: now, LastAt: now, NotifiedAt: &now,
		}
		st.Alerts[id] = al
		ev = alertView(al)
		return nil
	})
	if err != nil {
		s.Logger.Error("no se pudo crear la alerta", "error", err.Error())
		return
	}
	if ev.ID != "" {
		s.notify.Enqueue(ev)
	}
}

// alertFailedJob crea la alerta de un job fallido (dentro de un Update).
func (s *Server) alertFailedJob(j *store.Job, st *store.State) {
	now := s.Now().UTC()
	hostname := ""
	if a := st.Agents[j.AgentID]; a != nil {
		hostname = a.Hostname
	}
	// Si el agente tenía alertas de offline u otras del mismo plan, no se tocan.
	id, _ := newID()
	key := "job-failed:" + j.PlanID + ":" + j.AgentID
	if existing := st.AlertByKey(key); existing != nil {
		existing.Count++
		existing.LastAt = now
		existing.JobID = j.ID
		existing.Detail = j.ErrorCode + ": " + j.ErrorMessage
		return
	}
	st.Alerts[id] = &store.Alert{
		ID: id, Key: key, Kind: "job_failed", Severity: contracts.SeverityCritical,
		State: contracts.AlertOpen, Title: "Backup fallido" + orDefault(" en "+hostname, ""),
		Detail:  j.ErrorCode + ": " + j.ErrorMessage,
		AgentID: j.AgentID, PlanID: j.PlanID, JobID: j.ID,
		Count: 1, FirstAt: now, LastAt: now, NotifiedAt: &now,
	}
}

// alertView convierte una alerta para la API.
func alertView(a *store.Alert) contracts.AlertView {
	return contracts.AlertView{
		ID: a.ID, Key: a.Key, Kind: a.Kind, Severity: a.Severity, State: a.State,
		Title: a.Title, Detail: a.Detail, AgentID: a.AgentID, PlanID: a.PlanID, JobID: a.JobID,
		Count: a.Count, FirstAt: a.FirstAt, LastAt: a.LastAt,
		AckBy: a.AckBy, AckAt: a.AckAt, ResolvedBy: a.ResolvedBy, ResolvedAt: a.ResolvedAt,
		NotifiedAt: a.NotifiedAt,
	}
}

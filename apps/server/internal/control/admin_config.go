// Copia de seguridad y restauración de configuración del plano de control
// (feature de migración: GET /api/v1/admin/config/backup y
// POST /api/v1/admin/config/restore). El backup exporta la configuración
// operativa (ajustes, agentes enrolados, planes, grupos, destinos de
// almacenamiento, credenciales de repo, planes/invets/cates SQL) sin secretos
// de runtime (webhook, SMTP, passwords cifrados) — esos se reconfiguran en el
// servidor de destino.

package control

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/juansoler/bdr/apps/server/internal/store"
	"github.com/juansoler/bdr/packages/contracts"
)

// handleConfigBackup exporta la configuración del plano de control como
// snapshot JSON portable (formato v1), pensado para migrar a otro servidor.
// GET /api/v1/admin/config/backup
func (s *Server) handleConfigBackup(w http.ResponseWriter, r *http.Request) {
	if !allowMethod(w, r, http.MethodGet) {
		return
	}
	r, _, ok := s.authorize(w, r, "read")
	if !ok {
		return
	}
	var snap contracts.ConfigBackup
	err := s.Store.View(r.Context(), func(st *store.State) error {
		snap = s.buildConfigBackup(st)
		return nil
	})
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, snap)
}

// handleConfigRestore aplica una copia de seguridad exportada (snapshot v1)
// sobre el estado actual del plano, reemplazando la configuración migrada.
// POST /api/v1/admin/config/restore
func (s *Server) handleConfigRestore(w http.ResponseWriter, r *http.Request) {
	if !allowMethod(w, r, http.MethodPost) {
		return
	}
	r, a, ok := s.authorize(w, r, "operate")
	if !ok {
		return
	}
	var snap contracts.ConfigBackup
	if !decodeJSON(w, r, &snap, maxBodyAdmin) {
		return
	}
	if snap.SchemaVersion != contracts.ConfigBackupSchemaVersion {
		s.problem(w, r, http.StatusBadRequest, "E_VALIDATION",
			"copia de seguridad con schema_version no soportado (esperaba "+strconv.Itoa(contracts.ConfigBackupSchemaVersion)+")")
		return
	}
	err := s.Store.Update(r.Context(), func(st *store.State) error {
		return s.applyConfigBackup(st, snap)
	})
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	s.audit(r, a, "config.backup.restored", "config", "all", "ok", nil)
	writeJSON(w, http.StatusOK, map[string]any{
		"schema_version": snap.SchemaVersion,
		"restored":       true,
	})
}

// buildConfigBackup serializa la configuración relevante para migración.
func (s *Server) buildConfigBackup(st *store.State) contracts.ConfigBackup {
	now := s.Now().UTC()
	snap := contracts.ConfigBackup{
		SchemaVersion: contracts.ConfigBackupSchemaVersion,
		ExportedAt:    now.Format(time.RFC3339),
	}
	snap.Settings = contracts.ConfigBackupSettings{
		EnvironmentName:     st.Settings.EnvironmentName,
		AgentOfflineMinutes: st.Settings.AgentOfflineMinutes,
		AlertMinSeverity:    st.Settings.AlertMinSeverity,
		AlertRenotifyHours:  st.Settings.AlertRenotifyHours,
		WebhookURL:          st.Settings.WebhookURL,
		WebhookConfigured:   st.Settings.WebhookSecretEnc != "",
		SMTPHost:            st.Settings.SMTPHost,
		SMTPPort:            st.Settings.SMTPPort,
		SMTPUsername:        st.Settings.SMTPUsername,
		SMTPConfigured:      st.Settings.SMTPPasswordEnc != "",
		SMTPFrom:            st.Settings.SMTPFrom,
		SMTPTo:              st.Settings.SMTPTo,
		SMTPStartTLS:        st.Settings.SMTPStartTLS,
		SessionHours:        st.Settings.SessionHours,
	}
	for _, a := range st.Agents {
		if a == nil || a.ID == "" {
			continue
		}
		e := contracts.ConfigBackupAgent{
			ID: a.ID, Hostname: a.Hostname, OSFamily: a.OSFamily, OSVersion: a.OSVersion,
			Arch: a.Arch, AgentVersion: a.AgentVersion, EnrolledAt: a.EnrolledAt.UTC().Format(time.RFC3339),
		}
		if a.LastSeenAt != nil {
			e.LastSeenAt = a.LastSeenAt.UTC().Format(time.RFC3339)
		}
		if a.RevokedAt != nil {
			e.RevokedAt = a.RevokedAt.UTC().Format(time.RFC3339)
		}
		e.RevokedReason = a.RevokedReason
		if a.UpgradeRequestedAt != nil {
			e.UpgradeRequestedAt = a.UpgradeRequestedAt.UTC().Format(time.RFC3339)
		}
		snap.Agents = append(snap.Agents, e)
	}
	for _, p := range st.Plans {
		if p == nil {
			continue
		}
		snap.Plans = append(snap.Plans, contracts.ConfigBackupPlan{
			ID: p.ID, Name: p.Name, Description: p.Description, Kind: p.Kind,
			Paths: p.Paths, Excludes: p.Excludes, Disks: p.Disks, Enabled: p.Enabled,
			Schedule: p.Schedule, Timezone: p.Timezone,
			Window: p.Window, Retention: p.Retention, RPOHours: p.RPOHours,
			StorageTargetID: p.StorageTargetID,
		})
	}
	for _, a := range st.Assignments {
		if a == nil {
			continue
		}
		snap.Assignments = append(snap.Assignments, contracts.ConfigBackupAssignment{
			ID: a.ID, PlanID: a.PlanID, TargetType: a.TargetType, TargetID: a.TargetID,
			AgentID: a.AgentID, Enabled: a.Enabled,
		})
	}
	for _, g := range st.Groups {
		if g == nil {
			continue
		}
		snap.Groups = append(snap.Groups, contracts.ConfigBackupGroup{
			ID: g.ID, Name: g.Name, Description: g.Description, ParentID: g.ParentID,
			MemberIDs: g.MemberIDs,
		})
	}
	for _, t := range st.Storage {
		if t == nil {
			continue
		}
		snap.Storage = append(snap.Storage, contracts.ConfigBackupStorage{
			ID: t.ID, Name: t.Name, Kind: t.KindName(), Endpoint: t.Endpoint, Bucket: t.Bucket,
			Prefix: t.Prefix, Region: t.Region, Path: t.Path, SMB: t.IsSMB(), SMBUser: t.SMBUser,
			SMBDomain: t.SMBDomain, AccessKey: t.AccessKey, NoTLS: t.NoTLS, ObjectLock: t.ObjectLock,
			RetentionDays: t.RetentionDays, SyncIntervalMinutes: t.SyncIntervalMinutes, Enabled: t.Enabled,
		})
	}
	for _, rc := range st.RepoCreds {
		if rc == nil {
			continue
		}
		snap.RepoCredentials = append(snap.RepoCredentials, contracts.ConfigBackupRepoCredential{
			AgentID: rc.AgentID, Username: rc.Username, Hostname: rc.Hostname, Version: rc.Version,
			RotatedAt: rc.RotatedAt.UTC().Format(time.RFC3339),
		})
	}
	for _, p := range st.SQLPlans {
		if p == nil {
			continue
		}
		snap.SQLPlans = append(snap.SQLPlans, contracts.ConfigBackupSQLPlan{
			ID: p.ID, AgentID: p.AgentID, Server: p.Server, Instance: p.Instance,
			Database: p.Database, Type: p.Type, Schedule: p.Schedule, Timezone: p.Timezone,
			StorageTargetID: p.StorageTargetID, RetentionDays: p.RetentionDays,
			Verify: p.Verify, Enabled: p.Enabled, VerifyCron: p.VerifyCron,
		})
	}
	for _, c := range st.SQLCerts {
		if c == nil {
			continue
		}
		snap.SQLCerts = append(snap.SQLCerts, contracts.ConfigBackupSQLCert{
			Name: c.Name, Server: c.Server, Instance: c.Instance, TargetID: c.TargetID,
			CertURI: c.CertURI, PvkURI: c.PvkURI, LastBackupAt: c.LastBackupAt.UTC().Format(time.RFC3339),
			CreatedAt: c.CreatedAt.UTC().Format(time.RFC3339),
		})
	}
	for _, inv := range st.SQLInventories {
		if inv == nil {
			continue
		}
		items := make([]contracts.SQLDatabaseView, 0, len(inv.Items))
		for _, it := range inv.Items {
			items = append(items, it)
		}
		snap.SQLInventories = append(snap.SQLInventories, contracts.ConfigBackupSQLInventory{
			AgentID: inv.AgentID, Server: inv.Server, Instance: inv.Instance, Items: items,
			UpdatedAt: inv.UpdatedAt.UTC().Format(time.RFC3339),
		})
	}
	return snap
}

// applyConfigBackup aplica un snapshot de copia de seguridad sobre el estado
// (ajustes, agentes, planes, grupos, destinos, credenciales de repo, SQL).
func (s *Server) applyConfigBackup(st *store.State, snap contracts.ConfigBackup) error {
	now := s.Now().UTC()

	st.Settings = store.Settings{
		EnvironmentName:     snap.Settings.EnvironmentName,
		AgentOfflineMinutes: snap.Settings.AgentOfflineMinutes,
		AlertMinSeverity:    snap.Settings.AlertMinSeverity,
		AlertRenotifyHours:  snap.Settings.AlertRenotifyHours,
		WebhookURL:          snap.Settings.WebhookURL,
		SMTPHost:            snap.Settings.SMTPHost,
		SMTPPort:            snap.Settings.SMTPPort,
		SMTPUsername:        snap.Settings.SMTPUsername,
		SMTPFrom:            snap.Settings.SMTPFrom,
		SMTPTo:              snap.Settings.SMTPTo,
		SMTPStartTLS:        snap.Settings.SMTPStartTLS,
		SessionHours:        snap.Settings.SessionHours,
	}
	for _, a := range snap.Agents {
		if a.ID == "" {
			continue
		}
		ag := st.Agents[a.ID]
		if ag == nil {
			ag = &store.Agent{ID: a.ID}
			st.Agents[a.ID] = ag
		}
		ag.Hostname = a.Hostname
		ag.OSFamily = a.OSFamily
		ag.OSVersion = a.OSVersion
		ag.Arch = a.Arch
		ag.AgentVersion = a.AgentVersion
		ag.EnrolledAt = parseRFC3339(a.EnrolledAt)
		if a.LastSeenAt != "" {
			ag.LastSeenAt = parseRFC3339Ptr(a.LastSeenAt)
		}
		if a.RevokedAt != "" {
			ag.RevokedAt = parseRFC3339Ptr(a.RevokedAt)
		}
		ag.RevokedReason = a.RevokedReason
		if a.UpgradeRequestedAt != "" {
			ag.UpgradeRequestedAt = parseRFC3339Ptr(a.UpgradeRequestedAt)
		}
	}
	for _, p := range snap.Plans {
		if p.ID == "" {
			continue
		}
		st.Plans[p.ID] = &store.Plan{
			ID: p.ID, Name: p.Name, Description: p.Description, Kind: p.Kind,
			Paths: p.Paths, Excludes: p.Excludes, Disks: p.Disks, Enabled: p.Enabled,
			Schedule: p.Schedule, Timezone: p.Timezone, Window: p.Window,
			Retention: p.Retention, RPOHours: p.RPOHours, StorageTargetID: p.StorageTargetID,
			CreatedAt: now, UpdatedAt: now,
		}
	}
	for _, a := range snap.Assignments {
		if a.ID == "" {
			continue
		}
		st.Assignments[a.ID] = &store.Assignment{
			ID: a.ID, PlanID: a.PlanID, TargetType: a.TargetType, TargetID: a.TargetID,
			AgentID: a.AgentID, Enabled: a.Enabled, CreatedAt: now,
		}
	}
	groupIDs := map[string]bool{}
	for _, g := range snap.Groups {
		if g.ID == "" {
			continue
		}
		groupIDs[g.ID] = true
		gp := ""
		if g.ParentID != "" && groupIDs[g.ParentID] {
			gp = g.ParentID
		}
		gg := st.Groups[g.ID]
		if gg == nil {
			gg = &store.Group{ID: g.ID}
			st.Groups[g.ID] = gg
		}
		gg.Name = g.Name
		gg.Description = g.Description
		gg.ParentID = gp
		gg.MemberIDs = g.MemberIDs
		gg.UpdatedAt = now
	}
	st.Storage = map[string]*store.StorageTarget{}
	for _, t := range snap.Storage {
		if t.ID == "" {
			continue
		}
		st.Storage[t.ID] = &store.StorageTarget{
			ID: t.ID, Name: t.Name, Kind: t.Kind, Endpoint: t.Endpoint, Bucket: t.Bucket,
			Prefix: t.Prefix, Region: t.Region, Path: t.Path, SMBUser: t.SMBUser, SMBDomain: t.SMBDomain,
			AccessKey: t.AccessKey, NoTLS: t.NoTLS, ObjectLock: t.ObjectLock,
			RetentionDays: t.RetentionDays, SyncIntervalMinutes: t.SyncIntervalMinutes, Enabled: t.Enabled,
			CreatedAt: now,
		}
	}
	for _, rc := range snap.RepoCredentials {
		if rc.AgentID == "" {
			continue
		}
		st.RepoCreds[rc.AgentID] = &store.RepoCredential{
			AgentID: rc.AgentID, Username: rc.Username, Hostname: rc.Hostname, Version: rc.Version,
			CreatedAt: now, RotatedAt: parseRFC3339(rc.RotatedAt),
		}
	}
	for _, p := range snap.SQLPlans {
		if p.ID == "" {
			continue
		}
		enabled := p.Enabled
		st.SQLPlans[p.ID] = &store.SQLPlan{
			ID: p.ID, AgentID: p.AgentID, Server: p.Server, Instance: p.Instance,
			Database: p.Database, Type: p.Type, Schedule: p.Schedule, Timezone: p.Timezone,
			StorageTargetID: p.StorageTargetID, RetentionDays: p.RetentionDays,
			Verify: p.Verify, Enabled: enabled, VerifyCron: p.VerifyCron,
			CreatedAt: now, UpdatedAt: now,
		}
	}
	for _, c := range snap.SQLCerts {
		if c.Name == "" {
			continue
		}
		st.SQLCerts[c.Name] = &store.SQLCertificate{
			Name: c.Name, Server: c.Server, Instance: c.Instance, TargetID: c.TargetID,
			CertURI: c.CertURI, PvkURI: c.PvkURI, CreatedAt: parseRFC3339(c.CreatedAt),
		}
	}
	for _, inv := range snap.SQLInventories {
		if inv.AgentID == "" {
			continue
		}
		st.SQLInventories[inv.AgentID] = &store.SQLInventorySnapshot{
			AgentID: inv.AgentID, Server: inv.Server, Instance: inv.Instance,
			Items: inv.Items, UpdatedAt: parseRFC3339(inv.UpdatedAt),
		}
	}
	st.BumpConfig()
	return nil
}

// parseRFC3339 convierte una cadena RFC3339 (o vacía) a time.Time.
func parseRFC3339(s string) (t time.Time) {
	t, _ = time.Parse(time.RFC3339, s)
	return
}

// parseRFC3339Ptr convierte una cadena RFC3339 opcional a *time.Time.
func parseRFC3339Ptr(s string) *time.Time {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return nil
	}
	return &t
}

package contracts

import (
	"encoding/json"
	"time"
)

// --- Identidad de administración ---

// Roles de usuario (docs/13-seguridad.md, RBAC).
const (
	RoleAdmin    = "admin"    // todo, incluidos usuarios, ajustes, tokens y auditoría
	RoleOperator = "operator" // lectura + backup, restauración, cancelación y alertas
	RoleViewer   = "viewer"   // solo lectura
)

// LoginRequest es el cuerpo de POST /api/v1/auth/login.
type LoginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

// MFARequest es el cuerpo de POST /api/v1/auth/mfa y /auth/step-up.
// Se envía code (TOTP) o recovery_code.
type MFARequest struct {
	Code         string `json:"code,omitempty"`
	RecoveryCode string `json:"recovery_code,omitempty"`
}

// TOTPSetupResponse devuelve el secreto TOTP una sola vez.
type TOTPSetupResponse struct {
	Secret     string `json:"secret"`
	OTPAuthURL string `json:"otpauth_url"`
	Issuer     string `json:"issuer"`
	Account    string `json:"account"`
}

// TOTPEnableResponse devuelve los códigos de recuperación una sola vez.
type TOTPEnableResponse struct {
	RecoveryCodes []string `json:"recovery_codes"`
}

// PasswordChangeRequest es el cuerpo de POST /api/v1/auth/password.
type PasswordChangeRequest struct {
	CurrentPassword string `json:"current_password"`
	NewPassword     string `json:"new_password"`
}

// UserView es un usuario sin secretos.
type UserView struct {
	ID          string     `json:"id"`
	Email       string     `json:"email"`
	Name        string     `json:"name"`
	Role        string     `json:"role"`
	TOTPEnabled bool       `json:"totp_enabled"`
	Disabled    bool       `json:"disabled"`
	CreatedAt   time.Time  `json:"created_at"`
	LastLoginAt *time.Time `json:"last_login_at,omitempty"`
}

// UserList es la respuesta de GET /api/v1/admin/users.
type UserList struct {
	Items []UserView `json:"items"`
}

// UserRequest crea (POST) o modifica (PATCH) un usuario.
type UserRequest struct {
	Email    string  `json:"email,omitempty"`
	Name     *string `json:"name,omitempty"`
	Role     string  `json:"role,omitempty"`
	Password string  `json:"password,omitempty"`
	Disabled *bool   `json:"disabled,omitempty"`
}

// SessionInfo describe la sesión actual.
type SessionInfo struct {
	// AuthMethod ∈ session | api_token.
	AuthMethod       string     `json:"auth_method"`
	MFAPassed        bool       `json:"mfa_passed"`
	MFASetupRequired bool       `json:"mfa_setup_required"`
	ExpiresAt        *time.Time `json:"expires_at,omitempty"`
	StepUpUntil      *time.Time `json:"step_up_until,omitempty"`
}

// MeResponse es la respuesta de GET /api/v1/auth/me, /auth/login y /auth/mfa.
type MeResponse struct {
	User        UserView    `json:"user"`
	Session     SessionInfo `json:"session"`
	CSRFToken   string      `json:"csrf_token,omitempty"`
	Permissions []string    `json:"permissions"`
}

// APITokenRequest crea un token de integración.
type APITokenRequest struct {
	Name             string `json:"name"`
	Role             string `json:"role"`
	ExpiresInSeconds int    `json:"expires_in_seconds,omitempty"`
}

// APITokenView es un token de integración sin secreto.
type APITokenView struct {
	ID         string     `json:"id"`
	Name       string     `json:"name"`
	Role       string     `json:"role"`
	Prefix     string     `json:"prefix"`
	CreatedAt  time.Time  `json:"created_at"`
	CreatedBy  string     `json:"created_by,omitempty"`
	ExpiresAt  *time.Time `json:"expires_at,omitempty"`
	LastUsedAt *time.Time `json:"last_used_at,omitempty"`
	RevokedAt  *time.Time `json:"revoked_at,omitempty"`
}

// APITokenCreated devuelve el token en claro una sola vez.
type APITokenCreated struct {
	APITokenView
	Token string `json:"token"`
}

// APITokenList es la respuesta de GET /api/v1/admin/api-tokens.
type APITokenList struct {
	Items []APITokenView `json:"items"`
}

// --- Grupos ---

// GroupRequest crea (POST) o modifica (PATCH) un grupo.
type GroupRequest struct {
	Name        *string `json:"name,omitempty"`
	Description *string `json:"description,omitempty"`
	ParentID    *string `json:"parent_id,omitempty"`
}

// GroupView es un grupo de equipos.
type GroupView struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description,omitempty"`
	ParentID    string    `json:"parent_id,omitempty"`
	MemberIDs   []string  `json:"member_ids"`
	PlanIDs     []string  `json:"plan_ids"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// GroupList es la respuesta de GET /api/v1/admin/groups.
type GroupList struct {
	Items []GroupView `json:"items"`
}

// GroupMemberRequest añade un equipo a un grupo.
type GroupMemberRequest struct {
	AgentID string `json:"agent_id"`
}

// --- Puntos de recuperación y restauración ---

// Estados de verificación de un punto de recuperación.
const (
	VerifyNone    = "unverified"
	VerifyRunning = "running"
	VerifyOK      = "structural_ok" // kopia snapshot verify sin errores
	VerifyContent = "content_ok"    // lectura de contenido (porcentaje > 0) sin errores
	VerifyFailed  = "failed"
)

// RestorePointView es un punto de recuperación (snapshot de una ruta).
type RestorePointView struct {
	ID           string     `json:"id"`
	AgentID      string     `json:"agent_id"`
	Hostname     string     `json:"hostname,omitempty"`
	PlanID       string     `json:"plan_id,omitempty"`
	PlanName     string     `json:"plan_name,omitempty"`
	JobID        string     `json:"job_id"`
	Kind         string     `json:"kind"` // file
	SnapshotID   string     `json:"snapshot_id"`
	TargetID     string     `json:"target_id,omitempty"`
	RootObjectID string     `json:"root_object_id,omitempty"`
	SourcePath   string     `json:"source_path"`
	SourceHost   string     `json:"source_host,omitempty"`
	SourceUser   string     `json:"source_user,omitempty"`
	CreatedAt    time.Time  `json:"created_at"`
	StartTime    time.Time  `json:"start_time"`
	EndTime      time.Time  `json:"end_time"`
	SizeBytes    int64      `json:"size_bytes"`
	Files        int64      `json:"files"`
	Dirs         int64      `json:"dirs"`
	ErrorCount   int        `json:"error_count"`
	Consistency  string     `json:"consistency"` // crash | application
	Encrypted    bool       `json:"encrypted"`
	Destination  string     `json:"destination"` // NAS | S3 | PBS
	VerifyState  string     `json:"verify_state"`
	VerifiedAt   *time.Time `json:"verified_at,omitempty"`
	VerifyDetail string     `json:"verify_detail,omitempty"`
	ExpiredAt    *time.Time `json:"expired_at,omitempty"`
	Browsable    bool       `json:"browsable"`
	// SQL: metadatos del motor SQL Server (Kind "sql"); nil en el resto.
	SQL *RestorePointSQLView `json:"sql,omitempty"`
	// PG: metadatos del motor PostgreSQL (Kind "pg"); nil en el resto.
	PG *RestorePointPGView `json:"pg,omitempty"`
}

// RestorePointPGView son los metadatos PostgreSQL de un punto de
// restauración (host/puerto/base e integridad del dump).
type RestorePointPGView struct {
	Host      string `json:"host"`
	Port      string `json:"port,omitempty"`
	Database  string `json:"database"`
	RepoURI   string `json:"repo_uri,omitempty"`
	Verified  bool   `json:"verified"`
	SizeBytes int64  `json:"size_bytes,omitempty"`
}

// RestorePointSQLView son los metadatos SQL Server de un punto de
// restauración (instancia/base, tipo, LSNs como texto e integridad).
type RestorePointSQLView struct {
	Instance    string `json:"instance"`
	Database    string `json:"database"`
	BackupType  string `json:"backup_type"`
	DatabaseLSN string `json:"database_lsn,omitempty"`
	FirstLSN    string `json:"first_lsn,omitempty"`
	LastLSN     string `json:"last_lsn,omitempty"`
	Checksum    bool   `json:"checksum"`
	Compression bool   `json:"compression"`
	Verified    bool   `json:"verified"`
}

// RestorePointList es la respuesta de GET /api/v1/admin/restore-points.
type RestorePointList struct {
	Items      []RestorePointView `json:"items"`
	NextCursor string             `json:"next_cursor,omitempty"`
}

// FileEntry es un elemento de un directorio dentro de un snapshot.
type FileEntry struct {
	Name  string    `json:"name"`
	Path  string    `json:"path"` // relativa a la raíz del snapshot, separador "/"
	Type  string    `json:"type"` // file | dir | symlink | other
	Size  int64     `json:"size"`
	MTime time.Time `json:"mtime"`
	Mode  string    `json:"mode,omitempty"`
	Files int64     `json:"files,omitempty"` // directorios: archivos contenidos
}

// FileListing es la respuesta de GET /admin/restore-points/{id}/files?path=.
type FileListing struct {
	RestorePointID string      `json:"restore_point_id"`
	Path           string      `json:"path"`
	Entries        []FileEntry `json:"entries"`
}

// FileSearchResult es la respuesta de GET /admin/restore-points/{id}/files/search.
// Truncated indica que el recorrido se cortó por límites (profundidad,
// directorios visitados o resultados).
type FileSearchResult struct {
	RestorePointID string      `json:"restore_point_id"`
	Path           string      `json:"path"`
	Query          string      `json:"query"`
	Truncated      bool        `json:"truncated"`
	Items          []FileEntry `json:"items"`
}

// InstallerRequest crea un instalador preconfigurado: genera un token de
// enrolamiento de un solo uso y devuelve las URLs para descargar el script
// y el binario del agente ya listos para conectar al servidor.
type InstallerRequest struct {
	OS               string `json:"os"` // windows | linux | darwin (macOS)
	Arch             string `json:"arch"`
	ExpiresInSeconds int    `json:"expires_in_seconds,omitempty"`
	ExpectedHostname string `json:"expected_hostname,omitempty"`
	Note             string `json:"note,omitempty"`
}

// InstallerResponse es la respuesta de POST /admin/installers. InstallerURL
// es absoluta (usa PublicURL) y InstallerPath relativa (para el panel, que
// sirve la API por proxy). OneLiner es el comando para el equipo destino.
type InstallerResponse struct {
	OS            string    `json:"os"`
	Arch          string    `json:"arch"`
	Token         string    `json:"token"`
	ExpiresAt     time.Time `json:"expires_at"`
	InstallerURL  string    `json:"installer_url"`
	InstallerPath string    `json:"installer_path"`
	BinaryURL     string    `json:"binary_url"`
	OneLiner      string    `json:"one_liner"`
	AgentFilename string    `json:"agent_filename"`
}

// ReconnectTokenResponse es la respuesta de POST
// /admin/agents/{id}/reconnect-token. El token re-enrola el agente
// EXISTENTE (misma identidad: grupos, planes e historial se conservan);
// no crea un equipo nuevo. OneLiner/InstallerURL solo vienen si el SO y la
// arquitectura del agente tienen instalador (si no, usar el token a mano).
type ReconnectTokenResponse struct {
	ID            string    `json:"id"`
	Token         string    `json:"token"`
	ExpiresAt     time.Time `json:"expires_at"`
	AgentID       string    `json:"agent_id"`
	Hostname      string    `json:"hostname"`
	OS            string    `json:"os"`
	Arch          string    `json:"arch"`
	InstallerURL  string    `json:"installer_url,omitempty"`
	InstallerPath string    `json:"installer_path,omitempty"`
	OneLiner      string    `json:"one_liner,omitempty"`
}

// PVEInstance es una instancia de Proxmox registrada para inventario y
// operación. El secreto del token queda cifrado en reposo.
type PVEInstance struct {
	ID             string     `json:"ID"`
	Name           string     `json:"Name"`
	URL            string     `json:"URL"`
	TokenUser      string     `json:"TokenUser"` // p. ej. root@pam
	TokenID        string     `json:"TokenID"`   // p. ej. bdr
	TokenSecretEnc string     `json:"TokenSecretEnc"`
	VerifyTLS      bool       `json:"VerifyTLS"`
	CreatedAt      time.Time  `json:"CreatedAt"`
	LastVerifiedAt *time.Time `json:"LastVerifiedAt,omitempty"`
	LastCheck      string     `json:"LastCheck,omitempty"` // ok | error: …
	AgentID        string     `json:"AgentID,omitempty"`   // agente del nodo PVE
}

// PVEInstanceRequest crea o actualiza una instancia.
type PVEInstanceRequest struct {
	Name        string `json:"name"`
	URL         string `json:"url"` // https://host:8006
	TokenUser   string `json:"token_user"`
	TokenID     string `json:"token_id"`
	TokenSecret string `json:"token_secret"` // solo en creación/cambio
	VerifyTLS   bool   `json:"verify_tls"`
	AgentID     string `json:"agent_id,omitempty"` // agente del nodo PVE
}

// PVEVM es una máquina virtual reportada por la API de Proxmox.
type PVEVM struct {
	VMID    int    `json:"vmid"`
	Name    string `json:"name"`
	Node    string `json:"node"`
	Status  string `json:"status"` // running | stopped
	Type    string `json:"type"`   // qemu | lxc
	MemMiB  int64  `json:"mem_mib"`
	DiskGiB int64  `json:"disk_gib"`
	Cores   int    `json:"cores,omitempty"`
}

// PVEVMList es la respuesta de GET /admin/proxmox/instances/{id}/vms.
type PVEVMList struct {
	InstanceID string  `json:"instance_id"`
	Name       string  `json:"name"`
	URL        string  `json:"url"`
	AgentID    string  `json:"agent_id,omitempty"` // agente del nodo PVE
	Items      []PVEVM `json:"items"`
	LastCheck  string  `json:"last_check,omitempty"`
}

// StorageTargetRequest crea o actualiza un destino de réplica S3 o una ruta
// física local. Para filesystem, la ruta puede ser local (/mnt/backups) o
// compartida SMB (//servidor/recurso[/subdir] o smb://…), con credenciales
// opcionales (usuario, contraseña y dominio AD/workgroup).
type StorageTargetRequest struct {
	Name        string `json:"name"`
	Kind        string `json:"kind,omitempty"`     // s3 (por defecto) | filesystem
	Endpoint    string `json:"endpoint,omitempty"` // host:puerto o https://host (s3)
	Bucket      string `json:"bucket,omitempty"`   // s3
	Prefix      string `json:"prefix,omitempty"`
	Path        string `json:"path,omitempty"` // ruta física (filesystem)
	SMBUser     string `json:"smb_user,omitempty"`
	SMBPassword string `json:"smb_password,omitempty"` // en PUT, vacío = conserva la actual
	SMBDomain   string `json:"smb_domain,omitempty"`   // dominio AD o workgroup
	Region      string `json:"region,omitempty"`
	AccessKey   string `json:"access_key,omitempty"`
	SecretKey   string `json:"secret_key,omitempty"` // en PUT, vacío = conserva el actual
	// RepoPassword: contraseña del repo Kopia del destino. Vacío en create =
	// se genera; en PUT vacío = conserva la actual. Con valor = ADOPTAR el
	// repositorio existente (creado por otra instancia); nunca se formatea.
	RepoPassword        string `json:"repo_password,omitempty"`
	NoTLS               bool   `json:"no_tls"`                // MinIO local sin TLS
	ObjectLock          bool   `json:"object_lock"`           // el bucket tiene lock (inmutabilidad)
	RetentionDays       int    `json:"retention_days"`        // retención GOVERNANCE en días
	SyncIntervalMinutes int    `json:"sync_interval_minutes"` // 0 = solo manual
	Enabled             bool   `json:"enabled"`
}

// StorageTargetView es un destino sin secretos.
type StorageTargetView struct {
	ID                  string `json:"id"`
	Name                string `json:"name"`
	Kind                string `json:"kind"` // s3 (por defecto) | filesystem
	Endpoint            string `json:"endpoint,omitempty"`
	Bucket              string `json:"bucket,omitempty"`
	Prefix              string `json:"prefix,omitempty"`
	Region              string `json:"region,omitempty"`
	Path                string `json:"path,omitempty"` // ruta física (filesystem)
	SMB                 bool   `json:"smb,omitempty"`  // la ruta física es compartida SMB
	SMBUser             string `json:"smb_user,omitempty"`
	SMBDomain           string `json:"smb_domain,omitempty"`
	AccessKey           string `json:"access_key"`
	NoTLS               bool   `json:"no_tls"`
	ObjectLock          bool   `json:"object_lock"`
	RetentionDays       int    `json:"retention_days"`
	SyncIntervalMinutes int    `json:"sync_interval_minutes"`
	Enabled             bool   `json:"enabled"`
	// RepoPasswordSet: hay contraseña de repo configurada (generada o
	// adoptada); el panel puede ofrecer su revelado break-glass.
	RepoPasswordSet  bool       `json:"repo_password_set,omitempty"`
	CreatedAt        time.Time  `json:"created_at"`
	LastSyncAt       *time.Time `json:"last_sync_at,omitempty"`
	LastSyncStatus   string     `json:"last_sync_status,omitempty"`
	LastVerifyAt     *time.Time `json:"last_verify_at,omitempty"`
	LastVerifyStatus string     `json:"last_verify_status,omitempty"`
}

// StorageTargetList es la respuesta de GET /admin/storage/targets.
type StorageTargetList struct {
	Items []StorageTargetView `json:"items"`
}

// VerifyRequest es el cuerpo de POST /admin/restore-points/{id}/verify.
type VerifyRequest struct {
	// FilesPercent es el porcentaje de archivos cuyo contenido se lee (0–100).
	FilesPercent int `json:"files_percent"`
}

// FileRestoreRequest es el cuerpo de POST /api/v1/admin/restores/files.
type FileRestoreRequest struct {
	RestorePointID string   `json:"restore_point_id"`
	TargetAgentID  string   `json:"target_agent_id,omitempty"` // por defecto, el equipo de origen
	Items          []string `json:"items,omitempty"`
	Destination    string   `json:"destination,omitempty"`
	InPlace        bool     `json:"in_place,omitempty"`
	Overwrite      bool     `json:"overwrite,omitempty"`
}

// RestoreResponse es la respuesta 202 de una restauración.
type RestoreResponse struct {
	RestoreID string `json:"restore_id"`
	JobID     string `json:"job_id"`
	State     string `json:"state"`
}

// --- Alertas ---

// Estados de alerta.
const (
	AlertOpen         = "open"
	AlertAcknowledged = "acknowledged"
	AlertResolved     = "resolved"
)

// Severidades.
const (
	SeverityCritical = "critical"
	SeverityHigh     = "high"
	SeverityMedium   = "medium"
	SeverityLow      = "low"
)

// AlertView es una alerta deduplicada.
type AlertView struct {
	ID         string     `json:"id"`
	Key        string     `json:"key"`
	Kind       string     `json:"kind"`
	Severity   string     `json:"severity"`
	State      string     `json:"state"`
	Title      string     `json:"title"`
	Detail     string     `json:"detail,omitempty"`
	AgentID    string     `json:"agent_id,omitempty"`
	PlanID     string     `json:"plan_id,omitempty"`
	JobID      string     `json:"job_id,omitempty"`
	Count      int        `json:"count"`
	FirstAt    time.Time  `json:"first_at"`
	LastAt     time.Time  `json:"last_at"`
	AckBy      string     `json:"ack_by,omitempty"`
	AckAt      *time.Time `json:"ack_at,omitempty"`
	ResolvedBy string     `json:"resolved_by,omitempty"`
	ResolvedAt *time.Time `json:"resolved_at,omitempty"`
	NotifiedAt *time.Time `json:"notified_at,omitempty"`
}

// AlertList es la respuesta de GET /api/v1/admin/alerts.
type AlertList struct {
	Items []AlertView `json:"items"`
}

// --- Auditoría ---

// AuditEvent es un registro de auditoría encadenado por hash.
type AuditEvent struct {
	Seq        int64             `json:"seq"`
	ID         string            `json:"id"`
	At         time.Time         `json:"at"`
	ActorType  string            `json:"actor_type"` // user | api_token | agent | system
	ActorID    string            `json:"actor_id,omitempty"`
	ActorName  string            `json:"actor_name,omitempty"`
	Action     string            `json:"action"`
	TargetType string            `json:"target_type,omitempty"`
	TargetID   string            `json:"target_id,omitempty"`
	Result     string            `json:"result"` // ok | denied | error
	IP         string            `json:"ip,omitempty"`
	RequestID  string            `json:"request_id,omitempty"`
	Details    map[string]string `json:"details,omitempty"`
	PrevHash   string            `json:"prev_hash"`
	Hash       string            `json:"hash"`
}

// AuditList es la respuesta de GET /api/v1/admin/audit.
type AuditList struct {
	Items      []AuditEvent `json:"items"`
	NextCursor string       `json:"next_cursor,omitempty"`
}

// AuditVerify es la respuesta de GET /api/v1/admin/audit/verify.
type AuditVerify struct {
	OK        bool   `json:"ok"`
	Events    int64  `json:"events"`
	FirstSeq  int64  `json:"first_seq"`
	LastSeq   int64  `json:"last_seq"`
	LastHash  string `json:"last_hash"`
	BrokenAt  int64  `json:"broken_at,omitempty"`
	BrokenWhy string `json:"broken_reason,omitempty"`
}

// --- Ajustes y notificaciones ---

// Settings son los ajustes editables. Los secretos son de solo escritura:
// en GET se devuelven vacíos con *_configured=true.
type Settings struct {
	EnvironmentName     string   `json:"environment_name"`
	AgentOfflineMinutes int      `json:"agent_offline_minutes"`
	AlertMinSeverity    string   `json:"alert_min_severity"`
	AlertRenotifyHours  int      `json:"alert_renotify_hours"`
	WebhookURL          string   `json:"webhook_url"`
	WebhookSecret       string   `json:"webhook_secret,omitempty"`
	WebhookConfigured   bool     `json:"webhook_secret_configured"`
	SMTPHost            string   `json:"smtp_host"`
	SMTPPort            int      `json:"smtp_port"`
	SMTPUsername        string   `json:"smtp_username"`
	SMTPPassword        string   `json:"smtp_password,omitempty"`
	SMTPConfigured      bool     `json:"smtp_password_configured"`
	SMTPFrom            string   `json:"smtp_from"`
	SMTPTo              []string `json:"smtp_to"`
	SMTPStartTLS        bool     `json:"smtp_starttls"`
	SessionHours        int      `json:"session_hours"`
}

// NotificationTestResult es la respuesta de POST /admin/settings/test-notification.
type NotificationTestResult struct {
	Webhook string `json:"webhook"` // ok | skipped | error: ...
	Email   string `json:"email"`
}

// --- Repositorios ---

// RepositoryView describe el repositorio Kopia configurado.
type RepositoryView struct {
	ID              string     `json:"id"`
	Name            string     `json:"name"`
	Kind            string     `json:"kind"`
	URL             string     `json:"url,omitempty"`
	Path            string     `json:"path,omitempty"`
	Fingerprint     string     `json:"server_cert_fingerprint,omitempty"`
	ServerSide      bool       `json:"server_side"` // el plano de control tiene acceso propietario (navegación, verificación)
	Destination     string     `json:"destination"` // NAS
	Status          string     `json:"status"`      // ok | error | unknown | unconfigured
	StatusDetail    string     `json:"status_detail,omitempty"`
	CheckedAt       *time.Time `json:"checked_at,omitempty"`
	SnapshotCount   int        `json:"snapshot_count"`
	LogicalBytes    int64      `json:"logical_bytes"`
	StoredBytes     int64      `json:"stored_bytes,omitempty"`
	AgentsWithCreds int        `json:"agents_with_credentials"`
}

// RepositoryList es la respuesta de GET /api/v1/admin/repositories.
type RepositoryList struct {
	Items []RepositoryView `json:"items"`
}

// --- Resumen y sistema ---

// UpcomingRun es una próxima ejecución programada.
type UpcomingRun struct {
	At       time.Time `json:"at"`
	PlanID   string    `json:"plan_id"`
	PlanName string    `json:"plan_name"`
	AgentID  string    `json:"agent_id"`
	Hostname string    `json:"hostname"`
}

// Summary es la respuesta de GET /api/v1/admin/summary.
type Summary struct {
	GeneratedAt       time.Time      `json:"generated_at"`
	AgentsTotal       int            `json:"agents_total"`
	AgentsOnline      int            `json:"agents_online"`
	AgentsProtected   int            `json:"agents_protected"` // con plan y RP correcto dentro del RPO
	AgentsRevoked     int            `json:"agents_revoked"`
	PlansTotal        int            `json:"plans_total"`
	JobsActive        int            `json:"jobs_active"`
	Success24h        Ratio          `json:"success_24h"`
	Success7d         Ratio          `json:"success_7d"`
	RestorePoints     int            `json:"restore_points"`
	LogicalBytes      int64          `json:"logical_bytes"`
	StoredBytes       int64          `json:"stored_bytes,omitempty"`
	BytesUploaded24h  int64          `json:"bytes_read_24h"`
	Upcoming          []UpcomingRun  `json:"upcoming"`
	AlertsOpen        map[string]int `json:"alerts_open"` // por severidad
	RecentRestores    []JobView      `json:"recent_restores"`
	BackupsPerDay     []DayCount     `json:"backups_per_day"`
	RepositoryStatus  string         `json:"repository_status"`
	LastSchedulerTick *time.Time     `json:"last_scheduler_tick,omitempty"`
}

// Ratio es un porcentaje de éxito.
type Ratio struct {
	Succeeded int     `json:"succeeded"`
	Failed    int     `json:"failed"`
	Percent   float64 `json:"percent"` // 0–100; -1 si no hay datos
}

// DayCount es el número de backups correctos y fallidos de un día (UTC).
type DayCount struct {
	Day       string `json:"day"` // YYYY-MM-DD
	Succeeded int    `json:"succeeded"`
	Failed    int    `json:"failed"`
}

// SystemHealth es la respuesta de GET /api/v1/admin/system/health.
type SystemHealth struct {
	Status     string            `json:"status"` // ok | degraded
	Version    string            `json:"version"`
	Components map[string]string `json:"components"`
	StartedAt  time.Time         `json:"started_at"`
	AgentTLS   bool              `json:"agent_tls"`
	Details    json.RawMessage   `json:"details,omitempty"`
}

// UpdateStatus es la respuesta de GET /api/v1/admin/system/update: el
// resultado del último chequeo del canal de releases (modo notificación).
type UpdateStatus struct {
	// CheckEnabled es false si el binario no lleva clave pública del canal
	// o no se configuró --update-url.
	CheckEnabled bool   `json:"check_enabled"`
	Current      string `json:"current"`
	// Latest es la versión firmada del canal; vacío si aún no hubo chequeo.
	Latest   string `json:"latest,omitempty"`
	Channel  string `json:"channel,omitempty"`
	NotesURL string `json:"notes_url,omitempty"`
	// UpdateAvailable es true si Latest > Current.
	UpdateAvailable bool       `json:"update_available"`
	CheckedAt       *time.Time `json:"checked_at,omitempty"`
	LastError       string     `json:"last_error,omitempty"`
	// Platform del host para el asset de instalación.
	Platform string `json:"platform,omitempty"`
}

// UpdateInstallResponse confirma el arranque de la instalación.
type UpdateInstallResponse struct {
	// Restarting indica que el proceso se sustituye y el servidor vuelve
	// en unos segundos con la versión nueva.
	Restarting bool   `json:"restarting"`
	Version    string `json:"version"`
	Message    string `json:"message,omitempty"`
}

// BackupVMRequest dispara un backup de VM de Proxmox.
type BackupVMRequest struct {
	StorageTargetID string `json:"storage_target_id"`
}

// VMRestorePointList lista los puntos de restauración de una VM.
type VMRestorePointList struct {
	Items []RestorePointView `json:"items"`
}

// RestoreVMRequest restaura una VM de Proxmox desde un punto de
// restauración de imagen (en sitio o como instancia nueva).
type RestoreVMRequest struct {
	RestorePointID   string `json:"restore_point_id"`
	TargetInstanceID string `json:"target_instance_id"`
	Mode             string `json:"mode"` // in_place | new
	TargetVMID       int    `json:"target_vmid"`
	TargetName       string `json:"target_name,omitempty"`
	TargetStorage    string `json:"target_storage,omitempty"`
}

// --- Copia de seguridad y restauración de configuración (migración) ---

// ConfigBackupSchemaVersion fija el formato de la copia de seguridad de
// configuración exportada por GET /api/v1/admin/config/backup y aceptada por
// POST /api/v1/admin/config/restore.
const ConfigBackupSchemaVersion = 1

// ConfigBackupSettings son los ajustes exportables (sin secretos: webhook y
// SMTP se reconfiguran en destino).
type ConfigBackupSettings struct {
	EnvironmentName     string   `json:"environment_name"`
	AgentOfflineMinutes int      `json:"agent_offline_minutes"`
	AlertMinSeverity    string   `json:"alert_min_severity"`
	AlertRenotifyHours  int      `json:"alert_renotify_hours"`
	WebhookURL          string   `json:"webhook_url"`
	WebhookConfigured   bool     `json:"webhook_configured"`
	SMTPHost            string   `json:"smtp_host"`
	SMTPPort            int      `json:"smtp_port"`
	SMTPUsername        string   `json:"smtp_username"`
	SMTPConfigured      bool     `json:"smtp_configured"`
	SMTPFrom            string   `json:"smtp_from"`
	SMTPTo              []string `json:"smtp_to"`
	SMTPStartTLS        bool     `json:"smtp_starttls"`
	SessionHours        int      `json:"session_hours"`
}

// ConfigBackupAgent resume un agente para la exportación de configuración
// (equipos enrolados: hostname, SO, versión, estado de revocación…).
type ConfigBackupAgent struct {
	ID                 string `json:"id"`
	Hostname           string `json:"hostname"`
	OSFamily           string `json:"os_family"`
	OSVersion          string `json:"os_version"`
	Arch               string `json:"arch"`
	AgentVersion       string `json:"agent_version"`
	EnrolledAt         string `json:"enrolled_at"`
	LastSeenAt         string `json:"last_seen_at,omitempty"`
	RevokedAt          string `json:"revoked_at,omitempty"`
	RevokedReason      string `json:"revoked_reason,omitempty"`
	UpgradeRequestedAt string `json:"upgrade_requested_at,omitempty"`
}

// ConfigBackupPlan es un plan de protección exportable.
type ConfigBackupPlan struct {
	ID              string    `json:"id"`
	Name            string    `json:"name"`
	Description     string    `json:"description,omitempty"`
	Kind            string    `json:"kind"`
	Paths           []string  `json:"paths,omitempty"`
	Excludes        []string  `json:"excludes,omitempty"`
	Disks           []int     `json:"disks,omitempty"`
	Enabled         bool      `json:"enabled"`
	Schedule        string    `json:"schedule,omitempty"`
	Timezone        string    `json:"timezone,omitempty"`
	Window          *Window   `json:"window,omitempty"`
	Retention       Retention `json:"retention"`
	RPOHours        int       `json:"rpo_hours,omitempty"`
	StorageTargetID string    `json:"storage_target_id,omitempty"`
}

// ConfigBackupAssignment relaciona un plan con un agente o un grupo.
type ConfigBackupAssignment struct {
	ID         string `json:"id"`
	PlanID     string `json:"plan_id"`
	TargetType string `json:"target_type"`
	TargetID   string `json:"target_id"`
	AgentID    string `json:"agent_id,omitempty"`
	Enabled    bool   `json:"enabled"`
}

// ConfigBackupGroup es un grupo de equipos con jerarquía opcional.
type ConfigBackupGroup struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description,omitempty"`
	ParentID    string   `json:"parent_id,omitempty"`
	MemberIDs   []string `json:"member_ids"`
}

// ConfigBackupStorage es un destino de almacenamiento exportable (sin
// secretos: access_key se exporta porque es legible, los valores cifrados no).
type ConfigBackupStorage struct {
	ID                  string `json:"id"`
	Name                string `json:"name"`
	Kind                string `json:"kind"`
	Endpoint            string `json:"endpoint,omitempty"`
	Bucket              string `json:"bucket,omitempty"`
	Prefix              string `json:"prefix,omitempty"`
	Region              string `json:"region,omitempty"`
	Path                string `json:"path,omitempty"`
	SMB                 bool   `json:"smb,omitempty"`
	SMBUser             string `json:"smb_user,omitempty"`
	SMBDomain           string `json:"smb_domain,omitempty"`
	AccessKey           string `json:"access_key,omitempty"`
	NoTLS               bool   `json:"no_tls"`
	ObjectLock          bool   `json:"object_lock"`
	RetentionDays       int    `json:"retention_days"`
	SyncIntervalMinutes int    `json:"sync_interval_minutes"`
	Enabled             bool   `json:"enabled"`
}

// ConfigBackupRepoCredential resume la credencial de repositorio por agente
// (sin la contraseña: se re-adopta en destino).
type ConfigBackupRepoCredential struct {
	AgentID   string `json:"agent_id"`
	Username  string `json:"username"`
	Hostname  string `json:"hostname"`
	Version   int    `json:"version"`
	RotatedAt string `json:"rotated_at,omitempty"`
}

// ConfigBackupSQLPlan es un plan programado de backup SQL exportable.
type ConfigBackupSQLPlan struct {
	ID              string `json:"id"`
	AgentID         string `json:"agent_id"`
	Server          string `json:"server"`
	Instance        string `json:"instance,omitempty"`
	Database        string `json:"database"`
	Type            string `json:"type"`
	Schedule        string `json:"schedule"`
	Timezone        string `json:"timezone,omitempty"`
	StorageTargetID string `json:"storage_target_id"`
	RetentionDays   int    `json:"retention_days,omitempty"`
	Verify          *bool  `json:"verify,omitempty"`
	Enabled         bool   `json:"enabled"`
	VerifyCron      string `json:"verify_cron,omitempty"`
}

// ConfigBackupSQLCert resume la custodia de un certificado de SQL Server
// exportable (sin la contraseña sellada: se re-custodia en destino).
type ConfigBackupSQLCert struct {
	Name         string `json:"name"`
	Server       string `json:"server"`
	Instance     string `json:"instance,omitempty"`
	TargetID     string `json:"target_id,omitempty"`
	CertURI      string `json:"cert_uri,omitempty"`
	PvkURI       string `json:"pvk_uri,omitempty"`
	LastBackupAt string `json:"last_backup_at,omitempty"`
	CreatedAt    string `json:"created_at,omitempty"`
}

// ConfigBackupSQLInventory resume la caché del inventario de bases de una
// instancia SQL exportada para poblar el selector de bases en destino.
type ConfigBackupSQLInventory struct {
	AgentID   string            `json:"agent_id"`
	Server    string            `json:"server"`
	Instance  string            `json:"instance,omitempty"`
	Items     []SQLDatabaseView `json:"items"`
	UpdatedAt string            `json:"updated_at,omitempty"`
}

// ConfigBackup es el documento completo de exportación/restauración de la
// configuración del plano de control (formato v1, pensado para migración de
// servidor: se exporta de un control y se restaura en otro ya instalado).
type ConfigBackup struct {
	SchemaVersion   int                          `json:"schema_version"`
	ExportedAt      string                       `json:"exported_at"`
	Settings        ConfigBackupSettings         `json:"settings"`
	Agents          []ConfigBackupAgent          `json:"agents"`
	Plans           []ConfigBackupPlan           `json:"plans"`
	Assignments     []ConfigBackupAssignment     `json:"assignments"`
	Groups          []ConfigBackupGroup          `json:"groups"`
	Storage         []ConfigBackupStorage        `json:"storage"`
	RepoCredentials []ConfigBackupRepoCredential `json:"repo_credentials"`
	SQLPlans        []ConfigBackupSQLPlan        `json:"sql_plans"`
	SQLCerts        []ConfigBackupSQLCert        `json:"sql_certs"`
	SQLInventories  []ConfigBackupSQLInventory   `json:"sql_inventories"`
}

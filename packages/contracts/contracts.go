// Package contracts define los tipos JSON compartidos por servidor, agente y panel.
//
// Reglas de compatibilidad (docs/06-api.md §9): los cambios en /api/v1 son
// aditivos; los campos nuevos son opcionales y los clientes ignoran los
// desconocidos.
package contracts

import (
	"encoding/json"
	"time"
)

// Estados de un agente vistos por la API.
const (
	AgentPending = "pending"
	AgentOnline  = "online"
	AgentOffline = "offline"
	AgentRevoked = "revoked"
)

// Estados de heartbeat del agente.
const (
	StateIdle         = "IDLE"
	StateRunning      = "RUNNING_BACKUP"
	StateRestoring    = "RESTORING"
	StateError        = "ERROR"
	StateOfflineReady = "OFFLINE"
)

// Tipos de job.
const (
	JobTypeBackupFile    = "BACKUP_FILE"
	JobTypeRestoreFile   = "RESTORE_FILE"
	JobTypeBackupImage   = "BACKUP_IMAGE"
	JobTypeBackupVM      = "BACKUP_VM"
	JobTypeRestoreVM     = "RESTORE_VM"
	JobTypeBackupLXC     = "BACKUP_LXC"
	JobTypeRestoreLXC    = "RESTORE_LXC"
	JobTypeSQLBackup     = "SQL_BACKUP"
	JobTypeSQLRestore    = "SQL_RESTORE"
	JobTypeSQLVerify     = "SQL_VERIFY"
	JobTypeSQLInventory  = "SQL_INVENTORY"
	JobTypeSQLCheckDB    = "SQL_CHECKDB"
	JobTypeSQLCertBackup = "SQL_CERTBACKUP"
	JobTypePGDump        = "PG_DUMP"
	JobTypePGRestore     = "PG_RESTORE"
	JobTypePGVerify      = "PG_VERIFY"
	JobTypePGInventory   = "PG_INVENTORY"
	JobTypeSQLDiscover   = "SQL_DISCOVER"
	JobTypePGDiscover    = "PG_DISCOVER"
)

// Estados de job (docs/06-api.md §5).
const (
	JobPending   = "pending"
	JobLeased    = "leased"
	JobRunning   = "running"
	JobSucceeded = "succeeded"
	JobFailed    = "failed"
	JobCanceled  = "canceled"
	JobExpired   = "expired"
)

// JobStateFinal indica si un estado es terminal.
func JobStateFinal(state string) bool {
	switch state {
	case JobSucceeded, JobFailed, JobCanceled, JobExpired:
		return true
	}
	return false
}

// --- Enrolamiento y agente ---

// EnrollRequest es el cuerpo de POST /api/v1/agent/enroll.
type EnrollRequest struct {
	Token        string `json:"token"`
	Hostname     string `json:"hostname"`
	OSFamily     string `json:"os_family"`
	OSVersion    string `json:"os_version"`
	Arch         string `json:"arch"`
	AgentVersion string `json:"agent_version"`
	// CSRPEM es opcional: si se envía, el servidor emite un certificado de
	// cliente para mTLS firmado por la CA interna.
	CSRPEM string `json:"csr_pem,omitempty"`
}

// EnrollResponse se devuelve una sola vez; contiene la API key del agente.
type EnrollResponse struct {
	AgentID          string `json:"agent_id"`
	APIKey           string `json:"api_key"`
	HeartbeatSeconds int    `json:"heartbeat_seconds"`
	// Campos mTLS: solo presentes si se envió csr_pem y el servidor tiene
	// el listener de agentes con TLS activo.
	CertificatePEM    string     `json:"certificate_pem,omitempty"`
	CertificateExpiry *time.Time `json:"certificate_expires_at,omitempty"`
	CAPEM             string     `json:"ca_pem,omitempty"`
	ServerFingerprint string     `json:"server_fingerprint,omitempty"`
	AgentURL          string     `json:"agent_url,omitempty"`
}

// RenewCertRequest es el cuerpo de POST /api/v1/agent/renew-cert.
type RenewCertRequest struct {
	CSRPEM string `json:"csr_pem"`
}

// RenewCertResponse devuelve el nuevo certificado.
type RenewCertResponse struct {
	CertificatePEM    string    `json:"certificate_pem"`
	CertificateExpiry time.Time `json:"certificate_expires_at"`
	CAPEM             string    `json:"ca_pem"`
}

// HeartbeatRequest es el cuerpo de POST /api/v1/agent/heartbeat.
type HeartbeatRequest struct {
	AgentVersion string         `json:"agent_version"`
	State        string         `json:"state"`
	CurrentJobID string         `json:"current_job_id,omitempty"`
	Metrics      map[string]any `json:"metrics,omitempty"`
}

// HeartbeatResponse indica al agente qué hacer a continuación.
type HeartbeatResponse struct {
	ServerTime       time.Time `json:"server_time"`
	PendingJobs      []string  `json:"pending_jobs"`
	CancelRequested  []string  `json:"cancel_requested"`
	ConfigVersion    int       `json:"config_version"`
	HeartbeatSeconds int       `json:"heartbeat_seconds"`
	// AgentURL es la URL actual del plano de control para agentes. El
	// agente la persiste y reconfigura si difiere de la suya: cubre el caso
	// de que el operador cambió --agent-url (p. ej. de LAN a NetBird) tras
	// un enrolamiento — la URL antigua queda obsoleta y el TLS falla con
	// certificado emitido para la nueva.
	AgentURL string `json:"agent_url,omitempty"`
	// CertRenewAfter indica cuándo debe renovar el certificado mTLS.
	CertRenewAfter *time.Time `json:"cert_renew_after,omitempty"`
	// Upgrade ofrece al agente una versión nueva de sí mismo: los agentes
	// antiguos ignoran el campo (decodificadores JSON tolerantes) y los
	// actuales se autoactualizan cuando no tienen un job en curso.
	Upgrade *AgentUpgrade `json:"upgrade,omitempty"`
}

// AgentUpgrade describe el binario nuevo que el agente debe descargar.
type AgentUpgrade struct {
	Version string `json:"version"`
	URL     string `json:"url"`
	SHA256  string `json:"sha256"`
	// Force indica que el operador lo pidió explícitamente («Forzar
	// actualización» en el panel). El agente lo registra en su log y lo
	// aplica al quedar libre; no interrumpe un job en curso.
	Force bool `json:"force,omitempty"`
}

// GroupRef es una referencia corta a un grupo.
type GroupRef struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// AgentView es la representación de un agente en la API de administración.
type AgentView struct {
	ID              string          `json:"id"`
	Hostname        string          `json:"hostname"`
	OSFamily        string          `json:"os_family"`
	OSVersion       string          `json:"os_version"`
	Arch            string          `json:"arch"`
	AgentVersion    string          `json:"agent_version"`
	Status          string          `json:"status"`
	ReportedState   string          `json:"reported_state,omitempty"`
	CurrentJobID    string          `json:"current_job_id,omitempty"`
	LastSeenAt      *time.Time      `json:"last_seen_at,omitempty"`
	EnrolledAt      time.Time       `json:"enrolled_at"`
	RevokedAt       *time.Time      `json:"revoked_at,omitempty"`
	RevokedReason   string          `json:"revoked_reason,omitempty"`
	Inventory       json.RawMessage `json:"inventory,omitempty"`
	Groups          []GroupRef      `json:"groups"`
	CertExpiresAt   *time.Time      `json:"cert_expires_at,omitempty"`
	RepoUsername    string          `json:"repository_username,omitempty"`
	LastBackupAt    *time.Time      `json:"last_backup_at,omitempty"`
	LastBackupState string          `json:"last_backup_state,omitempty"`
	NextBackupAt    *time.Time      `json:"next_backup_at,omitempty"`
	// UpgradeRequestedAt marca «Forzar actualización» del operador pendiente
	// de entregar en el próximo heartbeat (oferta con force).
	UpgradeRequestedAt *time.Time `json:"upgrade_requested_at,omitempty"`
}

// AgentList es la respuesta de GET /api/v1/admin/agents.
type AgentList struct {
	Items []AgentView `json:"items"`
}

// AgentDetail es la ficha de GET /api/v1/admin/agents/{id}.
type AgentDetail struct {
	Agent          AgentView          `json:"agent"`
	EffectivePlans []EffectivePlan    `json:"effective_plans"`
	RecentJobs     []JobView          `json:"recent_jobs"`
	RestorePoints  []RestorePointView `json:"restore_points"`
	OpenAlerts     []AlertView        `json:"open_alerts"`
}

// RevokeRequest es el cuerpo de POST /api/v1/admin/agents/{id}/revoke.
type RevokeRequest struct {
	Reason string `json:"reason"`
}

// InventoryRecord es una entrada del histórico de inventario (por hash).
type InventoryRecord struct {
	Hash        string          `json:"hash"`
	CollectedAt time.Time       `json:"collected_at"`
	Inventory   json.RawMessage `json:"inventory"`
}

// InventoryHistory es la respuesta de GET /api/v1/admin/agents/{id}/inventory.
type InventoryHistory struct {
	Items []InventoryRecord `json:"items"`
}

// EnrollmentTokenRequest crea un token de un solo uso.
type EnrollmentTokenRequest struct {
	ExpiresInSeconds int    `json:"expires_in_seconds"`
	ExpectedHostname string `json:"expected_hostname"`
	// GroupID y PlanID se aplican automáticamente al agente al enrolarse.
	GroupID string `json:"group_id,omitempty"`
	PlanID  string `json:"plan_id,omitempty"`
	Note    string `json:"note,omitempty"`
}

// EnrollmentTokenResponse devuelve el token en claro una sola vez.
type EnrollmentTokenResponse struct {
	ID        string    `json:"id"`
	Token     string    `json:"token"`
	ExpiresAt time.Time `json:"expires_at"`
	// Datos para construir el comando de instalación.
	ServerURL         string `json:"server_url,omitempty"`
	AgentURL          string `json:"agent_url,omitempty"`
	ServerFingerprint string `json:"server_fingerprint,omitempty"`
}

// EnrollmentTokenView es la vista sin secreto.
type EnrollmentTokenView struct {
	ID               string     `json:"id"`
	CreatedAt        time.Time  `json:"created_at"`
	ExpiresAt        time.Time  `json:"expires_at"`
	UsedAt           *time.Time `json:"used_at,omitempty"`
	UsedByAgentID    string     `json:"used_by_agent_id,omitempty"`
	ExpectedHostname string     `json:"expected_hostname,omitempty"`
	Expired          bool       `json:"expired"`
	RevokedAt        *time.Time `json:"revoked_at,omitempty"`
	GroupID          string     `json:"group_id,omitempty"`
	PlanID           string     `json:"plan_id,omitempty"`
	Note             string     `json:"note,omitempty"`
	CreatedBy        string     `json:"created_by,omitempty"`
}

// EnrollmentTokenList es la respuesta de GET /api/v1/admin/enrollment-tokens.
type EnrollmentTokenList struct {
	Items []EnrollmentTokenView `json:"items"`
}

// Disk describe un volumen o punto de montaje del inventario.
type Disk struct {
	Name       string `json:"name"`
	Mountpoint string `json:"mountpoint"`
	FSType     string `json:"fs_type,omitempty"`
	TotalBytes int64  `json:"total_bytes"`
	FreeBytes  int64  `json:"free_bytes"`
}

// BlockDevice describe un disco físico o virtual y sus particiones.
type BlockDevice struct {
	Name       string        `json:"name"`
	Type       string        `json:"type"` // disk, part, lvm, crypt, raid, rom
	SizeBytes  int64         `json:"size_bytes"`
	Model      string        `json:"model,omitempty"`
	Serial     string        `json:"serial,omitempty"`
	FSType     string        `json:"fs_type,omitempty"`
	Mountpoint string        `json:"mountpoint,omitempty"`
	UUID       string        `json:"uuid,omitempty"`
	PartTable  string        `json:"partition_table,omitempty"` // gpt, dos
	Removable  bool          `json:"removable,omitempty"`
	Children   []BlockDevice `json:"children,omitempty"`
}

// NetInterface describe una interfaz de red.
type NetInterface struct {
	Name      string   `json:"name"`
	MAC       string   `json:"mac,omitempty"`
	Addresses []string `json:"addresses,omitempty"`
	Up        bool     `json:"up"`
}

// Firmware resume el arranque y la seguridad de plataforma.
type Firmware struct {
	Mode       string `json:"mode,omitempty"` // uefi, bios
	SecureBoot *bool  `json:"secure_boot,omitempty"`
	TPM        *bool  `json:"tpm,omitempty"`
	TPMVersion string `json:"tpm_version,omitempty"`
}

// EncryptedVolume describe BitLocker o LUKS.
type EncryptedVolume struct {
	Volume     string `json:"volume"`
	Kind       string `json:"kind"` // bitlocker, luks
	Protection string `json:"protection,omitempty"`
	Locked     bool   `json:"locked,omitempty"`
}

// Inventory es el inventario que envía el agente.
type Inventory struct {
	CollectedAt      time.Time         `json:"collected_at"`
	Hostname         string            `json:"hostname"`
	OSFamily         string            `json:"os_family"`
	OSVersion        string            `json:"os_version"`
	Arch             string            `json:"arch"`
	CPUCores         int               `json:"cpu_cores"`
	RAMMB            int64             `json:"ram_mb"`
	AgentVersion     string            `json:"agent_version"`
	Disks            []Disk            `json:"disks,omitempty"`
	BlockDevices     []BlockDevice     `json:"block_devices,omitempty"`
	Network          []NetInterface    `json:"network,omitempty"`
	Firmware         *Firmware         `json:"firmware,omitempty"`
	EncryptedVolumes []EncryptedVolume `json:"encrypted_volumes,omitempty"`
	Extra            map[string]any    `json:"extra,omitempty"`
}

// Problem es el formato de error RFC 7807 usado por la API.
type Problem struct {
	Type      string `json:"type"`
	Title     string `json:"title"`
	Status    int    `json:"status"`
	Detail    string `json:"detail,omitempty"`
	Code      string `json:"code,omitempty"`
	RequestID string `json:"request_id,omitempty"`
}

// Health es la respuesta de /healthz.
type Health struct {
	Status  string `json:"status"`
	Version string `json:"version"`
}

// --- Planes, asignaciones y jobs ---

// Retention es la política de retención GFS, aplicada como política de Kopia.
type Retention struct {
	KeepLatest  int `json:"keep_latest"`
	KeepHourly  int `json:"keep_hourly"`
	KeepDaily   int `json:"keep_daily"`
	KeepWeekly  int `json:"keep_weekly"`
	KeepMonthly int `json:"keep_monthly"`
	KeepAnnual  int `json:"keep_annual"`
}

// Window es una ventana horaria "HH:MM"–"HH:MM" en la zona del plan.
// Si End < Start la ventana cruza la medianoche.
type Window struct {
	Start string `json:"start"`
	End   string `json:"end"`
}

// PlanView es un plan de protección.
type PlanView struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description,omitempty"`
	Kind        string   `json:"kind"`
	Paths       []string `json:"paths"`
	Excludes    []string `json:"excludes,omitempty"`
	// Disks (solo planes de imagen): índices de disco a capturar;
	// vacío = todos los discos fijos.
	Disks     []int     `json:"disks,omitempty"`
	Enabled   bool      `json:"enabled"`
	Schedule  string    `json:"schedule,omitempty"` // cron de 5 campos; vacío = solo manual
	Timezone  string    `json:"timezone"`
	Window    *Window   `json:"window,omitempty"`
	Retention Retention `json:"retention"`
	RPOHours  int       `json:"rpo_hours,omitempty"`
	// Destino de almacenamiento del plan (repositorio Kopia S3 dedicado);
	// vacío = repositorio central.
	StorageTargetID string    `json:"storage_target_id,omitempty"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
	// Assignments solo se rellena en GET /admin/backup-plans/{id}.
	Assignments []AssignmentView `json:"assignments,omitempty"`
	// NextRuns son las próximas ejecuciones calculadas (máx. 5).
	NextRuns []time.Time `json:"next_runs,omitempty"`
}

// PlanRequest crea o reemplaza un plan. En PATCH los campos ausentes no cambian.
type PlanRequest struct {
	Name        *string  `json:"name,omitempty"`
	Description *string  `json:"description,omitempty"`
	Kind        string   `json:"kind,omitempty"`
	Paths       []string `json:"paths,omitempty"`
	Excludes    []string `json:"excludes,omitempty"`
	// Disks (solo planes de imagen): índices de disco a capturar. En PATCH
	// ausente/null no cambia; [] limpia la selección (= todos los discos).
	Disks     []int      `json:"disks"`
	Enabled   *bool      `json:"enabled,omitempty"`
	Schedule  *string    `json:"schedule,omitempty"`
	Timezone  *string    `json:"timezone,omitempty"`
	Window    *Window    `json:"window,omitempty"`
	Retention *Retention `json:"retention,omitempty"`
	RPOHours  *int       `json:"rpo_hours,omitempty"`
	// StorageTargetID: destino de almacenamiento donde escribe este plan
	// (repositorio Kopia S3 dedicado); vacío = repositorio central.
	StorageTargetID *string `json:"storage_target_id,omitempty"`
}

// PlanValidation es la respuesta de POST /admin/backup-plans/validate.
type PlanValidation struct {
	Valid    bool        `json:"valid"`
	Errors   []string    `json:"errors"`
	Warnings []string    `json:"warnings"`
	NextRuns []time.Time `json:"next_runs"`
}

// PlanList es la respuesta de GET /api/v1/admin/backup-plans.
type PlanList struct {
	Items []PlanView `json:"items"`
}

// Tipos de destino de una asignación.
const (
	TargetAgent = "agent"
	TargetGroup = "group"
)

// AssignmentRequest asigna un plan a un agente o grupo.
// Compatibilidad: si solo llega agent_id se interpreta como target_type=agent.
type AssignmentRequest struct {
	AgentID    string `json:"agent_id,omitempty"`
	TargetType string `json:"target_type,omitempty"`
	TargetID   string `json:"target_id,omitempty"`
	Enabled    *bool  `json:"enabled,omitempty"`
}

// AssignmentView es una asignación plan → agente o grupo.
type AssignmentView struct {
	ID         string    `json:"id"`
	PlanID     string    `json:"plan_id"`
	TargetType string    `json:"target_type"`
	TargetID   string    `json:"target_id"`
	TargetName string    `json:"target_name,omitempty"`
	AgentID    string    `json:"agent_id,omitempty"` // compatibilidad: igual a target_id si target_type=agent
	Enabled    bool      `json:"enabled"`
	CreatedAt  time.Time `json:"created_at"`
}

// AssignmentList es la respuesta de GET /admin/backup-plans/{id}/assignments.
type AssignmentList struct {
	Items []AssignmentView `json:"items"`
}

// EffectivePlan es un plan resuelto para un agente.
type EffectivePlan struct {
	Plan         PlanView   `json:"plan"`
	Via          string     `json:"via"` // agent | group
	AssignmentID string     `json:"assignment_id"`
	GroupID      string     `json:"group_id,omitempty"`
	GroupName    string     `json:"group_name,omitempty"`
	ShadowedBy   []string   `json:"shadowed,omitempty"` // asignaciones de grupo del mismo plan que quedan anuladas
	NextRunAt    *time.Time `json:"next_run_at,omitempty"`
	LastRunAt    *time.Time `json:"last_run_at,omitempty"`
}

// EffectivePlanList es la respuesta de GET /admin/agents/{id}/effective-plans.
type EffectivePlanList struct {
	Items []EffectivePlan `json:"items"`
}

// BackupRequest lanza un backup bajo demanda.
type BackupRequest struct {
	PlanID string `json:"plan_id,omitempty"`
}

// BackupResponse devuelve el job creado.
type BackupResponse struct {
	JobID string `json:"job_id"`
	State string `json:"state"`
}

// JobView es un job.
type JobView struct {
	ID                string          `json:"id"`
	Type              string          `json:"type"`
	State             string          `json:"state"`
	AgentID           string          `json:"agent_id"`
	PlanID            string          `json:"plan_id,omitempty"`
	RestorePointID    string          `json:"restore_point_id,omitempty"`
	Trigger           string          `json:"trigger,omitempty"` // manual | schedule | retry | api
	Progress          int             `json:"progress"`
	Message           string          `json:"message,omitempty"`
	BytesDone         int64           `json:"bytes_done,omitempty"`
	BytesTotal        int64           `json:"bytes_total,omitempty"`
	Payload           json.RawMessage `json:"payload,omitempty"`
	Result            json.RawMessage `json:"result,omitempty"`
	ErrorCode         string          `json:"error_code,omitempty"`
	ErrorMessage      string          `json:"error_message,omitempty"`
	Attempts          int             `json:"attempts"`
	MaxAttempts       int             `json:"max_attempts"`
	RetryOf           string          `json:"retry_of,omitempty"`
	CreatedBy         string          `json:"created_by,omitempty"`
	CreatedAt         time.Time       `json:"created_at"`
	UpdatedAt         time.Time       `json:"updated_at"`
	StartedAt         *time.Time      `json:"started_at,omitempty"`
	FinishedAt        *time.Time      `json:"finished_at,omitempty"`
	LeaseUntil        *time.Time      `json:"lease_until,omitempty"`
	CancelRequestedAt *time.Time      `json:"cancel_requested_at,omitempty"`
}

// JobList es la respuesta de GET /api/v1/admin/jobs.
type JobList struct {
	Items      []JobView `json:"items"`
	NextCursor string    `json:"next_cursor,omitempty"`
}

// JobEvent es un evento de job (estado, progreso o log).
type JobEvent struct {
	Seq     int       `json:"seq"`
	JobID   string    `json:"job_id"`
	At      time.Time `json:"at"`
	Kind    string    `json:"kind"` // state | progress | log
	State   string    `json:"state,omitempty"`
	Percent int       `json:"percent,omitempty"`
	Level   string    `json:"level,omitempty"`
	Message string    `json:"message,omitempty"`
}

// JobEventList es la respuesta JSON de GET /admin/jobs/{id}/events.
type JobEventList struct {
	Items []JobEvent `json:"items"`
}

// JobLeaseResponse es la respuesta de POST /api/v1/agent/jobs/lease.
type JobLeaseResponse struct {
	Job *JobView `json:"job,omitempty"`
}

// JobProgressRequest actualiza el progreso.
type JobProgressRequest struct {
	Progress   int    `json:"progress"`
	Message    string `json:"message,omitempty"`
	BytesDone  int64  `json:"bytes_done,omitempty"`
	BytesTotal int64  `json:"bytes_total,omitempty"`
}

// JobProgressResponse informa al agente de una cancelación pendiente.
type JobProgressResponse struct {
	CancelRequested bool `json:"cancel_requested"`
}

// JobCancelStatus es la respuesta de GET /api/v1/agent/jobs/{id}/cancel.
type JobCancelStatus struct {
	CancelRequested bool `json:"cancel_requested"`
}

// JobLogLine es una línea de log enviada por el agente.
type JobLogLine struct {
	At      time.Time `json:"at"`
	Level   string    `json:"level"`
	Message string    `json:"message"`
}

// JobLogRequest es el cuerpo de POST /api/v1/agent/jobs/{id}/log.
type JobLogRequest struct {
	Lines []JobLogLine `json:"lines"`
}

// JobCompleteRequest cierra un job. state ∈ succeeded | failed | canceled.
type JobCompleteRequest struct {
	State        string          `json:"state"`
	ErrorCode    string          `json:"error_code,omitempty"`
	ErrorMessage string          `json:"error_message,omitempty"`
	Result       json.RawMessage `json:"result,omitempty"`
}

// BackupJobPayload es el payload de un job BACKUP_FILE.
type BackupJobPayload struct {
	PlanID    string     `json:"plan_id"`
	PlanName  string     `json:"plan_name,omitempty"`
	Paths     []string   `json:"paths"`
	Excludes  []string   `json:"excludes,omitempty"`
	Retention *Retention `json:"retention,omitempty"`
	// StorageTargetID: si no está vacío, el agente escribe en el repositorio
	// Kopia S3 de ese destino (creds vía /agent/repository-credentials?target=).
	StorageTargetID string `json:"storage_target_id,omitempty"`
}

// ImageJobPayload es el payload de un job BACKUP_IMAGE: captura íntegra de
// los discos del equipo (tabla de particiones, ESP, sistema y datos) para
// despliegue bare-metal en otro equipo.
type ImageJobPayload struct {
	PlanID   string `json:"plan_id"`
	PlanName string `json:"plan_name,omitempty"`
	// Disks filtra índices de disco concretos; vacío = todos los discos fijos.
	Disks []int `json:"disks,omitempty"`
	// StorageTargetID: repositorio S3 destino (vacío = repo central).
	StorageTargetID string `json:"storage_target_id,omitempty"`
	// RetentionDays aplica Object Lock GOVERNANCE a los segmentos subidos
	// (0 = sin lock); lo rellena el servidor desde el destino.
	RetentionDays int `json:"retention_days,omitempty"`
}

// BackupVMJobPayload es el payload de un job BACKUP_VM: captura de los
// discos de una VM de Proxmox vía qemu-nbd en el nodo PVE. Lo ejecuta el
// agente instalado en el propio nodo.
type BackupVMJobPayload struct {
	InstanceID string `json:"instance_id"`
	Node       string `json:"node,omitempty"`
	VMID       int    `json:"vmid"`
	Name       string `json:"name,omitempty"`
	// StorageTargetID: bucket destino de los segmentos y del manifiesto.
	StorageTargetID string `json:"storage_target_id"`
	RetentionDays   int    `json:"retention_days,omitempty"`
}

// RestoreVMJobPayload es el payload de un job RESTORE_VM: repone los discos
// de una VM desde un manifiesto de imagen almacenado en el bucket del
// destino. Mode "in_place" reescribe los volúmenes de la VM indicada
// (deteniéndola y arrancándola); "new" crea una VM nueva con la
// configuración del manifiesto (cores, RAM, BIOS, red, discos) y escribe los
// datos en sus discos.
type RestoreVMJobPayload struct {
	InstanceID string `json:"instance_id"`
	Node       string `json:"node,omitempty"`
	// ManifestKey es el objeto del manifiesto en el bucket del destino.
	ManifestKey     string `json:"manifest_key"`
	StorageTargetID string `json:"storage_target_id"`
	Mode            string `json:"mode"` // in_place | new
	TargetVMID      int    `json:"target_vmid"`
	TargetName      string `json:"target_name,omitempty"`
	TargetStorage   string `json:"target_storage,omitempty"` // p. ej. local-lvm
	RetentionDays   int    `json:"retention_days,omitempty"`
}

// BackupLXCJobPayload es el payload de un job BACKUP_LXC: archiva un
// contenedor LXC con vzdump (snapshot o stop) en el nodo PVE y sube el
// archivo por segmentos al destino de almacenamiento. Lo ejecuta el agente
// instalado en el propio nodo.
type BackupLXCJobPayload struct {
	InstanceID string `json:"instance_id"`
	Node       string `json:"node,omitempty"`
	VMID       int    `json:"vmid"`
	Name       string `json:"name,omitempty"`
	// StorageTargetID: destino de los segmentos del archivo y del manifiesto.
	StorageTargetID string `json:"storage_target_id"`
	RetentionDays   int    `json:"retention_days,omitempty"`
}

// RestoreLXCJobPayload es el payload de un job RESTORE_LXC: descarga el
// archivo vzdump del destino, lo verifica (SHA-256 por segmento) y repone el
// contenedor con `pct restore`. Mode "in_place" destruye y restaura el CT
// con la misma VMID; "new" crea un contenedor nuevo.
type RestoreLXCJobPayload struct {
	InstanceID string `json:"instance_id"`
	Node       string `json:"node,omitempty"`
	// ManifestKey es el objeto del manifiesto en el destino.
	ManifestKey     string `json:"manifest_key"`
	StorageTargetID string `json:"storage_target_id"`
	Mode            string `json:"mode"` // in_place | new
	TargetVMID      int    `json:"target_vmid"`
	TargetName      string `json:"target_name,omitempty"`
	TargetStorage   string `json:"target_storage,omitempty"` // p. ej. local-lvm
	RetentionDays   int    `json:"retention_days,omitempty"`
}

// --- SQL Server (motor bdr-sql, RF-40/A-11) ---

// SQLBackupJobPayload es el payload de un job SQL_BACKUP: backup nativo
// (T-SQL BACKUP DATABASE/LOG) de una base de SQL Server en el host del
// agente con la identidad de su cuenta de servicio (SSPI), transferencia
// del fichero al destino de almacenamiento y registro en el catálogo.
type SQLBackupJobPayload struct {
	Server   string `json:"server"`             // host o host\instancia
	Instance string `json:"instance,omitempty"` // instancia con nombre
	Database string `json:"database"`
	Type     string `json:"type"` // FULL | DIFFERENTIAL | LOG | COPY_ONLY
	// StorageTargetID: destino de los ficheros de backup (filesystem/S3).
	// Los destinos Kopia server no soportan SQL directo.
	StorageTargetID string `json:"storage_target_id"`
	RetentionDays   int    `json:"retention_days,omitempty"` // Object Lock GOVERNANCE (S3)
	Verify          *bool  `json:"verify,omitempty"`         // nil = verificar (por defecto)
	// Encryption: cifrado nativo WITH ENCRYPTION (RF-45). El certificado
	// DEBE tener custodia (SQL_CERTBACKUP) o el medio es irrecuperable.
	Encryption *SQLEncryption `json:"encryption,omitempty"`
}

// SQLEncryption es el cifrado nativo de un backup SQL (RF-45).
type SQLEncryption struct {
	Algorithm string `json:"algorithm,omitempty"` // AES_128|AES_192|AES_256 (vacío = AES_256)
	CertName  string `json:"cert_name"`
}

// SQLRestoreJobPayload es el payload de un job SQL_RESTORE: descarga la
// cadena de ficheros del destino al staging del host y restaura la base
// con la secuencia nativa (NORECOVERY salvo el último, MOVE, STOPAT).
type SQLRestoreJobPayload struct {
	Server   string `json:"server"`
	Instance string `json:"instance,omitempty"`
	Database string `json:"database"` // base destino
	// Files: URIs de repositorio en orden (full, diff, logs...), tal como
	// las devuelve el catálogo (s3://bucket/clave, \\host\recurso\ruta,
	// ruta local).
	Files    []string `json:"files"`
	Mode     string   `json:"mode,omitempty"`    // "" | NORECOVERY | RECOVERY
	StopAt   string   `json:"stop_at,omitempty"` // RFC3339 (PITR, solo final)
	MoveData string   `json:"move_data,omitempty"`
	MoveLog  string   `json:"move_log,omitempty"`
	// StorageTargetID: de dónde descargar los ficheros.
	StorageTargetID string `json:"storage_target_id"`
}

// SQLVerifyJobPayload es el payload de un job SQL_VERIFY: descarga un
// fichero del destino y ejecuta RESTORE VERIFYONLY WITH CHECKSUM contra
// la instancia local (RF-44, verificación programada).
type SQLVerifyJobPayload struct {
	Server          string `json:"server"`
	Instance        string `json:"instance,omitempty"`
	RepoURI         string `json:"repo_uri"`
	StorageTargetID string `json:"storage_target_id"`
}

// SQLInventoryJobPayload es el payload de un job SQL_INVENTORY: inventario
// de bases de la instancia (rápido, sin backup).
type SQLInventoryJobPayload struct {
	Server   string `json:"server"`
	Instance string `json:"instance,omitempty"`
}

// SQLCheckDBJobPayload es el payload de un job SQL_CHECKDB: ejecuta
// DBCC CHECKDB (WITH NO_INFOMSGS, ALL_ERRORMSGS) sobre una base en vivo
// (RF-44). Con corrupción, el job FALLA con los mensajes de DBCC y el
// control plane levanta la alerta por el camino genérico de jobs fallidos.
type SQLCheckDBJobPayload struct {
	Server   string `json:"server"`
	Instance string `json:"instance,omitempty"`
	Database string `json:"database"`
}

// SQLCertBackupJobPayload es el payload de un job SQL_CERTBACKUP: custodia
// de un certificado de master (RF-45) — BACKUP CERTIFICATE con su clave
// privada al destino de almacenamiento. Sin custodia, los backups cifrados
// con ese certificado son irrecuperables.
type SQLCertBackupJobPayload struct {
	Server   string `json:"server"`
	Instance string `json:"instance,omitempty"`
	CertName string `json:"cert_name"`
	// CertPassword cifra el fichero .pvk de la clave privada. Lo genera y
	// custodia sellado el control plane; viaja al agente por el canal mTLS.
	CertPassword string `json:"cert_password"`
	// CertDecryptionPassword (opcional): solo si la clave privada del
	// certificado está protegida por contraseña en master (cuando fue
	// creada FROM FILE con ENCRYPTION BY PASSWORD). Vacío = la clave está
	// protegida por la master key.
	CertDecryptionPassword string `json:"cert_decryption_password,omitempty"`
	StorageTargetID        string `json:"storage_target_id"`
}

// SQLCertBackupJobResult es el resultado de un job SQL_CERTBACKUP.
type SQLCertBackupJobResult struct {
	CertName    string  `json:"cert_name"`
	CertURI     string  `json:"cert_uri"` // .cer en el destino
	PvkURI      string  `json:"pvk_uri"`  // .pvk (clave privada) en el destino
	DurationSec float64 `json:"duration_sec"`
}

// SQLBackupJobResult es el resultado de un job SQL_BACKUP. Los LSN viajan
// como texto (numeric(25,0) de msdb no cabe garantizado en un JSON number
// de los clientes JS).
type SQLBackupJobResult struct {
	Database     string  `json:"database"`
	Type         string  `json:"type"`
	RepoURI      string  `json:"repo_uri"`
	SizeBytes    int64   `json:"size_bytes"`
	DurationSec  float64 `json:"duration_sec"`
	Checksum     bool    `json:"checksum"`
	Compression  bool    `json:"compression"`
	Verified     bool    `json:"verified"`
	Encrypted    bool    `json:"encrypted"`           // WITH ENCRYPTION aplicado
	CertName     string  `json:"cert_name,omitempty"` // certificado usado (si cifrado)
	DatabaseLSN  string  `json:"database_lsn,omitempty"`
	FirstLSN     string  `json:"first_lsn,omitempty"`
	LastLSN      string  `json:"last_lsn,omitempty"`
	BackupStart  string  `json:"backup_start,omitempty"`  // msdb (UTC ISO)
	BackupFinish string  `json:"backup_finish,omitempty"` // msdb (UTC ISO)
}

// SQLRestoreJobResult es el resultado de un job SQL_RESTORE.
type SQLRestoreJobResult struct {
	Database    string   `json:"database"`
	Files       []string `json:"files"`
	Mode        string   `json:"mode"`
	DurationSec float64  `json:"duration_sec"`
}

// SQLVerifyJobResult es el resultado de un job SQL_VERIFY.
type SQLVerifyJobResult struct {
	RepoURI     string  `json:"repo_uri"`
	OK          bool    `json:"ok"`
	DurationSec float64 `json:"duration_sec"`
}

// SQLInventoryJobResult es el resultado de un job SQL_INVENTORY.
type SQLInventoryJobResult struct {
	Items []SQLDatabaseView `json:"items"`
}

// SQLCheckDBJobResult es el resultado de un job SQL_CHECKDB limpio
// (sin corrupción; con corrupción el job falla y lleva los mensajes).
type SQLCheckDBJobResult struct {
	Database    string  `json:"database"`
	DurationSec float64 `json:"duration_sec"`
}

// SQLDatabaseView describe una base de datos de la instancia (panel §23).
type SQLDatabaseView struct {
	Name          string `json:"name"`
	RecoveryModel string `json:"recovery_model"`
	State         string `json:"state"`
	ReadOnly      bool   `json:"read_only"`
	CompatLevel   int    `json:"compatibility_level"`
	System        bool   `json:"is_system"`
	Protectable   bool   `json:"protectable"`
}

// --- PostgreSQL (motor pg_dump/pg_restore, RF-41 extendido) ---
//
// Las contraseñas NO viajan en los payloads: el agente usa la variable
// BDR_PG_PASSWORD de su propio entorno (o .pgpass del host), igual que el
// motor SQL usa SSPI.

// PGDumpJobPayload es el payload de un job PG_DUMP: pg_dump -Fc de una
// base (formato custom, comprimido) en el host del agente, transferencia
// al destino de almacenamiento y registro en el catálogo.
type PGDumpJobPayload struct {
	Host     string `json:"host"`           // host o vacío = socket local
	Port     string `json:"port,omitempty"` // vacío = 5432
	User     string `json:"user,omitempty"` // vacío = usuario del proceso
	Database string `json:"database"`
	// StorageTargetID: destino de los ficheros (filesystem/S3).
	StorageTargetID string `json:"storage_target_id"`
	RetentionDays   int    `json:"retention_days,omitempty"` // Object Lock (S3)
}

// PGDumpJobResult es el resultado de un job PG_DUMP.
type PGDumpJobResult struct {
	Database    string  `json:"database"`
	RepoURI     string  `json:"repo_uri"`
	SizeBytes   int64   `json:"size_bytes"`
	DurationSec float64 `json:"duration_sec"`
	Verified    bool    `json:"verified"` // pg_restore --list OK
}

// PGVerifyJobPayload es el payload de un job PG_VERIFY: descarga el dump
// del destino y ejecuta pg_restore --list (integridad del archivo).
type PGVerifyJobPayload struct {
	RepoURI         string `json:"repo_uri"`
	StorageTargetID string `json:"storage_target_id"`
}

// PGVerifyJobResult es el resultado de un job PG_VERIFY.
type PGVerifyJobResult struct {
	RepoURI     string  `json:"repo_uri"`
	OK          bool    `json:"ok"`
	Objects     int     `json:"objects,omitempty"` // entradas del índice del dump
	DurationSec float64 `json:"duration_sec"`
}

// PGRestoreJobPayload es el payload de un job PG_RESTORE: descarga el dump
// y lo restaura con pg_restore. CreateDB ejecuta createdb antes (motor
// limpio); DropExisting pasa --clean --if-exists (reemplazo destructivo).
type PGRestoreJobPayload struct {
	Host            string `json:"host"`
	Port            string `json:"port,omitempty"`
	User            string `json:"user,omitempty"`
	Database        string `json:"database"` // base destino
	CreateDB        bool   `json:"create_db,omitempty"`
	DropExisting    bool   `json:"drop_existing,omitempty"`
	RepoURI         string `json:"repo_uri"`
	StorageTargetID string `json:"storage_target_id"`
}

// PGRestoreJobResult es el resultado de un job PG_RESTORE.
type PGRestoreJobResult struct {
	Database    string  `json:"database"`
	RepoURI     string  `json:"repo_uri"`
	DurationSec float64 `json:"duration_sec"`
}

// PGInventoryJobPayload es el payload de un job PG_INVENTORY: lista de
// bases de la instancia (rápido, sin backup).
type PGInventoryJobPayload struct {
	Host string `json:"host"`
	Port string `json:"port,omitempty"`
	User string `json:"user,omitempty"`
}

// PGInventoryJobResult es el resultado de un job PG_INVENTORY.
type PGInventoryJobResult struct {
	Items []PGDatabaseView `json:"items"`
}

// PGDatabaseView describe una base de datos PostgreSQL (panel).
type PGDatabaseView struct {
	Name     string `json:"name"`
	Owner    string `json:"owner,omitempty"`
	System   bool   `json:"is_system"` // template0/template1/postgres
	Connect  bool   `json:"connect"`   // datallowconn (template0 no acepta)
	Dumpable bool   `json:"dumpable"`  // candidata a pg_dump
}

// SQLDiscoverJobPayload es el payload de un job SQL_DISCOVER: enumera las
// instancias de SQL Server instaladas en el host del agente (solo Windows).
type SQLDiscoverJobPayload struct{}

// SQLInstanceView describe una instancia detectada (panel §23).
type SQLInstanceView struct {
	Name    string `json:"name"` // MSSQLSERVER (por defecto) o SQLEXPRESS…
	Default bool   `json:"default"`
	Version string `json:"version,omitempty"`
	Edition string `json:"edition,omitempty"`
}

// SQLDiscoverJobResult es el resultado de un job SQL_DISCOVER.
type SQLDiscoverJobResult struct {
	Items []SQLInstanceView `json:"items"`
}

// PGDiscoverJobPayload es el payload de un job PG_DISCOVER: detecta las
// instancias locales de PostgreSQL (servicios/clústers del host).
type PGDiscoverJobPayload struct{}

// PGInstanceView describe una instancia PostgreSQL detectada (panel).
type PGInstanceView struct {
	Port    string `json:"port,omitempty"`
	Service string `json:"service,omitempty"` // unidad/servicio detectado
	Version string `json:"version,omitempty"`
}

// PGDiscoverJobResult es el resultado de un job PG_DISCOVER.
type PGDiscoverJobResult struct {
	Items []PGInstanceView `json:"items"`
}

// SQLRestoreChainStep es un eslabón de la cadena de restauración
// calculada desde el catálogo (RF-42). TargetID: destino de almacenamiento
// donde vive el objeto (el restore lo necesita para descargar).
type SQLRestoreChainStep struct {
	RestorePointID string    `json:"restore_point_id"`
	JobID          string    `json:"job_id,omitempty"`
	BackupType     string    `json:"backup_type"` // FULL | DIFFERENTIAL | LOG
	BackupFinish   time.Time `json:"backup_finish"`
	FirstLSN       string    `json:"first_lsn,omitempty"`
	LastLSN        string    `json:"last_lsn,omitempty"`
	RepoURI        string    `json:"repo_uri"`
	SizeBytes      int64     `json:"size_bytes,omitempty"`
	TargetID       string    `json:"target_id,omitempty"`
}

// SQLRestoreChainView es la cadena calculada para restaurar una base
// hasta un punto en el tiempo (o el más reciente). Valid=false con Error
// cuando no hay cadena completa (p. ej. falta un FULL).
type SQLRestoreChainView struct {
	Database string                `json:"database"`
	Valid    bool                  `json:"valid"`
	Error    string                `json:"error,omitempty"`
	StopAt   *time.Time            `json:"stop_at,omitempty"`
	Files    []string              `json:"files,omitempty"` // URIs en orden de aplicación
	Steps    []SQLRestoreChainStep `json:"steps,omitempty"`
}

// SQLPlanRequest crea o actualiza un plan programado de backup SQL.
type SQLPlanRequest struct {
	AgentID         string `json:"agent_id"` // agente del host SQL
	Server          string `json:"server"`   // host o host\instancia
	Instance        string `json:"instance,omitempty"`
	Database        string `json:"database"`
	Type            string `json:"type,omitempty"` // FULL (default) | DIFF | LOG | COPY_ONLY
	Schedule        string `json:"schedule"`       // cron
	Timezone        string `json:"timezone,omitempty"`
	StorageTargetID string `json:"storage_target_id"`
	RetentionDays   int    `json:"retention_days,omitempty"` // 0 = el del destino
	Verify          *bool  `json:"verify,omitempty"`         // nil = verificar
	Enabled         *bool  `json:"enabled,omitempty"`        // nil = true
	// VerifyCron: verificación programada (RF-44) del último backup del
	// plan (RESTORE VERIFYONLY sobre su objeto en el destino). Vacío = solo
	// la verificación post-backup.
	VerifyCron string `json:"verify_cron,omitempty"`
	// EncryptCert: certificado de master para cifrar los backups del plan
	// (RF-45). Vacío = sin cifrar. El certificado debe tener custodia.
	EncryptCert string `json:"encrypt_cert,omitempty"`
}

// SQLPlanView es la vista de un plan SQL para la API y el panel.
type SQLPlanView struct {
	ID              string     `json:"id"`
	AgentID         string     `json:"agent_id"`
	Server          string     `json:"server"`
	Instance        string     `json:"instance,omitempty"`
	Database        string     `json:"database"`
	Type            string     `json:"type"`
	Schedule        string     `json:"schedule"`
	Timezone        string     `json:"timezone,omitempty"`
	StorageTargetID string     `json:"storage_target_id"`
	RetentionDays   int        `json:"retention_days,omitempty"`
	Verify          bool       `json:"verify"`
	VerifyCron      string     `json:"verify_cron,omitempty"`
	EncryptCert     string     `json:"encrypt_cert,omitempty"`
	Enabled         bool       `json:"enabled"`
	LastJobID       string     `json:"last_job_id,omitempty"`
	NextRunAt       *time.Time `json:"next_run_at,omitempty"`
	NextVerifyAt    *time.Time `json:"next_verify_at,omitempty"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
}

// ImageJobResult es el resultado de un job BACKUP_IMAGE.
type ImageJobResult struct {
	ManifestID      string            `json:"manifest_id"`
	ManifestKey     string            `json:"manifest_key,omitempty"`
	KopiaSnapshotID string            `json:"kopia_snapshot_id,omitempty"`
	Disks           []ImageDiskResult `json:"disks"`
	BytesTotal      int64             `json:"bytes_total"`
	// BytesUploaded es lo realmente transferido al destino: los segmentos
	// a cero no se suben y los runs dispersos se suben a tamaño real.
	BytesUploaded int64 `json:"bytes_uploaded,omitempty"`
	DurationMS    int64 `json:"duration_ms"`
}

// ImageDiskResult resume la captura de un disco.
type ImageDiskResult struct {
	Index      int    `json:"index"`
	Device     string `json:"device"`
	Model      string `json:"model,omitempty"`
	Table      string `json:"table,omitempty"`
	TotalBytes int64  `json:"total_bytes"`
	Segments   int    `json:"segments"`
}

// SnapshotSummary describe un snapshot de Kopia creado por el agente.
type SnapshotSummary struct {
	Path          string    `json:"path"`
	ID            string    `json:"id"`
	RootObjectID  string    `json:"root_object_id,omitempty"`
	SourceHost    string    `json:"source_host,omitempty"`
	SourceUser    string    `json:"source_user,omitempty"`
	StartTime     time.Time `json:"start_time,omitempty"`
	EndTime       time.Time `json:"end_time,omitempty"`
	SizeBytes     int64     `json:"size_bytes"`
	Files         int64     `json:"files"`
	Dirs          int64     `json:"dirs"`
	ErrorCount    int       `json:"error_count"`
	ExcludedFiles int64     `json:"excluded_files,omitempty"`
	ExcludedDirs  int64     `json:"excluded_dirs,omitempty"`
}

// BackupJobResult es el resultado de un job BACKUP_FILE.
type BackupJobResult struct {
	Snapshots     []SnapshotSummary `json:"snapshots"`
	BytesTotal    int64             `json:"bytes_total"`
	DurationMS    int64             `json:"duration_ms"`
	KopiaVersion  string            `json:"kopia_version,omitempty"`
	RetentionNote string            `json:"retention_note,omitempty"`
}

// RestoreJobPayload es el payload de un job RESTORE_FILE.
//
// Items son rutas relativas dentro del snapshot (separador "/"); vacío
// restaura todo. Destination es una ruta absoluta en el equipo destino; si
// InPlace es true se restaura sobre SourcePath y Destination se ignora.
type RestoreJobPayload struct {
	RestorePointID string   `json:"restore_point_id"`
	SnapshotID     string   `json:"snapshot_id"`
	RootObjectID   string   `json:"root_object_id,omitempty"`
	SourcePath     string   `json:"source_path"`
	Items          []string `json:"items,omitempty"`
	Destination    string   `json:"destination,omitempty"`
	InPlace        bool     `json:"in_place,omitempty"`
	Overwrite      bool     `json:"overwrite"`
}

// RestoredItem describe un elemento restaurado.
type RestoredItem struct {
	Item        string `json:"item"`
	Destination string `json:"destination"`
	Files       int64  `json:"files"`
	Dirs        int64  `json:"dirs"`
	Bytes       int64  `json:"bytes"`
}

// RestoreJobResult es el resultado de un job RESTORE_FILE.
type RestoreJobResult struct {
	Restored   []RestoredItem `json:"restored"`
	DurationMS int64          `json:"duration_ms"`
}

// --- Repositorio ---

// Tipos de repositorio Kopia.
const (
	RepoKindServer     = "server"     // Kopia Repository Server con usuario por agente
	RepoKindFilesystem = "filesystem" // ruta local/NAS compartida (laboratorio)
	RepoKindS3         = "s3"         // repositorio Kopia directo sobre destino S3
)

// RepositoryCredentials es la respuesta de GET /api/v1/agent/repository-credentials.
type RepositoryCredentials struct {
	RepositoryID string `json:"repository_id"`
	Kind         string `json:"kind"`
	// server
	URL         string `json:"url,omitempty"`
	Fingerprint string `json:"server_cert_fingerprint,omitempty"`
	Username    string `json:"username,omitempty"`
	Hostname    string `json:"hostname,omitempty"`
	// filesystem
	Path string `json:"path,omitempty"`
	// s3 (repositorio Kopia directo sobre un destino de almacenamiento)
	Endpoint  string `json:"endpoint,omitempty"`
	Bucket    string `json:"bucket,omitempty"`
	Prefix    string `json:"prefix,omitempty"`
	Region    string `json:"region,omitempty"`
	AccessKey string `json:"access_key,omitempty"`
	SecretKey string `json:"secret_key,omitempty"`
	NoTLS     bool   `json:"no_tls,omitempty"`
	// SMB (destino filesystem con ruta //servidor/recurso): credenciales
	// para el cliente SMB (usuario, contraseña y dominio AD/workgroup).
	SMBUser     string `json:"smb_user,omitempty"`
	SMBPassword string `json:"smb_password,omitempty"`
	SMBDomain   string `json:"smb_domain,omitempty"`
	// Password es la contraseña de usuario (server), del repositorio
	// (filesystem) o del repositorio S3.
	Password  string    `json:"password"`
	Version   int       `json:"version"`
	IssuedAt  time.Time `json:"issued_at"`
	ExpiresAt time.Time `json:"expires_at"`
}

// RepoCredsView es el estado de la credencial de repositorio de un agente
// (sin la contraseña). PendingApply indica que la versión actual aún no pudo
// aplicarse en el repositorio y se reintentará al entregar credenciales.
type RepoCredsView struct {
	AgentID      string    `json:"agent_id"`
	Username     string    `json:"username"`
	Hostname     string    `json:"hostname"`
	Version      int       `json:"version"`
	RotatedAt    time.Time `json:"rotated_at"`
	PendingApply bool      `json:"pending_apply,omitempty"`
}

// AgentPlansResponse es la respuesta de GET /api/v1/agent/plans.
type AgentPlansResponse struct {
	Items []PlanView `json:"items"`
}

// Package store define el almacenamiento del plano de control.
//
// Diseño (0.4): un documento de estado tipado (State) con transacciones
// View/Update serializadas. Memory lo mantiene en RAM; File lo persiste en
// state.json con escritura atómica tras cada Update. La auditoría va aparte,
// en un registro append-only encadenado por hash (AuditLog).
//
// Este modelo es adecuado para un solo nodo y decenas/cientos de equipos. La
// migración a PostgreSQL (docs/05-modelo-datos.md) sustituirá esta capa: cada
// colección de State corresponde a una tabla.
package store

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"time"

	"github.com/juansoler/bdr/packages/contracts"
)

// Errores de dominio del almacenamiento.
var (
	ErrNotFound      = errors.New("not found")
	ErrTokenUsed     = errors.New("enrollment token already used")
	ErrTokenExpired  = errors.New("enrollment token expired")
	ErrTokenHostname = errors.New("enrollment token hostname mismatch")
	ErrTokenRevoked  = errors.New("enrollment token revoked")
	ErrConflict      = errors.New("conflict")
)

// SchemaVersion es la versión actual de state.json.
const SchemaVersion = 2

// EnrollmentToken es un token de un solo uso. Solo se persiste su hash.
type EnrollmentToken struct {
	ID               string     `json:"ID"`
	TokenHash        string     `json:"TokenHash"`
	ExpectedHostname string     `json:"ExpectedHostname"`
	CreatedAt        time.Time  `json:"CreatedAt"`
	ExpiresAt        time.Time  `json:"ExpiresAt"`
	UsedAt           *time.Time `json:"UsedAt"`
	UsedByAgentID    string     `json:"UsedByAgentID"`
	RevokedAt        *time.Time `json:"RevokedAt,omitempty"`
	GroupID          string     `json:"GroupID,omitempty"`
	PlanID           string     `json:"PlanID,omitempty"`
	Note             string     `json:"Note,omitempty"`
	CreatedBy        string     `json:"CreatedBy,omitempty"`
}

// Agent es un equipo registrado. Solo se persiste el hash de su API key.
type Agent struct {
	ID            string     `json:"ID"`
	Hostname      string     `json:"Hostname"`
	OSFamily      string     `json:"OSFamily"`
	OSVersion     string     `json:"OSVersion"`
	Arch          string     `json:"Arch"`
	AgentVersion  string     `json:"AgentVersion"`
	Status        string     `json:"Status"`
	ReportedState string     `json:"ReportedState,omitempty"`
	CurrentJobID  string     `json:"CurrentJobID,omitempty"`
	EnrolledAt    time.Time  `json:"EnrolledAt"`
	LastSeenAt    *time.Time `json:"LastSeenAt"`
	RevokedAt     *time.Time `json:"RevokedAt"`
	RevokedReason string     `json:"RevokedReason,omitempty"`
	APIKeyHash    string     `json:"APIKeyHash"`
	Inventory     []byte     `json:"Inventory"`
	InventoryHash string     `json:"InventoryHash"`
	CertSerial    string     `json:"CertSerial,omitempty"`
	CertNotAfter  *time.Time `json:"CertNotAfter,omitempty"`
	// UpgradeRequestedAt marca la solicitud del operador de forzar la
	// actualización del agente (POST /admin/agents/{id}/upgrade); se limpia
	// al entregar la oferta por heartbeat.
	UpgradeRequestedAt *time.Time `json:"UpgradeRequestedAt,omitempty"`
}

// Plan es un plan de protección.
type Plan struct {
	ID          string   `json:"ID"`
	Name        string   `json:"Name"`
	Description string   `json:"Description,omitempty"`
	Kind        string   `json:"Kind"`
	Paths       []string `json:"Paths"`
	Excludes    []string `json:"Excludes"`
	// Disks (planes de imagen): índices de disco a capturar; vacío = todos
	// los discos fijos.
	Disks     []int               `json:"Disks"`
	Enabled   bool                `json:"Enabled"`
	Schedule  string              `json:"Schedule,omitempty"`
	Timezone  string              `json:"Timezone,omitempty"`
	Window    *contracts.Window   `json:"Window,omitempty"`
	Retention contracts.Retention `json:"Retention"`
	RPOHours  int                 `json:"RPOHours,omitempty"`
	// StorageTargetID: destino S3 donde el agente escribe este plan
	// (repositorio Kopia dedicado); vacío = repositorio central.
	StorageTargetID string    `json:"StorageTargetID,omitempty"`
	CreatedAt       time.Time `json:"CreatedAt"`
	UpdatedAt       time.Time `json:"UpdatedAt"`
}

// Assignment relaciona un plan con un agente o un grupo.
type Assignment struct {
	ID         string    `json:"ID"`
	PlanID     string    `json:"PlanID"`
	TargetType string    `json:"TargetType"`
	TargetID   string    `json:"TargetID"`
	AgentID    string    `json:"AgentID,omitempty"` // solo para leer estados 0.2/0.3
	Enabled    bool      `json:"Enabled"`
	CreatedAt  time.Time `json:"CreatedAt"`
}

// Group es un grupo de equipos con jerarquía opcional.
type Group struct {
	ID          string    `json:"ID"`
	Name        string    `json:"Name"`
	Description string    `json:"Description,omitempty"`
	ParentID    string    `json:"ParentID,omitempty"`
	MemberIDs   []string  `json:"MemberIDs"`
	CreatedAt   time.Time `json:"CreatedAt"`
	UpdatedAt   time.Time `json:"UpdatedAt"`
}

// Estados de un job (alias de contracts).
const (
	JobPending   = contracts.JobPending
	JobLeased    = contracts.JobLeased
	JobRunning   = contracts.JobRunning
	JobSucceeded = contracts.JobSucceeded
	JobFailed    = contracts.JobFailed
	JobCanceled  = contracts.JobCanceled
	JobExpired   = contracts.JobExpired
)

// Job es una unidad de trabajo asignada a un agente.
type Job struct {
	ID                string     `json:"ID"`
	Type              string     `json:"Type"`
	State             string     `json:"State"`
	AgentID           string     `json:"AgentID"`
	PlanID            string     `json:"PlanID"`
	RestorePointID    string     `json:"RestorePointID,omitempty"`
	Trigger           string     `json:"Trigger,omitempty"`
	Payload           []byte     `json:"Payload"`
	Result            []byte     `json:"Result"`
	Progress          int        `json:"Progress"`
	Message           string     `json:"Message,omitempty"`
	BytesDone         int64      `json:"BytesDone,omitempty"`
	BytesTotal        int64      `json:"BytesTotal,omitempty"`
	ErrorCode         string     `json:"ErrorCode"`
	ErrorMessage      string     `json:"ErrorMessage"`
	Attempts          int        `json:"Attempts,omitempty"`
	MaxAttempts       int        `json:"MaxAttempts,omitempty"`
	RetryOf           string     `json:"RetryOf,omitempty"`
	CreatedBy         string     `json:"CreatedBy,omitempty"`
	IdempotencyKey    string     `json:"IdempotencyKey,omitempty"`
	CreatedAt         time.Time  `json:"CreatedAt"`
	UpdatedAt         time.Time  `json:"UpdatedAt"`
	StartedAt         *time.Time `json:"StartedAt,omitempty"`
	FinishedAt        *time.Time `json:"FinishedAt,omitempty"`
	LeaseUntil        *time.Time `json:"LeaseUntil"`
	CancelRequestedAt *time.Time `json:"CancelRequestedAt,omitempty"`
}

// RestorePoint es un snapshot recuperable de una ruta.
type RestorePoint struct {
	ID           string     `json:"ID"`
	AgentID      string     `json:"AgentID"`
	PlanID       string     `json:"PlanID,omitempty"`
	JobID        string     `json:"JobID"`
	Kind         string     `json:"Kind"`
	SnapshotID   string     `json:"SnapshotID"`
	RootObjectID string     `json:"RootObjectID,omitempty"`
	SourcePath   string     `json:"SourcePath"`
	SourceHost   string     `json:"SourceHost,omitempty"`
	SourceUser   string     `json:"SourceUser,omitempty"`
	CreatedAt    time.Time  `json:"CreatedAt"`
	StartTime    time.Time  `json:"StartTime"`
	EndTime      time.Time  `json:"EndTime"`
	SizeBytes    int64      `json:"SizeBytes"`
	Files        int64      `json:"Files"`
	Dirs         int64      `json:"Dirs"`
	ErrorCount   int        `json:"ErrorCount"`
	VerifyState  string     `json:"VerifyState"`
	VerifiedAt   *time.Time `json:"VerifiedAt,omitempty"`
	VerifyDetail string     `json:"VerifyDetail,omitempty"`
	ExpiredAt    *time.Time `json:"ExpiredAt,omitempty"`
	// TargetID: destino de almacenamiento donde vive este punto (objetos de
	// imagen/VM o repo Kopia del destino en backups de archivo); vacío en
	// puntos del repo central.
	TargetID string `json:"TargetID,omitempty"`
}

// Alert es una alerta deduplicada por Key.
type Alert struct {
	ID         string     `json:"ID"`
	Key        string     `json:"Key"`
	Kind       string     `json:"Kind"`
	Severity   string     `json:"Severity"`
	State      string     `json:"State"`
	Title      string     `json:"Title"`
	Detail     string     `json:"Detail,omitempty"`
	AgentID    string     `json:"AgentID,omitempty"`
	PlanID     string     `json:"PlanID,omitempty"`
	JobID      string     `json:"JobID,omitempty"`
	Count      int        `json:"Count"`
	FirstAt    time.Time  `json:"FirstAt"`
	LastAt     time.Time  `json:"LastAt"`
	AckBy      string     `json:"AckBy,omitempty"`
	AckAt      *time.Time `json:"AckAt,omitempty"`
	ResolvedBy string     `json:"ResolvedBy,omitempty"`
	ResolvedAt *time.Time `json:"ResolvedAt,omitempty"`
	NotifiedAt *time.Time `json:"NotifiedAt,omitempty"`
}

// User es un usuario de administración.
type User struct {
	ID                string     `json:"ID"`
	Email             string     `json:"Email"`
	Name              string     `json:"Name"`
	Role              string     `json:"Role"`
	PasswordHash      string     `json:"PasswordHash"`
	TOTPSecret        string     `json:"TOTPSecret,omitempty"` // cifrado con secrets.Box
	TOTPPending       string     `json:"TOTPPending,omitempty"`
	TOTPEnabled       bool       `json:"TOTPEnabled"`
	TOTPLastStep      int64      `json:"TOTPLastStep,omitempty"`
	RecoveryHashes    []string   `json:"RecoveryHashes,omitempty"`
	Disabled          bool       `json:"Disabled"`
	CreatedAt         time.Time  `json:"CreatedAt"`
	UpdatedAt         time.Time  `json:"UpdatedAt"`
	LastLoginAt       *time.Time `json:"LastLoginAt,omitempty"`
	PasswordChangedAt time.Time  `json:"PasswordChangedAt"`
}

// Session es una sesión de panel. Solo se guarda el hash del identificador.
type Session struct {
	IDHash      string     `json:"IDHash"`
	UserID      string     `json:"UserID"`
	CSRF        string     `json:"CSRF"`
	CreatedAt   time.Time  `json:"CreatedAt"`
	ExpiresAt   time.Time  `json:"ExpiresAt"`
	LastSeenAt  time.Time  `json:"LastSeenAt"`
	MFAPassed   bool       `json:"MFAPassed"`
	MFAFailures int        `json:"MFAFailures,omitempty"`
	StepUpUntil *time.Time `json:"StepUpUntil,omitempty"`
	IP          string     `json:"IP,omitempty"`
	UserAgent   string     `json:"UserAgent,omitempty"`
}

// APIToken es un token de integración. Solo se guarda su hash.
type APIToken struct {
	ID         string     `json:"ID"`
	Name       string     `json:"Name"`
	Role       string     `json:"Role"`
	Prefix     string     `json:"Prefix"`
	TokenHash  string     `json:"TokenHash"`
	CreatedAt  time.Time  `json:"CreatedAt"`
	CreatedBy  string     `json:"CreatedBy,omitempty"`
	ExpiresAt  *time.Time `json:"ExpiresAt,omitempty"`
	LastUsedAt *time.Time `json:"LastUsedAt,omitempty"`
	RevokedAt  *time.Time `json:"RevokedAt,omitempty"`
}

// IdempotencyRecord guarda la respuesta de un POST con Idempotency-Key (24 h).
type IdempotencyRecord struct {
	Key       string    `json:"Key"` // actor|método|ruta|clave
	BodyHash  string    `json:"BodyHash"`
	Status    int       `json:"Status"`
	Response  []byte    `json:"Response"`
	CreatedAt time.Time `json:"CreatedAt"`
}

// InventoryRecord es una versión distinta del inventario de un agente.
type InventoryRecord struct {
	Hash        string    `json:"Hash"`
	CollectedAt time.Time `json:"CollectedAt"`
	Data        []byte    `json:"Data"`
}

// RepoCredential es la credencial Kopia de un agente (contraseña cifrada).
type RepoCredential struct {
	AgentID     string    `json:"AgentID"`
	Username    string    `json:"Username"`
	Hostname    string    `json:"Hostname"`
	PasswordEnc string    `json:"PasswordEnc"`
	Version     int       `json:"Version"`
	CreatedAt   time.Time `json:"CreatedAt"`
	RotatedAt   time.Time `json:"RotatedAt"`
}

// ScheduleEntry es el estado del programador por (plan, agente).
type ScheduleEntry struct {
	PlanID     string     `json:"PlanID"`
	AgentID    string     `json:"AgentID"`
	Schedule   string     `json:"Schedule"`
	NextRunAt  time.Time  `json:"NextRunAt"`
	LastRunAt  *time.Time `json:"LastRunAt,omitempty"`
	LastJobID  string     `json:"LastJobID,omitempty"`
	LastSkipAt *time.Time `json:"LastSkipAt,omitempty"`
	SkipReason string     `json:"SkipReason,omitempty"`
}

// Settings son los ajustes del sistema (secretos cifrados).
type Settings struct {
	EnvironmentName     string   `json:"EnvironmentName"`
	AgentOfflineMinutes int      `json:"AgentOfflineMinutes"`
	AlertMinSeverity    string   `json:"AlertMinSeverity"`
	AlertRenotifyHours  int      `json:"AlertRenotifyHours"`
	WebhookURL          string   `json:"WebhookURL"`
	WebhookSecretEnc    string   `json:"WebhookSecretEnc,omitempty"`
	SMTPHost            string   `json:"SMTPHost"`
	SMTPPort            int      `json:"SMTPPort"`
	SMTPUsername        string   `json:"SMTPUsername"`
	SMTPPasswordEnc     string   `json:"SMTPPasswordEnc,omitempty"`
	SMTPFrom            string   `json:"SMTPFrom"`
	SMTPTo              []string `json:"SMTPTo"`
	SMTPStartTLS        bool     `json:"SMTPStartTLS"`
	SessionHours        int      `json:"SessionHours"`
}

// PVEInstance es una instancia de Proxmox registrada para inventario y
// operación desde el panel. El secreto del token viaja cifrado.
type PVEInstance struct {
	ID             string     `json:"ID"`
	Name           string     `json:"Name"`
	URL            string     `json:"URL"`
	TokenUser      string     `json:"TokenUser"`
	TokenID        string     `json:"TokenID"`
	TokenSecretEnc string     `json:"TokenSecretEnc"`
	VerifyTLS      bool       `json:"VerifyTLS"`
	CreatedAt      time.Time  `json:"CreatedAt"`
	LastVerifiedAt *time.Time `json:"LastVerifiedAt,omitempty"`
	LastCheck      string     `json:"LastCheck,omitempty"`
	// AgentID del agente instalado en el nodo PVE: ejecuta los jobs de
	// backup/restore de VMs de esta instancia. Vacío = sin agente.
	AgentID string `json:"AgentID,omitempty"`
}

// StorageTarget es un destino de réplica/remoto para los backups del plano
// de control: S3-compatible (MinIO, AWS, Wasabi…), ruta física local/NFS o
// compartida SMB (//servidor/recurso). El secreto viaja cifrado. ObjectLock
// indica que el bucket tiene lock activo (inmutabilidad, docs/15); en
// filesystem no aplica.
type StorageTarget struct {
	ID                  string    `json:"ID"`
	Name                string    `json:"Name"`
	Kind                string    `json:"Kind,omitempty"` // vacío = s3 | filesystem
	Endpoint            string    `json:"Endpoint,omitempty"`
	Bucket              string    `json:"Bucket,omitempty"`
	Prefix              string    `json:"Prefix,omitempty"`
	Path                string    `json:"Path,omitempty"` // ruta física o SMB (//servidor/recurso)
	SMBUser             string    `json:"SMBUser,omitempty"`
	SMBDomain           string    `json:"SMBDomain,omitempty"`
	SMBPasswordEnc      string    `json:"SMBPasswordEnc,omitempty"`
	Region              string    `json:"Region,omitempty"`
	AccessKey           string    `json:"AccessKey,omitempty"`
	SecretKeyEnc        string    `json:"SecretKeyEnc,omitempty"`
	NoTLS               bool      `json:"NoTLS"`
	ObjectLock          bool      `json:"ObjectLock"`
	RetentionDays       int       `json:"RetentionDays"`
	SyncIntervalMinutes int       `json:"SyncIntervalMinutes"`
	Enabled             bool      `json:"Enabled"`
	CreatedAt           time.Time `json:"CreatedAt"`
	// Repositorio Kopia sobre este destino: contraseña cifrada y marca de
	// repos creado (se aprovisiona en el primer uso). RepoAdopted indica que
	// la contraseña la aportó el operador (repo creado por otra instancia):
	// en ese caso nunca se formatea el destino, solo se conecta.
	RepoPasswordEnc  string     `json:"RepoPasswordEnc,omitempty"`
	RepoAdopted      bool       `json:"RepoAdopted,omitempty"`
	RepoReady        bool       `json:"RepoReady,omitempty"`
	LastSyncAt       *time.Time `json:"LastSyncAt,omitempty"`
	LastSyncStatus   string     `json:"LastSyncStatus,omitempty"`
	LastVerifyAt     *time.Time `json:"LastVerifyAt,omitempty"`
	LastVerifyStatus string     `json:"LastVerifyStatus,omitempty"`
}

// KindName devuelve el tipo normalizado del destino: "s3" (por defecto) o
// "filesystem".
func (t *StorageTarget) KindName() string {
	if t != nil && t.Kind == contracts.RepoKindFilesystem {
		return contracts.RepoKindFilesystem
	}
	return contracts.RepoKindS3
}

// IsFilesystem indica si el destino es una ruta física (no S3).
func (t *StorageTarget) IsFilesystem() bool {
	return t != nil && t.Kind == contracts.RepoKindFilesystem
}

// IsSMB indica si el destino filesystem es una compartida SMB.
func (t *StorageTarget) IsSMB() bool {
	if t == nil || !t.IsFilesystem() {
		return false
	}
	return contracts.IsSMBPath(t.Path)
}

// SMBPath descompone la ruta SMB (host, recurso y subdirectorio). Solo
// válido si IsSMB().
func (t *StorageTarget) SMBPath() contracts.SMBPathInfo {
	info, _ := contracts.ParseSMBPath(t.Path)
	return info
}

// RepoPath es la ruta efectiva de datos para destinos filesystem locales:
// la base con el prefijo como subdirectorio (equivale a bucket+prefix en
// S3). En destinos SMB devuelve la ruta cruda (la descomposición SMB hace
// SMBPath).
func (t *StorageTarget) RepoPath() string {
	if t == nil {
		return ""
	}
	if p := strings.Trim(t.Prefix, "/"); p != "" {
		return filepath.Join(t.Path, p)
	}
	return t.Path
}

// SMBRoot es el subdirectorio raíz de objetos dentro del recurso SMB:
// el subdir de la ruta más el prefijo del destino (equivale a bucket+prefix
// en S3). Devuelve la ruta con "/" como separador, sin "/" inicial.
func (t *StorageTarget) SMBRoot() string {
	if t == nil || !t.IsSMB() {
		return ""
	}
	info := t.SMBPath()
	parts := []string{}
	if info.Subdir != "" {
		parts = append(parts, strings.Trim(info.Subdir, "/"))
	}
	if p := strings.Trim(t.Prefix, "/"); p != "" {
		parts = append(parts, p)
	}
	return strings.Join(parts, "/")
}

// DefaultSettings son los valores iniciales.
func DefaultSettings() Settings {
	return Settings{
		EnvironmentName:     "Producción",
		AgentOfflineMinutes: 15,
		AlertMinSeverity:    contracts.SeverityMedium,
		AlertRenotifyHours:  6,
		SMTPPort:            587,
		SMTPStartTLS:        true,
		SessionHours:        12,
	}
}

// Meta son metadatos del estado.
type Meta struct {
	SchemaVersion int       `json:"SchemaVersion"`
	CreatedAt     time.Time `json:"CreatedAt"`
	ConfigVersion int       `json:"ConfigVersion"`
}

// MaxJobEvents es el máximo de eventos que se guardan por job.
const MaxJobEvents = 300

// MaxInventoryHistory es el máximo de versiones de inventario por agente.
const MaxInventoryHistory = 30

// State es el documento completo del plano de control.
type State struct {
	Meta          Meta                            `json:"meta"`
	Settings      Settings                        `json:"settings"`
	Tokens        map[string]*EnrollmentToken     `json:"tokens"`
	Agents        map[string]*Agent               `json:"agents"`
	Plans         map[string]*Plan                `json:"plans"`
	Assignments   map[string]*Assignment          `json:"assignments"`
	Groups        map[string]*Group               `json:"groups"`
	Jobs          map[string]*Job                 `json:"jobs"`
	JobEvents     map[string][]contracts.JobEvent `json:"job_events"`
	RestorePoints map[string]*RestorePoint        `json:"restore_points"`
	Alerts        map[string]*Alert               `json:"alerts"`
	Users         map[string]*User                `json:"users"`
	Sessions      map[string]*Session             `json:"sessions"`
	APITokens     map[string]*APIToken            `json:"api_tokens"`
	Idempotency   map[string]*IdempotencyRecord   `json:"idempotency"`
	Inventory     map[string][]InventoryRecord    `json:"inventory_history"`
	RepoCreds     map[string]*RepoCredential      `json:"repository_credentials"`
	Schedule      map[string]*ScheduleEntry       `json:"schedule"`
	PVE           map[string]*PVEInstance         `json:"proxmox_instances"`
	Storage       map[string]*StorageTarget       `json:"storage_targets"`
}

// NewState crea un estado vacío.
func NewState(now time.Time) *State {
	s := &State{
		Meta:     Meta{SchemaVersion: SchemaVersion, CreatedAt: now},
		Settings: DefaultSettings(),
	}
	s.ensureMaps()
	return s
}

func (s *State) ensureMaps() {
	if s.Tokens == nil {
		s.Tokens = map[string]*EnrollmentToken{}
	}
	if s.Agents == nil {
		s.Agents = map[string]*Agent{}
	}
	if s.Plans == nil {
		s.Plans = map[string]*Plan{}
	}
	if s.Assignments == nil {
		s.Assignments = map[string]*Assignment{}
	}
	if s.Groups == nil {
		s.Groups = map[string]*Group{}
	}
	if s.Jobs == nil {
		s.Jobs = map[string]*Job{}
	}
	if s.JobEvents == nil {
		s.JobEvents = map[string][]contracts.JobEvent{}
	}
	if s.RestorePoints == nil {
		s.RestorePoints = map[string]*RestorePoint{}
	}
	if s.Alerts == nil {
		s.Alerts = map[string]*Alert{}
	}
	if s.Users == nil {
		s.Users = map[string]*User{}
	}
	if s.Sessions == nil {
		s.Sessions = map[string]*Session{}
	}
	if s.APITokens == nil {
		s.APITokens = map[string]*APIToken{}
	}
	if s.Idempotency == nil {
		s.Idempotency = map[string]*IdempotencyRecord{}
	}
	if s.Inventory == nil {
		s.Inventory = map[string][]InventoryRecord{}
	}
	if s.RepoCreds == nil {
		s.RepoCreds = map[string]*RepoCredential{}
	}
	if s.Schedule == nil {
		s.Schedule = map[string]*ScheduleEntry{}
	}
	if s.PVE == nil {
		s.PVE = map[string]*PVEInstance{}
	}
	if s.Storage == nil {
		s.Storage = map[string]*StorageTarget{}
	}
}

// normalize completa campos tras cargar estados antiguos.
func (s *State) normalize() {
	s.ensureMaps()
	if s.Meta.SchemaVersion == 0 {
		s.Meta.SchemaVersion = SchemaVersion
	}
	if s.Settings.SessionHours == 0 && s.Settings.AgentOfflineMinutes == 0 {
		s.Settings = DefaultSettings()
	}
	for _, a := range s.Assignments {
		if a.TargetType == "" && a.AgentID != "" {
			a.TargetType = contracts.TargetAgent
			a.TargetID = a.AgentID
		}
		if a.TargetType == contracts.TargetAgent {
			a.AgentID = a.TargetID
		}
	}
	for _, p := range s.Plans {
		if p.UpdatedAt.IsZero() {
			p.UpdatedAt = p.CreatedAt
		}
		if p.Timezone == "" {
			p.Timezone = "UTC"
		}
		if p.Kind == "" {
			p.Kind = "file"
		}
	}
	for _, j := range s.Jobs {
		if j.MaxAttempts == 0 {
			j.MaxAttempts = 3
		}
	}
	for _, g := range s.Groups {
		if g.MemberIDs == nil {
			g.MemberIDs = []string{}
		}
	}
}

// AgentByAPIKeyHash busca un agente por hash de API key.
func (s *State) AgentByAPIKeyHash(hash string) *Agent {
	for _, a := range s.Agents {
		if a.APIKeyHash == hash {
			return a
		}
	}
	return nil
}

// TokenByHash busca un token de enrolamiento por hash.
func (s *State) TokenByHash(hash string) *EnrollmentToken {
	for _, t := range s.Tokens {
		if t.TokenHash == hash {
			return t
		}
	}
	return nil
}

// UserByEmail busca un usuario por email (sin distinguir mayúsculas).
func (s *State) UserByEmail(email string) *User {
	for _, u := range s.Users {
		if equalFold(u.Email, email) {
			return u
		}
	}
	return nil
}

// APITokenByHash busca un token de API.
func (s *State) APITokenByHash(hash string) *APIToken {
	for _, t := range s.APITokens {
		if t.TokenHash == hash {
			return t
		}
	}
	return nil
}

// AlertByKey devuelve la alerta no resuelta con esa clave.
func (s *State) AlertByKey(key string) *Alert {
	for _, a := range s.Alerts {
		if a.Key == key && a.State != contracts.AlertResolved {
			return a
		}
	}
	return nil
}

// AppendJobEvent añade un evento con secuencia creciente y recorta el histórico.
func (s *State) AppendJobEvent(ev contracts.JobEvent) contracts.JobEvent {
	list := s.JobEvents[ev.JobID]
	ev.Seq = 1
	if n := len(list); n > 0 {
		ev.Seq = list[n-1].Seq + 1
	}
	list = append(list, ev)
	if len(list) > MaxJobEvents {
		list = append([]contracts.JobEvent(nil), list[len(list)-MaxJobEvents:]...)
	}
	s.JobEvents[ev.JobID] = list
	return ev
}

// LeaseJobFor entrega al agente el job pendiente más antiguo. Recupera
// primero los leases caducados (quedan como pending; el programador decide
// el reintento). Devuelve nil si no hay trabajo.
func (s *State) LeaseJobFor(agentID string, now time.Time, lease, _ time.Duration) *Job {
	for _, j := range s.Jobs {
		if (j.State == JobLeased || j.State == JobRunning) && j.LeaseUntil != nil && now.After(*j.LeaseUntil) {
			j.State = JobPending
			j.LeaseUntil = nil
			j.UpdatedAt = now
		}
	}
	var best *Job
	for _, j := range s.Jobs {
		if j.AgentID != agentID || j.State != JobPending {
			continue
		}
		if best == nil || j.CreatedAt.Before(best.CreatedAt) {
			best = j
		}
	}
	if best == nil {
		return nil
	}
	until := now.Add(lease)
	best.State = JobLeased
	best.LeaseUntil = &until
	best.UpdatedAt = now
	return best
}

// BumpConfig incrementa la versión de configuración visible para los agentes.
func (s *State) BumpConfig() { s.Meta.ConfigVersion++ }

func equalFold(a, b string) bool {
	if len(a) != len(b) {
		return len([]rune(a)) == len([]rune(b)) && toLower(a) == toLower(b)
	}
	return toLower(a) == toLower(b)
}

func toLower(s string) string {
	b := []rune(s)
	for i, r := range b {
		if r >= 'A' && r <= 'Z' {
			b[i] = r + 32
		}
	}
	return string(b)
}

// Store es el contrato de persistencia del plano de control.
type Store interface {
	Ping(ctx context.Context) error
	// View ejecuta fn con el estado en modo lectura. fn no debe modificarlo
	// ni retener punteros tras volver.
	View(ctx context.Context, fn func(*State) error) error
	// Update ejecuta fn en exclusiva y persiste si fn devuelve nil. fn debe
	// validar antes de mutar: si devuelve error no se persiste nada, pero las
	// mutaciones ya hechas en memoria no se deshacen.
	Update(ctx context.Context, fn func(*State) error) error
	// UpdateLazy es como Update pero difiere la escritura a disco (heartbeats).
	UpdateLazy(ctx context.Context, fn func(*State) error) error
	// Audit devuelve el registro de auditoría.
	Audit() *AuditLog
	// Flush fuerza la escritura de cambios diferidos.
	Flush() error
	Close() error
}

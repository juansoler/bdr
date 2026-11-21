package connector

import "time"

// Instance representa una instancia registrada de Microsoft SQL Server.
type Instance struct {
	Name         string    `json:"name"`
	Key          string    `json:"key"` // nombre de clave del registro (ej. MSSQLSERVER, SQLEXPRESS)
	Version      string    `json:"version,omitempty"`
	Edition      string    `json:"edition,omitempty"`
	SQLPath      string    `json:"sql_path,omitempty"`
	Default      bool      `json:"default"` // true si es la instancia predeterminada
	DiscoveredAt time.Time `json:"discovered_at,omitempty"`
}

// Database representa una base de datos de una instancia.
type Database struct {
	Name          string    `json:"name"`
	RecoveryModel string    `json:"recovery_model"` // SIMPLE, FULL, BULK_LOGGED
	State         string    `json:"state"`          // ONLINE, OFFLINE, RESTORING, ...
	ReadOnly      bool      `json:"read_only"`
	CompatLevel   int       `json:"compatibility_level"`
	System        bool      `json:"is_system"`   // master, model, msdb
	Protectable   bool      `json:"protectable"` // puede incluirse en un plan de backup
	UpdatedAt     time.Time `json:"updated_at,omitempty"`
}

// ConnectionParams contiene los parámetros de conexión a SQL Server.
type ConnectionParams struct {
	Server   string // host o host\instancia
	Instance string // instancia con nombre (opcional)
	User     string // login SQL (vacío = Windows Auth/SSPI)
	Password string // contraseña SQL (vacío con SSPI)
}

// AuthMode indica el modo de autenticación usado.
type AuthMode string

const (
	AuthWindows AuthMode = "windows" // SSPI, cuenta de servicio del agente
	AuthSQL     AuthMode = "sql"     // login/password de SQL Server
)

// CurrentAuth devuelve el modo de autenticación que usaría el conector.
func (p ConnectionParams) CurrentAuth() AuthMode {
	if p.User != "" {
		return AuthSQL
	}
	return AuthWindows
}

// LSN es un número de secuencia de registro de SQL Server.
type LSN uint64

// ChainEntry representa una fila de msdb.dbo.backupset.
type ChainEntry struct {
	Type            string `json:"type"` // D = full, I = differential, L = log
	BackupStart     string `json:"backup_start"`
	BackupFinish    string `json:"backup_finish"`
	DatabaseLSN     LSN    `json:"database_backup_lsn"`
	CheckpointLSN   LSN    `json:"checkpoint_lsn"`
	FirstLSN        LSN    `json:"first_lsn"`
	LastLSN         LSN    `json:"last_lsn"`
	CopyOnly        bool   `json:"copy_only"`
	CompressedBytes uint64 `json:"compressed_backup_size"`
}

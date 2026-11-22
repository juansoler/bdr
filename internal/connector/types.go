package connector

import (
	"fmt"
	"math/big"
	"strings"
	"time"
)

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

// LSN es un número de secuencia de registro de SQL Server. En msdb los
// campos *_lsn son numeric(25,0): hasta 25 dígitos, que NO caben en un
// uint64 (hallazgo del lab P-26: LSN real de 21 dígitos,
// 338420000000652000001). Se transporta como texto decimal y se compara
// con big.Int.
type LSN string

// Compare compara dos LSNs como números enteros (a<b → -1, = → 0, a>b →
// 1). Un LSN vacío o no numérico vale 0.
func (l LSN) Compare(o LSN) int {
	ai, aok := new(big.Int).SetString(strings.TrimSpace(string(l)), 10)
	bi, bok := new(big.Int).SetString(strings.TrimSpace(string(o)), 10)
	if !aok {
		ai = big.NewInt(0)
	}
	if !bok {
		bi = big.NewInt(0)
	}
	return ai.Cmp(bi)
}

// LSNSet agrupa los LSN que msdb.dbo.backupset registra para un backup.
type LSNSet struct {
	DatabaseLSN LSN
	FirstLSN    LSN
	LastLSN     LSN
}

// parseLSN valida y normaliza un LSN recibido como texto (las consultas
// hacen CAST a varchar; ver P-26). Acepta cualquier longitud de dígitos
// que big.Int soporte (numeric(25,0) incluido); rechaza negativos y no
// numéricos.
func parseLSN(s string) (LSN, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", nil
	}
	if s[0] == '-' || s[0] == '+' {
		return "", fmt.Errorf("LSN inválido %q (no negativo)", s)
	}
	if _, ok := new(big.Int).SetString(s, 10); !ok {
		return "", fmt.Errorf("LSN inválido %q", s)
	}
	return LSN(s), nil
}

// ChainEntry representa una fila de msdb.dbo.backupset.
type ChainEntry struct {
	Type            string `json:"type"` // D = full, I = differential, L = log
	BackupStart     string `json:"backup_start"`
	BackupFinish    string `json:"backup_finish"`
	DatabaseLSN     LSN    `json:"database_backup_lsn"` // numeric(25,0) como texto
	CheckpointLSN   LSN    `json:"checkpoint_lsn"`
	FirstLSN        LSN    `json:"first_lsn"`
	LastLSN         LSN    `json:"last_lsn"`
	CopyOnly        bool   `json:"copy_only"`
	CompressedBytes LSN    `json:"compressed_backup_size"` // numeric(25,0) como texto
}

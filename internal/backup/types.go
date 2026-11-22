package backup

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/juansoler/bdr/internal/connector"
)

// Type es el tipo de backup SQL Server.
type Type string

const (
	TypeFull         Type = "FULL"         // BACKUP DATABASE
	TypeDifferential Type = "DIFFERENTIAL" // BACKUP DATABASE ... WITH DIFFERENTIAL
	TypeLog          Type = "LOG"          // BACKUP LOG
	TypeCopyOnly     Type = "COPY_ONLY"    // WITH COPY_ONLY (no rompe la cadena)
)

// Result contiene el resultado de una operación de backup.
type Result struct {
	Database    string    `json:"database"`
	Type        Type      `json:"type"`
	Path        string    `json:"path"`
	DurationSec float64   `json:"duration_sec"`
	SizeBytes   int64     `json:"size_bytes"`
	Checksum    bool      `json:"checksum"`
	Compression bool      `json:"compression"`
	Verified    bool      `json:"verified"`  // RESTORE VERIFYONLY ejecutado y OK
	Encrypted   bool      `json:"encrypted"` // WITH ENCRYPTION aplicado (RF-45)
	CertName    string    `json:"cert_name,omitempty"`
	StartedAt   time.Time `json:"started_at"`
	FinishedAt  time.Time `json:"finished_at"`
	Error       string    `json:"error,omitempty"`
}

// ToJSON serializa el resultado a JSON para logging/transferencia.
func (r *Result) ToJSON() ([]byte, error) {
	return json.MarshalIndent(r, "", "  ")
}

// Plan describe un plan de backup para una base de datos.
type Plan struct {
	Database       string          `json:"database"`
	Schedule       map[Type]string `json:"schedule"`
	Retention      RetentionPolicy `json:"retention"`
	StagingPath    string          `json:"staging_path"`
	VerifyAfter    bool            `json:"verify_after"`
	Encrypt        bool            `json:"encrypt"`
	CertThumbprint string          `json:"cert_thumbprint,omitempty"`
}

// RetentionPolicy define la retención de backups.
type RetentionPolicy struct {
	Daily   int `json:"daily"`
	Weekly  int `json:"weekly"`
	Monthly int `json:"monthly"`
	Yearly  int `json:"yearly"`
}

// StagingManager gestiona la carpeta de staging local.
type StagingManager struct {
	root string
}

// NewStagingManager crea un gestor de staging. La ruta debe ser accesible
// para la cuenta de servicio de SQL Server (no solo para el agente).
func NewStagingManager(root string) (*StagingManager, error) {
	if err := os.MkdirAll(root, 0750); err != nil {
		return nil, fmt.Errorf("crear carpeta staging %s: %w", root, err)
	}
	return &StagingManager{root: root}, nil
}

// FullPath genera la ruta FINAL (fichero) de un nuevo backup para la base
// y tipo indicados. El nombre lleva timestamp; si el fichero ya existe
// (dos ejecuciones en el mismo segundo), añade un sufijo incremental para
// que SQL Server nunca escriba sobre un medio existente.
//
// Este es el contrato único de rutas del motor: el orquestador resuelve el
// path de fichero aquí, y backup/restore/storage trabajan siempre con la
// ruta del FICHERO, nunca con la carpeta staging.
func (s *StagingManager) FullPath(dbName string, t Type) string {
	ts := time.Now().Format("20060102_150405")
	path := filepath.Join(s.root, fmt.Sprintf("%s_%s_%s.%s", dbName, t, ts, ext(t)))
	for i := 1; i <= 99; i++ {
		if _, err := os.Stat(path); os.IsNotExist(err) {
			return path
		}
		path = filepath.Join(s.root, fmt.Sprintf("%s_%s_%s_%d.%s", dbName, t, ts, i, ext(t)))
	}
	return path
}

// Cleanup elimina el archivo de staging tras transferencia exitosa.
func (s *StagingManager) Cleanup(path string) error {
	return os.Remove(path)
}

// BackupOperations define la interfaz de operaciones de backup.
// backupPath es siempre la ruta FINAL del fichero; verifyAfter controla
// si se verifica el medio (RESTORE VERIFYONLY) justo tras el backup y enc
// activa el cifrado nativo (nil = sin cifrar).
type BackupOperations interface {
	Full(ctx context.Context, db *connector.DB, database, backupPath string, verifyAfter bool, enc *Encryption) (*Result, error)
	Differential(ctx context.Context, db *connector.DB, database, backupPath string, verifyAfter bool, enc *Encryption) (*Result, error)
	Log(ctx context.Context, db *connector.DB, database, backupPath string, verifyAfter bool, enc *Encryption) (*Result, error)
	CopyOnly(ctx context.Context, db *connector.DB, database, backupPath string, verifyAfter bool, enc *Encryption) (*Result, error)
	Verify(ctx context.Context, db *connector.DB, path string) error
}

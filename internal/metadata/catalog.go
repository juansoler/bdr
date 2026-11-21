package metadata

import (
	"context"
	"fmt"
	"time"

	"github.com/juansoler/bdr/internal/backup"
	"github.com/juansoler/bdr/internal/connector"
)

type BackupInfo struct {
	ID          string            `json:"id"` // UUID generado por la plataforma
	Database    string            `json:"database"`
	Instance    string            `json:"instance"` // nombre o IP:puerto
	BackupType  backup.Type       `json:"backup_type"`
	StartedAt   time.Time         `json:"started_at"`
	FinishedAt  time.Time         `json:"finished_at"`
	SizeBytes   int64             `json:"size_bytes"`
	Checksum    bool              `json:"checksum"`
	Compression bool              `json:"compression"`
	Path        string            `json:"path"`      // ubicación original (staging)
	RepoPath    string            `json:"repo_path"` // ubicación final en NAS/S3
	LSN         connector.LSN     `json:"lsn"`       // database_backup_lsn
	FirstLSN    connector.LSN     `json:"first_lsn"`
	LastLSN     connector.LSN     `json:"last_lsn"`
	CopyOnly    bool              `json:"copy_only"`
	Verify      bool              `json:"verify"` // resultado VERIFYONLY
	Error       string            `json:"error,omitempty"`
	Tags        map[string]string `json:"tags,omitempty"` // etiquetas: plan_id, schedule, etc.
}

type ChainInfo struct {
	Database    string           `json:"database"`
	Instance    string           `json:"instance"`
	Recovery    string           `json:"recovery_model"`
	Entries     []ChainEntryInfo `json:"entries"`
	BestRestore string           `json:"best_restore"` // descripción del último restore para PITR
	Validated   bool             `json:"validated"`
	Error       string           `json:"error,omitempty"`
}

type ChainEntryInfo struct {
	Type         backup.Type   `json:"type"`
	BackupStart  string        `json:"backup_start"`
	BackupFinish string        `json:"backup_finish"`
	DatabaseLSN  connector.LSN `json:"database_backup_lsn"`
	FirstLSN     connector.LSN `json:"first_lsn"`
	LastLSN      connector.LSN `json:"last_lsn"`
	CopyOnly     bool          `json:"copy_only"`
	Compressed   bool          `json:"compressed"`
}

// Catalog define el almacén de metadatos (ej. PostgreSQL por ahora, con interfaz).
type Catalog interface {
	StoreBackup(ctx context.Context, info *BackupInfo) error
	GetBackup(ctx context.Context, id string) (*BackupInfo, error)
	ListBackups(ctx context.Context, database string) ([]*BackupInfo, error)
	StoreChain(ctx context.Context, info *ChainInfo) error
	GetChain(ctx context.Context, database string) (*ChainInfo, error)
	Close() error
}

// InMemoryCatalog es un catálogo para pruebas (no persiste). Se reemplaza por
// una implementación real de PostgreSQL en producción.
type InMemoryCatalog struct {
	backups map[string]*BackupInfo
	chains  map[string]*ChainInfo
}

// NewInMemoryCatalog crea un catálogo en memoria.
func NewInMemoryCatalog() *InMemoryCatalog {
	return &InMemoryCatalog{
		backups: make(map[string]*BackupInfo),
		chains:  make(map[string]*ChainInfo),
	}
}

// StoreBackup implementa Catalog.
func (c *InMemoryCatalog) StoreBackup(ctx context.Context, info *BackupInfo) error {
	info.ID = fmt.Sprintf("backup_%d", time.Now().UnixNano())
	c.backups[info.ID] = info
	return nil
}

// GetBackup implementa Catalog.
func (c *InMemoryCatalog) GetBackup(ctx context.Context, id string) (*BackupInfo, error) {
	if info, ok := c.backups[id]; ok {
		return info, nil
	}
	return nil, fmt.Errorf("backup %s no encontrado", id)
}

// ListBackups implementa Catalog.
func (c *InMemoryCatalog) ListBackups(ctx context.Context, database string) ([]*BackupInfo, error) {
	var result []*BackupInfo
	for _, info := range c.backups {
		if info.Database == database {
			result = append(result, info)
		}
	}
	return result, nil
}

// StoreChain implementa Catalog.
func (c *InMemoryCatalog) StoreChain(ctx context.Context, info *ChainInfo) error {
	key := "chain_" + info.Database
	c.chains[key] = info
	return nil
}

// GetChain implementa Catalog.
func (c *InMemoryCatalog) GetChain(ctx context.Context, database string) (*ChainInfo, error) {
	key := "chain_" + database
	if info, ok := c.chains[key]; ok {
		return info, nil
	}
	return nil, fmt.Errorf("cadena no encontrada para %s", database)
}

// Close no-op para catálogo en memoria.
func (c *InMemoryCatalog) Close() error { return nil }

// NewBackupInfo construye un BackupInfo a partir de un Result y metadatos adicionales.
func NewBackupInfo(res *backup.Result, database, instance string, t backup.Type, path, repoPath string, lsn connector.LSN, firstLSN, lastLSN connector.LSN) *BackupInfo {
	now := time.Now()
	info := &BackupInfo{
		Database:    database,
		Instance:    instance,
		BackupType:  t,
		FinishedAt:  now,
		SizeBytes:   res.SizeBytes,
		Checksum:    res.Checksum,
		Compression: res.Compression,
		Path:        path,
		RepoPath:    repoPath,
		LSN:         lsn,
		FirstLSN:    firstLSN,
		LastLSN:     lastLSN,
		CopyOnly:    t == backup.TypeCopyOnly,
		Tags:        map[string]string{"type": string(t)},
	}
	if !res.FinishedAt.IsZero() {
		info.StartedAt = now.Add(-time.Duration(res.DurationSec) * time.Second)
	}
	return info
}

// ChainEntryFromDBEntry transforma una fila msdb.dbo.backupset en ChainEntryInfo.
func ChainEntryFromDBEntry(e connector.ChainEntry) ChainEntryInfo {
	return ChainEntryInfo{
		Type:         backup.Type(e.Type),
		BackupStart:  e.BackupStart,
		BackupFinish: e.BackupFinish,
		DatabaseLSN:  e.DatabaseLSN,
		FirstLSN:     e.FirstLSN,
		LastLSN:      e.LastLSN,
		CopyOnly:     e.CopyOnly,
		Compressed:   e.CompressedBytes > 0,
	}
}

// CatalogToJSON serializa el catálogo a JSON para depuración/transporte ligero.
func CatalogToJSON(c Catalog) ([]byte, error) {
	// versión simple: solo in-memory
	// en producción se usaría una serialización real o consulta a PostgreSQL
	return nil, fmt.Errorf("serialización de catálogo delegada al backend real")
}

// Registry define las claves de catálogo para persistencia posterior.
// Se usa al integrar con el módulo de PostgreSQL del control plane.
type Registry struct {
	BackupTable   string // tabla principal de backups
	ChainTable    string // tabla de cadenas de backup
	LSNIndex      string // índice para consultas por LSN
	InstanceField string // campo de nombre de instancia
	DatabaseField string // campo de nombre de base
}

// DefaultRegistry devuelve un registro con nombres de tablas estándar.
func DefaultRegistry() Registry {
	return Registry{
		BackupTable:   "sql_backups",
		ChainTable:    "sql_backup_chains",
		LSNIndex:      "idx_backup_lsn",
		InstanceField: "instance_name",
		DatabaseField: "database_name",
	}
}

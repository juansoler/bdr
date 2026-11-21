package metadata

import (
	"context"
	"fmt"
	"time"

	"github.com/juansoler/bdr/internal/backup"
)

// BackupInfo stores metadata of a registered backup.
type BackupInfo struct {
	ID           string            `json:"id"`
	Database     string            `json:"database"`
	Instance     string            `json:"instance"`
	BackupType  backup.Type       `json:"backup_type"`
	StartedAt    time.Time         `json:"started_at"`
	FinishedAt   time.Time         `json:"finished_at"`
	SizeBytes    int64             `json:"size_bytes"`
	Checksum     bool              `json:"checksum"`
	Compression  bool              `json:"compression"`
	Path         string            `json:"path"`
	RepoPath     string            `json:"repo_path"`
	FirstLSN     uint64            `json:"first_lsn"`
	LastLSN      uint64            `json:"last_lsn"`
	CopyOnly     bool              `json:"copy_only"`
	Verify       bool              `json:"verify"`
	Error        string            `json:"error,omitempty"`
}

// ChainInfo describes a backup chain for restore/PITR.
type ChainInfo struct {
	Database string         `json:"database"`
	Entries  []ChainEntry  `json:"entries"`
	Validated bool        `json:"validated"`
}

// ChainEntry is one entry of msdb.dbo.backupset.
type ChainEntry struct {
	Type         backup.Type `json:"type"`
	BackupStart string      `json:"backup_start"`
	BackupFinish string     `json:"backup_finish"`
	FirstLSN    uint64      `json:"first_lsn"`
	LastLSN     uint64      `json:"last_lsn"`
	CopyOnly    bool        `json:"copy_only"`
}

// Catalog is the metadata backend interface.
type Catalog interface {
	StoreBackup(ctx context.Context, info *BackupInfo) error
	GetBackup(ctx context.Context, id string) (*BackupInfo, error)
	ListBackups(ctx context.Context, database string) ([]*BackupInfo, error)
	StoreChain(ctx context.Context, info *ChainInfo) error
	GetChain(ctx context.Context, database string) (*ChainInfo, error)
	Close() error
}

// InMemoryCatalog is a stub catalog implementation.
type InMemoryCatalog struct {
	backups map[string]*BackupInfo
	chains  map[string]*ChainInfo
}

func NewInMemoryCatalog() *InMemoryCatalog {
	return &InMemoryCatalog{
		backups: make(map[string]*BackupInfo),
		chains:  make(map[string]*ChainInfo),
	}
}

func (c *InMemoryCatalog) StoreBackup(ctx context.Context, info *BackupInfo) error {
	info.ID = fmt.Sprintf("backup_%d", time.Now().UnixNano())
	c.backups[info.ID] = info
	return nil
}

func (c *InMemoryCatalog) GetBackup(ctx context.Context, id string) (*BackupInfo, error) {
	if info, ok := c.backups[id]; ok {
		return info, nil
	}
	return nil, fmt.Errorf("backup no encontrado: %s", id)
}

func (c *InMemoryCatalog) ListBackups(ctx context.Context, database string) ([]*BackupInfo, error) {
	var result []*BackupInfo
	for _, info := range c.backups {
		if info.Database == database {
			result = append(result, info)
		}
	}
	return result, nil
}

func (c *InMemoryCatalog) StoreChain(ctx context.Context, info *ChainInfo) error {
	key := "chain_" + info.Database
	c.chains[key] = info
	return nil
}

func (c *InMemoryCatalog) GetChain(ctx context.Context, database string) (*ChainInfo, error) {
	key := "chain_" + database
	if info, ok := c.chains[key]; ok {
		return info, nil
	}
	return nil, fmt.Errorf("cadena no encontrada")
}

func (c *InMemoryCatalog) Close() error { return nil }
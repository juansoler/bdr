package orchestrator

import (
	"context"
	"fmt"

	"github.com/juansoler/bdr/internal/backup"
	"github.com/juansoler/bdr/internal/connector"
	"github.com/juansoler/bdr/internal/metadata"
	"github.com/juansoler/bdr/internal/restore"
	"github.com/juansoler/bdr/internal/storage"
)

type Orquestador struct {
	db         *connector.DB
	staging    *backup.StagingManager
	transferer *storage.Transferer
	catalog    metadata.Catalog
	database   string
}

func New(db *connector.DB, sm *backup.StagingManager, t *storage.Transferer, c metadata.Catalog, database string) *Orquestador {
	return &Orquestador{db: db, staging: sm, transferer: t, catalog: c, database: database}
}

func (o *Orquestador) RunBackup(ctx context.Context, t backup.Type, copyOnly bool) (*backup.Result, string, error) {
	path := o.staging.FullPath(o.database, t)
	var res *backup.Result
	var err error
	switch t {
	case backup.TypeFull:
		res, err = backup.Full(ctx, o.db, o.database, path)
	case backup.TypeDifferential:
		res, err = backup.Differential(ctx, o.db, o.database, path)
	case backup.TypeLog:
		res, err = backup.Log(ctx, o.db, o.database, path)
	case backup.TypeCopyOnly:
		res, err = backup.CopyOnly(ctx, o.db, o.database, path)
	default:
		return nil, "", fmt.Errorf("tipo no soportado")
	}
	if err != nil {
		return nil, "", err
	}

	repoPath, err := o.transferer.Transfer(ctx, path)
	if err != nil {
		return nil, "", err
	}

	if err := o.staging.Cleanup(path); err != nil {
		return nil, "", err
	}

	_ = o.catalog.StoreBackup(ctx, &metadata.BackupInfo{
		Database: o.database,
		Path:     path,
		RepoPath: repoPath,
	})

	return res, repoPath, nil
}

func (o *Orquestador) RunRestore(ctx context.Context, opts restore.Options) (*restore.Result, error) {
	return restore.Execute(ctx, o.db, opts)
}

func (o *Orquestador) Health(ctx context.Context) error {
	return nil
}
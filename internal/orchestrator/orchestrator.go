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

// New crea un Orquestador listo para ejecutar planes de backup.
func New(db *connector.DB, sm *backup.StagingManager, t *storage.Transferer, c metadata.Catalog, database string) *Orquestador {
	return &Orquestador{
		db:         db,
		staging:    sm,
		transferer: t,
		catalog:    c,
		database:   database,
	}
}

// RunBackup ejecuta un backup del tipo indicado y lo transfiere al repositorio.
// Devuelve el resultado del backup y la ruta en el repositorio.
func (o *Orquestador) RunBackup(ctx context.Context, t backup.Type, copyOnly bool) (*backup.Result, string, error) {
	stagingPath := o.staging.FullPath(o.database, t)

	res, err := runSingle(ctx, o.db, o.database, t, copyOnly, stagingPath)
	if err != nil {
		return nil, "", err
	}

	repoPath, err := o.transferer.Transfer(ctx, stagingPath)
	if err != nil {
		// el staging se mantiene para reintento
		return res, "", err
	}

	// limpiar staging tras transferencia exitosa
	if err := o.staging.Cleanup(stagingPath); err != nil {
		return res, repoPath, fmt.Errorf("transferido pero error limpiando staging: %w", err)
	}

	if err := o.catalog.StoreBackup(ctx, metadata.NewBackupInfo(res, o.database, o.db.Params().Server, t, stagingPath, repoPath, 0, 0, 0)); err != nil {
		return res, repoPath, fmt.Errorf("guardado en catálogo: %w", err)
	}

	return res, repoPath, nil
}

func runSingle(ctx context.Context, db *connector.DB, database string, t backup.Type, copyOnly bool, stagingPath string) (*backup.Result, error) {
	switch t {
	case backup.TypeFull:
		return backup.Full(ctx, db, database, stagingPath)
	case backup.TypeDifferential:
		return backup.Differential(ctx, db, database, stagingPath)
	case backup.TypeLog:
		return backup.Log(ctx, db, database, stagingPath)
	case backup.TypeCopyOnly:
		return backup.CopyOnly(ctx, db, database, stagingPath)
	default:
		return nil, fmt.Errorf("tipo no soportado: %s", t)
	}
}

// RunRestore ejecuta una restauración desde una lista de archivos origen.
func (o *Orquestador) RunRestore(ctx context.Context, opts restore.Options) (*restore.Result, error) {
	return restore.Execute(ctx, o.db, opts)
}

// Health verifica la conexión a SQL Server.
func (o *Orquestador) Health(ctx context.Context) error {
	return o.db.Ping(ctx)
}

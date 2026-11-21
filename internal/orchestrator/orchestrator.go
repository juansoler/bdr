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
	transferer storage.Transferer
	catalog    metadata.Catalog
	database   string
}

// New crea un Orquestador listo para ejecutar planes de backup.
// t puede ser cualquier backend de storage.Transferer (local, SMB o S3).
func New(db *connector.DB, sm *backup.StagingManager, t storage.Transferer, c metadata.Catalog, database string) *Orquestador {
	return &Orquestador{
		db:         db,
		staging:    sm,
		transferer: t,
		catalog:    c,
		database:   database,
	}
}

// RunBackup ejecuta un backup del tipo indicado y lo transfiere al repositorio.
// verifyAfter controla si se verifica el medio tras el backup (RESTORE
// VERIFYONLY; por diseño true en los planes, RF-44).
// Devuelve el resultado del backup y la ruta en el repositorio.
func (o *Orquestador) RunBackup(ctx context.Context, t backup.Type, copyOnly, verifyAfter bool) (*backup.Result, string, error) {
	// Contrato de rutas: FullPath devuelve el FICHERO final; backup escribe
	// ahí, storage transfiere ese fichero y Cleanup lo elimina.
	stagingPath := o.staging.FullPath(o.database, t)

	res, err := runSingle(ctx, o.db, o.database, t, copyOnly, stagingPath, verifyAfter)
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

	// RF-41: capturar los LSN reales que SQL Server registró en msdb tras
	// el BACKUP (antes iban siempre a 0 en el catálogo).
	lsns, err := o.db.LastBackupLSNs(ctx, o.database)
	if err != nil {
		return res, repoPath, fmt.Errorf("backup transferido pero captura de LSN falló: %w", err)
	}

	if err := o.catalog.StoreBackup(ctx, metadata.NewBackupInfo(res, o.database, o.db.Params().Server, t, stagingPath, repoPath, lsns.DatabaseLSN, lsns.FirstLSN, lsns.LastLSN)); err != nil {
		return res, repoPath, fmt.Errorf("guardado en catálogo: %w", err)
	}

	return res, repoPath, nil
}

func runSingle(ctx context.Context, db *connector.DB, database string, t backup.Type, copyOnly bool, backupPath string, verifyAfter bool) (*backup.Result, error) {
	switch t {
	case backup.TypeFull:
		return backup.Full(ctx, db, database, backupPath, verifyAfter)
	case backup.TypeDifferential:
		return backup.Differential(ctx, db, database, backupPath, verifyAfter)
	case backup.TypeLog:
		return backup.Log(ctx, db, database, backupPath, verifyAfter)
	case backup.TypeCopyOnly:
		return backup.CopyOnly(ctx, db, database, backupPath, verifyAfter)
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

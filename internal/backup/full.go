package backup

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/juansoler/bdr/internal/connector"
)

func ext(t Type) string {
	if t == TypeLog {
		return "trn"
	}
	return "bak"
}

// quoteTSQL escapa comillas simples para literales T-SQL.
func quoteTSQL(s string) string {
	return "N'" + strings.ReplaceAll(s, "'", "''") + "'"
}

func buildBackupTSQL(database string, t Type, copyOnly bool, path string) string {
	dbq := quoteTSQL(database)
	pathq := quoteTSQL(path)

	switch t {
	case TypeFull:
		opts := "WITH COMPRESSION, CHECKSUM, STATS = 10"
		if copyOnly {
			opts = "WITH COMPRESSION, CHECKSUM, COPY_ONLY, STATS = 10"
		}
		return fmt.Sprintf("BACKUP DATABASE %s TO DISK = %s %s", dbq, pathq, opts)
	case TypeDifferential:
		return fmt.Sprintf("BACKUP DATABASE %s TO DISK = %s WITH DIFFERENTIAL, COMPRESSION, CHECKSUM, STATS = 10", dbq, pathq)
	case TypeLog:
		return fmt.Sprintf("BACKUP LOG %s TO DISK = %s WITH COMPRESSION, CHECKSUM, STATS = 10", dbq, pathq)
	case TypeCopyOnly:
		return fmt.Sprintf("BACKUP DATABASE %s TO DISK = %s WITH COMPRESSION, CHECKSUM, COPY_ONLY, STATS = 10", dbq, pathq)
	default:
		return fmt.Sprintf("BACKUP DATABASE %s TO DISK = %s WITH COMPRESSION, CHECKSUM, STATS = 10", dbq, pathq)
	}
}

// Full ejecuta un backup completo.
//
// backupPath es la ruta FINAL del fichero .bak (resuelta por el
// StagingManager); el motor no genera nombres, solo escribe en esa ruta.
// verifyAfter controla si se ejecuta RESTORE VERIFYONLY tras el backup.
func Full(ctx context.Context, db *connector.DB, database, backupPath string, verifyAfter bool) (*Result, error) {
	return execute(ctx, db, database, TypeFull, false, backupPath, verifyAfter)
}

// Differential ejecuta un backup diferencial. Mismo contrato de ruta que Full.
func Differential(ctx context.Context, db *connector.DB, database, backupPath string, verifyAfter bool) (*Result, error) {
	return execute(ctx, db, database, TypeDifferential, false, backupPath, verifyAfter)
}

// Log ejecuta un backup de log (transacciones). Mismo contrato de ruta que Full.
func Log(ctx context.Context, db *connector.DB, database, backupPath string, verifyAfter bool) (*Result, error) {
	return execute(ctx, db, database, TypeLog, false, backupPath, verifyAfter)
}

// CopyOnly ejecuta un backup copy-only (no rompe la cadena de restauración).
// Mismo contrato de ruta que Full.
func CopyOnly(ctx context.Context, db *connector.DB, database, backupPath string, verifyAfter bool) (*Result, error) {
	return execute(ctx, db, database, TypeCopyOnly, true, backupPath, verifyAfter)
}

func execute(ctx context.Context, db *connector.DB, database string, t Type, copyOnly bool, backupPath string, verifyAfter bool) (*Result, error) {
	start := time.Now()
	path := backupPath

	q := buildBackupTSQL(database, t, copyOnly, path)

	if err := db.ExecContext(ctx, q); err != nil {
		return &Result{
			Database:   database,
			Type:       t,
			Path:       path,
			StartedAt:  start,
			FinishedAt: time.Now(),
			Error:      err.Error(),
		}, fmt.Errorf("BACKUP %s de %s: %w", t, database, err)
	}

	verified := false
	if verifyAfter {
		if err := Verify(ctx, db, path); err != nil {
			return &Result{
				Database:   database,
				Type:       t,
				Path:       path,
				StartedAt:  start,
				FinishedAt: time.Now(),
				Error:      err.Error(),
			}, err
		}
		verified = true
	}

	fi, err := os.Stat(path)
	if err != nil {
		return &Result{
			Database:   database,
			Type:       t,
			Path:       path,
			StartedAt:  start,
			FinishedAt: time.Now(),
			Error:      err.Error(),
		}, fmt.Errorf("stat staging %s: %w", path, err)
	}

	size := int64(0)
	if fi != nil {
		size = fi.Size()
	}

	return &Result{
		Database:    database,
		Type:        t,
		Path:        path,
		DurationSec: time.Since(start).Seconds(),
		SizeBytes:   size,
		Checksum:    true, // CHECKSUM siempre activo por diseño
		Compression: true, // COMPRESSION siempre activo por diseño
		Verified:    verified,
		StartedAt:   start,
		FinishedAt:  time.Now(),
	}, nil
}

func Verify(ctx context.Context, db *connector.DB, path string) error {
	q := fmt.Sprintf("RESTORE VERIFYONLY FROM DISK = %s WITH CHECKSUM", quoteTSQL(path))
	if err := db.ExecContext(ctx, q); err != nil {
		return fmt.Errorf("RESTORE VERIFYONLY %s: %w", path, err)
	}
	return nil
}

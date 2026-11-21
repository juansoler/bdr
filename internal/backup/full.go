package backup

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
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
func Full(ctx context.Context, db *connector.DB, database, stagingPath string) (*Result, error) {
	return execute(ctx, db, database, TypeFull, false, stagingPath)
}

func Differential(ctx context.Context, db *connector.DB, database, stagingPath string) (*Result, error) {
	return execute(ctx, db, database, TypeDifferential, false, stagingPath)
}

func Log(ctx context.Context, db *connector.DB, database, stagingPath string) (*Result, error) {
	return execute(ctx, db, database, TypeLog, false, stagingPath)
}

func CopyOnly(ctx context.Context, db *connector.DB, database, stagingPath string) (*Result, error) {
	return execute(ctx, db, database, TypeCopyOnly, true, stagingPath)
}

func execute(ctx context.Context, db *connector.DB, database string, t Type, copyOnly bool, stagingPath string) (*Result, error) {
	start := time.Now()

	path := filepath.Join(stagingPath, fmt.Sprintf("%s_%s_%s.%s",
		database, t, start.Format("20060102_150405"), ext(t)))

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

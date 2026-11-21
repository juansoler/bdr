// PGCatalog implementa metadata.Catalog sobre PostgreSQL (pgx v5) — RF-41.
//
// Las tablas son sql_backups y sql_backup_chains (ver metadata.Registry).
// El esquema lo aplica el control plane con la migración
// apps/server/internal/store/migrations/0002_sql_catalog.sql; para uso
// independiente (laboratorio, sin servidor), EnsureSchema ejecuta la misma
// DDL idempotente (¡mantener ambas en sync!).
//
// Los LSN de SQL Server son numeric(25,0): se escriben como int64 (cabe de
// sobra en 25 dígitos) y se leen casteándolos a texto para parsearlos sin
// depender de cómo pgx decodifica el decimal (mismo criterio que el
// conector, ver P-26).
package metadata

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/juansoler/bdr/internal/backup"
	"github.com/juansoler/bdr/internal/connector"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// EnsureSchemaDDL es la DDL del catálogo SQL. Idempotente y en sync con la
// migración 0002_sql_catalog.sql del control plane.
const EnsureSchemaDDL = `
CREATE TABLE IF NOT EXISTS sql_backups (
    id           text PRIMARY KEY,
    instance     text NOT NULL,
    database     text NOT NULL,
    backup_type  text NOT NULL,
    copy_only    boolean NOT NULL DEFAULT false,
    started_at   timestamptz NOT NULL,
    finished_at  timestamptz NOT NULL,
    duration_sec double precision NOT NULL DEFAULT 0,
    size_bytes   bigint NOT NULL DEFAULT 0,
    checksum     boolean NOT NULL DEFAULT false,
    compression  boolean NOT NULL DEFAULT false,
    verified     boolean NOT NULL DEFAULT false,
    lsn          numeric(25,0),
    first_lsn    numeric(25,0),
    last_lsn     numeric(25,0),
    staging_path text NOT NULL DEFAULT '',
    repo_path    text NOT NULL DEFAULT '',
    error        text NOT NULL DEFAULT '',
    tags         jsonb NOT NULL DEFAULT '{}',
    created_at   timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_sql_backups_db
    ON sql_backups (instance, database, finished_at DESC);
CREATE INDEX IF NOT EXISTS idx_sql_backups_lsn
    ON sql_backups (instance, database, last_lsn);

CREATE TABLE IF NOT EXISTS sql_backup_chains (
    instance     text NOT NULL,
    database     text NOT NULL,
    recovery     text NOT NULL DEFAULT '',
    validated    boolean NOT NULL DEFAULT false,
    error        text NOT NULL DEFAULT '',
    best_restore text NOT NULL DEFAULT '',
    entries      jsonb NOT NULL DEFAULT '[]',
    updated_at   timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (instance, database)
);
`

// PGCatalog es la implementación duradera de Catalog.
type PGCatalog struct {
	pool *pgxpool.Pool
}

// compile-time: PGCatalog debe satisfacer Catalog.
var _ Catalog = (*PGCatalog)(nil)

// NewPGCatalog construye el catálogo sobre un pool existente (el del
// control plane). Las tablas deben existir: el server las aplica por
// migración; en otros contextos llama EnsureSchema antes.
func NewPGCatalog(pool *pgxpool.Pool) *PGCatalog {
	return &PGCatalog{pool: pool}
}

// EnsureSchema aplica la DDL idempotente del catálogo (lab sin server).
func (c *PGCatalog) EnsureSchema(ctx context.Context) error {
	if _, err := c.pool.Exec(ctx, EnsureSchemaDDL); err != nil {
		return fmt.Errorf("aplicar esquema del catálogo SQL: %w", err)
	}
	return nil
}

// StoreBackup inserta (o actualiza, por id) un backup en el catálogo.
func (c *PGCatalog) StoreBackup(ctx context.Context, info *BackupInfo) error {
	if info.ID == "" {
		id, err := newBackupID()
		if err != nil {
			return err
		}
		info.ID = id
	}
	tags, err := json.Marshal(info.Tags)
	if err != nil {
		return fmt.Errorf("serializar tags: %w", err)
	}
	started := info.StartedAt
	if started.IsZero() {
		started = info.FinishedAt
	}
	const q = `
INSERT INTO sql_backups (
    id, instance, database, backup_type, copy_only,
    started_at, finished_at, duration_sec, size_bytes,
    checksum, compression, verified,
    lsn, first_lsn, last_lsn,
    staging_path, repo_path, error, tags
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19::jsonb)
ON CONFLICT (id) DO UPDATE SET
    instance = EXCLUDED.instance,
    database = EXCLUDED.database,
    backup_type = EXCLUDED.backup_type,
    copy_only = EXCLUDED.copy_only,
    started_at = EXCLUDED.started_at,
    finished_at = EXCLUDED.finished_at,
    duration_sec = EXCLUDED.duration_sec,
    size_bytes = EXCLUDED.size_bytes,
    checksum = EXCLUDED.checksum,
    compression = EXCLUDED.compression,
    verified = EXCLUDED.verified,
    lsn = EXCLUDED.lsn,
    first_lsn = EXCLUDED.first_lsn,
    last_lsn = EXCLUDED.last_lsn,
    staging_path = EXCLUDED.staging_path,
    repo_path = EXCLUDED.repo_path,
    error = EXCLUDED.error,
    tags = EXCLUDED.tags`

	_, err = c.pool.Exec(ctx, q,
		info.ID, info.Instance, info.Database, string(info.BackupType), info.CopyOnly,
		started, info.FinishedAt, info.DurationSec, info.SizeBytes,
		info.Checksum, info.Compression, info.Verify,
		int64(info.LSN), int64(info.FirstLSN), int64(info.LastLSN),
		info.Path, info.RepoPath, info.Error, string(tags),
	)
	if err != nil {
		return fmt.Errorf("guardar backup %s: %w", info.ID, err)
	}
	return nil
}

// GetBackup lee un backup por id.
func (c *PGCatalog) GetBackup(ctx context.Context, id string) (*BackupInfo, error) {
	const q = `
SELECT id, instance, database, backup_type, copy_only,
       started_at, finished_at, duration_sec, size_bytes,
       checksum, compression, verified,
       COALESCE(lsn::text, ''), COALESCE(first_lsn::text, ''), COALESCE(last_lsn::text, ''),
       staging_path, repo_path, error, tags::text
FROM sql_backups WHERE id = $1`
	row := c.pool.QueryRow(ctx, q, id)
	info, err := scanBackup(row)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, fmt.Errorf("backup %s no encontrado", id)
		}
		return nil, fmt.Errorf("leer backup %s: %w", id, err)
	}
	return info, nil
}

// ListBackups lista los backups de una base ("" = todas), más recientes
// primero, con un tope de lectura de 500 filas.
func (c *PGCatalog) ListBackups(ctx context.Context, database string) ([]*BackupInfo, error) {
	q := `
SELECT id, instance, database, backup_type, copy_only,
       started_at, finished_at, duration_sec, size_bytes,
       checksum, compression, verified,
       COALESCE(lsn::text, ''), COALESCE(first_lsn::text, ''), COALESCE(last_lsn::text, ''),
       staging_path, repo_path, error, tags::text
FROM sql_backups`
	args := []any{}
	if database != "" {
		q += ` WHERE database = $1`
		args = append(args, database)
	}
	q += ` ORDER BY finished_at DESC LIMIT 500`

	rows, err := c.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("listar backups: %w", err)
	}
	defer rows.Close()

	var out []*BackupInfo
	for rows.Next() {
		info, err := scanBackup(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, info)
	}
	return out, rows.Err()
}

// scanner abstrae pgx.Row y pgx.Rows para reutilizar el escaneo.
type scanner interface{ Scan(dest ...any) error }

func scanBackup(r scanner) (*BackupInfo, error) {
	var info BackupInfo
	var backupType, lsnS, firstS, lastS, tagsS string
	var started, finished time.Time
	if err := r.Scan(&info.ID, &info.Instance, &info.Database, &backupType, &info.CopyOnly,
		&started, &finished, &info.DurationSec, &info.SizeBytes,
		&info.Checksum, &info.Compression, &info.Verify,
		&lsnS, &firstS, &lastS,
		&info.Path, &info.RepoPath, &info.Error, &tagsS,
	); err != nil {
		return nil, err
	}
	info.BackupType = backup.Type(backupType)
	info.StartedAt, info.FinishedAt = started, finished

	var err error
	if info.LSN, err = parseCatalogLSN(lsnS); err != nil {
		return nil, err
	}
	if info.FirstLSN, err = parseCatalogLSN(firstS); err != nil {
		return nil, err
	}
	if info.LastLSN, err = parseCatalogLSN(lastS); err != nil {
		return nil, err
	}
	if tagsS != "" {
		if err := json.Unmarshal([]byte(tagsS), &info.Tags); err != nil {
			return nil, fmt.Errorf("leer tags: %w", err)
		}
	}
	return &info, nil
}

// parseCatalogLSN parsea un LSN leído como texto del numeric(25,0).
func parseCatalogLSN(s string) (connector.LSN, error) {
	if s == "" {
		return 0, nil
	}
	v, err := strconv.ParseUint(s, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("LSN del catálogo inválido %q: %w", s, err)
	}
	return connector.LSN(v), nil
}

// StoreChain guarda (upsert por instance+database) la cadena de una base.
func (c *PGCatalog) StoreChain(ctx context.Context, info *ChainInfo) error {
	entries, err := json.Marshal(info.Entries)
	if err != nil {
		return fmt.Errorf("serializar entradas de cadena: %w", err)
	}
	const q = `
INSERT INTO sql_backup_chains (instance, database, recovery, validated, error, best_restore, entries, updated_at)
VALUES ($1,$2,$3,$4,$5,$6,$7::jsonb, now())
ON CONFLICT (instance, database) DO UPDATE SET
    recovery = EXCLUDED.recovery,
    validated = EXCLUDED.validated,
    error = EXCLUDED.error,
    best_restore = EXCLUDED.best_restore,
    entries = EXCLUDED.entries,
    updated_at = now()`
	if _, err := c.pool.Exec(ctx, q,
		info.Instance, info.Database, info.Recovery, info.Validated,
		info.Error, info.BestRestore, string(entries),
	); err != nil {
		return fmt.Errorf("guardar cadena de %s: %w", info.Database, err)
	}
	return nil
}

// GetChain lee la cadena más reciente de una base. La clave natural es
// (instance, database), pero la interfaz del catálogo indexa por base:
// devuelve la última actualizada (el flujo normal solo gestiona una
// instancia por base).
func (c *PGCatalog) GetChain(ctx context.Context, database string) (*ChainInfo, error) {
	const q = `
SELECT instance, database, recovery, validated, error, best_restore, entries::text
FROM sql_backup_chains
WHERE database = $1
ORDER BY updated_at DESC
LIMIT 1`
	var info ChainInfo
	var entriesS string
	err := c.pool.QueryRow(ctx, q, database).Scan(
		&info.Instance, &info.Database, &info.Recovery, &info.Validated,
		&info.Error, &info.BestRestore, &entriesS,
	)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, fmt.Errorf("cadena no encontrada para %s", database)
		}
		return nil, fmt.Errorf("leer cadena de %s: %w", database, err)
	}
	if entriesS != "" {
		if err := json.Unmarshal([]byte(entriesS), &info.Entries); err != nil {
			return nil, fmt.Errorf("leer entradas de cadena: %w", err)
		}
	}
	return &info, nil
}

// Close cierra el pool subyacente. El control plane comparte su pool: ahí
// no debe usarse (usar NewPGCatalogShared con Close no-op si hace falta).
func (c *PGCatalog) Close() error {
	c.pool.Close()
	return nil
}

// newBackupID genera un id aleatorio (la plataforma lo sustituye por UUID
// cuando registra el backup).
func newBackupID() (string, error) {
	var b [12]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("generar id de backup: %w", err)
	}
	return "sql_" + hex.EncodeToString(b[:]), nil
}

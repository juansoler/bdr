-- 0002_sql_catalog.sql — Catálogo del motor SQL Server (RF-41).
-- Tablas sql_backups y sql_backup_chains (metadata.Registry).
--
-- ¡EN SYNC con internal/metadata/pgcatalog.go (EnsureSchemaDDL)!: la DDL
-- es idempotente en ambos sitios para poder aplicarla desde el server
-- (migración) o desde el lab sin server (EnsureSchema).
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

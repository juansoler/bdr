package connector

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	_ "github.com/microsoft/go-mssqldb"
)

// DB es una conexión a SQL Server envuelta para pruebas y logging.
type DB struct {
	*sql.DB
	params ConnectionParams
}

// Open abre una conexión a SQL Server usando los parámetros dados.
// Usa Windows Auth (SSPI) si User está vacío, o SQL Auth en caso contrario.
func Open(ctx context.Context, params ConnectionParams) (*DB, error) {
	dsn := buildDSN(params)
	db, err := sql.Open("sqlserver", dsn)
	if err != nil {
		return nil, fmt.Errorf("abrir conexión a %s: %w", params.Server, err)
	}
	db.SetConnMaxLifetime(5 * time.Minute)
	db.SetMaxOpenConns(1)
	// Ping con timeout para validar la conexión
	ctxPing, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := db.PingContext(ctxPing); err != nil {
		db.Close()
		return nil, fmt.Errorf("ping a %s: %w", params.Server, err)
	}
	return &DB{DB: db, params: params}, nil
}

// buildDSN construye la cadena de conexión para go-mssqldb.
// Ver: https://github.com/microsoft/go-mssqldb#connection-string-parameters
func buildDSN(p ConnectionParams) string {
	if p.User != "" {
		// SQL Auth
		return fmt.Sprintf("sqlserver://%s:%s@%s/%s?database=master&encrypt=disable",
			p.User, p.Password, p.Server, p.Instance)
	}
	// Windows Auth (SSPI): sin usuario/contraseña en el DSN, el driver usa
	// la identidad del proceso que ejecuta el agente (cuenta de servicio).
	return fmt.Sprintf("sqlserver://%s/%s?database=master&encrypt=disable&trusted=yes",
		p.Server, p.Instance)
}

// Close cierra la conexión y libera recursos.
func (d *DB) Close() error {
	return d.DB.Close()
}

// Params devuelve los parámetros de conexión usados.
func (d *DB) Params() ConnectionParams {
	return d.params
}

// ExecContext ejecuta una sentencia que no devuelve filas (BACKUP, RESTORE, etc.).
func (d *DB) ExecContext(ctx context.Context, query string) error {
	_, err := d.DB.ExecContext(ctx, query)
	return err
}

// QueryContext ejecuta una consulta que devuelve filas.
func (d *DB) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	return d.DB.QueryContext(ctx, query, args...)
}

// Ping verifica que la conexión sigue viva.
func (d *DB) Ping(ctx context.Context) error {
	return d.DB.PingContext(ctx)
}

// ListDatabases enumera las bases de datos de la instancia.
// Incluye las del sistema (master, model, msdb) y excluye tempdb (decisión A-12).
func (d *DB) ListDatabases(ctx context.Context) ([]Database, error) {
	const q = `
SELECT name,
       recovery_model_desc,
       state_desc,
       CONVERT(bit, is_read_only),
       compatibility_level,
       CASE WHEN database_id <= 4 THEN 1 ELSE 0 END AS is_system
FROM sys.databases
WHERE name <> 'tempdb'
ORDER BY name`

	rows, err := d.QueryContext(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("consultar sys.databases: %w", err)
	}
	defer rows.Close()

	var result []Database
	for rows.Next() {
		var db Database
		if err := rows.Scan(&db.Name, &db.RecoveryModel, &db.State,
			&db.ReadOnly, &db.CompatLevel, &db.System); err != nil {
			return nil, fmt.Errorf("leer fila sys.databases: %w", err)
		}
		// Una base es protegible si está ONLINE (offline/restoring no admiten BACKUP).
		db.Protectable = db.State == "ONLINE"
		db.UpdatedAt = time.Now()
		result = append(result, db)
	}
	return result, rows.Err()
}

// ListBackupChain consulta msdb.dbo.backupset para reconstruir la cadena de
// backups de una base, ordenada cronológicamente (más antiguo primero).
func (d *DB) ListBackupChain(ctx context.Context, database string) ([]ChainEntry, error) {
	const q = `
SELECT type,
       CONVERT(varchar(30), backup_start_date, 127)  AS backup_start,
       CONVERT(varchar(30), backup_finish_date, 127) AS backup_finish,
       database_backup_lsn,
       checkpoint_lsn,
       first_lsn,
       last_lsn,
       is_copy_only,
       compressed_backup_size
FROM msdb.dbo.backupset
WHERE database_name = @db
ORDER BY backup_start_date ASC`

	rows, err := d.QueryContext(ctx, q, sql.Named("db", database))
	if err != nil {
		return nil, fmt.Errorf("consultar msdb.dbo.backupset: %w", err)
	}
	defer rows.Close()

	var result []ChainEntry
	for rows.Next() {
		var e ChainEntry
		if err := rows.Scan(&e.Type, &e.BackupStart, &e.BackupFinish,
			&e.DatabaseLSN, &e.CheckpointLSN, &e.FirstLSN, &e.LastLSN,
			&e.CopyOnly, &e.CompressedBytes); err != nil {
			return nil, fmt.Errorf("leer fila backupset: %w", err)
		}
		result = append(result, e)
	}
	return result, rows.Err()
}

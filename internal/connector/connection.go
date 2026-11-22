package connector

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"strings"
	"time"

	_ "github.com/microsoft/go-mssqldb"
)

// DB es una conexión a SQL Server envuelta para pruebas y logging.
type DB struct {
	*sql.DB
	params ConnectionParams
}

// normalized corrige las trampas habituales de los parámetros de conexión
// (hallazgo del lab P-24, Win11 + SQL Server 2022):
//
//   - "MSSQLSERVER" es la instancia por defecto y NO se nombra: conectar a
//     "host\MSSQLSERVER" falla con error 87 ("cadena de conexión no es
//     válida"). Se elimina del DSN.
//   - "host\instancia" en Server (el panel lo permite): se separa si la
//     instancia no vino en su campo.
func (p ConnectionParams) normalized() ConnectionParams {
	q := p
	if i := strings.LastIndex(q.Server, `\`); i >= 0 {
		inst := strings.TrimSpace(q.Server[i+1:])
		if q.Instance == "" {
			q.Instance = inst
		}
		q.Server = strings.TrimSpace(q.Server[:i])
	}
	if strings.EqualFold(strings.TrimSpace(q.Instance), "MSSQLSERVER") {
		q.Instance = ""
	}
	q.Instance = strings.TrimSpace(q.Instance)
	return q
}

// Open abre una conexión a SQL Server usando los parámetros dados.
// Usa Windows Auth (SSPI) si User está vacío, o SQL Auth en caso contrario.
func Open(ctx context.Context, params ConnectionParams) (*DB, error) {
	params = params.normalized()
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
// Normaliza los parámetros (MSSQLSERVER = instancia por defecto,
// host\instancia en server) antes de componer la URL.
// Ver: https://github.com/microsoft/go-mssqldb#connection-string-parameters
func buildDSN(p ConnectionParams) string {
	p = p.normalized()
	if p.User != "" {
		// SQL Auth: usuario y contraseña van en la URL, hay que escaparlos
		// (contraseñas con @, / o : romperían el parseo del DSN).
		user := url.PathEscape(p.User)
		pass := url.PathEscape(p.Password)
		return fmt.Sprintf("sqlserver://%s:%s@%s/%s?database=master&encrypt=disable",
			user, pass, p.Server, p.Instance)
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

// QueryRowContext ejecuta una consulta que devuelve como máximo una fila.
func (d *DB) QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row {
	return d.DB.QueryRowContext(ctx, query, args...)
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
//
// Los campos *_lsn son numeric(25,0): se CASTean a varchar y se parsean en
// Go (parseLSN) para no depender de cómo el driver represente el decimal.
func (d *DB) ListBackupChain(ctx context.Context, database string) ([]ChainEntry, error) {
	const q = `
SELECT type,
       CONVERT(varchar(30), backup_start_date, 127)  AS backup_start,
       CONVERT(varchar(30), backup_finish_date, 127) AS backup_finish,
       CAST(database_backup_lsn   AS varchar(30)),
       CAST(checkpoint_lsn        AS varchar(30)),
       CAST(first_lsn             AS varchar(30)),
       CAST(last_lsn              AS varchar(30)),
       CONVERT(bit, is_copy_only),
       CAST(compressed_backup_size AS varchar(30))
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
		var dbLSN, ckptLSN, firstLSN, lastLSN, compressed string
		if err := rows.Scan(&e.Type, &e.BackupStart, &e.BackupFinish,
			&dbLSN, &ckptLSN, &firstLSN, &lastLSN,
			&e.CopyOnly, &compressed); err != nil {
			return nil, fmt.Errorf("leer fila backupset: %w", err)
		}
		if e.DatabaseLSN, err = parseLSN(dbLSN); err != nil {
			return nil, err
		}
		if e.CheckpointLSN, err = parseLSN(ckptLSN); err != nil {
			return nil, err
		}
		if e.FirstLSN, err = parseLSN(firstLSN); err != nil {
			return nil, err
		}
		if e.LastLSN, err = parseLSN(lastLSN); err != nil {
			return nil, err
		}
		if cbLSN, err := parseLSN(compressed); err != nil {
			return nil, err
		} else {
			e.CompressedBytes = uint64(cbLSN)
		}
		result = append(result, e)
	}
	return result, rows.Err()
}

// LastBackupLSNs devuelve los LSN del backup más reciente registrado en
// msdb.dbo.backupset para la base indicada (cualquier tipo: D, I o L).
// Se ejecuta tras cada BACKUP para capturar los LSN reales del catálogo.
func (d *DB) LastBackupLSNs(ctx context.Context, database string) (LSNSet, error) {
	const q = `
SELECT TOP 1 CAST(database_backup_lsn AS varchar(30)),
       CAST(first_lsn AS varchar(30)),
       CAST(last_lsn  AS varchar(30))
FROM msdb.dbo.backupset
WHERE database_name = @db
ORDER BY backup_finish_date DESC, first_lsn DESC`

	var set LSNSet
	var dbLSN, firstLSN, lastLSN string
	err := d.QueryRowContext(ctx, q, sql.Named("db", database)).
		Scan(&dbLSN, &firstLSN, &lastLSN)
	if err != nil {
		if err == sql.ErrNoRows {
			return set, fmt.Errorf("msdb no registra ningún backup de %s", database)
		}
		return set, fmt.Errorf("consultar LSN post-backup: %w", err)
	}
	if set.DatabaseLSN, err = parseLSN(dbLSN); err != nil {
		return set, err
	}
	if set.FirstLSN, err = parseLSN(firstLSN); err != nil {
		return set, err
	}
	if set.LastLSN, err = parseLSN(lastLSN); err != nil {
		return set, err
	}
	return set, nil
}

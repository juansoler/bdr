// SQLRunner real: ejecuta el motor SQL Server embebido (internal/backup,
// internal/restore, internal/connector, internal/storage) en el host del
// agente. El agente corre con la cuenta de servicio del host SQL, así que
// la conexión usa siempre SSPI (Windows Auth): nunca viajan contraseñas SQL
// en los payloads de los jobs.
package jobs

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/juansoler/bdr/internal/backup"
	"github.com/juansoler/bdr/internal/connector"
	"github.com/juansoler/bdr/internal/metadata"
	"github.com/juansoler/bdr/internal/orchestrator"
	"github.com/juansoler/bdr/internal/restore"
	"github.com/juansoler/bdr/internal/storage"
	"github.com/juansoler/bdr/packages/contracts"
)

// SQLMotor implementa SQLRunner con el motor embebido.
type SQLMotor struct{}

// NewSQLMotor crea el runner SQL.
func NewSQLMotor() *SQLMotor { return &SQLMotor{} }

var _ SQLRunner = (*SQLMotor)(nil)

// sqlStagingRoot devuelve la carpeta de staging donde SQL Server escribirá
// los backups y donde se descargan los ficheros para restaurar. Debe ser
// accesible por la cuenta de servicio de SQL Server, no solo por el agente
// (BDR_SQL_STAGING para sobreescribir).
func sqlStagingRoot() string {
	if p := os.Getenv("BDR_SQL_STAGING"); p != "" {
		return p
	}
	return filepath.Join(os.TempDir(), "bdr-sql-staging")
}

func sqlOpen(ctx context.Context, server, instance string) (*connector.DB, error) {
	db, err := connector.Open(ctx, connector.ConnectionParams{Server: server, Instance: instance})
	if err != nil {
		return nil, &CodeError{Code: "E_SQL_CONNECT", Message: err.Error()}
	}
	return db, nil
}

// sqlTargetTransferer valida el destino y construye el transferer. Los
// destinos Kopia server no sirven para el motor SQL (no son filesystem/S3).
func sqlTargetTransferer(ctx context.Context, creds *contracts.RepositoryCredentials, retainDays int) (storage.Transferer, error) {
	if creds == nil {
		return nil, &CodeError{Code: "E_VALIDATION",
			Message: "el job SQL requiere un destino de almacenamiento (storage_target_id)"}
	}
	tr, err := storage.NewTransfererFor(ctx, creds, retainDays)
	if err != nil {
		return nil, &CodeError{Code: "E_STORAGE", Message: err.Error()}
	}
	return tr, nil
}

// sqlBackupType normaliza el tipo del payload a backup.Type (se aceptan
// "copy-only" y "copy_only", como en el CLI).
func sqlBackupType(s string) (backup.Type, bool, error) {
	t := backup.Type(strings.ReplaceAll(strings.ToUpper(strings.TrimSpace(s)), "-", "_"))
	switch t {
	case backup.TypeFull, backup.TypeDifferential, backup.TypeLog, backup.TypeCopyOnly:
		return t, t == backup.TypeCopyOnly, nil
	case "":
		return backup.TypeFull, false, nil
	default:
		return "", false, &CodeError{Code: "E_VALIDATION",
			Message: "tipo de backup SQL no soportado: " + s + " (full|diff|log|copy-only)"}
	}
}

// formatLSN serializa un LSN como texto (vacío si es 0: omitempty).
func formatLSN(l connector.LSN) string {
	if l == 0 {
		return ""
	}
	return strconv.FormatUint(uint64(l), 10)
}

// Backup ejecuta un job SQL_BACKUP.
func (m *SQLMotor) Backup(ctx context.Context, creds *contracts.RepositoryCredentials,
	payload contracts.SQLBackupJobPayload, progress func(percent int, msg string, bytesDone int64)) (json.RawMessage, error) {

	progress(5, "conectando a SQL Server ("+payload.Server+")", 0)
	db, err := sqlOpen(ctx, payload.Server, payload.Instance)
	if err != nil {
		return nil, err
	}
	defer db.Close()

	progress(10, "preparando staging y destino", 0)
	sm, err := backup.NewStagingManager(sqlStagingRoot())
	if err != nil {
		return nil, &CodeError{Code: "E_INTERNAL", Message: err.Error()}
	}
	tr, err := sqlTargetTransferer(ctx, creds, payload.RetentionDays)
	if err != nil {
		return nil, err
	}
	defer tr.Close()

	t, copyOnly, err := sqlBackupType(payload.Type)
	if err != nil {
		return nil, err
	}
	verify := payload.Verify == nil || *payload.Verify

	progress(15, "BACKUP "+string(t)+" de "+payload.Database, 0)
	// Catálogo en memoria del proceso: el registro duradero lo hace el
	// control plane al ingestar el resultado del job (RestorePoint SQL).
	orch := orchestrator.New(db, sm, tr, metadata.NewInMemoryCatalog(), payload.Database)
	res, repoURI, err := orch.RunBackup(ctx, t, copyOnly, verify)
	if err != nil {
		return nil, &CodeError{Code: "E_JOB_FAILED", Message: err.Error()}
	}

	progress(90, "capturando LSNs de msdb", res.SizeBytes)
	lsns, lerr := db.LastBackupLSNs(ctx, payload.Database)
	if lerr != nil {
		// El fichero ya está en el repositorio; LSNs ausentes se reportan
		// vacíos y el administrador puede re-capturarlos con el CLI.
		progress(95, "aviso: no se pudieron capturar LSNs ("+lerr.Error()+")", res.SizeBytes)
		lsns = connector.LSNSet{}
	}

	progress(100, "backup completado", res.SizeBytes)
	out, err := json.Marshal(contracts.SQLBackupJobResult{
		Database:     payload.Database,
		Type:         string(t),
		RepoURI:      repoURI,
		SizeBytes:    res.SizeBytes,
		DurationSec:  res.DurationSec,
		Checksum:     res.Checksum,
		Compression:  res.Compression,
		Verified:     res.Verified,
		DatabaseLSN:  formatLSN(lsns.DatabaseLSN),
		FirstLSN:     formatLSN(lsns.FirstLSN),
		LastLSN:      formatLSN(lsns.LastLSN),
		BackupStart:  res.StartedAt.UTC().Format(time.RFC3339),
		BackupFinish: res.FinishedAt.UTC().Format(time.RFC3339),
	})
	if err != nil {
		return nil, &CodeError{Code: "E_INTERNAL", Message: err.Error()}
	}
	return out, nil
}

// Restore ejecuta un job SQL_RESTORE: descarga la cadena al staging y
// restaura con la secuencia nativa. master se rechaza en la librería.
func (m *SQLMotor) Restore(ctx context.Context, creds *contracts.RepositoryCredentials,
	payload contracts.SQLRestoreJobPayload, progress func(percent int, msg string, bytesDone int64)) (json.RawMessage, error) {

	if len(payload.Files) == 0 {
		return nil, &CodeError{Code: "E_VALIDATION", Message: "el restore SQL requiere al menos un fichero (files)"}
	}

	progress(5, "conectando a SQL Server ("+payload.Server+")", 0)
	db, err := sqlOpen(ctx, payload.Server, payload.Instance)
	if err != nil {
		return nil, err
	}
	defer db.Close()

	tr, err := sqlTargetTransferer(ctx, creds, 0)
	if err != nil {
		return nil, err
	}
	defer tr.Close()

	// Descargar la cadena en orden (full -> diff -> logs).
	var staged []string
	for i, uri := range payload.Files {
		name := filepath.Base(strings.ReplaceAll(strings.ReplaceAll(uri, `\`, "/"), "//", "/"))
		dest := filepath.Join(sqlStagingRoot(), "restore_"+name)
		progress(10+(60*(i+1))/len(payload.Files), fmt.Sprintf("descargando %s (%d/%d)", name, i+1, len(payload.Files)), 0)
		if err := tr.Fetch(ctx, uri, dest); err != nil {
			return nil, &CodeError{Code: "E_STORAGE", Message: err.Error()}
		}
		staged = append(staged, dest)
	}
	defer func() {
		for _, f := range staged {
			_ = os.Remove(f)
		}
	}()

	var stopT *time.Time
	if payload.StopAt != "" {
		t, perr := time.Parse(time.RFC3339, payload.StopAt)
		if perr != nil {
			return nil, &CodeError{Code: "E_VALIDATION", Message: "stop_at inválido (RFC3339): " + perr.Error()}
		}
		stopT = &t
	}

	progress(70, "RESTORE de "+payload.Database, 0)
	res, err := restore.Execute(ctx, db, restore.Options{
		Database:    payload.Database,
		SourceFiles: staged,
		Mode:        restore.Mode(strings.ToUpper(strings.TrimSpace(payload.Mode))),
		MoveData:    payload.MoveData,
		MoveLog:     payload.MoveLog,
		StopAt:      stopT,
		Stats:       10,
	})
	if err != nil {
		return nil, &CodeError{Code: "E_JOB_FAILED", Message: err.Error()}
	}

	progress(100, "restauración completada", 0)
	out, err := json.Marshal(contracts.SQLRestoreJobResult{
		Database:    res.Database,
		Files:       payload.Files,
		Mode:        string(res.Mode),
		DurationSec: res.DurationSec,
	})
	if err != nil {
		return nil, &CodeError{Code: "E_INTERNAL", Message: err.Error()}
	}
	return out, nil
}

// Verify ejecuta un job SQL_VERIFY: descarga el fichero del destino y
// lanza RESTORE VERIFYONLY WITH CHECKSUM (RF-44).
func (m *SQLMotor) Verify(ctx context.Context, creds *contracts.RepositoryCredentials,
	payload contracts.SQLVerifyJobPayload, progress func(percent int, msg string, bytesDone int64)) (json.RawMessage, error) {

	if payload.RepoURI == "" {
		return nil, &CodeError{Code: "E_VALIDATION", Message: "el verify SQL requiere repo_uri"}
	}

	progress(5, "conectando a SQL Server ("+payload.Server+")", 0)
	db, err := sqlOpen(ctx, payload.Server, payload.Instance)
	if err != nil {
		return nil, err
	}
	defer db.Close()

	tr, err := sqlTargetTransferer(ctx, creds, 0)
	if err != nil {
		return nil, err
	}
	defer tr.Close()

	dest := filepath.Join(sqlStagingRoot(), "verify_"+filepath.Base(strings.ReplaceAll(strings.ReplaceAll(payload.RepoURI, `\`, "/"), "//", "/")))
	defer func() { _ = os.Remove(dest) }()

	progress(30, "descargando "+filepath.Base(payload.RepoURI), 0)
	if err := tr.Fetch(ctx, payload.RepoURI, dest); err != nil {
		return nil, &CodeError{Code: "E_STORAGE", Message: err.Error()}
	}

	progress(70, "RESTORE VERIFYONLY", 0)
	start := time.Now()
	if err := backup.Verify(ctx, db, dest); err != nil {
		return nil, &CodeError{Code: "E_VERIFY_FAILED", Message: err.Error()}
	}

	progress(100, "verificación OK", 0)
	out, err := json.Marshal(contracts.SQLVerifyJobResult{
		RepoURI:     payload.RepoURI,
		OK:          true,
		DurationSec: time.Since(start).Seconds(),
	})
	if err != nil {
		return nil, &CodeError{Code: "E_INTERNAL", Message: err.Error()}
	}
	return out, nil
}

// Inventory ejecuta un job SQL_INVENTORY: lista las bases de la instancia.
func (m *SQLMotor) Inventory(ctx context.Context, payload contracts.SQLInventoryJobPayload) (json.RawMessage, error) {
	db, err := sqlOpen(ctx, payload.Server, payload.Instance)
	if err != nil {
		return nil, err
	}
	defer db.Close()

	dbs, err := db.ListDatabases(ctx)
	if err != nil {
		return nil, &CodeError{Code: "E_JOB_FAILED", Message: err.Error()}
	}
	items := make([]contracts.SQLDatabaseView, 0, len(dbs))
	for _, d := range dbs {
		items = append(items, contracts.SQLDatabaseView{
			Name:          d.Name,
			RecoveryModel: d.RecoveryModel,
			State:         d.State,
			ReadOnly:      d.ReadOnly,
			CompatLevel:   d.CompatLevel,
			System:        d.System,
			Protectable:   d.Protectable,
		})
	}
	out, err := json.Marshal(contracts.SQLInventoryJobResult{Items: items})
	if err != nil {
		return nil, &CodeError{Code: "E_INTERNAL", Message: err.Error()}
	}
	return out, nil
}

// sqlBracketId cita un identificador T-SQL con corchetes (escapa ]).
func sqlBracketId(name string) string {
	return "[" + strings.ReplaceAll(name, "]", "]]") + "]"
}

// asStringLocal normaliza un valor escaneado dinámicamente a texto.
func asStringLocal(v any) string {
	switch s := v.(type) {
	case string:
		return s
	case []byte:
		return string(s)
	case nil:
		return ""
	default:
		return fmt.Sprint(s)
	}
}

// CheckDB ejecuta un job SQL_CHECKDB: DBCC CHECKDB (NO_INFOMSGS,
// ALL_ERRORMSGS) sobre una base en vivo. La salida de DBCC llega como
// filas de mensajes; se escanea dinámicamente para no acoplarse al esquema
// exacto del result set. Con corrupción el job FALLA (E_CHECKDB_FAILED)
// con los mensajes: el control plane levanta alerta por el camino
// genérico de jobs fallidos (RF-44).
func (m *SQLMotor) CheckDB(ctx context.Context, payload contracts.SQLCheckDBJobPayload,
	progress func(percent int, msg string, bytesDone int64)) (json.RawMessage, error) {

	if payload.Database == "" {
		return nil, &CodeError{Code: "E_VALIDATION", Message: "el checkdb requiere database"}
	}

	progress(20, "conectando a SQL Server ("+payload.Server+")", 0)
	db, err := sqlOpen(ctx, payload.Server, payload.Instance)
	if err != nil {
		return nil, err
	}
	defer db.Close()

	progress(40, "DBCC CHECKDB de "+payload.Database, 0)
	start := time.Now()

	// Con NO_INFOMSGS una base sana no devuelve filas; la corrupción emite
	// filas de mensajes (Error/Level/State/MessageText/...).
	rows, err := db.QueryContext(ctx,
		"DBCC CHECKDB ("+sqlBracketId(payload.Database)+") WITH NO_INFOMSGS, ALL_ERRORMSGS")
	if err != nil {
		return nil, &CodeError{Code: "E_JOB_FAILED", Message: "DBCC CHECKDB: " + err.Error()}
	}
	defer rows.Close()

	cols, err := rows.Columns()
	if err != nil {
		return nil, &CodeError{Code: "E_JOB_FAILED", Message: "DBCC CHECKDB: " + err.Error()}
	}
	var messages []string
	for rows.Next() {
		vals := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return nil, &CodeError{Code: "E_JOB_FAILED", Message: "leer salida DBCC: " + err.Error()}
		}
		parts := make([]string, 0, len(cols))
		for _, v := range vals {
			if s := asStringLocal(v); s != "" {
				parts = append(parts, s)
			}
		}
		if len(parts) > 0 {
			messages = append(messages, strings.Join(parts, " | "))
		}
	}
	if err := rows.Err(); err != nil {
		return nil, &CodeError{Code: "E_JOB_FAILED", Message: "leer salida DBCC: " + err.Error()}
	}

	if len(messages) > 0 {
		return nil, &CodeError{Code: "E_CHECKDB_FAILED",
			Message: "DBCC CHECKDB reporta problemas en " + payload.Database + ": " + strings.Join(messages, " ;; ")}
	}

	progress(100, "CHECKDB limpio", 0)
	out, err := json.Marshal(contracts.SQLCheckDBJobResult{
		Database:    payload.Database,
		DurationSec: time.Since(start).Seconds(),
	})
	if err != nil {
		return nil, &CodeError{Code: "E_INTERNAL", Message: err.Error()}
	}
	return out, nil
}

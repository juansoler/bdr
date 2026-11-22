// PGMotor implementa el runner PostgreSQL del agente: pg_dump (formato
// custom comprimido) → transferencia al destino → catálogo. El restore
// usa pg_restore (con createdb opcional para motores limpios) y la
// verificación es pg_restore --list (integridad del archivo).
//
// Las contraseñas no viajan en los payloads: el proceso hijo hereda
// PGPASSWORD desde BDR_PG_PASSWORD del entorno del agente (o .pgpass).
package jobs

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/juansoler/bdr/internal/storage"
	"github.com/juansoler/bdr/packages/contracts"

	"github.com/jackc/pgx/v5"
)

// PGMotor implementa PGRunner.
type PGMotor struct{}

// NewPGMotor crea el runner PostgreSQL.
func NewPGMotor() *PGMotor { return &PGMotor{} }

var _ PGRunner = (*PGMotor)(nil)

// pgStagingRoot: carpeta donde pg_dump escribe y de la que se restauran
// los dumps. Aquí NO cruza cuentas de servicio (pg_dump lo ejecuta el
// propio agente), así que el temp del agente vale. BDR_PG_STAGING para
// sobreescribir.
func pgStagingRoot() string {
	if p := os.Getenv("BDR_PG_STAGING"); p != "" {
		return p
	}
	return filepath.Join(os.TempDir(), "bdr-pg-staging")
}

// pgPassword devuelve la contraseña para el proceso hijo (env del agente).
func pgPassword() string { return os.Getenv("BDR_PG_PASSWORD") }

// connEnv compone el entorno del proceso hijo con PGPASSWORD si la hay.
func connEnv() []string {
	if pw := pgPassword(); pw != "" {
		return append(os.Environ(), "PGPASSWORD="+pw)
	}
	return os.Environ()
}

// buildDumpArgs construye los argumentos de pg_dump (función pura, tests).
// Host vacío = socket local; sin user → usuario del proceso.
func buildDumpArgs(host, port, user, database, path string) []string {
	args := []string{"--format=custom"}
	if host != "" {
		args = append(args, "-h", host)
	}
	if port != "" {
		args = append(args, "-p", port)
	}
	if user != "" {
		args = append(args, "-U", user)
	}
	args = append(args, "-f", path, database)
	return args
}

// buildRestoreArgs construye los de pg_restore (función pura, tests).
func buildRestoreArgs(host, port, user, destDB, path string, dropExisting bool) []string {
	var args []string
	if host != "" {
		args = append(args, "-h", host)
	}
	if port != "" {
		args = append(args, "-p", port)
	}
	if user != "" {
		args = append(args, "-U", user)
	}
	if dropExisting {
		args = append(args, "--clean", "--if-exists")
	}
	return append(args, "-d", destDB, path)
}

// buildCreatedbArgs construye los de createdb (motor limpio).
func buildCreatedbArgs(host, port, user, destDB string) []string {
	var args []string
	if host != "" {
		args = append(args, "-h", host)
	}
	if port != "" {
		args = append(args, "-p", port)
	}
	if user != "" {
		args = append(args, "-U", user)
	}
	return append(args, destDB)
}

// pgBin localiza un binario de cliente PostgreSQL y devuelve el error
// accionable si no está instalado en el host.
func pgBin(name string) (string, error) {
	p, err := exec.LookPath(name)
	if err != nil {
		return "", &CodeError{Code: "E_PG_CLIENT",
			Message: "no se encontró " + name + " en el PATH del agente: instala el cliente PostgreSQL (postgresql-client / postgresql-client-16) en el host"}
	}
	return p, nil
}

// runPG ejecuta un binario de cliente con timeout y captura de salida.
func runPG(ctx context.Context, bin string, args []string) (string, error) {
	c, cancel := context.WithTimeout(ctx, 6*time.Hour)
	defer cancel()
	cmd := exec.CommandContext(c, bin, args...)
	cmd.Env = connEnv()
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(out.String())
		if c.Err() == context.DeadlineExceeded {
			return msg, fmt.Errorf("%s excedió el tiempo límite", bin)
		}
		if msg != "" {
			return msg, fmt.Errorf("%s: %s", bin, msg)
		}
		return msg, fmt.Errorf("%s: %w", bin, err)
	}
	return out.String(), nil
}

func pgTargetTransferer(ctx context.Context, creds *contracts.RepositoryCredentials, retainDays int) (storage.Transferer, error) {
	tr, err := storage.NewTransfererFor(ctx, creds, retainDays)
	if err != nil {
		return nil, &CodeError{Code: "E_STORAGE", Message: err.Error()}
	}
	return tr, nil
}

// pgRepoName construye el nombre de fichero del dump en el staging.
func pgRepoName(database string, t time.Time) string {
	return fmt.Sprintf("%s_PGDUMP_%s.dump", database, t.UTC().Format("20060102_150405"))
}

// Dump ejecuta un job PG_DUMP.
func (m *PGMotor) Dump(ctx context.Context, creds *contracts.RepositoryCredentials,
	payload contracts.PGDumpJobPayload, progress func(percent int, msg string, bytesDone int64)) (json.RawMessage, error) {

	if payload.Database == "" {
		return nil, &CodeError{Code: "E_VALIDATION", Message: "el dump PG requiere database"}
	}

	bin, err := pgBin("pg_dump")
	if err != nil {
		return nil, err
	}
	tr, err := pgTargetTransferer(ctx, creds, payload.RetentionDays)
	if err != nil {
		return nil, err
	}
	defer tr.Close()

	if err := os.MkdirAll(pgStagingRoot(), 0o750); err != nil {
		return nil, &CodeError{Code: "E_INTERNAL", Message: err.Error()}
	}
	path := filepath.Join(pgStagingRoot(), pgRepoName(payload.Database, time.Now()))
	defer func() { _ = os.Remove(path) }()

	progress(15, "pg_dump "+payload.Database, 0)
	start := time.Now()
	if _, err := runPG(ctx, bin, buildDumpArgs(payload.Host, payload.Port, payload.User, payload.Database, path)); err != nil {
		return nil, &CodeError{Code: "E_JOB_FAILED", Message: err.Error()}
	}

	fi, err := os.Stat(path)
	if err != nil {
		return nil, &CodeError{Code: "E_JOB_FAILED", Message: "stat del dump: " + err.Error()}
	}
	progress(60, "dump generado ("+strconv.FormatInt(fi.Size(), 10)+" bytes); transfiriendo", fi.Size())

	uri, err := tr.Transfer(ctx, path)
	if err != nil {
		return nil, &CodeError{Code: "E_STORAGE", Message: err.Error()}
	}

	// Verificación barata del archivo recién creado (índice del dump).
	verified := true
	if vb, verr := pgBin("pg_restore"); verr == nil {
		if _, verr := runPG(ctx, vb, []string{"--list", path}); verr != nil {
			verified = false
			progress(95, "aviso: el índice del dump no verificó limpio: "+verr.Error(), fi.Size())
		}
	}

	progress(100, "dump transferido", fi.Size())
	out, err := json.Marshal(contracts.PGDumpJobResult{
		Database:    payload.Database,
		RepoURI:     uri,
		SizeBytes:   fi.Size(),
		DurationSec: time.Since(start).Seconds(),
		Verified:    verified,
	})
	if err != nil {
		return nil, &CodeError{Code: "E_INTERNAL", Message: err.Error()}
	}
	return out, nil
}

// Verify ejecuta un job PG_VERIFY: descarga el dump y ejecuta
// pg_restore --list (integridad del archivo; RF-44 análogo).
func (m *PGMotor) Verify(ctx context.Context, creds *contracts.RepositoryCredentials,
	payload contracts.PGVerifyJobPayload, progress func(percent int, msg string, bytesDone int64)) (json.RawMessage, error) {

	if payload.RepoURI == "" {
		return nil, &CodeError{Code: "E_VALIDATION", Message: "el verify PG requiere repo_uri"}
	}
	bin, err := pgBin("pg_restore")
	if err != nil {
		return nil, err
	}
	tr, err := pgTargetTransferer(ctx, creds, 0)
	if err != nil {
		return nil, err
	}
	defer tr.Close()

	if err := os.MkdirAll(pgStagingRoot(), 0o750); err != nil {
		return nil, &CodeError{Code: "E_INTERNAL", Message: err.Error()}
	}
	dest := filepath.Join(pgStagingRoot(), "verify_"+filepath.Base(strings.ReplaceAll(strings.ReplaceAll(payload.RepoURI, `\`, "/"), "//", "/")))
	defer func() { _ = os.Remove(dest) }()

	progress(30, "descargando "+filepath.Base(payload.RepoURI), 0)
	if err := tr.Fetch(ctx, payload.RepoURI, dest); err != nil {
		return nil, &CodeError{Code: "E_STORAGE", Message: err.Error()}
	}

	progress(70, "pg_restore --list", 0)
	start := time.Now()
	outStr, err := runPG(ctx, bin, []string{"--list", dest})
	if err != nil {
		return nil, &CodeError{Code: "E_VERIFY_FAILED", Message: err.Error()}
	}
	objects := 0
	for _, line := range strings.Split(outStr, "\n") {
		if n := strings.TrimSpace(line); strings.HasPrefix(n, "1; ") || strings.Contains(n, ";") && !strings.HasPrefix(n, ";") {
			objects++
		}
	}

	progress(100, "verificación OK", 0)
	out, err := json.Marshal(contracts.PGVerifyJobResult{
		RepoURI:     payload.RepoURI,
		OK:          true,
		Objects:     objects,
		DurationSec: time.Since(start).Seconds(),
	})
	if err != nil {
		return nil, &CodeError{Code: "E_INTERNAL", Message: err.Error()}
	}
	return out, nil
}

// Restore ejecuta un job PG_RESTORE: descarga el dump y lo aplica con
// pg_restore sobre la base destino (createdb opcional para motores
// limpios; --clean --if-exists para reemplazo destructivo).
func (m *PGMotor) Restore(ctx context.Context, creds *contracts.RepositoryCredentials,
	payload contracts.PGRestoreJobPayload, progress func(percent int, msg string, bytesDone int64)) (json.RawMessage, error) {

	if payload.RepoURI == "" || payload.Database == "" {
		return nil, &CodeError{Code: "E_VALIDATION", Message: "el restore PG requiere repo_uri y database"}
	}
	rbin, err := pgBin("pg_restore")
	if err != nil {
		return nil, err
	}
	tr, err := pgTargetTransferer(ctx, creds, 0)
	if err != nil {
		return nil, err
	}
	defer tr.Close()

	if err := os.MkdirAll(pgStagingRoot(), 0o750); err != nil {
		return nil, &CodeError{Code: "E_INTERNAL", Message: err.Error()}
	}
	dest := filepath.Join(pgStagingRoot(), "restore_"+filepath.Base(strings.ReplaceAll(strings.ReplaceAll(payload.RepoURI, `\`, "/"), "//", "/")))
	defer func() { _ = os.Remove(dest) }()

	progress(30, "descargando "+filepath.Base(payload.RepoURI), 0)
	if err := tr.Fetch(ctx, payload.RepoURI, dest); err != nil {
		return nil, &CodeError{Code: "E_STORAGE", Message: err.Error()}
	}

	start := time.Now()
	if payload.CreateDB {
		cbin, cerr := pgBin("createdb")
		if cerr != nil {
			return nil, cerr
		}
		progress(55, "createdb "+payload.Database, 0)
		if _, err := runPG(ctx, cbin, buildCreatedbArgs(payload.Host, payload.Port, payload.User, payload.Database)); err != nil {
			return nil, &CodeError{Code: "E_JOB_FAILED", Message: err.Error()}
		}
	}

	progress(70, "pg_restore sobre "+payload.Database, 0)
	if _, err := runPG(ctx, rbin, buildRestoreArgs(payload.Host, payload.Port, payload.User, payload.Database, dest, payload.DropExisting)); err != nil {
		return nil, &CodeError{Code: "E_JOB_FAILED", Message: err.Error()}
	}

	progress(100, "restauración completada", 0)
	out, err := json.Marshal(contracts.PGRestoreJobResult{
		Database:    payload.Database,
		RepoURI:     payload.RepoURI,
		DurationSec: time.Since(start).Seconds(),
	})
	if err != nil {
		return nil, &CodeError{Code: "E_INTERNAL", Message: err.Error()}
	}
	return out, nil
}

// Inventory ejecuta un job PG_INVENTORY: lista de bases vía pgx.
func (m *PGMotor) Inventory(ctx context.Context, payload contracts.PGInventoryJobPayload) (json.RawMessage, error) {
	dsn := pgDSN(payload.Host, payload.Port, payload.User)
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		return nil, &CodeError{Code: "E_PG_CONNECT", Message: err.Error()}
	}
	defer conn.Close(ctx)

	rows, err := conn.Query(ctx, `
SELECT datname,
       pg_catalog.pg_get_userbyid(datdba),
       datistemplate,
       datallowconn
FROM pg_database
ORDER BY datname`)
	if err != nil {
		return nil, &CodeError{Code: "E_JOB_FAILED", Message: err.Error()}
	}
	defer rows.Close()

	items := []contracts.PGDatabaseView{}
	for rows.Next() {
		var v contracts.PGDatabaseView
		var isTemplate, allowConn bool
		if err := rows.Scan(&v.Name, &v.Owner, &isTemplate, &allowConn); err != nil {
			return nil, &CodeError{Code: "E_JOB_FAILED", Message: err.Error()}
		}
		v.System = isTemplate || v.Name == "postgres"
		v.Connect = allowConn
		v.Dumpable = allowConn && !isTemplate
		items = append(items, v)
	}
	if err := rows.Err(); err != nil {
		return nil, &CodeError{Code: "E_JOB_FAILED", Message: err.Error()}
	}
	out, err := json.Marshal(contracts.PGInventoryJobResult{Items: items})
	if err != nil {
		return nil, &CodeError{Code: "E_INTERNAL", Message: err.Error()}
	}
	return out, nil
}

// pgDSN compone la cadena de conexión de pgx (contraseña del entorno).
func pgDSN(host, port, user string) string {
	var b strings.Builder
	b.WriteString("postgres://")
	if user != "" {
		b.WriteString(user)
		b.WriteString("@")
	}
	h := host
	if h == "" {
		h = "127.0.0.1"
	}
	b.WriteString(h)
	if port != "" {
		b.WriteString(":")
		b.WriteString(port)
	}
	b.WriteString("/postgres")
	if pw := pgPassword(); pw != "" {
		b.WriteString("?password=")
		b.WriteString(pw)
	}
	return b.String()
}

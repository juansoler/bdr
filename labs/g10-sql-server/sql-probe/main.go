// sql-probe.go — G0.10: probe de SQL Server para el motor de la plataforma.
//
// Solo lectura salvo backup/restore/verify, que generan o restauran archivos
// de backup en laboratorio. `restore` exige -confirm.
//
// Uso: sql-probe <comando> [flags]
//
//	discover                          enumera instancias por registro (solo lectura)
//	inventory   -server [ -instance ]  lista bases, modelo de recuperación y estado
//	backup      -db -type full|diff|log -out [ -server ... ]
//	verify      -file                 RESTORE VERIFYONLY
//	chain       -db                   LSN y tipos desde msdb.dbo.backupset
//	restore     -file -database [-norecovery|-recovery] [-stopat] [-confirm]
//
// Autenticación: Windows (SSPI) por defecto; -user/-password para SQL Auth.
// Las credenciales solo por flags/variables de entorno, nunca en ficheros.
package main

import (
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"

	_ "github.com/microsoft/go-mssqldb"
	"golang.org/x/sys/windows/registry"
)

// ---- validación de identificadores y rutas -------------------------------

var idRe = regexp.MustCompile(`^[A-Za-z0-9_@#\-]{1,128}$`)
var pathRe = regexp.MustCompile(`^[A-Za-z]:\\[^<>:"|?*\x00]{1,240}$`)

func checkID(s string) string {
	if !idRe.MatchString(s) {
		fail("identificador no válido (se permite [A-Za-z0-9_@#-]): %q", s)
	}
	return s
}

func checkPath(s string) string {
	if !pathRe.MatchString(s) {
		fail("ruta no válida (unidad local absoluta): %q", s)
	}
	return s
}

func quoteID(s string) string { return "[" + strings.ReplaceAll(s, "]", "]]") + "]" }

func quoteStr(s string) string { return "N'" + strings.ReplaceAll(s, "'", "''") + "'" }

func fail(format string, a ...any) {
	fmt.Fprintf(os.Stderr, "ERROR: "+format+"\n", a...)
	os.Exit(1)
}

// ---- conexión -------------------------------------------------------------

func dsn(server, instance, user, pass string) string {
	if user != "" {
		return fmt.Sprintf("sqlserver://%s:%s@%s/%s",
			user, pass, server, instance)
	}
	// Windows Auth (SSPI): sin usuario, el driver usa la identidad del proceso.
	return fmt.Sprintf("sqlserver://%s/%s?database=master&encrypt=disable",
		server, instance)
}

func connect(server, instance, user, pass string) *sql.DB {
	if server == "" {
		fail("falta -server")
	}
	db, err := sql.Open("sqlserver", dsn(server, instance, user, pass))
	if err != nil {
		fail("abrir conexión: %v", err)
	}
	db.SetConnMaxLifetime(time.Minute)
	if err := db.Ping(); err != nil {
		fail("conectar a %s%s: %v", server, instance, err)
	}
	return db
}

// ---- discover: registro de Windows ---------------------------------------

type instance struct {
	Name    string `json:"name"`
	Key     string `json:"key"`
	Version string `json:"version,omitempty"`
	Edition string `json:"edition,omitempty"`
	SQLPath string `json:"sql_path,omitempty"`
}

func discover() {
	key, err := registry.OpenKey(registry.LOCAL_MACHINE,
		`SOFTWARE\Microsoft\Microsoft SQL Server\Instance Names\SQL`,
		registry.QUERY_VALUE)
	if err != nil {
		fail("no hay instancias registradas (¿SQL Server instalado?): %v", err)
	}
	defer key.Close()

	names, err := key.ReadValueNames(-1)
	if err != nil {
		fail("leer nombres de instancia: %v", err)
	}

	out := []instance{}
	for _, name := range names {
		inst := instance{Name: name}
		if v, _, _ := key.GetStringValue(name); v != "" {
			inst.Key = v
		}
		if inst.Key != "" {
			sk, err := registry.OpenKey(registry.LOCAL_MACHINE,
				`SOFTWARE\Microsoft\Microsoft SQL Server\`+inst.Key+`\Setup`,
				registry.QUERY_VALUE)
			if err == nil {
				inst.Version, _, _ = sk.GetStringValue("Version")
				inst.Edition, _, _ = sk.GetStringValue("Edition")
				inst.SQLPath, _, _ = sk.GetStringValue("SQLPath")
				sk.Close()
			}
		}
		out = append(out, inst)
	}
	emit(out)
}

// ---- inventory ------------------------------------------------------------

type dbRow struct {
	Name        string `json:"name"`
	Recovery    string `json:"recovery_model"`
	State       string `json:"state"`
	ReadOnly    bool   `json:"read_only"`
	CompatLevel int    `json:"compatibility_level"`
	System      bool   `json:"is_system"`
	Protectable bool   `json:"protectable"`
}

func inventory(db *sql.DB) {
	rows, err := db.Query(`
		SELECT name, recovery_model_desc, state_desc,
		       CONVERT(bit, is_read_only), compatibility_level,
		       CONVERT(bit, is_msdb) + CONVERT(bit, is_master) + CONVERT(bit, is_model) AS is_system
		FROM sys.databases
		WHERE database_id <> 2  -- tempdb excluida del inventario protegible
		ORDER BY name`)
	if err != nil {
		fail("consultar sys.databases: %v", err)
	}
	defer rows.Close()
	out := []dbRow{}
	for rows.Next() {
		var r dbRow
		if err := rows.Scan(&r.Name, &r.Recovery, &r.State, &r.ReadOnly,
			&r.CompatLevel, &r.System); err != nil {
			fail("escanear fila: %v", err)
		}
		// tempdb excluida; el resto es protegible si está ONLINE y accesible.
		r.Protectable = r.State == "ONLINE"
		out = append(out, r)
	}
	emit(out)
}

// ---- backup / verify / restore -------------------------------------------

func backup(db *sql.DB, database, backupType, path string) {
	d := quoteID(checkID(database))
	p := quoteStr(checkPath(path))
	var q string
	switch backupType {
	case "full":
		q = fmt.Sprintf("BACKUP DATABASE %s TO DISK = %s WITH COMPRESSION, CHECKSUM, STATS = 10", d, p)
	case "diff":
		q = fmt.Sprintf("BACKUP DATABASE %s TO DISK = %s WITH DIFFERENTIAL, COMPRESSION, CHECKSUM, STATS = 10", d, p)
	case "log":
		q = fmt.Sprintf("BACKUP LOG %s TO DISK = %s WITH COMPRESSION, CHECKSUM, STATS = 10", d, p)
	default:
		fail("-type debe ser full, diff o log")
	}
	start := time.Now()
	if _, err := db.Exec(q); err != nil {
		fail("BACKUP %s: %v", backupType, err)
	}
	fi, err := os.Stat(path)
	if err != nil {
		fail("backup creado pero no se pudo medir: %v", err)
	}
	emit(map[string]any{
		"database":   database,
		"type":       backupType,
		"path":       path,
		"duration_s": time.Since(start).Seconds(),
		"size_bytes": fi.Size(),
		"checksum":   true,
	})
}

func verify(db *sql.DB, path string) {
	q := fmt.Sprintf("RESTORE VERIFYONLY FROM DISK = %s WITH CHECKSUM", quoteStr(checkPath(path)))
	if _, err := db.Exec(q); err != nil {
		fail("VERIFYONLY: %v", err)
	}
	emit(map[string]any{"file": path, "verify": "OK"})
}

func restore(db *sql.DB, file, database string, norecovery, recovery bool, stopat string) {
	if strings.EqualFold(database, "master") {
		fail("master no se restaura con este probe: single-user manual (README §4)")
	}
	if norecovery == recovery && !norecovery {
		fail("indica -norecovery o -recovery")
	}
	q := fmt.Sprintf("RESTORE DATABASE %s FROM DISK = %s WITH REPLACE, %s",
		quoteID(checkID(database)), quoteStr(checkPath(file)),
		map[bool]string{true: "NORECOVERY", false: "RECOVERY"}[norecovery])
	if stopat != "" {
		t, err := time.Parse(time.RFC3339, stopat)
		if err != nil {
			fail("-stopat debe ser RFC3339 (2026-11-19T14:30:00Z): %v", err)
		}
		q += fmt.Sprintf(", STOPAT = %s", quoteStr(t.Format("2006-01-02T15:04:05")))
	}
	start := time.Now()
	if _, err := db.Exec(q); err != nil {
		fail("RESTORE: %v", err)
	}
	emit(map[string]any{"database": database, "file": file,
		"duration_s": time.Since(start).Seconds(),
		"mode":       map[bool]string{true: "NORECOVERY", false: "RECOVERY"}[norecovery]})
}

// ---- chain: msdb.dbo.backupset --------------------------------------------

type chainRow struct {
	Type            string `json:"type"` // D=full, I=diff, L=log
	BackupStart     string `json:"backup_start"`
	BackupFinish    string `json:"backup_finish"`
	DatabaseLSN     uint64 `json:"database_backup_lsn"`
	CheckpointLSN   uint64 `json:"checkpoint_lsn"`
	FirstLSN        uint64 `json:"first_lsn"`
	LastLSN         uint64 `json:"last_lsn"`
	CopyOnly        bool   `json:"copy_only"`
	CompressedBytes uint64 `json:"compressed_backup_size"`
}

func chain(db *sql.DB, database string) {
	rows, err := db.Query(`
		SELECT type, backup_start_date, backup_finish_date,
		       database_backup_lsn, checkpoint_lsn, first_lsn, last_lsn,
		       CONVERT(bit, is_copy_only), compressed_backup_size
		FROM msdb.dbo.backupset
		WHERE database_name = @p1
		ORDER BY backup_start_date DESC`, checkID(database))
	if err != nil {
		fail("consultar msdb.dbo.backupset: %v", err)
	}
	defer rows.Close()
	out := []chainRow{}
	for rows.Next() {
		var r chainRow
		if err := rows.Scan(&r.Type, &r.BackupStart, &r.BackupFinish,
			&r.DatabaseLSN, &r.CheckpointLSN, &r.FirstLSN, &r.LastLSN,
			&r.CopyOnly, &r.CompressedBytes); err != nil {
			fail("escanear backupset: %v", err)
		}
		out = append(out, r)
	}
	emit(out)
}

// ---- salida y main --------------------------------------------------------

func emit(v any) {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		fail("serializar salida: %v", err)
	}
}

func main() {
	server := flag.String("server", "", "host o host\\instancia")
	instance := flag.String("instance", "", "instancia con nombre (opcional)")
	user := flag.String("user", "", "login SQL (vacío = Windows Auth)")
	password := flag.String("password", "", "contraseña SQL (usar variable de entorno)")
	db := flag.String("db", "", "base de datos")
	backupType := flag.String("type", "", "full | diff | log")
	out := flag.String("out", "", "ruta del archivo de backup")
	file := flag.String("file", "", "archivo de backup")
	database := flag.String("database", "", "nombre de la base restaurada")
	norecovery := flag.Bool("norecovery", false, "dejar NORECOVERY")
	recovery := flag.Bool("recovery", false, "finalizar con RECOVERY")
	stopat := flag.String("stopat", "", "PITR RFC3339 (solo con RECOVERY)")
	confirm := flag.Bool("confirm", false, "obligatorio para restore")
	flag.Parse()

	if *password == "" && *user != "" {
		// Permite G10_SQL_PASSWORD sin exponerla en la línea de comandos.
		if env := os.Getenv("G10_SQL_PASSWORD"); env != "" {
			*password = env
		}
	}

	switch flag.Arg(0) {
	case "discover":
		discover()
	case "inventory":
		d := connect(*server, *instance, *user, *password)
		defer d.Close()
		inventory(d)
	case "backup":
		if *db == "" || *backupType == "" || *out == "" {
			fail("backup exige -db, -type y -out")
		}
		d := connect(*server, *instance, *user, *password)
		defer d.Close()
		backup(d, *db, *backupType, *out)
	case "verify":
		if *file == "" {
			fail("verify exige -file")
		}
		d := connect(*server, *instance, *user, *password)
		defer d.Close()
		verify(d, *file)
	case "chain":
		if *db == "" {
			fail("chain exige -db")
		}
		d := connect(*server, *instance, *user, *password)
		defer d.Close()
		chain(d, *db)
	case "restore":
		if *file == "" || *database == "" {
			fail("restore exige -file y -database")
		}
		if !*confirm {
			fail("restore es destructivo: repite con -confirm")
		}
		d := connect(*server, *instance, *user, *password)
		defer d.Close()
		restore(d, *file, *database, *norecovery, *recovery, *stopat)
	default:
		fail("comando: discover | inventory | backup | verify | chain | restore")
	}
}

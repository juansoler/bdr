package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/juansoler/bdr/internal/backup"
	"github.com/juansoler/bdr/internal/connector"
	"github.com/juansoler/bdr/internal/metadata"
	"github.com/juansoler/bdr/internal/orchestrator"
	"github.com/juansoler/bdr/internal/restore"
	"github.com/juansoler/bdr/internal/storage"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "Uso: bdr-sql <comando> [opciones]")
		fmt.Fprintln(os.Stderr, "  discover  - enumera instancias registradas")
		fmt.Fprintln(os.Stderr, "  inventory         - lista bases de datos de una instancia")
		fmt.Fprintln(os.Stderr, "  backup              - ejecuta backups de bases de datos")
		fmt.Fprintln(os.Stderr, "  chain               - muestra y valida la cadena de backups (msdb)")
		fmt.Fprintln(os.Stderr, "  restore                 - restaura una base de datos")
		fmt.Fprintln(os.Stderr, "  health                - verifica la conexión")
		fmt.Fprintln(os.Stderr, "  runbook-master        - genera el runbook de restauración de master (RF-46)")
		os.Exit(1)
	}

	ctx := context.Background()
	cmd := os.Args[1]
	args := os.Args[2:]

	var err error
	switch cmd {
	case "discover":
		err = runDiscover(ctx, args)
	case "inventory":
		err = runInventory(ctx, args)
	case "backup":
		err = runBackup(ctx, args)
	case "chain":
		err = runChain(ctx, args)
	case "runbook-master":
		err = runRunbookMaster(args)
	case "restore":
		err = runRestore(ctx, args)
	case "health":
		err = runHealth(ctx, args)
	default:
		fmt.Fprintf(os.Stderr, "comando desconocido: %s\n", cmd)
		os.Exit(2)
	}

	if err != nil {
		log.Fatalf("%v", err)
	}
}

func runDiscover(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("discover", flag.ExitOnError)
	fs.Parse(args)
	inst, err := connector.Discover(ctx)
	if err != nil {
		return err
	}
	for _, i := range inst {
		fmt.Printf("  %-20s  default=%v  version=%s  edition=%s\n", i.Name, i.Default, i.Version, i.Edition)
	}
	return nil
}

func runInventory(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("inventory", flag.ExitOnError)
	server := fs.String("server", "localhost", "host o host\\instancia")
	instance := fs.String("instance", "", "instancia con nombre (opcional)")
	user := fs.String("user", "", "login SQL (vacío = Windows Auth)")
	password := fs.String("password", "", "contraseña SQL (usar G10_SQL_PASSWORD)")
	fs.Parse(args)

	params := connectionParams(*server, *instance, *user, *password)
	db, err := connector.Open(ctx, params)
	if err != nil {
		return err
	}
	defer db.Close()
	dbs, err := db.ListDatabases(ctx)
	if err != nil {
		return err
	}
	for _, d := range dbs {
		fmt.Printf("  %-30s | %-8s | %-10s | Protectable:%v\n", d.Name, d.State, d.RecoveryModel, d.Protectable)
	}
	return nil
}

func runBackup(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("backup", flag.ExitOnError)
	server := fs.String("server", "localhost", "host o host\\instancia")
	instance := fs.String("instance", "", "instancia con nombre (opcional)")
	user := fs.String("user", "", "login SQL (vacío = Windows Auth)")
	password := fs.String("password", "", "contraseña SQL (usar G10_SQL_PASSWORD)")
	database := fs.String("db", "", "base de datos")
	bt := fs.String("type", "full", "tipo: full|diff|log|copy-only")
	verify := fs.Bool("verify", true, "verificar el medio tras el backup (RESTORE VERIFYONLY)")
	cert := fs.String("cert", "", "certificado en master para cifrar el backup (RF-45; debe estar custodiado)")
	algorithm := fs.String("algorithm", "AES_256", "algoritmo de cifrado: AES_128|AES_192|AES_256")
	stagingRoot := fs.String("staging", "/tmp/bdr-staging", "carpeta staging (accesible por SQL Server)")
	repoPath := fs.String("repo", "/tmp/bdr-repo", "carpeta destino repositorio local (NAS local)")
	target := fs.String("target", "", "destino alternativo: s3://endpoint/bucket[/prefijo] o //host/recurso[/ruta]; credenciales por entorno (BDR_S3_*, BDR_SMB_*)")
	retainDays := fs.Int("retain-days", 0, "Object Lock GOVERNANCE en S3 (días; 0 = sin bloqueo)")
	fs.Parse(args)

	if *database == "" {
		return fmt.Errorf("falta -db")
	}

	db, err := connector.Open(ctx, connectionParams(*server, *instance, *user, *password))
	if err != nil {
		return err
	}
	defer db.Close()

	sm, err := backup.NewStagingManager(*stagingRoot)
	if err != nil {
		return err
	}

	// Destino del repositorio: -target (S3/SMB con credenciales por entorno)
	// o -repo (ruta local del lab).
	var transferer storage.Transferer
	if *target != "" {
		transferer, err = storage.ParseTargetURI(*target, os.Getenv, *retainDays)
	} else {
		transferer = storage.NewTransferer(storage.Repository{Type: "nas", Path: *repoPath})
	}
	if err != nil {
		return err
	}
	defer transferer.Close()

	catalog := metadata.NewInMemoryCatalog()
	orch := orchestrator.New(db, sm, transferer, catalog, *database)

	// Normalización: se aceptan "copy-only" y "copy_only" (TypeCopyOnly es COPY_ONLY).
	t := backup.Type(strings.ReplaceAll(strings.ToUpper(*bt), "-", "_"))
	copyOnly := t == backup.TypeCopyOnly

	// Cifrado nativo (RF-45): el certificado DEBE estar custodiado antes
	// (bdr-sql certbackup) o el medio sería irrecuperable.
	var enc *backup.Encryption
	if *cert != "" {
		alg, aerr := backup.NormalizeAlgorithm(*algorithm)
		if aerr != nil {
			return aerr
		}
		enc = &backup.Encryption{Algorithm: alg, CertName: *cert}
	}

	res, repoFile, err := orch.RunBackup(ctx, t, copyOnly, *verify, enc)
	if err != nil {
		return err
	}
	if res != nil {
		j, _ := res.ToJSON()
		fmt.Println(string(j))
	}
	fmt.Printf("archivo guardado en repositorio: %s\n", repoFile)
	return nil
}

func runRestore(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("restore", flag.ExitOnError)
	var (
		database string
		server   string
		instance string
		user     string
		password string
		norecov  bool
		recover  bool
		stopat   string
		moveData string
		moveLog  string
		stats    int
		confirm  bool
		files    stringSlice
	)
	fs.StringVar(&server, "server", "localhost", "")
	fs.StringVar(&instance, "instance", "", "")
	fs.StringVar(&user, "user", "", "login SQL")
	fs.StringVar(&password, "password", "", "contraseña SQL")
	fs.StringVar(&database, "db", "", "base de datos destino")
	fs.BoolVar(&norecov, "norecovery", false, "NORECOVERY")
	fs.BoolVar(&recover, "recovery", false, "RECOVERY")
	fs.StringVar(&stopat, "stopat", "", "PITR RFC3339")
	fs.StringVar(&moveData, "move-data", "", "mover .mdf a ruta")
	fs.StringVar(&moveLog, "move-log", "", "mover .ldf a ruta")
	fs.IntVar(&stats, "stats", 10, "stats cada N")
	fs.BoolVar(&confirm, "confirm", false, "obligatorio (destructivo)")
	fs.Var(&files, "file", "archivo de backup (puede repetirse)")
	fs.Parse(args)

	if database == "" {
		return fmt.Errorf("falta -db")
	}
	if !confirm {
		return fmt.Errorf("restore requiere -confirm (destructivo)")
	}
	if len(files) == 0 {
		return fmt.Errorf("falta -file (puede repetirse)")
	}
	if norecov && recover {
		return fmt.Errorf("-norecovery y -recovery son excluyentes")
	}

	db, err := connector.Open(ctx, connectionParams(server, instance, user, password))
	if err != nil {
		return err
	}
	defer db.Close()

	mode := restore.Mode("")
	switch {
	case norecov:
		mode = restore.ModeNoRecovery
	case recover:
		mode = restore.ModeRecovery
	}

	var stopT *time.Time
	if stopat != "" {
		t, err := time.Parse(time.RFC3339, stopat)
		if err != nil {
			return fmt.Errorf("-stopat inválido: %w", err)
		}
		stopT = &t
	}

	// El restore no necesita staging/transfer/catálogo: ejecuta RESTORE
	// directamente contra la instancia.
	res, err := restore.Execute(ctx, db, restore.Options{
		Database:    database,
		SourceFiles: files,
		Mode:        mode,
		MoveData:    moveData,
		MoveLog:     moveLog,
		StopAt:      stopT,
		Stats:       stats,
	})
	if err != nil {
		return err
	}
	j, _ := json.MarshalIndent(res, "", "  ")
	fmt.Println(string(j))
	return nil
}

// runChain muestra la cadena de backups de una base (msdb.dbo.backupset)
// y valida la continuidad de LSN con restore.ValidateChain.
func runChain(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("chain", flag.ExitOnError)
	server := fs.String("server", "localhost", "host o host\\instancia")
	instance := fs.String("instance", "", "instancia con nombre (opcional)")
	user := fs.String("user", "", "login SQL (vacío = Windows Auth)")
	password := fs.String("password", "", "contraseña SQL (usar G10_SQL_PASSWORD)")
	database := fs.String("db", "", "base de datos")
	fs.Parse(args)

	if *database == "" {
		return fmt.Errorf("falta -db")
	}

	db, err := connector.Open(ctx, connectionParams(*server, *instance, *user, *password))
	if err != nil {
		return err
	}
	defer db.Close()

	chain, err := db.ListBackupChain(ctx, *database)
	if err != nil {
		return err
	}

	if err := restore.ValidateChain(chain); err != nil {
		fmt.Printf("cadena de %s: %d backups — INVÁLIDA: %v\n", *database, len(chain), err)
	} else {
		fmt.Printf("cadena de %s: %d backups — válida\n", *database, len(chain))
	}
	for _, e := range chain {
		fmt.Printf("  %s | %s -> %s | dbLSN=%d first=%d last=%d | copy_only=%v | %d bytes\n",
			e.Type, e.BackupStart, e.BackupFinish,
			e.DatabaseLSN, e.FirstLSN, e.LastLSN, e.CopyOnly, e.CompressedBytes)
	}
	return nil
}

// sqlMasterRunbook genera el runbook de restauración de master (RF-46).
// La restauración de master exige arrancar la instancia en single-user y no
// se automatiza: este texto es el procedimiento exacto para el operador,
// con el servidor y la ruta del medio ya interpolados. Función pura para
// poder testearla.
func sqlMasterRunbook(server, instance, backupFile string) string {
	svc := "MSSQLSERVER"
	exe := "sqlservr.exe"
	if instance != "" {
		svc = "MSSQL$" + strings.ToUpper(instance)
		exe = "sqlservr.exe -s " + strings.ToUpper(instance)
	}
	backupQ := strings.ReplaceAll(backupFile, "'", "''")
	return `# RUNBOOK — Restauración de master (RF-46)
# Servidor: ` + server + (map[bool]string{true: `\` + instance, false: ""}[instance != ""]) + `
# Medio:    ` + backupFile + `
#
# ADVERTENCIA: detener el servicio deja la instancia fuera de línea.
# Ejecutar desde una consola de administrador en el propio host SQL.

# 1) Detener el servicio de SQL Server.
NET STOP ` + svc + `

# 2) Arrancar la instancia en modo single-user (solo conexiones del admin).
#    La consola queda ocupada por el proceso; NO cerrarla.
` + exe + ` -m

# 3) Desde OTRO cmd (sqlcmd o SSMS) restaurar master:
sqlcmd -S "` + server + (map[bool]string{true: `\` + instance, false: ""}[instance != ""]) + `" -E -Q "RESTORE DATABASE master FROM DISK = N'` + backupQ + `' WITH REPLACE"

# 4) Detener el proceso single-user (Ctrl+C en la consola del paso 2).

# 5) Arrancar el servicio en modo normal y verificar.
NET START ` + svc + `
sqlcmd -S "` + server + (map[bool]string{true: `\` + instance, false: ""}[instance != ""]) + `" -E -Q "SELECT name, state_desc FROM sys.databases ORDER BY name"

# ORDEN DE RESTAURACIÓN DE LA INSTANCIA COMPLETA (RF-46):
#   1. master (este runbook, single-user manual)
#   2. model y msdb (FULL normal; pueden restaurarse con el motor estándar)
#   3. bases de usuario por cadena: FULL -> DIFFERENTIAL -> LOG con STOPAT
#   4. si los backups estaban CIFRADOS: restaurar primero el certificado
#      (CREATE CERTIFICATE ... FROM FILE + WITH PRIVATE KEY) con el .cer/.pvk
#      custodiados y su contraseña; sin él, los medios son irrecuperables.
`
}

// runRunbookMaster imprime el runbook de restauración de master.
func runRunbookMaster(args []string) error {
	fs := flag.NewFlagSet("runbook-master", flag.ExitOnError)
	server := fs.String("server", "localhost", "host o host\\instancia")
	instance := fs.String("instance", "", "instancia con nombre (opcional)")
	file := fs.String("file", "", "ruta del backup de master (.bak)")
	fs.Parse(args)
	if *file == "" {
		return fmt.Errorf("falta -file (ruta del backup de master)")
	}
	fmt.Print(sqlMasterRunbook(*server, *instance, *file))
	return nil
}

func runHealth(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("health", flag.ExitOnError)
	server := fs.String("server", "localhost", "")
	instance := fs.String("instance", "", "")
	fs.Parse(args)

	db, err := connector.Open(ctx, connectionParams(*server, *instance, "", ""))
	if err != nil {
		return err
	}
	defer db.Close()
	if err := db.Ping(ctx); err != nil {
		return err
	}
	fmt.Println("conexión OK")
	return nil
}

func connectionParams(server, instance, user, password string) connector.ConnectionParams {
	if user != "" && password == "" {
		if env := os.Getenv("G10_SQL_PASSWORD"); env != "" {
			password = env
		}
	}
	return connector.ConnectionParams{Server: server, Instance: instance, User: user, Password: password}
}

// stringSlice permite repetir flag -file
type stringSlice []string

func (s *stringSlice) String() string     { return strings.Join(*s, ", ") }
func (s *stringSlice) Set(v string) error { *s = append(*s, v); return nil }

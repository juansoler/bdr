package main

import (
	"context"
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
		fmt.Fprintln(os.Stderr, "  restore                 - restaura una base de datos")
		fmt.Fprintln(os.Stderr, "  health                - verifica la conexión")
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
	stagingRoot := fs.String("staging", "/tmp/bdr-staging", "carpeta staging (accesible por SQL Server)")
	repoPath := fs.String("repo", "/tmp/bdr-repo", "carpeta destino repositorio (NAS local)")
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
	transferer := storage.NewTransferer(storage.Repository{Type: "nas", Path: *repoPath})
	catalog := metadata.NewInMemoryCatalog()
	orch := orchestrator.New(db, sm, transferer, catalog, *database)

	t := backup.Type(strings.ToUpper(*bt))
	copyOnly := t == backup.TypeCopyOnly

	res, repoFile, err := orch.RunBackup(ctx, t, copyOnly)
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

	db, err := connector.Open(ctx, connectionParams(server, instance, user, password))
	if err != nil {
		return err
	}
	defer db.Close()

	sm, err := backup.NewStagingManager("/tmp/bdr-staging")
	if err != nil {
		return err
	}
	transferer := storage.NewTransferer(storage.Repository{Type: "nas", Path: "/tmp/bdr-repo"})
	orch := orchestrator.New(db, sm, transferer, metadata.NewInMemoryCatalog(), database)

	mode := restore.Mode("RECOVERY")
	if norecov {
		mode = restore.Mode("NORECOVERY")
	}

	var stopT *time.Time
	if stopat != "" {
		t, err := time.Parse(time.RFC3339, stopat)
		if err != nil {
			return fmt.Errorf("-stopat inválido: %w", err)
		}
		stopT = &t
	}

	opts := restore.Options{
		Database:    database,
		SourceFiles: files,
		Mode:        mode,
		MoveData:    moveData,
		MoveLog:     moveLog,
		StopAt:      stopT,
		Stats:       stats,
	}
	_, err = orch.RunRestore(ctx, opts)
	return err
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

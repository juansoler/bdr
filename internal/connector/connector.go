package connector

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"time"

	_ "github.com/microsoft/go-mssqldb"
)

type Instance struct {
	Name       string    `json:"name"`
	Key        string    `json:"key"`
	Version    string    `json:"version,omitempty"`
	Edition    string    `json:"edition,omitempty"`
	Default     bool      `json:"default"`
	DiscoveredAt time.Time `json:"discovered_at,omitempty"`
}

type Database struct {
	Name          string `json:"name"`
	RecoveryModel string `json:"recovery_model"`
	State         string `json:"state"`
	ReadOnly      bool   `json:"read_only"`
	CompatLevel   int    `json:"compatibility_level"`
	System        bool   `json:"is_system"`
	Protectable   bool   `json:"protectable"`
}

type ConnectionParams struct {
	Server   string
	Instance string
	User     string
	Password string
}

type DB struct {
	*sql.DB
	params ConnectionParams
}

func Open(ctx context.Context, params ConnectionParams) (*DB, error) {
	dsn := buildDSN(params)
	db, err := sql.Open("sqlserver", dsn)
	if err != nil {
		return nil, fmt.Errorf("abrir conexión: %w", err)
	}
	db.SetConnMaxLifetime(5 * time.Minute)
	db.SetMaxOpenConns(1)
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("ping: %w", err)
	}
	return &DB{DB: db, params: params}, nil
}

func buildDSN(p ConnectionParams) string {
	if p.User != "" {
		return fmt.Sprintf("sqlserver://%s:%s@%s/%s?database=master&encrypt=disable",
			p.User, p.Password, p.Server, p.Instance)
	}
	return fmt.Sprintf("sqlserver://%s/%s?database=master&encrypt=disable&trusted=yes",
		p.Server, p.Instance)
}

func (d *DB) Close() error {
	return d.DB.Close()
}

func (d *DB) Ping(ctx context.Context) error {
	return d.DB.PingContext(ctx)
}

func (d *DB) ListDatabases(ctx context.Context) ([]Database, error) {
	rows, err := d.QueryContext(ctx, `
		SELECT name, recovery_model_desc, state_desc,
		       CONVERT(bit, is_read_only), compatibility_level,
		       CONVERT(bit, is_msdb) + CONVERT(bit, is_master) + CONVERT(bit, is_model) AS is_system
		FROM sys.databases
		WHERE database_id <> 2
		ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []Database
	for rows.Next() {
		var db Database
		if err := rows.Scan(&db.Name, &db.RecoveryModel, &db.State, &db.ReadOnly,
			&db.CompatLevel, &db.System); err != nil {
			return nil, err
		}
		db.Protectable = db.State == "ONLINE"
		result = append(result, db)
	}
	return result, nil
}

func Discover(ctx context.Context) ([]Instance, error) {
	// Stub: return mock instances.
	return []Instance{
		{Name: "MSSQLSERVER", Key: "MSSQLSERVER", Version: "15.0", Edition: "Enterprise", Default: true, DiscoveredAt: time.Now()},
		{Name: "SQL2022", Key: "SQL2022", Version: "15.0", Edition: "Standard", Default: false, DiscoveredAt: time.Now()},
	}, nil
}

func connectionParams(server, instance, user, password string) ConnectionParams {
	if password == "" {
		if env := os.Getenv("G10_SQL_PASSWORD"); env != "" {
			password = env
		}
	}
	return ConnectionParams{
		Server:   server,
		Instance: instance,
		User:     user,
		Password: password,
	}
}
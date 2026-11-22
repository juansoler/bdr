package metadata

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/juansoler/bdr/internal/backup"
	"github.com/juansoler/bdr/internal/connector"

	"github.com/jackc/pgx/v5/pgxpool"
)

// TestPGCatalog verifica el catálogo PostgreSQL contra una base real.
// Requiere BDR_TEST_POSTGRES (DSN pgx, p. ej. docker run postgres:16);
// sin la variable los tests se omiten (mismo patrón que store).
func TestPGCatalog(t *testing.T) {
	dsn := os.Getenv("BDR_TEST_POSTGRES")
	if dsn == "" {
		t.Skip("BDR_TEST_POSTGRES no definida; tests de PostgreSQL omitidos")
	}
	ctx := context.Background()

	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("conectar a PostgreSQL: %v", err)
	}
	defer pool.Close()

	c := NewPGCatalog(pool)
	if err := c.EnsureSchema(ctx); err != nil {
		t.Fatalf("EnsureSchema: %v", err)
	}

	info := &BackupInfo{
		Database:    "app",
		Instance:    "sql-vm-01",
		BackupType:  backup.TypeFull,
		StartedAt:   time.Now().Add(-time.Minute),
		FinishedAt:  time.Now(),
		DurationSec: 42.5,
		SizeBytes:   123456,
		Checksum:    true,
		Compression: true,
		Verify:      true,
		LSN:         connector.LSN("10"),
		FirstLSN:    connector.LSN("100"),
		LastLSN:     connector.LSN("200"),
		Path:        "/staging/app_FULL.bak",
		RepoPath:    "s3://bdr/sql/app_FULL.bak",
		Tags:        map[string]string{"type": "FULL"},
	}
	if err := c.StoreBackup(ctx, info); err != nil {
		t.Fatalf("StoreBackup: %v", err)
	}
	if info.ID == "" {
		t.Fatal("StoreBackup debe asignar id")
	}

	got, err := c.GetBackup(ctx, info.ID)
	if err != nil {
		t.Fatalf("GetBackup: %v", err)
	}
	if got.Database != "app" || got.Instance != "sql-vm-01" {
		t.Errorf("backup leído: %+v", got)
	}
	if got.LSN != "10" || got.FirstLSN != "100" || got.LastLSN != "200" {
		t.Errorf("LSNs mal leídos: db=%q first=%q last=%q", got.LSN, got.FirstLSN, got.LastLSN)
	}
	if !got.Verify || !got.Compression || got.DurationSec != 42.5 {
		t.Errorf("metadatos mal leídos: %+v", got)
	}

	lst, err := c.ListBackups(ctx, "app")
	if err != nil || len(lst) == 0 {
		t.Fatalf("ListBackups: %v (%d)", err, len(lst))
	}

	// Cadena.
	chain := &ChainInfo{
		Instance:  "sql-vm-01",
		Database:  "app",
		Recovery:  "FULL",
		Validated: true,
		Entries: []ChainEntryInfo{{
			Type:        backup.TypeFull,
			DatabaseLSN: connector.LSN("10"),
			FirstLSN:    connector.LSN("100"),
			LastLSN:     connector.LSN("200"),
		}},
	}
	if err := c.StoreChain(ctx, chain); err != nil {
		t.Fatalf("StoreChain: %v", err)
	}
	gotChain, err := c.GetChain(ctx, "app")
	if err != nil {
		t.Fatalf("GetChain: %v", err)
	}
	if !gotChain.Validated || len(gotChain.Entries) != 1 || gotChain.Entries[0].LastLSN != "200" {
		t.Fatalf("cadena mal leída: %+v", gotChain)
	}

	if _, err := c.GetBackup(ctx, "nope"); err == nil {
		t.Error("GetBackup inexistente debe fallar")
	}
	if _, err := c.GetChain(ctx, "nope"); err == nil {
		t.Error("GetChain inexistente debe fallar")
	}

	// Limpieza de lo creado por este test (idempotencia entre ejecuciones).
	if _, err := pool.Exec(ctx,
		`DELETE FROM sql_backups WHERE id = $1`, info.ID); err != nil {
		t.Fatalf("limpiar backup: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`DELETE FROM sql_backup_chains WHERE instance = $1 AND database = $2`,
		chain.Instance, chain.Database); err != nil {
		t.Fatalf("limpiar cadena: %v", err)
	}

	if err := c.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

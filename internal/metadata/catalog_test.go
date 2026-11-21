package metadata

import (
	"context"
	"testing"
	"time"

	"github.com/juansoler/bdr/internal/backup"
	"github.com/juansoler/bdr/internal/connector"
)

func TestNewBackupInfoMapeaVerificaciónYLSNs(t *testing.T) {
	res := &backup.Result{
		SizeBytes:   1234,
		Checksum:    true,
		Compression: true,
		Verified:    true,
		FinishedAt:  time.Now(),
	}
	info := NewBackupInfo(res, "app", "inst1", backup.TypeFull,
		"/staging/app_FULL.bak", "/repo/app_FULL.bak",
		connector.LSN(10), connector.LSN(100), connector.LSN(200))

	if !info.Verify {
		t.Error("Verify debe heredar res.Verified")
	}
	if info.LSN != 10 || info.FirstLSN != 100 || info.LastLSN != 200 {
		t.Errorf("LSNs mal mapeados: db=%d first=%d last=%d", info.LSN, info.FirstLSN, info.LastLSN)
	}
	if !info.Checksum || !info.Compression {
		t.Error("Checksum/Compression no heredados del resultado")
	}
	if info.CopyOnly {
		t.Error("un FULL no debe marcarse CopyOnly")
	}

	co := NewBackupInfo(res, "app", "inst1", backup.TypeCopyOnly, "/s", "/r", 0, 0, 0)
	if !co.CopyOnly {
		t.Error("un COPY_ONLY debe marcarse CopyOnly")
	}
}

func TestInMemoryCatalog(t *testing.T) {
	ctx := context.Background()
	c := NewInMemoryCatalog()

	res := &backup.Result{SizeBytes: 1, FinishedAt: time.Now()}
	info := NewBackupInfo(res, "app", "inst1", backup.TypeFull, "/s", "/r", 1, 2, 3)
	if err := c.StoreBackup(ctx, info); err != nil {
		t.Fatalf("StoreBackup: %v", err)
	}
	if info.ID == "" {
		t.Fatal("StoreBackup debe asignar un ID")
	}

	got, err := c.GetBackup(ctx, info.ID)
	if err != nil {
		t.Fatalf("GetBackup: %v", err)
	}
	if got.Database != "app" || got.LastLSN != 3 {
		t.Fatalf("GetBackup devolvió %+v", got)
	}

	// ListBackups filtra por base de datos.
	otra := NewBackupInfo(res, "otra", "inst1", backup.TypeLog, "/s2", "/r2", 0, 0, 0)
	if err := c.StoreBackup(ctx, otra); err != nil {
		t.Fatalf("StoreBackup otra: %v", err)
	}
	lst, err := c.ListBackups(ctx, "app")
	if err != nil {
		t.Fatalf("ListBackups: %v", err)
	}
	if len(lst) != 1 || lst[0].Database != "app" {
		t.Fatalf("ListBackups(app) = %d elementos; esperaba 1 de app", len(lst))
	}

	// GetBackup inexistente.
	if _, err := c.GetBackup(ctx, "nope"); err == nil {
		t.Fatal("GetBackup inexistente debe fallar")
	}

	// Cadenas.
	chain := &ChainInfo{Database: "app"}
	if err := c.StoreChain(ctx, chain); err != nil {
		t.Fatalf("StoreChain: %v", err)
	}
	gotChain, err := c.GetChain(ctx, "app")
	if err != nil || gotChain.Database != "app" {
		t.Fatalf("GetChain: %+v, %v", gotChain, err)
	}
	if _, err := c.GetChain(ctx, "noexiste"); err == nil {
		t.Fatal("GetChain inexistente debe fallar")
	}

	if err := c.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

func TestChainEntryFromDBEntry(t *testing.T) {
	e := connector.ChainEntry{
		Type:            "D",
		BackupStart:     "2026-11-21T10:00:00",
		BackupFinish:    "2026-11-21T10:05:00",
		DatabaseLSN:     10,
		FirstLSN:        100,
		LastLSN:         200,
		CopyOnly:        false,
		CompressedBytes: 4096,
	}
	info := ChainEntryFromDBEntry(e)
	if info.Type != backup.TypeFull {
		t.Errorf("Type = %s; esperaba FULL (código D)", info.Type)
	}
	if !info.Compressed {
		t.Error("Compressed debe ser true con CompressedBytes > 0")
	}
	if info.LastLSN != 200 {
		t.Errorf("LastLSN = %d; esperaba 200", info.LastLSN)
	}

	// Códigos restantes y flag copy_only.
	if got := msdbTypeToBackupType("I"); got != backup.TypeDifferential {
		t.Errorf("I = %s; esperaba DIFFERENTIAL", got)
	}
	if got := msdbTypeToBackupType("L"); got != backup.TypeLog {
		t.Errorf("L = %s; esperaba LOG", got)
	}
	if got := msdbTypeToBackupType("X"); got != "X" {
		t.Errorf("código desconocido debe pasar tal cual; devolvió %s", got)
	}

	e.Type = "L"
	e.CopyOnly = true
	if info := ChainEntryFromDBEntry(e); !info.CopyOnly {
		t.Error("CopyOnly no propagado")
	}
}

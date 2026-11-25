// El estado de reanudación de restauraciones: marca streams y persiste
// atómicamente para que un reintento continúe donde quedó.
package jobs

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRestoreStateResume(t *testing.T) {
	dataDir := t.TempDir()
	key := "backup/pve1/web-105/2026-11-23/vm/manifest-cap-1.json"

	s := loadRestoreState(dataDir, key, "")
	if len(s.Done) != 0 {
		t.Fatalf("estado inicial con %d streams", len(s.Done))
	}

	s.Mark("disk0-seg0000-abc")
	s.Mark("disk0-seg0001-def")
	s.Mark("disk0-seg0000-abc") // duplicado: no debe repetirse
	if !s.Has("disk0-seg0000-abc") || !s.Has("disk0-seg0001-def") {
		t.Fatal("marks no registrados en memoria")
	}
	if len(s.Done) != 2 {
		t.Fatalf("duplicado en la lista: %d", len(s.Done))
	}

	// Simula un reintento (nueva carga desde disco).
	s2 := loadRestoreState(dataDir, key, "")
	if len(s2.Done) != 2 || !s2.Has("disk0-seg0001-def") {
		t.Fatalf("estado no persistió: %v", s2.Done)
	}

	// Un manifiesto distinto no comparte estado.
	s3 := loadRestoreState(dataDir, "backup/otro/manifest.json", "")
	if len(s3.Done) != 0 {
		t.Fatalf("estado cruzado entre manifiestos: %v", s3.Done)
	}

	// Al terminar bien se elimina.
	s2.Clear()
	if r := loadRestoreState(dataDir, key, ""); len(r.Done) != 0 {
		t.Fatalf("Clear no eliminó el estado: %v", r.Done)
	}

	// Sin DataDir el estado es solo en memoria (sin pánico).
	mem := loadRestoreState("", key, "")
	mem.Mark("x")
	if !mem.Has("x") {
		t.Fatal("mark en memoria falló")
	}
}

// TestRestoreStateAlcance: un mismo manifiesto restaurado sobre destinos
// distintos (otra VM, o como VM nueva) no comparte el estado de reanudación,
// y el estado huérfano de agentes anteriores (sin ámbito) no se reutiliza.
func TestRestoreStateAlcance(t *testing.T) {
	dataDir := t.TempDir()
	key := "backup/pve1/web-105/2026-11-23/vm/manifest-cap-1.json"

	huerfano := restoreStatePath(dataDir, key, "") // así lo guardaban los agentes <= 0.16.54
	if err := os.MkdirAll(filepath.Dir(huerfano), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(huerfano, []byte(`{"manifest_key":"`+key+`","done":["disk0-seg0000"]}`), 0o600); err != nil {
		t.Fatal(err)
	}

	inPlace := loadRestoreState(dataDir, key, "vm:105")
	if inPlace.Has("disk0-seg0000") {
		t.Fatal("un estado sin ámbito se aplicó a un destino con ámbito")
	}
	inPlace.Mark("disk1-seg0341")

	for _, scope := range []string{"vm:200", ""} {
		if loadRestoreState(dataDir, key, scope).Has("disk1-seg0341") {
			t.Fatalf("el estado de vm:105 se filtró al ámbito %q", scope)
		}
	}
	if !loadRestoreState(dataDir, key, "vm:105").Has("disk1-seg0341") {
		t.Fatal("el estado con ámbito no persistió")
	}
}

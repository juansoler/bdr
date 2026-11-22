// Test interno del constructor de cadenas de restauración (RF-42): función
// pura sobre puntos del store, sin SQL Server.
package control

import (
	"strings"
	"testing"
	"time"

	"github.com/juansoler/bdr/apps/server/internal/store"
)

func mkSQLRP(id, db, typ, first, last string, end time.Time) *store.RestorePoint {
	return &store.RestorePoint{
		ID: id, Kind: "sql", SnapshotID: "repo://" + id, JobID: "job-" + id,
		EndTime: end, CreatedAt: end,
		SQL: &store.SQLBackupMeta{
			Instance: "sqlhost", Database: db, BackupType: typ,
			FirstLSN: first, LastLSN: last,
		},
	}
}

func sqlPoints() map[string]*store.RestorePoint {
	base := time.Date(2026, 11, 22, 8, 0, 0, 0, time.UTC)
	return map[string]*store.RestorePoint{
		"full1":  mkSQLRP("full1", "app", "FULL", "100", "200", base),
		"diff1":  mkSQLRP("diff1", "app", "DIFFERENTIAL", "100", "500", base.Add(2*time.Hour)),
		"log1":   mkSQLRP("log1", "app", "LOG", "200", "300", base.Add(1*time.Hour)),
		"log2":   mkSQLRP("log2", "app", "LOG", "300", "400", base.Add(90*time.Minute)),
		"log3":   mkSQLRP("log3", "app", "LOG", "400", "600", base.Add(3*time.Hour)),
		"log4":   mkSQLRP("log4", "app", "LOG", "600", "700", base.Add(4*time.Hour)),
		"otra":   mkSQLRP("otra", "otra", "FULL", "10", "20", base),
		"coFull": mkSQLRP("coFull", "app", "COPY_ONLY", "900", "950", base.Add(5*time.Hour)),
	}
}

func TestBuildSQLRestoreChainCompleta(t *testing.T) {
	pts := sqlPoints()
	chain, err := buildSQLRestoreChain(sliceVals(pts), "app", nil)
	if err != nil {
		t.Fatalf("cadena completa: %v", err)
	}
	var ids []string
	for _, rp := range chain {
		ids = append(ids, rp.ID)
	}
	// full + el DIFF con base correcta + logs que extienden (log3, log4;
	// log1/log2 quedan cubiertos por el DIFF).
	want := "full1,diff1,log3,log4"
	if got := strings.Join(ids, ","); got != want {
		t.Fatalf("cadena = %s; esperaba %s", got, want)
	}
}

func TestBuildSQLRestoreChainSinDiff(t *testing.T) {
	pts := sqlPoints()
	delete(pts, "diff1")
	chain, err := buildSQLRestoreChain(sliceVals(pts), "app", nil)
	if err != nil {
		t.Fatalf("sin diff: %v", err)
	}
	var ids []string
	for _, rp := range chain {
		ids = append(ids, rp.ID)
	}
	if got := strings.Join(ids, ","); got != "full1,log1,log2,log3,log4" {
		t.Fatalf("cadena = %s", got)
	}
}

func TestBuildSQLRestoreChainStopAt(t *testing.T) {
	pts := sqlPoints()
	cut := time.Date(2026, 11, 22, 9, 30, 0, 0, time.UTC) // tras log1
	chain, err := buildSQLRestoreChain(sliceVals(pts), "app", &cut)
	if err != nil {
		t.Fatalf("stopAt: %v", err)
	}
	var ids []string
	for _, rp := range chain {
		ids = append(ids, rp.ID)
	}
	// El DIFF (10:00) es posterior al corte: full + log1 + log2 (log2
	// termina exactamente en el corte: incluido, STOPAT lo trunca).
	if got := strings.Join(ids, ","); got != "full1,log1,log2" {
		t.Fatalf("cadena con corte = %s", got)
	}
}

func TestBuildSQLRestoreChainCasosError(t *testing.T) {
	pts := sqlPoints()

	// Sin FULL de la base.
	delete(pts, "full1")
	if _, err := buildSQLRestoreChain(sliceVals(pts), "app", nil); err == nil {
		t.Fatal("sin FULL debe fallar")
	}

	// Hueco en el log: quitamos log3 (500-600) y dejamos log4 (600-700)
	// con el diff presente: tras diff1 (last=500), log4 empieza en 600 >
	// 500 → hueco detectado.
	pts = sqlPoints()
	delete(pts, "log3")
	if _, err := buildSQLRestoreChain(sliceVals(pts), "app", nil); err == nil {
		t.Fatal("hueco de log debe fallar")
	}

	// COPY_ONLY no sirve de base.
	pts = sqlPoints()
	delete(pts, "full1")
	pts["coFull2"] = mkSQLRP("coFull2", "app", "COPY_ONLY", "100", "200",
		time.Date(2026, 11, 22, 8, 0, 0, 0, time.UTC))
	if _, err := buildSQLRestoreChain(sliceVals(pts), "app", nil); err == nil {
		t.Fatal("un COPY_ONLY no debe usarse como base")
	}
}

func sliceVals(m map[string]*store.RestorePoint) []*store.RestorePoint {
	out := make([]*store.RestorePoint, 0, len(m))
	for _, v := range m {
		out = append(out, v)
	}
	return out
}

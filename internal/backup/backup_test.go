package backup

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestQuoteTSQL(t *testing.T) {
	if got := quoteTSQL("O'Brien"); got != `N'O''Brien'` {
		t.Fatalf("quoteTSQL(O'Brien) = %s; esperaba N'O''Brien'", got)
	}
	if got := quoteTSQL(""); got != `N''` {
		t.Fatalf("quoteTSQL(vacío) = %s; esperaba N''", got)
	}
}

func TestExt(t *testing.T) {
	if ext(TypeLog) != "trn" {
		t.Fatalf("ext(LOG) = %s; esperaba trn", ext(TypeLog))
	}
	for _, ty := range []Type{TypeFull, TypeDifferential, TypeCopyOnly} {
		if ext(ty) != "bak" {
			t.Fatalf("ext(%s) = %s; esperaba bak", ty, ext(ty))
		}
	}
}

func TestBuildBackupTSQL(t *testing.T) {
	cases := []struct {
		nombre   string
		database string
		t        Type
		copyOnly bool
		path     string
		quiere   []string
		prohibe  []string
	}{
		{
			nombre: "full", database: "app", t: TypeFull, path: `D:\bk\app.bak`,
			quiere:  []string{"BACKUP DATABASE N'app' TO DISK = N'D:\\bk\\app.bak'", "COMPRESSION", "CHECKSUM", "STATS = 10"},
			prohibe: []string{"COPY_ONLY", "DIFFERENTIAL", "BACKUP LOG"},
		},
		{
			nombre: "full con flag copy_only", database: "app", t: TypeFull, copyOnly: true, path: `D:\bk\app.bak`,
			quiere:  []string{"COPY_ONLY", "COMPRESSION, CHECKSUM"},
			prohibe: []string{"DIFFERENTIAL"},
		},
		{
			nombre: "differential", database: "app", t: TypeDifferential, path: `D:\bk\app.bak`,
			quiere:  []string{"WITH DIFFERENTIAL", "COMPRESSION, CHECKSUM", "STATS = 10"},
			prohibe: []string{"COPY_ONLY", "BACKUP LOG"},
		},
		{
			nombre: "log", database: "app", t: TypeLog, path: `D:\bk\app.trn`,
			quiere:  []string{"BACKUP LOG N'app'", "COMPRESSION, CHECKSUM"},
			prohibe: []string{"DIFFERENTIAL", "COPY_ONLY"},
		},
		{
			nombre: "tipo copy_only", database: "app", t: TypeCopyOnly, path: `D:\bk\app.bak`,
			quiere:  []string{"COPY_ONLY"},
			prohibe: []string{"DIFFERENTIAL"},
		},
		{
			nombre: "nombre con apóstrofo", database: "O'Brien", t: TypeFull, path: `D:\bk\ob.bak`,
			quiere: []string{"N'O''Brien'"},
		},
	}

	for _, c := range cases {
		t.Run(c.nombre, func(t *testing.T) {
			got := buildBackupTSQL(c.database, c.t, c.copyOnly, c.path)
			for _, want := range c.quiere {
				if !strings.Contains(got, want) {
					t.Errorf("T-SQL sin %q:\n%s", want, got)
				}
			}
			for _, bad := range c.prohibe {
				if strings.Contains(got, bad) {
					t.Errorf("T-SQL con %q inesperado:\n%s", bad, got)
				}
			}
		})
	}
}

func TestStagingManagerFullPathYCleanup(t *testing.T) {
	root := t.TempDir()
	sm, err := NewStagingManager(root)
	if err != nil {
		t.Fatalf("NewStagingManager: %v", err)
	}

	p1 := sm.FullPath("app", TypeFull)
	if filepath.Dir(p1) != root {
		t.Errorf("FullPath fuera del root: %s", p1)
	}
	if filepath.Ext(p1) != ".bak" {
		t.Errorf("extensión de full = %s; esperaba .bak", filepath.Ext(p1))
	}
	if base := filepath.Base(p1); !strings.HasPrefix(base, "app_FULL_") {
		t.Errorf("nombre %s; esperaba prefijo app_FULL_", base)
	}

	// Unicidad: si el fichero ya existe, el siguiente nombre es distinto.
	f, err := os.Create(p1)
	if err != nil {
		t.Fatalf("crear %s: %v", p1, err)
	}
	f.Close()
	p2 := sm.FullPath("app", TypeFull)
	if p2 == p1 {
		t.Fatalf("FullPath reutiliza un nombre existente: %s", p1)
	}

	// El log de transacciones lleva .trn.
	if ext := filepath.Ext(sm.FullPath("app", TypeLog)); ext != ".trn" {
		t.Errorf("extensión de log = %s; esperaba .trn", ext)
	}

	// Cleanup elimina el fichero de staging.
	if err := sm.Cleanup(p1); err != nil {
		t.Fatalf("Cleanup: %v", err)
	}
	if _, err := os.Stat(p1); !os.IsNotExist(err) {
		t.Errorf("Cleanup no eliminó %s", p1)
	}
}

package restore

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/juansoler/bdr/internal/connector"
)

func TestBuildRestoreTSQL(t *testing.T) {
	t.Run("recovery simple", func(t *testing.T) {
		got := buildRestoreTSQL("app", `D:\bk\app.bak`, ModeRecovery, nil, nil, 10)
		want := `RESTORE DATABASE [app] FROM DISK = N'D:\bk\app.bak' WITH REPLACE, RECOVERY, STATS = 10`
		if got != want {
			t.Fatalf("got:\n%s\nwant:\n%s", got, want)
		}
	})

	t.Run("norecovery", func(t *testing.T) {
		got := buildRestoreTSQL("app", `D:\bk\app.bak`, ModeNoRecovery, nil, nil, 10)
		want := `RESTORE DATABASE [app] FROM DISK = N'D:\bk\app.bak' WITH REPLACE, NORECOVERY, STATS = 10`
		if got != want {
			t.Fatalf("got:\n%s\nwant:\n%s", got, want)
		}
	})

	t.Run("con MOVE", func(t *testing.T) {
		moves := []movePair{
			{Logical: "app", Physical: `E:\data\app.mdf`},
			{Logical: "app_log", Physical: `E:\logs\app.ldf`},
		}
		got := buildRestoreTSQL("app", `C:\bk\app.bak`, ModeNoRecovery, moves, nil, 10)
		want := `RESTORE DATABASE [app] FROM DISK = N'C:\bk\app.bak' WITH REPLACE, NORECOVERY, STATS = 10,` +
			` MOVE N'app' TO N'E:\data\app.mdf', MOVE N'app_log' TO N'E:\logs\app.ldf'`
		if got != want {
			t.Fatalf("got:\n%s\nwant:\n%s", got, want)
		}
	})

	t.Run("STOPAT solo en RECOVERY", func(t *testing.T) {
		stop := time.Date(2026, 11, 21, 10, 30, 0, 0, time.UTC)

		gotRec := buildRestoreTSQL("app", "x", ModeRecovery, nil, &stop, 10)
		if !strings.Contains(gotRec, "STOPAT = N'2026-11-21T10:30:00'") {
			t.Errorf("RECOVERY sin STOPAT:\n%s", gotRec)
		}

		gotNoRec := buildRestoreTSQL("app", "x", ModeNoRecovery, nil, &stop, 10)
		if strings.Contains(gotNoRec, "STOPAT") {
			t.Errorf("NORECOVERY no debe llevar STOPAT:\n%s", gotNoRec)
		}
	})

	t.Run("escape de nombres", func(t *testing.T) {
		got := buildRestoreTSQL("O'Brien", `D:\bk\ob.bak`, ModeRecovery, nil, nil, 10)
		if !strings.Contains(got, "RESTORE DATABASE [O'Brien]") {
			t.Errorf("nombre como identificador esperado:\n%s", got)
		}
		got2 := buildRestoreTSQL("Weird]Name", `D:\bk\w.bak`, ModeRecovery, nil, nil, 10)
		if !strings.Contains(got2, "RESTORE DATABASE [Weird]]Name]") {
			t.Errorf("corchete sin doblar:\n%s", got2)
		}
	})
}

func TestValidateChain(t *testing.T) {
	chain := []connector.ChainEntry{
		{Type: "D", FirstLSN: 100, LastLSN: 200},
		{Type: "I", FirstLSN: 200, LastLSN: 300},
		{Type: "L", FirstLSN: 300, LastLSN: 400},
	}
	if err := ValidateChain(chain); err != nil {
		t.Fatalf("cadena continua rechazada: %v", err)
	}

	if err := ValidateChain(nil); err == nil {
		t.Fatal("cadena vacía debe fallar")
	}

	if err := ValidateChain(chain[:1]); err != nil {
		t.Fatalf("cadena de un elemento rechazada: %v", err)
	}

	rota := append([]connector.ChainEntry(nil), chain...)
	rota[2].FirstLSN = 150 // discontinuidad respecto a last_lsn=300 del anterior
	if err := ValidateChain(rota); err == nil {
		t.Fatal("debe detectar la discontinuidad de LSN")
	}
}

// Execute hace validaciones previas antes de tocar la conexión: con db nil
// solo se comprueba que el rechazo ocurre sin error de conexión.
func TestExecuteValidacionesPrevias(t *testing.T) {
	ctx := context.Background()

	t.Run("master se rechaza", func(t *testing.T) {
		res, err := Execute(ctx, nil, Options{Database: "master", SourceFiles: []string{"x.bak"}})
		if err == nil {
			t.Fatal("master debe rechazarse")
		}
		if res == nil || res.Error == "" {
			t.Fatalf("resultado debe explicar el rechazo: %+v", res)
		}
	})

	t.Run("MASTER en mayúsculas también", func(t *testing.T) {
		if _, err := Execute(ctx, nil, Options{Database: "MASTER", SourceFiles: []string{"x.bak"}}); err == nil {
			t.Fatal("MASTER debe rechazarse (comparación case-insensitive)")
		}
	})

	t.Run("sin archivos", func(t *testing.T) {
		if _, err := Execute(ctx, nil, Options{Database: "app"}); err == nil {
			t.Fatal("sin SourceFiles debe fallar")
		}
	})

	t.Run("STOPAT con NORECOVERY", func(t *testing.T) {
		stop := time.Now()
		if _, err := Execute(ctx, nil, Options{
			Database:    "app",
			SourceFiles: []string{"x.bak"},
			Mode:        ModeNoRecovery,
			StopAt:      &stop,
		}); err == nil {
			t.Fatal("STOPAT con NORECOVERY debe fallar")
		}
	})
}

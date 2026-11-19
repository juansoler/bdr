package store

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// TestContractPostgres ejecuta la batería de contrato contra PostgreSQL.
// Requiere BDR_TEST_POSTGRES con un DSN de administración (p. ej.
// postgres://bdr:bdr@localhost:15432/postgres); crea una base efímera por
// ejecución y la borra al terminar. Sin la variable, se omite.
func TestContractPostgres(t *testing.T) {
	adminDSN := os.Getenv("BDR_TEST_POSTGRES")
	if adminDSN == "" {
		t.Skip("BDR_TEST_POSTGRES no definida; tests de PostgreSQL omitidos")
	}

	db := fmt.Sprintf("bdr_test_%d", time.Now().UnixNano()%1_000_000_000)
	admin := openPool(t, adminDSN)
	ctx := context.Background()
	if _, err := admin.Exec(ctx, fmt.Sprintf(`CREATE DATABASE %s`, db)); err != nil {
		t.Fatalf("no se pudo crear la base efímera %s: %v", db, err)
	}
	t.Cleanup(func() {
		admin.Exec(context.Background(), fmt.Sprintf(`DROP DATABASE IF EXISTS %s WITH (FORCE)`, db))
		admin.Close()
	})

	dsn := replaceDatabase(adminDSN, db)
	// ... batería de contrato sobre la base efímera db ...
	runStoreContract(t, storeFixture{
		make: func(t *testing.T) Store {
			p, err := OpenPostgres(context.Background(), dsn)
			if err != nil {
				t.Fatal(err)
			}
			return p
		},
		reopen: func(t *testing.T, prev Store) Store {
			_ = prev.Close()
			p, err := OpenPostgres(context.Background(), dsn)
			if err != nil {
				t.Fatal(err)
			}
			return p
		},
		peek: func(t *testing.T, key string) bool {
			peeper := openPool(t, dsn)
			defer peeper.Close()
			var raw []byte
			if err := peeper.QueryRow(context.Background(),
				`SELECT state FROM control_state WHERE id = 1`,
			).Scan(&raw); err != nil {
				return false
			}
			// jsonb normaliza con espacio tras los dos puntos.
			return containsRaw(raw, `"ID":"`+key+`"`) || containsRaw(raw, `"ID": "`+key+`"`)
		},
		durable: true,
	})

	// ImportState: necesita una base limpia propia (la de la batería ya
	// tiene estado). Se crea otra efímera y se borra al terminar.
	dbImp := db + "_imp"
	if _, err := admin.Exec(ctx, fmt.Sprintf(`CREATE DATABASE %s`, dbImp)); err != nil {
		t.Fatalf("no se pudo crear la base de importación: %v", err)
	}
	t.Cleanup(func() {
		admin.Exec(context.Background(), fmt.Sprintf(`DROP DATABASE IF EXISTS %s WITH (FORCE)`, dbImp))
	})
	p := openStore(t, replaceDatabase(adminDSN, dbImp))
	if err := p.ImportState(ctx, "testdata/import-state.json"); err != nil {
		t.Fatal(err)
	}
	if err := p.View(ctx, func(s *State) error {
		if len(s.Agents) != 1 || s.Agents["a-import"] == nil {
			t.Fatal("estado importado no visible")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := p.ImportState(ctx, "testdata/import-state.json"); err == nil {
		t.Fatal("la segunda importación debía ser rechazada")
	}
	_ = p.Close()
}

func openStore(t *testing.T, dsn string) *Postgres {
	t.Helper()
	p, err := OpenPostgres(context.Background(), dsn)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// openPool abre un pool de administración con el DSN tal cual.
func openPool(t *testing.T, dsn string) *pgxpool.Pool {
	t.Helper()
	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := pool.Ping(ctx); err != nil {
		t.Fatalf("PostgreSQL de prueba no responde: %v", err)
	}
	return pool
}

// replaceDatabase cambia la base de un DSN manteniendo el resto de parámetros.
// Soporta la forma URL (postgres://...) y la keyword=value.
func replaceDatabase(dsn, db string) string {
	if u, err := url.Parse(dsn); err == nil && (u.Scheme == "postgres" || u.Scheme == "postgresql") && u.Host != "" {
		u.Path = "/" + db
		return u.String()
	}
	if strings.Contains(dsn, "dbname=") {
		return regexp.MustCompile(`dbname=\S+`).ReplaceAllString(dsn, "dbname="+db)
	}
	return dsn + " dbname=" + db
}

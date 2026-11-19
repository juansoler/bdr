// Postgres es un almacenamiento duradero en PostgreSQL.
//
// La interfaz Store trabaja con transacciones View/Update sobre el documento
// completo (State), así que la implementación mantiene el mismo núcleo en
// memoria que File y persiste el documento serializado en la fila única de
// control_state (JSONB) tras cada Update — o de forma diferida con
// UpdateLazy. La auditoría encadenada va en su propia tabla append-only
// audit_events. El esquema y la decisión están en docs/adr/0007 y
// docs/adr/0006 (cola River, pendiente de este slice).
package store

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/juansoler/bdr/packages/contracts"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed migrations
var migrationFS embed.FS

// Postgres es el almacén duradero en PostgreSQL.
type Postgres struct {
	*Memory
	pool *pgxpool.Pool
}

// OpenPostgres conecta, aplica las migraciones y carga el estado y la
// auditoría existentes. Un documento más nuevo que el soportado se rechaza,
// igual que en File.
func OpenPostgres(ctx context.Context, dsn string) (*Postgres, error) {
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("DSN de PostgreSQL inválido: %w", err)
	}
	// Persistencias cortas y acotadas: el cerrojo del estado se toma al
	// escribir, así que no conviene acumular conexiones.
	cfg.MaxConns = 4
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, err
	}
	pctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	err = pool.Ping(pctx)
	cancel()
	if err != nil {
		pool.Close()
		return nil, fmt.Errorf("PostgreSQL no responde: %w", err)
	}
	if err := applyMigrations(ctx, pool); err != nil {
		pool.Close()
		return nil, err
	}

	st, err := loadPostgresState(ctx, pool)
	if err != nil {
		pool.Close()
		return nil, err
	}
	events, err := loadPostgresAudit(ctx, pool)
	if err != nil {
		pool.Close()
		return nil, err
	}

	p := &Postgres{pool: pool}
	p.Memory = newMemory(st, p.write, NewAudit(events, p.appendAudit))
	return p, nil
}

// loadPostgresState lee el documento de control_state (o crea uno vacío).
func loadPostgresState(ctx context.Context, pool *pgxpool.Pool) (*State, error) {
	var (
		schemaVersion int
		raw           []byte
	)
	err := pool.QueryRow(ctx, `SELECT schema_version, state FROM control_state WHERE id = 1`).
		Scan(&schemaVersion, &raw)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return NewState(time.Now().UTC()), nil
	case err != nil:
		return nil, fmt.Errorf("no se pudo leer control_state: %w", err)
	}
	var st State
	if err := json.Unmarshal(raw, &st); err != nil {
		return nil, fmt.Errorf("estado corrupto en control_state: %w", err)
	}
	if st.Meta.SchemaVersion > SchemaVersion {
		return nil, fmt.Errorf("la base usa el esquema %d, más nuevo que el soportado (%d)", st.Meta.SchemaVersion, SchemaVersion)
	}
	st.normalize()
	return &st, nil
}

// loadPostgresAudit lee la cadena de auditoría completa, en orden de secuencia.
func loadPostgresAudit(ctx context.Context, pool *pgxpool.Pool) ([]contracts.AuditEvent, error) {
	rows, err := pool.Query(ctx, `SELECT event FROM audit_events ORDER BY seq`)
	if err != nil {
		return nil, fmt.Errorf("no se pudo leer audit_events: %w", err)
	}
	defer rows.Close()
	var events []contracts.AuditEvent
	for rows.Next() {
		var raw []byte
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		var ev contracts.AuditEvent
		if err := json.Unmarshal(raw, &ev); err != nil {
			return nil, fmt.Errorf("auditoría corrupta en audit_events: %w", err)
		}
		events = append(events, ev)
	}
	return events, rows.Err()
}

// write persiste el documento completo. Se llama con el cerrojo del estado
// tomado y sin contexto de petición, de ahí el timeout propio.
func (p *Postgres) write(st *State) error {
	b, err := json.Marshal(st)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, err = p.pool.Exec(ctx, `
		INSERT INTO control_state (id, schema_version, state)
		VALUES (1, $1, $2)
		ON CONFLICT (id) DO UPDATE
		SET schema_version = $1, state = $2, updated_at = now()`,
		st.Meta.SchemaVersion, b)
	if err != nil {
		return fmt.Errorf("no se pudo persistir el estado: %w", err)
	}
	return nil
}

// appendAudit inserta un evento de auditoría. El Seq y el Hash ya vienen
// calculados; la tabla es append-only.
func (p *Postgres) appendAudit(ev contracts.AuditEvent) error {
	b, err := json.Marshal(ev)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := p.pool.Exec(ctx,
		`INSERT INTO audit_events (seq, event) VALUES ($1, $2)`, ev.Seq, b,
	); err != nil {
		return fmt.Errorf("no se pudo persistir el evento de auditoría: %w", err)
	}
	return nil
}

// Ping comprueba la conexión con la base.
func (p *Postgres) Ping(ctx context.Context) error { return p.pool.Ping(ctx) }

// Close vuelca lo pendiente, cierra el registro de auditoría y libera la conexión.
func (p *Postgres) Close() error {
	err := p.Memory.Close()
	p.pool.Close()
	return err
}

// ImportState importa un state.json (formato 0.2 en adelante) en la base.
// Se niega si la base ya tiene estado: es una operación de migración única,
// no de mezcla.
func (p *Postgres) ImportState(ctx context.Context, path string) error {
	st, err := loadState(path)
	if err != nil {
		return err
	}
	var exists bool
	if err := p.pool.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM control_state WHERE id = 1)`,
	).Scan(&exists); err != nil {
		return err
	}
	if exists {
		return errors.New("la base ya tiene estado; la importación solo está permitida sobre una base vacía")
	}
	// Se persiste con el cerrojo tomado para no abrir una ventana de escritura.
	p.Memory.mu.Lock()
	defer p.Memory.mu.Unlock()
	if err := p.write(st); err != nil {
		return err
	}
	p.Memory.state = st
	return nil
}

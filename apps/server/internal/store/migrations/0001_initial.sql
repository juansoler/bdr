-- 0001_initial: documento de estado transaccional y auditoría encadenada.
--
-- El estado completo del plano de control (State) vive como documento JSONB
-- en una sola fila: la interfaz Store trabaja con transacciones View/Update
-- sobre el documento completo (store.go), así que la fila única es la unidad
-- de consistencia. El esquema relacional de docs/05-modelo-datos.md queda
-- como evolución posterior detrás de la misma interfaz (docs/adr/0007).
CREATE TABLE IF NOT EXISTS control_state (
    id             integer PRIMARY KEY CHECK (id = 1),
    schema_version integer NOT NULL,
    state          jsonb NOT NULL,
    created_at     timestamptz NOT NULL DEFAULT now(),
    updated_at     timestamptz NOT NULL DEFAULT now()
);

-- Auditoría append-only: un evento por fila, con el JSON íntegro del
-- contracts.AuditEvent (incluye prev_hash y hash de la cadena). Insertar es
-- la única escritura permitida; editar o borrar rompe la cadena y Verify
-- (o un SELECT contra los hashes) lo detecta.
CREATE TABLE IF NOT EXISTS audit_events (
    seq   bigint PRIMARY KEY,
    event jsonb NOT NULL,
    at    timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS audit_events_at_idx ON audit_events (at);

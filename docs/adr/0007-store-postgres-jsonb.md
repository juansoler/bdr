# ADR-0007: store.Postgres como documento JSONB transaccional

- **Estado:** Aceptado
- **Fecha:** 2026-11-17
- **Relacionado:** `05-modelo-datos.md`, `adr/0006-cola-river-postgresql.md`

## Contexto

La interfaz `store.Store` (0.4) expone transacciones de documento completo: `View`/`Update` reciben un closure sobre el `State` entero, y los handlers y el programador trabajan con métodos del documento (`LeaseJobFor`, `AppendJobEvent`, índices reconstruidos al cargar). Llegaba el momento de dar a ese estado durabilidad en PostgreSQL (`docs/05-modelo-datos.md` describe el modelo relacional objetivo, colección por colección).

Dos caminos:

1. **Esquema relacional completo** (17 tablas): requiere reescribir cada handler sobre CRUD por colección, o diff-write del documento tras cada Update sobre todas las tablas. Gran superficie de errores y una iteración enorme.
2. **Documento JSONB transaccional**: la misma fila es la unidad de consistencia; el núcleo en memoria y la semántica de cierres no cambian.

## Decisión

Implementar `store.Postgres` con el **documento completo como JSONB** en la tabla `control_state` (fila única) y la **auditoría encadenada en su propia tabla append-only** `audit_events`. Migraciones embebidas aplicadas al arrancar; importación única de `state.json` con `--import-state`. La cola River (ADR-0006) queda para el paso a multi-nodo: el lease y los reintentos siguen siendo nuestros, ahora respaldados por la base.

## Alternativas

1. **Relacional completo ya:** máximo valor a largo plazo, pero sin consumidores de consultas SQL hoy; coste desproporcionado.
2. **No hacer nada (fichero):** válido para laboratorio, sin copia de seguridad centralizada ni base para HA.

## Consecuencias

- Durabilidad y atomicidad reales: cada `Update` es una transacción; `pg_dump`/`pg_restore` cubren estado y auditoría.
- La escalabilidad de escritura sigue siendo de un solo escritor por proceso (igual que el fichero); no resuelve HA.
- El esquema relacional de `docs/05` queda como evolución detrás de la misma interfaz: cuando existan consultas reales (informes, búsqueda), se migra colección a colección.
- La batería de contrato (`contract_test.go`) exige paridad memory/file/postgres; toda implementación nueva debe pasarla sin cambios.

## Criterios de revisión

- Si aparece consultas SQL reales sobre colecciones (informes, búsqueda global), migrar esas colecciones a tablas.
- Si se necesita HA del control plane, revisar concurrencia multi-escritor y mover la cola a River (ADR-0006).

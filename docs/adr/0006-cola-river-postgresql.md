# ADR-0006: Cola River sobre PostgreSQL

- **Estado:** Aceptado
- **Fecha:** 2026-11-16
- **Relacionado:** `04-arquitectura.md`, `06-api.md`

## Contexto

El control plane necesita una cola para jobs programados, reintentos y operaciones largas. Las opciones eran Redis + Asynq, una cola artesanal con `SKIP LOCKED`, o River sobre PostgreSQL.

## Decisión

Usar **River** (cola en PostgreSQL para Go) como opción principal. Mantener Asynq/Redis como alternativa si River no cubre un caso.

## Alternativas

1. **Redis + Asynq:** maduro y rápido, pero añade un servicio y una dependencia operativa.
2. **Cola artesanal con `SELECT ... FOR UPDATE SKIP LOCKED`:** control total, ~500 líneas, sin dependencia extra; más código que mantener.
3. **River:** reintentos, unicidad, periodicidad y UI de inspección sobre PostgreSQL; una sola dependencia de estado.

## Consecuencias

- El despliegue mínimo no necesita Redis.
- La cola y el catálogo comparten transacción y backup.
- River es menos conocido que Asynq; hay que validar su madurez en Fase 0/1.
- Si se necesita throughput muy alto, Asynq puede ser mejor.
- El diseño de estados, leases, idempotencia y cancelación sigue siendo nuestro.

## Criterios de revisión

- Si River no soporta periodicidad, unicidad o reintentos como se espera, cambiar a Asynq.
- Si el volumen de jobs crece mucho, evaluar Redis.
- Si se necesita una UI de cola, River la ofrece; Asynq requiere otra.

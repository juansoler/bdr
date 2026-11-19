# ADR-0001: Go para servidor y agentes

- **Estado:** Aceptado
- **Fecha:** 2026-11-16
- **Relacionado:** `01-especificacion-maestra.md`, `04-arquitectura.md`

## Contexto

Las dos especificaciones proponían stacks distintos: Node.js/TypeScript con Fastify y Prisma para el servidor, o Go para servidor y agentes. El agente debía ser Go en ambos casos por los binarios nativos y el acceso a VSS/snapshots. El panel sería Next.js en ambos.

## Decisión

Usar **Go** para el servidor de control, el worker, el agente Windows/Linux y el agente PVE. El panel sigue en Next.js/TypeScript.

## Alternativas

1. **Node/TS + Prisma + BullMQ:** desarrollo rápido y ecosistema conocido; pero obliga a dos runtimes (Node + Redis) y no comparte tipos ni utilidades con el agente.
2. **Híbrido Next.js full-stack + worker Go:** menos piezas, pero acopla API y frontend, complica mTLS y la emisión de certificados.
3. **Go + Next.js:** un lenguaje para todo el backend y los agentes, binario único, mTLS nativo, contratos compartidos.

## Consecuencias

- Un solo lenguaje para servidor, agentes y herramientas de recuperación.
- Binarios estáticos y despliegue más simple.
- Menos ecosistema de ORM; se usa pgx + sqlc y migraciones SQL.
- El panel sigue siendo TypeScript; la integración es por OpenAPI/JSON.
- El equipo debe mantener disciplina de contratos entre Go y TypeScript.

## Criterios de revisión

- Si el desarrollo del panel se vuelve dominante y el servidor Go añade fricción, evaluar un BFF en TypeScript sin cambiar los agentes.
- Si se necesita un ecosistema de librerías exclusivo de Node, reconsiderar solo la capa de API, no los agentes.

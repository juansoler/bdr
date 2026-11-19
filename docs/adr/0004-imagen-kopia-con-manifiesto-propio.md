# ADR-0004: Imagen con manifiesto propio y Kopia como almacén

- **Estado:** Aceptado
- **Fecha:** 2026-11-16
- **Relacionado:** `03-evaluacion-motores.md`, `10-motor-imagen-manifiestos.md`

## Contexto

Kopia no es un motor de imagen de disco con CBT, pero acepta flujos por `--stdin-file`. El informe adicional recomendaba no acoplar la recuperación a Kopia y construir un `BlockImageRepository` propio. La especificación original proponía usar Kopia para los flujos.

## Decisión

Definir un **manifiesto de imagen versionado propio** y una interfaz `ImageEngine`/`ImageRepository`. En Fase 2, Kopia actúa como almacén de segmentos; el manifiesto y el contrato de recuperación son nuestros.

## Alternativas

1. **Formato Kopia puro:** menos código, pero acopla la recuperación a la CLI y a sus cambios.
2. **Repositorio de chunks propio desde el inicio:** control total, pero duplica dedup, cifrado, compresión y replicación; alto coste.
3. **Manifiesto propio + Kopia como backend:** equilibrio; se puede cambiar el backend sin reescribir el panel ni el orquestador.

## Consecuencias

- Se implementa segmentación, journal, hashes y validación de manifiesto.
- Kopia aporta dedup, cifrado, compresión y replicación.
- El manifiesto es inmutable y versionado; un restaurador rechaza versiones desconocidas.
- Si la PoC demuestra problemas de fidelidad, rendimiento o reanudación, se puede sustituir el backend.
- Más código propio y más pruebas que un formato Kopia puro.

## Criterios de revisión

- Si `--stdin-file` no permite reanudar y la segmentación no es suficiente, evaluar un almacén de chunks propio.
- Si Kopia cambia la semántica de `--stdin-file`, revisar el contrato.
- Si el coste de mantenimiento del manifiesto supera el beneficio, reconsiderar.

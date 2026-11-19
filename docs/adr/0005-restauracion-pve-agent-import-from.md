# ADR-0005: Restauración con agente PVE e `import-from`

- **Estado:** Aceptado
- **Fecha:** 2026-11-16
- **Relacionado:** `11-restauracion-p2v-proxmox.md`, `12-iso-rescate.md`

## Contexto

Hay dos estrategias para restaurar una máquina como VM en Proxmox:

1. ISO de rescate que arranca dentro de la VM, descarga y escribe el disco.
2. Agente en el nodo Proxmox que escribe el disco en el storage y crea la VM con `import-from`.

El propietario eligió la segunda como principal. El informe adicional recomendaba la ISO primero por independencia del storage.

## Decisión

Usar **agente PVE + `import-from`** como vía principal. La **ISO de rescate** se construye en Fase 2 como fallback universal y mecanismo break-glass.

## Alternativas

1. **ISO primero:** universal, pero más lenta y con más piezas móviles; requiere arrancar una VM de rescate.
2. **Agente PVE primero:** menos pasos, más rápido, pero requiere root en el nodo y staging local.
3. **Ambas:** máxima cobertura, más esfuerzo.

## Consecuencias

- El agente PVE es un componente sensible: root, token de mínimo privilegio, staging acotado, auditoría.
- `import-from` requiere que el archivo sea local al nodo o un volumen con content type `import`.
- El arranque Windows sigue siendo el riesgo principal; se usa SATA+e1000 y luego VirtIO.
- La ISO sigue siendo obligatoria para break-glass y bare-metal.
- La restauración no depende del control plane si se usa la ISO.

## Criterios de revisión

- Si `import-from` no funciona de forma fiable en los storages objetivo, priorizar la ISO.
- Si el agente PVE no puede operar con privilegios mínimos, revisar el modelo de seguridad.
- Si el tiempo de restauración no mejora respecto a la ISO, reconsiderar la estrategia principal.

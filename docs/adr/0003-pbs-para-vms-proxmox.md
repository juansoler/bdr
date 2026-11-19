# ADR-0003: PBS para VMs de Proxmox

- **Estado:** Aceptado
- **Fecha:** 2026-11-16
- **Relacionado:** `02-analisis-funcional.md`, `09-agente-proxmox-pbs.md`

## Contexto

Las VMs ya alojadas en Proxmox pueden protegerse con un agente dentro de la VM, con un motor propio desde el hipervisor o con Proxmox Backup Server. El requisito es backup incremental sin instalar agentes en cada VM.

## Decisión

Usar **PBS** como motor de backup/restore de VMs de Proxmox y orquestarlo por API desde el control plane. No se reimplementa el backup de VMs.

## Alternativas

1. **Agente dentro de cada VM:** funciona, pero requiere instalar y mantener agentes y no es agentless.
2. **Motor propio desde el hipervisor:** reimplementa snapshots, CBT, dedup y verificación; alto coste y riesgo.
3. **PBS:** ya hace incremental por bloque sucio, dedup, compresión, verificación e integración con PVE.

## Consecuencias

- Dependencia de PBS y de su versión.
- El control plane solo cataloga y orquesta; el tráfico de datos no pasa por él.
- PBS 4.x permite datastore S3, con caché local de 64–128 GiB, una instancia por datastore y sin gestión de bucket/ACL.
- Object Lock no está documentado para el datastore S3; la inmutabilidad de VMs se resuelve con política del bucket o réplica PBS.
- Los tokens deben ser de mínimo privilegio por función.

## Criterios de revisión

- Si PBS no cubre un caso (por ejemplo, LXC o live-restore), documentar la limitación y evaluar alternativas.
- Si el datastore S3 no cumple los requisitos de inmutabilidad, usar un segundo PBS o una réplica.

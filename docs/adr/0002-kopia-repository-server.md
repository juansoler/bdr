# ADR-0002: Kopia Repository Server central

- **Estado:** Aceptado
- **Fecha:** 2026-11-16
- **Relacionado:** `04-arquitectura.md`, `13-seguridad.md`, `15-almacenamiento-retencion.md`

## Contexto

Los agentes necesitan escribir en un repositorio Kopia en NAS y S3. Las opciones eran distribuir credenciales del repositorio a cada agente o centralizar el acceso en un Repository Server.

## Decisión

Desplegar un **Kopia Repository Server** central. Los agentes se conectan por HTTPS/mTLS a través del control plane y usan un usuario de repositorio con ACL. La contraseña maestra solo la conoce el servidor.

## Alternativas

1. **Acceso directo del agente al NAS/S3:** menos saltos y más velocidad, pero distribuye la clave maestra, complica la rotación y permite a un agente comprometido atacar el repositorio.
2. **Repositorio por agente:** aislamiento, pero destruye la deduplicación global y multiplica la custodia de claves.
3. **Repository Server central:** una custodia, ACL por agente, réplica centralizada y una sola superficie de exposición.

## Consecuencias

- El Repository Server es un **SPOF** y un cuello de banda; se recomienda host/VM dedicado y NAS local.
- Los agentes no reciben la clave maestra.
- Las ACL de Kopia protegen manifiestos y políticas, **no contenidos**; los agentes del mismo repositorio se consideran del mismo dominio de confianza.
- La revocación de un usuario puede tardar; se fuerza con `kopia server refresh`/SIGHUP y se aplica en el proxy/API.
- La CLI de Kopia se fija por versión y se envuelve; no se importan paquetes internos.

## Criterios de revisión

- Si el rendimiento del servidor limita los backups, evaluar un host dedicado o un modo directo para archivos.
- Si se requiere aislamiento estricto entre agentes, separar repositorios por grupo.
- Si Kopia cambia el modelo de ACL o de servidor, revisar el diseño.

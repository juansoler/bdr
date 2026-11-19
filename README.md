# BDR Self-Hosted

Plataforma propia de copias de seguridad y recuperación ante desastres (BDR), construida sobre componentes open source (Kopia, Proxmox Backup Server, VSS, LVM/Btrfs/ZFS y QEMU).

**Estado actual:** documentación de diseño v1.2 y vertical slice funcional `0.5.0-dev` — Fase 1 del roadmap completa en código (servidor con RBAC/sesiones+TOTP, auditoría encadenada, alertas con webhook/email, programación, Kopia real con restore points verificados, restauración de archivos, agentes instalables y panel completo) más estado duradero en **PostgreSQL** opcional (`--store postgres`). El contrato técnico sigue en `docs/`; el estado del código, en [`DEVELOPMENT.md`](DEVELOPMENT.md).

## Estado de implementación

| Capacidad | Estado |
|---|---|
| Servidor API (Go), logs JSON, errores RFC 7807 | Implementado |
| Enrolamiento con token de un solo uso y API key | Implementado |
| mTLS: CA interna, listener TLS, CSR y renovación | Implementado (obligatorio por defecto; `--insecure-agents` solo para transición) |
| Heartbeat e inventario Windows/Linux (discos, TPM, BitLocker/LUKS) | Implementado |
| Almacenamiento persistente: fichero o PostgreSQL | Implementado (modelo relacional docs/05 como evolución) |
| Sesiones + TOTP + RBAC + tokens de integración | Implementado |
| Planes: cron, ventanas, retención GFS, grupos con jerarquía | Implementado |
| Programador con ventanas, reintentos y alertas de contacto | Implementado |
| Jobs: idempotencia, cancelación, reintentos, SSE | Implementado |
| Backup de archivos con Kopia real (credenciales por agente) | Implementado |
| Restore points, verificación nivel 1–2 y explorador | Implementado |
| Restauración de archivos al equipo origen u otro agente | Implementado |
| Alertas con deduplicación, webhook firmado y email | Implementado |
| Panel Next.js: resumen, equipos, planes, jobs, RPs, alertas | Implementado |
| Instaladores de agentes (systemd, .deb, PowerShell) | Implementado |
| Imagen, VSS, P2V e ISO | Pendiente (Fase 2) |

Lo implementado se prueba con `make test` y `make smoke`. Lo pendiente está descrito en `docs/20-backlog.md`.

## Qué resuelve

- Backup de **imagen completa** y de **archivos** en Windows 11, Windows Server 2025 y Debian.
- Copias **incrementales** con deduplicación por contenido, compresión Zstandard y cifrado en cliente.
- Protección de VMs de Proxmox **sin agente**, mediante PBS orquestado por API.
- Restauración de una máquina con agente como **VM nueva en Proxmox** (P2V/V2V) desde el panel.
- Destino principal en **NAS** y réplica **S3** con inmutabilidad cuando el proveedor la soporte.
- Verificación automática de que las copias son recuperables, no solo de que el job terminó.

## Decisiones principales

| Tema | Decisión |
|---|---|
| Servidor y agentes | Go |
| Panel | Next.js + TypeScript + Tailwind |
| Base de datos y cola | PostgreSQL + River (alternativa: Asynq/Redis) |
| Motor de archivos y repositorio | Kopia (Apache-2.0), fijado por versión |
| VMs de Proxmox | Proxmox Backup Server (PBS) orquestado por API |
| Imagen de máquina | Motor propio con manifiesto versionado; Kopia como almacén de segmentos |
| Restauración a Proxmox | Agente en el nodo PVE + `import-from`; ISO de rescate como fallback |
| Repositorio | Kopia Repository Server central; los agentes no reciben la clave maestra |
| Seguridad | mTLS, TOTP, secretos cifrados, binarios firmados, auditoría |
| Despliegue | Docker Compose para el servidor; instaladores nativos para agentes |

## Mapa de documentación

### Visión y requisitos

- [`docs/00-informe-ejecutivo.md`](docs/00-informe-ejecutivo.md) — informe ejecutivo y recomendación.
- [`docs/01-especificacion-maestra.md`](docs/01-especificacion-maestra.md) — requisitos RF/RNF, prioridades y criterios de aceptación.
- [`docs/02-analisis-funcional.md`](docs/02-analisis-funcional.md) — análisis funcional del alcance y decisiones.
- [`docs/03-evaluacion-motores.md`](docs/03-evaluacion-motores.md) — Kopia vs UrBackup vs Duplicacy vs PBS.
- [`docs/decisiones.md`](docs/decisiones.md) — registro de decisiones y puntos verificados.
- [`docs/poc-restauracion.md`](docs/poc-restauracion.md) — registro de evidencias de la Fase 0.

### Diseño técnico

- [`docs/04-arquitectura.md`](docs/04-arquitectura.md) — componentes, flujos, despliegue y modos de fallo.
- [`docs/05-modelo-datos.md`](docs/05-modelo-datos.md) — modelo PostgreSQL.
- [`docs/06-api.md`](docs/06-api.md) — contrato REST de agentes y administración.
- [`docs/07-agente-windows.md`](docs/07-agente-windows.md) — agente Windows y VSS.
- [`docs/08-agente-linux.md`](docs/08-agente-linux.md) — agente Linux y snapshots.
- [`docs/09-agente-proxmox-pbs.md`](docs/09-agente-proxmox-pbs.md) — agente PVE y orquestación PBS.
- [`docs/10-motor-imagen-manifiestos.md`](docs/10-motor-imagen-manifiestos.md) — formato de imagen y manifiesto.
- [`docs/11-restauracion-p2v-proxmox.md`](docs/11-restauracion-p2v-proxmox.md) — restauración como VM.
- [`docs/12-iso-rescate.md`](docs/12-iso-rescate.md) — ISO de rescate y break-glass.
- [`docs/22-panel-ui-ux.md`](docs/22-panel-ui-ux.md) — navegación, pantallas, wizard de restauración y permisos.
- [`docs/esquemas/manifest-v1.schema.json`](docs/esquemas/manifest-v1.schema.json) — JSON Schema del manifiesto de imagen.

### Seguridad y operación

- [`docs/13-seguridad.md`](docs/13-seguridad.md) — modelo de amenazas y controles.
- [`docs/14-custodia-clave-maestra.md`](docs/14-custodia-clave-maestra.md) — custodia y recuperación de claves.
- [`docs/15-almacenamiento-retencion.md`](docs/15-almacenamiento-retencion.md) — NAS, S3, Object Lock y retención.
- [`docs/16-verificacion-pruebas.md`](docs/16-verificacion-pruebas.md) — verificación de recuperación y matriz de pruebas.
- [`docs/17-operacion-runbooks.md`](docs/17-operacion-runbooks.md) — runbooks operativos.
- [`docs/18-riesgos.md`](docs/18-riesgos.md) — registro de riesgos.
- [`docs/19-roadmap.md`](docs/19-roadmap.md) — fases y puertas de salida.
- [`docs/20-backlog.md`](docs/20-backlog.md) — épicas e historias.
- [`docs/21-matriz-trazabilidad.md`](docs/21-matriz-trazabilidad.md) — requisito → documento → prueba → fase.
- [`docs/23-registro-de-cambios.md`](docs/23-registro-de-cambios.md) — historial del paquete documental.

### Anexos y ADRs

- [`docs/anexos/`](docs/anexos/) — matriz funcional, plan de PoC, comandos verificados, compatibilidad, contrato de Kopia y diagramas.
- [`docs/esquemas/`](docs/esquemas/) — JSON Schema del manifiesto y reglas de validación.
- [`docs/adr/`](docs/adr/) — decisiones arquitectónicas registradas.
- [`docs/README.md`](docs/README.md) — índice con estado, dependencias y rutas de lectura.
- [`docs/glosario.md`](docs/glosario.md) — términos y siglas.

## Cómo leer este paquete

1. **Dirección**: `00-informe-ejecutivo.md`.
2. **Alcance y aceptación**: `01-especificacion-maestra.md`.
3. **Decisiones y evidencia**: `02`, `03`, `decisiones.md` y ADRs.
4. **Diseño**: `04` a `12` y `22-panel-ui-ux.md`.
5. **Seguridad, operación y plan**: `13` a `21`.
6. **Antes de programar**: ejecutar la Fase 0 descrita en `anexos/b-poc-0-plan-evidencias.md`.

El índice completo, con el estado de cada documento y qué depende de qué, está en [`docs/README.md`](docs/README.md).

## Desarrollo

Requisitos: Go ≥ 1.26, Node 22 (solo para compilar el panel), curl y Python
para el smoke test.

```bash
make build       # exporta el panel y compila bin/bdr-control + bin/bdr-agent
make test        # tests unitarios y de contrato
make smoke       # servidor + agente reales en localhost
make smoke-web   # panel embebido: /healthz, SPA, assets y API en el mismo origen
make run-server
```

Guía completa, API implementada y límites: [`DEVELOPMENT.md`](DEVELOPMENT.md).

## Instalación (usuarios finales)

El servidor es **un único binario** con el panel web embebido y kopia como
binario auxiliar, para Linux, macOS y Windows (Docker opcional).

```bash
# Linux / macOS — descarga, revisa y ejecuta (ejecuta código remoto):
curl -fsSL https://github.com/juansoler/bdr/releases/latest/download/install-linux.sh -o install.sh
less install.sh
sudo bash install.sh
```

```powershell
# Windows — PowerShell como administrador:
irm https://github.com/juansoler/bdr/releases/latest/download/install-windows.ps1 -OutFile install-windows.ps1
notepad install-windows.ps1
.\install-windows.ps1
```

```bash
# Docker / Compose:
docker compose pull && docker compose -f deploy/docker-compose.prod.yml up -d
```

El instalador genera los secretos (`BDR_ADMIN_TOKEN`, `BDR_SECRET_KEY`,
`BDR_KOPIA_PASSWORD`), registra el servicio (systemd, launchd o servicio de
Windows) y arranca el panel en el puerto 8080. Verificación:

```bash
curl http://127.0.0.1:8080/healthz   # {"status":"ok","version":"…"}
```

Después de exponer el panel, añade TLS delante (Caddy incluido en Compose,
o tu proxy corporativo) y revisa el checklist de
[`deploy/PRODUCTION.md`](deploy/PRODUCTION.md) §8.

### Actualizaciones

El servidor consulta el canal de releases firmado (Ed25519; la clave pública
va embebida en el binario) y **notifica** la versión disponible en el panel:
nada se instala sin que el administrador confirme «Actualizar ahora». Los
agentes se actualizan solos cuando están ociosos, con el binario servido por
el propio plano de control y verificación SHA-256. En Windows, la
actualización del servidor se aplica ejecutando el instalador de la release
(un binario no puede sustituirse a sí mismo en Windows). Sin clave pública
embebida el chequeo queda deshabilitado (fallo cerrado).

Sin `make` (no hay Node instalado): usa el placeholder del panel embebido
(`apps/server/internal/webui/out/index.html`) o compila el panel a mano:

```bash
cd apps/web && npm ci && npm run build
mkdir -p apps/server/internal/webui/out && cp -R apps/web/out/. apps/server/internal/webui/out/
go build ./...
go test ./...
./scripts/smoke.sh
./scripts/smoke-web.sh
```

## Laboratorio Fase 0

El kit ejecutable está en [`labs/`](labs/README.md): captura de entorno, probes de VSS, scripts de streaming y ACL, creación de VM con `import-from`, chroot de Debian, Object Lock, custodia, rendimiento y comprobación de puerta.

```bash
./labs/common/capture-env.sh labs/evidence/$(date +%Y-%m-%d)   # Linux
./labs/gate-check.sh                                            # estado de la puerta
```

Los scripts que crean VMs o escriben discos son dry-run por defecto; los probes de VSS exigen `-confirm`. El directorio `labs/evidence/` está ignorado por Git porque puede contener datos del host.

## Convenciones

- `RF-xx` requisito funcional; `RNF-xx` requisito no funcional.
- Prioridad MoSCoW: **Must**, **Should**, **Could**, **Won't** (fuera de alcance).
- Fases: **0** PoC, **1** MVP de archivos, **2** imagen y P2V, **3** robustez 3-2-1, **4** avanzado.
- `[VERIFICAR]` significa que la afirmación requiere documentación oficial o prueba real. Ninguno debe quedar sin dueño ni experimento asociado.
- Los datos técnicos ya verificados citan la fuente en `decisiones.md`.

## Fuera de alcance

Antimalware/EDR, filtrado de URL, gestión de parches, escritorio remoto, archivado de correo, macOS, móviles, Microsoft 365/Google Workspace, cintas, multi-tenant, facturación y licencias. No se implementa CBT propio en las primeras fases.

## Licencias y atribución

- **Kopia**: Apache-2.0. Se distribuye como binario, con aviso de licencia y NOTICE.
- **Proxmox Backup Server / Proxmox VE**: se usan por API; no se redistribuyen.
- **UrBackup**: AGPLv3+. Solo se usa como referencia y banco de pruebas conceptual; no se incorpora código.
- **Duplicacy**: no se usa. Su CLI es gratuita solo para uso personal y su licencia comercial es por equipo.

## Siguiente paso

Fase 2 del roadmap: motor de imagen con manifiesto v1, captura VSS en Windows, snapshots LVM/Btrfs en Linux y restauración P2V en Proxmox. En paralelo, la Fase 0 (evidencias de `docs/anexos/b-poc-0-plan-evidencias.md`) sigue siendo requisito antes de programar la imagen: el primer experimento es la restauración de Windows Server 2025 UEFI como VM en Proxmox.

También pendientes de Fase 1 ya en el backlog del código: PostgreSQL + River (sustituir `store.File`), Kopia Repository Server con usuario por agente y mTLS obligatorio.
# backupTool

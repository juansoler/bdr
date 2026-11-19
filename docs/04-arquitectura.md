# Arquitectura

**Documento:** 04-arquitectura  
**Versión:** 1.0  
**Requisitos relacionados:** `01-especificacion-maestra.md`, `05-modelo-datos.md`, `06-api.md`, `13-seguridad.md`

---

## 1. Principios

1. **El control plane no transporta datos de backup.** Solo metadatos, órdenes y estado.
2. **Los agentes inician la conexión.** No se abren puertos en las máquinas protegidas.
3. **El repositorio custodia la clave.** Los agentes usan credenciales de repositorio con ACL; nunca la clave maestra.
4. **La recuperación no depende del control plane.** La ISO y la clave custodiada permiten el break-glass.
5. **Los motores se encapsulan.** Kopia, PBS y Proxmox se usan por CLI/API, no se importan sus internals.
6. **Todo es observable.** Un job se sigue por `job_id`/`request_id` de extremo a extremo.
7. **La seguridad es un requisito de arquitectura**, no una capa añadida al final.

---

## 2. Contexto y actores

| Actor | Interacción |
|---|---|
| Administrador | Panel web, 2FA, operaciones destructivas con step-up |
| Agente Windows/Linux | mTLS, heartbeat, jobs, backup/restore |
| Agente PVE | mTLS, restauración, staging, PBS local |
| Proxmox VE | API REST con token de mínimo privilegio |
| PBS | API REST con tokens por función |
| NAS | Backend filesystem del repositorio, montaje local |
| S3 | Réplica y Object Lock |
| Operador de break-glass | ISO de rescate + clave custodiada |

---

## 3. Componentes

| Componente | Proceso | Dónde | Responsabilidad |
|---|---|---|---|
| `bdr-control` | Go | Host de control, contenedor | API admin y agentes, authn/authz, catálogo, planificador, emisión de certificados, orquestación PVE/PBS |
| `bdr-worker` | Mismo binario, modo worker | Mismo host, N réplicas | Sync NAS→S3, verificaciones, maintenance, recolección PBS, alertas |
| `bdr-panel` | Next.js | Contenedor | UI en español |
| PostgreSQL 16 | Servicio | Contenedor/VM | Estado, catálogo, cola River, auditoría |
| Kopia Repository Server | `kopia server start` | Host/VM dedicado recomendado | Custodia de la clave, ACL, exposición HTTPS |
| Repositorio primario | Kopia | NAS montado localmente | Datos deduplicados/cifrados |
| Repositorio réplica | Kopia | S3 compatible | 3-2-1, Object Lock |
| `bdr-agent` | Go | Windows/Linux | Inventario, snapshots, captura, restauración |
| `bdr-agent-pve` | Go | Nodos Proxmox | Staging, `import-from`, comprobación de arranque, PBS |
| Proxy inverso | Caddy/Traefik | Host de control | TLS del panel, cabeceras, rate limit |
| Prometheus/Grafana/Loki | Opcional | Host de control | Métricas y logs |

---

## 4. Diagrama de contenedores

```
┌─────────────────────────────────────────────────────────────────────────────┐
│ Host de control                                                             │
│                                                                             │
│  ┌──────────────┐   ┌──────────────┐   ┌───────────────────────────────┐   │
│  │ bdr-panel    │──▶│ proxy :443   │──▶│ bdr-control :8080            │   │
│  │ Next.js      │   │ TLS/rate     │   │ API admin + agentes           │   │
│  └──────────────┘   └──────────────┘   └───────────┬───────────────────┘   │
│                                                     │                       │
│  ┌──────────────────────────────────────────────────▼───────────────────┐   │
│  │ bdr-worker (N réplicas) · River · alertas · sync · verify            │   │
│  └──────────────────────────────────────────────────┬───────────────────┘   │
│                                                     │                       │
│  ┌──────────────┐   ┌───────────────────────────────▼───────────────────┐   │
│  │ PostgreSQL 16│   │ Kopia Repository Server :51515 (host/VM dedicado) │   │
│  └──────────────┘   └───────────────────────────────┬───────────────────┘   │
└─────────────────────────────────────────────────────┼───────────────────────┘
                                                      │ filesystem local
                                              ┌───────▼────────┐
                                              │ NAS (repo)     │
                                              └───────┬────────┘
                                                      │ sync-to
                                              ┌───────▼────────┐
                                              │ S3 + ObjectLock│
                                              └────────────────┘
```

---

## 5. Planos de red y puertos

| Plano | Puerto | Autenticación | Destino |
|---|---|---|---|
| Panel/API admin | 443 | TLS de navegador, sesión + TOTP, CSRF | `bdr-panel`/`bdr-control` |
| API agentes | 8443 | mTLS obligatorio (`RequireAndVerifyClientCert`) | `bdr-control` |
| Repository Server | 51515 | TLS + Basic (usuario de repositorio) | Kopia |
| PostgreSQL | 5432 | Solo red interna, TLS opcional | `bdr-control`/worker |
| Proxmox VE | 8006 | Token API | PVE |
| PBS | 8007 | Token API | PBS |
| Métricas | 9090/3000 | Red interna | Prometheus/Grafana |

**Regla:** no se mezclan los listeners de panel y agentes. La verificación del certificado de cliente se hace en el servidor Go, no se delega a un proxy que no propague la identidad de forma fiable.

---

## 6. Flujos

### 6.1 Enrolamiento

1. Admin crea token (32 bytes, hash argon2id, TTL 1 h, un uso, binding opcional).
2. Agente genera par de claves y CSR.
3. `POST /api/v1/agent/enroll` con token + CSR.
4. Servidor valida y firma certificado `CN=<agent_uuid>`, SAN URI, EKU clientAuth, 90 días.
5. Agente fija la huella del servidor y guarda configuración.
6. Servidor crea usuario de repositorio `agent-<uuid>` con ACL APPEND/READ sobre su prefijo.

### 6.2 Heartbeat y lease de jobs

1. Agente hace `POST /agent/heartbeat` cada 30 s con estado, versión, job actual y métricas.
2. Para recibir trabajo, `POST /agent/jobs/lease` con long-poll de 25 s.
3. El servidor devuelve un job con `lease_id` y `lease_expires_at`.
4. El agente confirma con `ack`, reporta progreso y completa.
5. Si el lease expira, el job vuelve a la cola con `attempt+1`.
6. La cancelación es cooperativa: el agente la consulta en heartbeat/lease y aborta entre segmentos.

### 6.3 Backup de archivos

```
Panel → job BACKUP_FILE → agente
Agente → kopia snapshot create <ruta> --json → Repository Server → NAS
Agente → POST /jobs/{id}/progress (bytes, archivos)
Agente → POST /jobs/{id}/complete (snapshot_id, bytes)
Control → restore_point + manifiesto de archivos
```

### 6.4 Backup de imagen

```
Panel/plan → job BACKUP_IMAGE → agente
Agente → inventario + preflight de espacio
Agente → snapshot VSS/LVM/Btrfs/ZFS o lectura crash-consistent
Agente → lector por segmentos (1–8 GiB) + sha256
Agente → kopia snapshot create --stdin-file=disk0.seg0001 --json
Agente → POST /jobs/{id}/segments/{n} (snapshot_id, sha256)
Agente → cierre de manifiesto + snapshot del manifiesto
Control → restore_point COMPLETED
```

### 6.5 Restauración de archivos

```
Panel → job RESTORE_FILES (snapshot, rutas, destino)
Control → agente
Agente → preflight (espacio, permisos) → kopia snapshot restore → verificación
Agente → POST /jobs/{id}/complete
```

### 6.6 Restauración P2V/V2V

```
Panel → wizard (restore point, nodo, storage, recursos)
Control → preflight sobre manifiesto (firmware, TPM, BitLocker, espacio, VMID)
Control → agente PVE del nodo
Agente PVE → descarga segmentos → archivo disperso en staging
Agente PVE → POST /nodes/{node}/qemu con import-from
Agente PVE → arranque SATA+e1000 → comprobación → (Fase 3) VirtIO
Agente PVE → POST /agent/restores/{id}/steps/{seq}
Control → estado final + evidencia
```

### 6.7 PBS

```
Control → POST /nodes/{node}/vzdump (storage PBS)
Nodo PVE → PBS
Control → GET PBS snapshots → catálogo
Control → POST PBS verify/prune/gc
Control → POST /nodes/{node}/qemu (archive PBS) para restore
```

### 6.8 Réplica NAS → S3

Un worker singleton toma un advisory lock y ejecuta `kopia repository sync-to s3 ...` desde el host del repositorio, con el NAS montado localmente. Registra `replication_run` y verifica el destino. No se ejecuta durante una ventana de mantenimiento incompatible.

### 6.9 Break-glass

```
Operador → arranca ISO → red aislada
ISO → conecta a NAS/S3 con configuración custodiada
Operador → introduce shares Shamir (3 de 5) → clave maestra
ISO → kopia repository connect → snapshot restore → repara arranque
Operador → registra incidente → rota clave
```

---

## 7. Repository Server

### 7.1 Despliegue

- **Recomendado:** host/VM dedicado, con el NAS montado localmente y enlace 10/25 GbE.
- **Mínimo:** mismo host que el control plane, asumiendo el riesgo de SPOF y cuello de banda; documentado y monitorizado.
- No se expone a Internet. Los agentes lo alcanzan por la red interna o VPN.
- Kopia escucha en `:51515`; el proxy del control plane puede terminar TLS, pero la autenticación de repositorio es de Kopia.

### 7.2 Supervisión

- `kopia repository status --json`: accesibilidad, modo, parámetros.
- `kopia snapshot list --json`: antigüedad del último snapshot por agente.
- `kopia maintenance info`: última y próxima ejecución.
- Endpoint HTTPS del servidor.
- Métricas `bdr_kopia_repo_*` y alertas por repositorio inaccesible, maintenance atrasado o deriva de versión.

### 7.3 Limitaciones asumidas

- Las ACL de Kopia protegen manifiestos y políticas, no contenidos.
- Los cambios de usuario pueden tardar 5–10 minutos; la revocación se aplica también en el proxy/API y se fuerza con `kopia server refresh`/SIGHUP.
- La CLI no es una API estable; se fija la versión y se ejecutan contract tests.

---

## 8. Cola de trabajos

**River (PostgreSQL)** como opción principal. Si no cubre un caso, Asynq + Redis.

Estados:

```
pending → queued → leased → running → {succeeded, failed, canceled, expired}
```

Reglas:

- `idempotency_key` único; mismo cuerpo + misma clave = replay; cuerpo distinto = 409.
- `lease_owner`, `lease_expires_at`; heartbeat cada 15 s.
- Reintentos con backoff exponencial y jitter; errores permanentes no se reintentan.
- Jobs singleton (`sync`, `maintenance`, `pbs-gc`) con advisory lock.
- Job padre/hijo para restauraciones por etapas.
- `job_events` append-only para SSE.
- Cancelación cooperativa; nunca `SIGKILL` a mitad de una subida.

---

## 9. Modos de fallo y degradación

| Fallo | Efecto | Comportamiento |
|---|---|---|
| Control plane caído | No hay nuevos jobs ni panel | Backups en curso continúan; agentes reintentan; la restauración break-glass sigue disponible |
| PostgreSQL caído | No hay catálogo ni cola | Backups en curso continúan; se reconcilia al volver; break-glass disponible |
| Repository Server caído | No hay nuevos backups ni restores online | El agente conserva estado local; se alerta; break-glass contra el repositorio directo |
| NAS caído | No hay backups ni réplica | Se alerta; los agentes reintentan; S3 sigue como copia |
| S3 caído | La réplica se atrasa | El backup local continúa; se reintenta; no se bloquea |
| Agente caído | Job lease expira | El job vuelve a la cola; otro intento o el mismo agente al volver |
| Certificado caducado | Agente no conecta | Renovación automática al 60 %; alerta previa |
| Disco lleno en staging | Restore falla | Preflight y cuota; limpieza; error accionable |
| VM no arranca | Restore falla | Se deja la VM para diagnóstico, se adjunta evidencia, no se destruye automáticamente |

---

## 10. Capacidad y rendimiento

Fórmulas de estimación:

- **Repositorio:** `datos_únicos × (1 + overhead) / ratio_dedup`. El ratio depende de la carga; medir en Fase 0.
- **Índice de Kopia:** RAM proporcional a metadatos y número de snapshots; medir con 1–5 TB.
- **Ventana de backup:** `tamaño_leído / throughput_lectura + tamaño_subido / throughput_red`. Sin CBT, `tamaño_leído` es el disco completo.
- **Staging de restore:** `tamaño_virtual × 1.1` si se escribe archivo completo; `tamaño_datos_únicos` si se usa archivo disperso.
- **NAS:** preferir montaje local; NFS añade latencia y problemas de locking.

Objetivos iniciales (a validar):

- 50 agentes, 10 restauraciones concurrentes.
- 1 GbE mínimo; 10 GbE recomendado para el repositorio.
- Backup incremental de archivos: minutos.
- Imagen de 1 TB: horas; planificar ventana.

---

## 11. Interfaces de código

Contratos conceptuales, no API definitiva:

```go
type BackupEngine interface {
    CreateBackup(ctx context.Context, req BackupRequest) (*BackupResult, error)
    ListRestorePoints(ctx context.Context, hostID string) ([]RestorePoint, error)
    VerifyBackup(ctx context.Context, id string) (*VerifyResult, error)
    RestoreFiles(ctx context.Context, req RestoreRequest) error
}

type ImageEngine interface {
    Capture(ctx context.Context, req ImageRequest) (*ImageResult, error)
    Inspect(ctx context.Context, imageID string) (*ImageManifest, error)
    Restore(ctx context.Context, req ImageRestoreRequest) error
}

type VirtualizationProvider interface {
    ListNodes(ctx context.Context) ([]Node, error)
    ListStorages(ctx context.Context, node string) ([]Storage, error)
    CreateVM(ctx context.Context, req VMCreateRequest) (*VM, error)
    StartVM(ctx context.Context, vmID string) error
}

type Repository interface {
    Connect(ctx context.Context, agentID string) error
    Snapshot(ctx context.Context, req SnapshotRequest) (*SnapshotResult, error)
    Restore(ctx context.Context, req RestoreRequest) error
    Verify(ctx context.Context, req VerifyRequest) (*VerifyResult, error)
    Sync(ctx context.Context, req SyncRequest) error
}
```

La implementación de `Repository` envuelve la CLI de Kopia. `VirtualizationProvider` envuelve la API de PVE/PBS. Ningún paquete interno de Kopia se importa.

---

## 12. Monorepo futuro

```
backupTool/
├── apps/
│   ├── server/                 Go: control plane + worker
│   │   ├── cmd/bdr-control/
│   │   ├── cmd/bdr-worker/
│   │   └── internal/
│   │       ├── api/            handlers, middleware, OpenAPI
│   │       ├── auth/           sesiones, TOTP, RBAC, mTLS
│   │       ├── catalog/        agentes, planes, restore points
│   │       ├── jobs/           River, leases, estados
│   │       ├── kopia/          wrapper CLI + contract tests
│   │       ├── proxmox/        cliente PVE
│   │       ├── pbs/            cliente PBS
│   │       ├── storage/        NAS/S3, sync, Object Lock
│   │       ├── verify/         niveles 1–3
│   │       ├── alerts/         email, webhook
│   │       ├── audit/          hash chain
│   │       └── secrets/        envelope encryption
│   ├── agent/                  Go: agente Windows/Linux
│   │   ├── cmd/agent/
│   │   └── internal/
│   │       ├── service/        SCM/systemd
│   │       ├── enroll/
│   │       ├── transport/      mTLS, reintentos, offline
│   │       ├── inventory/
│   │       ├── snapshot/       vss, lvm, btrfs, zfs, freeze
│   │       ├── stream/         segmentos, journal
│   │       ├── kopia/
│   │       ├── manifest/
│   │       ├── limits/
│   │       └── update/
│   ├── agent-pve/              Go: agente de nodo Proxmox
│   └── web/                    Next.js
├── packages/
│   ├── contracts/              OpenAPI, tipos generados, esquemas JSON
│   └── ui/                     componentes compartidos
├── deploy/
│   ├── docker-compose.yml
│   ├── proxy/
│   └── migrations/
├── iso/                        construcción de la ISO de rescate
├── scripts/
├── docs/
└── tests/
    ├── integration/
    ├── e2e/
    └── contract/kopia/
```

---

## 13. Despliegue mínimo

`docker-compose.yml`:

- `postgres:16`
- `bdr-control` (API)
- `bdr-worker`
- `bdr-panel`
- `proxy` (Caddy/Traefik)
- `kopia-server` (puede ir en el mismo host en despliegue mínimo; dedicado en producción)
- Volumen del NAS montado en `kopia-server`
- Prometheus/Grafana/Loki opcionales

Los agentes se instalan con:

- Windows: MSI o script PowerShell firmado, servicio SYSTEM.
- Debian: paquete `.deb`, unidad systemd, usuario con capacidades mínimas.
- PVE: paquete `.deb` y servicio systemd en el nodo.

---

## 14. Observabilidad

- Logs JSON con `slog`; redacción automática de secretos.
- Métricas: jobs por estado, duración, bytes, dedup ratio, latencia de API, salud del repositorio, leases expirados, verificaciones.
- Healthchecks: `/healthz` (proceso), `/readyz` (BD, repositorio, PVE/PBS).
- SSE para eventos de job en la UI.
- Correlación `request_id` → `job_id` → `agent_id` → `snapshot_id`.

---

## 15. Preguntas abiertas

- ¿El Repository Server dedicado es obligatorio en este hardware o basta el despliegue mínimo?
- ¿El NAS permite montaje local con el rendimiento necesario?
- ¿Cuántos agentes y cuántos TB se esperan en 12 meses?
- ¿Se usará S3 con Object Lock desde Fase 3 o antes?
- ¿La red interna permite mTLS con CA propia sin fricción operativa?

Las respuestas se registran en `decisiones.md`.

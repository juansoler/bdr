# Modelo de datos

**Documento:** 05-modelo-datos  
**Versión:** 1.1  
**Motor:** PostgreSQL 16  
**Acceso:** pgx + sqlc; migraciones con goose o golang-migrate

---

## 1. Convenciones

- PK `uuid` (preferible UUIDv7) salvo tablas de catálogo con clave natural.
- `created_at`, `updated_at` en todas las tablas mutables.
- `deleted_at` solo en `agents`, `backup_plans` y `repositories` (borrado lógico).
- JSONB para datos variables, validados por esquema en aplicación.
- Bytes en `bigint`; nunca `float`.
- Secretos cifrados con envelope encryption; la BD nunca ve el texto plano.
- Auditoría append-only; la aplicación no tiene `UPDATE`/`DELETE` sobre `audit_log`.
- Nombres en inglés en la BD y el código; documentación en español.

---

## 2. Diagrama entidad-relación (resumen)

```
users ──< sessions
  │
  └──< audit_log

agents ──< agent_inventory
  │   ├──< disks ──< partitions
  │   ├──< jobs
  │   ├──< restore_points ──< manifests ──< snapshot_segments
  │   ├──< agent_updates
  │   └──< agent_group_members >── agent_groups

backup_plans ──< plan_assignments >── agents
  │                           └──>── agent_groups   (una de las dos)
  └──< plan_runs ──< jobs

repositories ──< storage_targets
  │           └──< replication_runs
  └──< restore_points

proxmox_clusters ──< proxmox_nodes ──< proxmox_storages
pbs_servers ──< pbs_datastores ──< pbs_snapshots

restore_points ──< restores ──< restore_steps
restore_points ──< verifications
agents ──< alerts
```

---

## 3. Identidad y seguridad

### `users`

| Columna | Tipo | Notas |
|---|---|---|
| id | uuid PK | |
| email | citext UNIQUE | |
| password_hash | text | argon2id |
| role | text | `admin`, `operator`, `viewer` |
| totp_secret_enc | bytea | cifrado |
| totp_enabled | boolean | |
| disabled_at | timestamptz | |
| last_login_at | timestamptz | |
| created_at/updated_at | timestamptz | |

### `sessions`

| Columna | Tipo | Notas |
|---|---|---|
| id | uuid PK | |
| user_id | uuid FK users | |
| token_hash | bytea | no se guarda el token |
| csrf_token | text | |
| ip | inet | |
| user_agent | text | |
| mfa_at | timestamptz | |
| expires_at | timestamptz | 12 h |
| revoked_at | timestamptz | |

### `recovery_codes`

`id`, `user_id`, `code_hash`, `used_at`.

### `api_tokens`

`id`, `name`, `token_hash`, `scopes[]`, `created_by`, `expires_at`, `last_used_at`.

### `secrets`

| Columna | Tipo | Notas |
|---|---|---|
| id | uuid PK | |
| name | text UNIQUE | |
| ciphertext | bytea | AES-256-GCM |
| nonce | bytea | |
| key_version | int | rotación |
| aad | bytea | nombre + id |
| created_at/rotated_at | timestamptz | |

### `audit_log`

| Columna | Tipo | Notas |
|---|---|---|
| id | uuid PK | |
| at | timestamptz | particionado mensual |
| actor_type | text | `user`, `agent`, `system` |
| actor_id | uuid/text | |
| action | text | |
| object_type | text | |
| object_id | text | |
| ip | inet | |
| user_agent | text | |
| request_id | text | |
| before | jsonb | |
| after | jsonb | |
| prev_hash | bytea | hash chain |
| hash | bytea | |

---

## 4. Agentes e inventario

### `agents`

| Columna | Tipo | Notas |
|---|---|---|
| id | uuid PK | = CN del certificado |
| hostname | text | |
| fqdn | text | |
| os_family | text | `windows`, `linux` |
| os_version | text | |
| arch | text | `amd64`, `arm64` |
| agent_version | text | |
| cert_serial | text | |
| cert_fingerprint | text | |
| cert_expires_at | timestamptz | |
| public_key | bytea | |
| status | text | `pending`, `active`, `offline`, `revoked` |
| last_seen_at | timestamptz | |
| labels | jsonb | |
| repository_id | uuid FK repositories | |
| kopia_username | text | `agent-<uuid>` |
| kopia_password_secret_id | uuid FK secrets | |
| enrolled_at | timestamptz | |
| revoked_at | timestamptz | |

### `agent_groups`

| Columna | Tipo | Notas |
|---|---|---|
| id | uuid PK | |
| name | text UNIQUE | |
| description | text | |
| parent_group_id | uuid FK agent_groups | jerarquía opcional; sin ciclos |
| labels | jsonb | |
| created_at/updated_at | timestamptz | |

### `agent_group_members`

| Columna | Tipo | Notas |
|---|---|---|
| agent_id | uuid FK agents | |
| group_id | uuid FK agent_groups | |
| added_at | timestamptz | |

PK `(agent_id, group_id)`. Un equipo puede pertenecer a varios grupos; las asignaciones se resuelven con la sección «Resolución de planes efectivos».

### `enrollment_tokens`

`id`, `token_prefix`, `token_hash`, `created_by`, `expires_at`, `used_at`, `used_by_agent_id`, `expected_hostname`, `max_uses` (default 1), `labels`.

### `agent_inventory`

| Columna | Tipo | Notas |
|---|---|---|
| id | uuid PK | |
| agent_id | uuid FK agents | |
| collected_at | timestamptz | |
| hash | text | evita duplicados |
| os | jsonb | build, kernel, etc. |
| hardware | jsonb | CPU, RAM, fabricante |
| disks | jsonb | |
| partitions | jsonb | |
| volumes | jsonb | LVM/btrfs/ZFS |
| security | jsonb | UEFI, Secure Boot, TPM, BitLocker/LUKS |
| network | jsonb | |

### `disks` / `partitions`

Vista materializada del último inventario para consultas y preflight.

`disks`: `id`, `agent_id`, `index`, `model`, `serial`, `size_bytes`, `bus`, `partition_table`, `disk_guid`, `is_system`.

`partitions`: `id`, `disk_id`, `index`, `offset_bytes`, `size_bytes`, `type_guid`, `fs`, `label`, `volume_guid`, `mountpoint`, `bitlocker_state`, `role` (`efi`, `msr`, `recovery`, `data`).

---

## 5. Repositorios y almacenamiento

### `repositories`

| Columna | Tipo | Notas |
|---|---|---|
| id | uuid PK | |
| name | text UNIQUE | |
| kind | text | `kopia` |
| config_enc | bytea | conexión cifrada |
| master_key_secret_id | uuid FK secrets | |
| status | text | `online`, `degraded`, `offline` |
| capacity_bytes | bigint | |
| used_bytes | bigint | |
| last_maintenance_at | timestamptz | |
| object_lock_mode | text | `none`, `GOVERNANCE`, `COMPLIANCE` |
| object_lock_days | int | |
| created_at/updated_at | timestamptz | |

### `storage_targets`

| Columna | Tipo | Notas |
|---|---|---|
| id | uuid PK | |
| repository_id | uuid FK | |
| role | text | `primary`, `replica` |
| kind | text | `filesystem`, `s3` |
| endpoint | text | |
| bucket | text | |
| region | text | |
| credentials_secret_id | uuid FK secrets | |
| object_lock | boolean | |
| lifecycle_days | int | |
| created_at/updated_at | timestamptz | |

### `replication_runs`

`id`, `repository_id`, `target_id`, `state`, `started_at`, `finished_at`, `point_in_time`, `delete_mode`, `bytes_copied`, `snapshots_copied`, `error`.

---

## 6. Planes

### `backup_plans`

| Columna | Tipo | Notas |
|---|---|---|
| id | uuid PK | |
| name | text | |
| kind | text | `file`, `image`, `pbs` |
| enabled | boolean | |
| schedule_cron | text | |
| timezone | text | |
| retention | jsonb | `{daily, weekly, monthly, annual}` |
| compression | text | `zstd` |
| image_mode | text | `vss_volumes`, `raw_disk`, `lvm_volumes`, `btrfs_volumes`, `zfs_volumes`, `crash_direct` |
| paths | text[] | |
| excludes | text[] | |
| repository_id | uuid FK | |
| bandwidth_kbps | int | |
| cpu_priority | text | `low`, `normal` |
| verify_policy | jsonb | niveles y frecuencia |
| window_start/window_end | time | |
| created_by | uuid FK users | |
| created_at/updated_at | timestamptz | |

### `plan_assignments`

| Columna | Tipo | Notas |
|---|---|---|
| id | uuid PK | |
| plan_id | uuid FK backup_plans | |
| agent_id | uuid FK agents NULL | |
| group_id | uuid FK agent_groups NULL | |
| enabled | boolean | |
| overrides | jsonb | ventana, límites, exclusiones |
| next_run_at | timestamptz | |
| last_run_at | timestamptz | |

Restricciones:

```sql
CHECK (num_nonnulls(agent_id, group_id) = 1)
UNIQUE (plan_id, agent_id) WHERE agent_id IS NOT NULL
UNIQUE (plan_id, group_id) WHERE group_id IS NOT NULL
```

### Resolución de planes efectivos

1. Un plan asignado directamente al agente **prevalece** sobre el mismo plan asignado a un grupo.
2. Un agente puede recibir varios planes distintos; cada uno se programa de forma independiente.
3. Si dos asignaciones del **mismo** plan llegan por grupos diferentes, no se duplica: se aplica una sola vez y la UI marca la coincidencia.
4. Los `overrides` del agente se aplican después de los del grupo.
5. Un cambio de membresía recalcula `next_run_at` de los planes afectados.
6. El agente recibe su lista efectiva en `GET /agent/plans`; no resuelve jerarquías por su cuenta.

### `plan_runs`

`id`, `plan_id`, `scheduled_at`, `state`, `materialized_job_id`.

### `retention_policies`

Opcional si la retención vive en `backup_plans`; se separa si se reutiliza entre planes. `id`, `name`, `daily`, `weekly`, `monthly`, `annual`, `keep_last`, `created_at`.

---

## 7. Jobs

### `jobs`

| Columna | Tipo | Notas |
|---|---|---|
| id | uuid PK | |
| type | text | `BACKUP_FILE`, `BACKUP_IMAGE`, `RESTORE_FILES`, `RESTORE_VM`, `VERIFY`, `SYNC`, `PBS_BACKUP`, `PBS_RESTORE`, `PBS_VERIFY`, `PBS_PRUNE`, `PBS_GC`, `MAINTENANCE` |
| state | text | ver enums |
| priority | int | |
| plan_id | uuid FK | |
| agent_id | uuid FK | |
| repository_id | uuid FK | |
| restore_id | uuid FK restores | |
| payload | jsonb | validado por tipo |
| idempotency_key | text UNIQUE | |
| scheduled_at | timestamptz | |
| lease_owner | text | |
| lease_expires_at | timestamptz | |
| attempt | int | |
| max_attempts | int | |
| started_at/finished_at | timestamptz | |
| progress_pct | int | |
| bytes_total/bytes_done | bigint | |
| error_code | text | |
| error_message | text | |
| cancel_requested_at | timestamptz | |
| parent_job_id | uuid FK jobs | |
| created_at/updated_at | timestamptz | |

Índices: `(state, priority, scheduled_at)`, `(agent_id, created_at DESC)`, `(plan_id, created_at DESC)`.

### `job_attempts`

`id`, `job_id`, `attempt`, `state`, `started_at`, `finished_at`, `error_code`, `agent_log_ref`.

### `job_events`

`id`, `job_id`, `at`, `level`, `code`, `message`, `data` jsonb. Append-only; SSE.

---

## 8. Restore points y manifiestos

### `restore_points`

| Columna | Tipo | Notas |
|---|---|---|
| id | uuid PK | |
| agent_id | uuid FK | |
| plan_id | uuid FK | |
| kind | text | `file`, `image`, `pbs` |
| repository_id | uuid FK | |
| captured_at | timestamptz | |
| consistency | text | `crash`, `application` |
| size_logical_bytes | bigint | |
| size_stored_bytes | bigint | |
| manifest_id | uuid FK manifests | |
| verification_state | text | `pending`, `passed`, `failed` |
| retention_class | text | |
| expires_at | timestamptz | |
| tags | jsonb | |
| pbs_snapshot_id | text | |
| created_at | timestamptz | |

Índice `(agent_id, captured_at DESC)`.

### `manifests`

`id`, `restore_point_id`, `schema_version`, `sha256`, `body` jsonb, `location` (`db`, `repo`). `body` inmutable.

### `snapshot_segments`

`id`, `restore_point_id`, `disk_index`, `segment_index`, `name`, `offset_bytes`, `size_bytes`, `sha256`, `kopia_snapshot_id`, `state` (`pending`, `uploading`, `complete`, `failed`). UNIQUE `(restore_point_id, disk_index, segment_index)`.

---

## 9. Proxmox y PBS

### `proxmox_clusters`

`id`, `name`, `api_url`, `token_secret_id`, `ca_pin`, `enabled`, `created_at`.

### `proxmox_nodes`

`id`, `cluster_id`, `name`, `status`, `last_seen_at`, `agent_installed`, `agent_version`, `capabilities` jsonb.

### `proxmox_storages`

`id`, `node_id`, `storage_id`, `type`, `content`, `avail_bytes`, `shared`, `supports_import`, `updated_at`.

### `pbs_servers`

`id`, `name`, `api_url`, `token_secret_id`, `fingerprint`, `version`, `created_at`.

### `pbs_datastores`

`id`, `server_id`, `name`, `usage` jsonb, `s3_backend` boolean, `last_sync_at`.

### `pbs_snapshots`

`id`, `datastore_id`, `backup_type`, `backup_id`, `backup_time`, `size`, `verification_state`, `protected`.

---

## 10. Restauraciones y verificaciones

### `restores`

| Columna | Tipo | Notas |
|---|---|---|
| id | uuid PK | |
| restore_point_id | uuid FK | |
| kind | text | `file`, `vm` |
| target_agent_id | uuid FK | |
| request | jsonb | nodo, storage, VMID, CPU, RAM, red |
| state | text | `preflight`, `staging`, `creating_vm`, `booting`, `verifying`, `succeeded`, `failed_boot`, `failed`, `canceled` |
| created_by | uuid FK users | |
| approved_by | uuid FK users | regla de dos personas para VM |
| started_at/finished_at | timestamptz | |
| result | jsonb | |

### `restore_steps`

`id`, `restore_id`, `seq`, `name`, `state`, `started_at`, `finished_at`, `detail`.

### `verifications`

`id`, `restore_point_id`, `level` (1/2/3), `state`, `started_at`, `finished_at`, `details`, `vm_id`, `result`.

---

## 11. Alertas, actualizaciones y custodia

### `alerts`

`id`, `rule_id`, `severity`, `state` (`open`, `ack`, `resolved`), `subject_type`, `subject_id`, `first_seen`, `last_seen`, `resolved_at`, `dedup_key`.

### `alert_rules`

`id`, `name`, `expr`, `severity`, `channels` jsonb, `enabled`.

### `notifications`

`id`, `alert_id`, `channel`, `sent_at`, `status`, `error`.

### `agent_binaries`

`id`, `version`, `os`, `arch`, `sha256`, `signature`, `published_at`, `rollout_pct`, `revoked`.

### `agent_updates`

`id`, `agent_id`, `from_version`, `to_version`, `state`, `started_at`, `finished_at`, `error`.

### `key_custody_shares`

`id`, `holder`, `share_index`, `share_hash`, `created_at`, `last_verified_at`. **Nunca se guarda el share en la BD.**

### `maintenance_windows`

`id`, `name`, `cron`, `scope`, `enabled`.

### `settings`

`key` PK, `value` jsonb, `key_version`, `updated_by`, `updated_at`.

---

## 12. Enums

| Enum | Valores |
|---|---|
| `agent_status` | `pending`, `active`, `offline`, `revoked` |
| `job_state` | `pending`, `queued`, `leased`, `running`, `succeeded`, `failed`, `canceled`, `expired` |
| `restore_state` | `preflight`, `staging`, `creating_vm`, `booting`, `verifying`, `succeeded`, `failed_boot`, `failed`, `canceled` |
| `consistency` | `crash`, `application` |
| `verification_state` | `pending`, `passed`, `failed` |
| `backup_kind` | `file`, `image`, `pbs` |
| `image_mode` | `vss_volumes`, `raw_disk`, `lvm_volumes`, `btrfs_volumes`, `zfs_volumes`, `crash_direct` |
| `repository_status` | `online`, `degraded`, `offline` |
| `object_lock_mode` | `none`, `GOVERNANCE`, `COMPLIANCE` |
| `assignment_target` | `agent`, `group` |

---

## 13. Invariantes y constraints

1. `restore_points` inmutable salvo `verification_state` y `expires_at`.
2. `manifests.body` inmutable; `schema_version` obligatorio.
3. `jobs.idempotency_key` UNIQUE; mismo cuerpo + clave = replay; distinto = 409.
4. Un agente no puede tener dos jobs activos del mismo plan (lease único).
5. `snapshot_segments` UNIQUE por `(restore_point, disk, segment)`.
6. `audit_log` sin `UPDATE`/`DELETE` para el rol de aplicación.
7. `secrets` nunca se devuelve por API; solo se referencia por ID.
8. `agents.kopia_username` UNIQUE.
9. `plan_assignments` apunta exactamente a un agente **o** a un grupo (`num_nonnulls = 1`), con unicidad parcial por objetivo.
10. `pbs_snapshots` UNIQUE `(datastore_id, backup_id, backup_time)`.
11. `agent_group_members` PK `(agent_id, group_id)`; `agent_groups.parent_group_id` no puede formar ciclos.
12. Un agente recibe un plan como máximo una vez, aunque llegue por varios grupos.

---

## 14. Índices y particionado

- `audit_log`: partición mensual; retención por defecto **24 meses** (configurable); export a S3 con Object Lock.
- `job_events`: partición mensual; retención por defecto **90 días**; los eventos de restauraciones y verificaciones se conservan **12 meses** o se archivan.
- `job_attempts`: retención por defecto **12 meses**.
- `restore_points`: índice `(agent_id, captured_at DESC)`, `(repository_id, expires_at)`.
- `jobs`: índice parcial `WHERE state IN ('pending','queued','leased','running')`.
- `alerts`: índice `(state, severity, last_seen DESC)`.
- `agent_group_members`: índice inverso `(group_id, agent_id)`.
- Búsqueda de texto de hostname/etiquetas con `pg_trgm` si es necesario.

---

## 15. Migraciones y evolución

- Migraciones versionadas y reversibles; una migración no elimina columnas en el mismo release que deja de usarlas.
- Cambios de `schema_version` del manifiesto: compatibilidad hacia atrás documentada; un restaurador rechaza versiones desconocidas.
- Rotación de claves: `key_version` permite recifrar por lotes sin downtime.
- Backup de la BD: `pg_dump` diario + WAL archiving si se requiere RPO bajo; el catálogo se puede reconstruir parcialmente desde los manifiestos del repositorio.

---

## 16. Preguntas abiertas

- ¿Se necesita multi-repositorio por grupo para aislar contenidos en Kopia?
- ¿La retención se modela en el plan o en una tabla separada reutilizable?
- ¿Se guarda el inventario histórico completo o solo el último hash + cambios?
- ¿El catálogo de PBS se replica en PostgreSQL o se consulta en vivo con caché?

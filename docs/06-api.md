# Contrato de API

**Documento:** 06-api  
**Versión:** 1.1  
**Base:** `/api/v1`  
**Formato:** JSON UTF-8  
**Autenticación:** mTLS para agentes; sesión + TOTP para administración

---

## 1. Convenciones

- Versión en la ruta; cambios incompatibles → `/api/v2`.
- Errores RFC 7807: `type`, `title`, `status`, `detail`, `code`, `request_id`.
- Paginación por cursor: `?cursor=<opaque>&limit=50`; respuesta `{items, next_cursor}`.
- `X-Request-Id` en todas las peticiones; si no se envía, el servidor lo genera.
- `Idempotency-Key` obligatorio en POST que crean jobs o recursos. Se guarda hash del cuerpo 24 h:
  - misma clave + mismo cuerpo → replay de la respuesta;
  - misma clave + cuerpo distinto → `409`.
- Fechas ISO 8601 UTC.
- Bytes como enteros (`int64`), nunca strings con unidades.
- Los secretos nunca se devuelven; solo IDs y metadatos.
- Límite de cuerpo: 1 MiB para API admin, 8 MiB para logs de agente; los segmentos de backup no van por esta API.

---

## 2. Autenticación

### 2.1 Administración

| Método | Ruta | Función |
|---|---|---|
| POST | `/auth/login` | Email + contraseña; devuelve sesión y reto TOTP si aplica |
| POST | `/auth/mfa` | Código TOTP; completa la sesión |
| POST | `/auth/step-up` | Re-autenticación para operaciones destructivas |
| POST | `/auth/logout` | Revoca la sesión |
| GET | `/auth/me` | Usuario, rol, sesión y caducidad |

Cookie `bdr_session` HttpOnly, Secure, SameSite=Lax; CSRF token en cabecera `X-CSRF-Token` para métodos mutables.

### 2.2 Agentes

- mTLS obligatorio en `:8443`; la identidad es el SAN del certificado.
- El cuerpo nunca puede declarar otro `agent_id`.
- El enrolamiento usa `Authorization: Bearer <token de un solo uso>` sobre TLS con pin del servidor.
- La API key efímera del repositorio se obtiene con `GET /agent/repository-credentials` y se usa contra Kopia, no contra esta API.

---

## 3. API de agente

| Método | Ruta | Función |
|---|---|---|
| POST | `/agent/enroll` | Token + CSR → certificado, CA y configuración inicial |
| POST | `/agent/renew-cert` | Renovación mTLS con nuevo CSR |
| GET | `/agent/me` | Identidad, versión, plan y ventanas |
| POST | `/agent/heartbeat` | Estado, versión, job actual, métricas y cancelación |
| PUT | `/agent/inventory` | Inventario completo con hash |
| GET | `/agent/plans` | Planes asignados |
| POST | `/agent/jobs/lease` | Long-poll 25 s; devuelve job o 204 |
| POST | `/agent/jobs/{id}/ack` | Confirma inicio y `lease_id` |
| POST | `/agent/jobs/{id}/progress` | Progreso y heartbeat del job |
| POST | `/agent/jobs/{id}/segments/{n}` | Segmento completado (nombre, sha256, snapshot ID) |
| POST | `/agent/jobs/{id}/complete` | Resultado final, estadísticas y error |
| POST | `/agent/jobs/{id}/log` | Chunks de log comprimidos |
| GET | `/agent/jobs/{id}/cancel` | Consulta de cancelación |
| GET | `/agent/repository-credentials` | Credencial efímera del repositorio |
| GET | `/agent/updates/check` | Comprueba versión firmada |
| GET | `/agent/updates/{version}/download` | Descarga firmada |
| POST | `/agent/restores/{id}/steps/{seq}` | Progreso de restauración (agente PVE) |

### 3.1 Ejemplo de enrolamiento

```http
POST /api/v1/agent/enroll
Authorization: Bearer <token>
Content-Type: application/json

{
  "hostname": "srv01",
  "os_family": "windows",
  "os_version": "Server 2025",
  "arch": "amd64",
  "agent_version": "1.0.0",
  "csr_pem": "-----BEGIN CERTIFICATE REQUEST-----..."
}
```

Respuesta:

```json
{
  "agent_id": "0192f...",
  "certificate_pem": "-----BEGIN CERTIFICATE-----...",
  "ca_pem": "-----BEGIN CERTIFICATE-----...",
  "server_fingerprint": "sha256:...",
  "repository": {
    "id": "0192a...",
    "server_url": "https://repo.example:51515",
    "username": "agent-0192f..."
  },
  "heartbeat_seconds": 30
}
```

La contraseña del repositorio se entrega por `GET /agent/repository-credentials`, con TTL corto y rotación; nunca se incluye en el enrolamiento.

### 3.2 Heartbeat

```http
POST /api/v1/agent/heartbeat

{
  "agent_version": "1.0.0",
  "state": "RUNNING_BACKUP",
  "current_job_id": "0192b...",
  "metrics": {
    "cpu_pct": 12.4,
    "ram_used_mb": 2048,
    "disks": [{"mount": "C:", "free_bytes": 123456789}]
  },
  "cancel_ack": []
}
```

Respuesta:

```json
{
  "server_time": "2026-11-16T02:00:00Z",
  "pending_jobs": 1,
  "cancel_requested": ["0192b..."],
  "config_version": 7
}
```

### 3.3 Lease de job

```http
POST /api/v1/agent/jobs/lease
{"wait_seconds": 25}
```

Respuesta `200`:

```json
{
  "job": {
    "id": "0192b...",
    "type": "BACKUP_FILE",
    "lease_id": "lease-...",
    "lease_expires_at": "2026-11-16T02:05:00Z",
    "payload": {
      "plan_id": "0192c...",
      "paths": ["C:\\Users", "D:\\Data"],
      "excludes": ["C:\\Windows\\Temp"],
      "repository_id": "0192a..."
    }
  }
}
```

Sin trabajo: `204 No Content`.

### 3.4 Progreso y segmentos

```http
POST /api/v1/agent/jobs/{id}/progress

{
  "lease_id": "lease-...",
  "bytes_done": 10737418240,
  "bytes_total": 53687091200,
  "percent": 20,
  "message": "segmento 5/25"
}
```

```http
POST /api/v1/agent/jobs/{id}/segments/5

{
  "lease_id": "lease-...",
  "disk_index": 0,
  "segment_index": 5,
  "name": "disk0.seg0005",
  "offset_bytes": 21474836480,
  "size_bytes": 2147483648,
  "sha256": "hex...",
  "kopia_snapshot_id": "k..."
}
```

### 3.5 Finalización

```http
POST /api/v1/agent/jobs/{id}/complete

{
  "lease_id": "lease-...",
  "state": "succeeded",
  "snapshot_id": "k...",
  "bytes_read": 53687091200,
  "bytes_uploaded": 1073741824,
  "duration_seconds": 3600,
  "consistency": "application",
  "error": null
}
```

Errores permanentes: `state=failed`, `error.code` del catálogo.

---

## 4. API de administración

### 4.1 Recursos

| Método | Ruta | Función |
|---|---|---|
| GET/POST | `/admin/users` | Usuarios |
| GET/POST/DELETE | `/admin/api-tokens` | Tokens de integración |
| GET | `/admin/agents` | Inventario y estado |
| GET | `/admin/agents/{id}` | Ficha, inventario y último backup |
| POST | `/admin/agents/{id}/revoke` | Revoca certificado y acceso |
| POST | `/admin/enrollment-tokens` | Crea token de un solo uso |
| GET | `/admin/agents/{id}/inventory` | Histórico de inventario |
| GET/POST | `/admin/groups` | Grupos de equipos |
| GET/PATCH/DELETE | `/admin/groups/{id}` | Edición y borrado |
| GET/POST | `/admin/groups/{id}/members` | Consulta y alta de miembros |
| DELETE | `/admin/groups/{id}/members/{agentId}` | Baja de miembro |
| GET | `/admin/agents/{id}/effective-plans` | Planes efectivos tras resolver grupos y overrides |
| GET/POST | `/admin/repositories` | Repositorios |
| POST | `/admin/repositories/{id}/test` | Prueba de conexión |
| GET | `/admin/repositories/{id}/status` | Estado, capacidad, maintenance |
| POST | `/admin/repositories/{id}/sync` | Lanza réplica |
| GET/POST | `/admin/storage-targets` | NAS/S3 |
| GET/POST | `/admin/backup-plans` | Planes (los de imagen admiten `disks` para limitar discos capturados) |
| POST | `/admin/backup-plans/{id}/validate` | Valida cron, rutas/discos, retención |
| GET/POST | `/admin/backup-plans/{id}/assignments` | Asignaciones a agente **o** grupo (`target_type`, `target_id`) |
| DELETE | `/admin/backup-plans/{id}/assignments/{assignmentId}` | Elimina asignación |
| POST | `/admin/agents/{id}/backup` | Backup bajo demanda |
| GET | `/admin/jobs` | Lista y filtros |
| GET | `/admin/jobs/{id}` | Detalle |
| GET | `/admin/jobs/{id}/events` | SSE de eventos |
| POST | `/admin/jobs/{id}/cancel` | Cancelación cooperativa |
| POST | `/admin/jobs/{id}/retry` | Reintento |
| GET | `/admin/restore-points` | Puntos recuperables |
| GET | `/admin/restore-points/{id}` | Manifiesto y metadatos |
| GET | `/admin/restore-points/{id}/files?path=` | Navegación de archivos |
| GET | `/admin/restore-points/{id}/files/content?path=` | Descarga de un archivo por streaming (sin cargarlo en memoria) |
| GET | `/admin/reports` | Informes generados |
| POST | `/admin/reports` | Genera un informe bajo demanda |
| GET/PUT | `/admin/reports/schedules` | Programación de informes |
| POST | `/admin/restore-points/{id}/verify` | Verificación |
| POST | `/admin/restores/files` | Restauración de archivos |
| POST | `/admin/restores/proxmox` | Restauración como VM |
| POST | `/admin/restores/{id}/approve` | Aprobación de dos personas |
| GET | `/admin/restores/{id}` | Estado y pasos |
| POST | `/admin/restores/{id}/cancel` | Cancelación |
| GET | `/admin/proxmox/resources` | Nodos, storages y bridges |
| POST | `/admin/proxmox/discover` | Descubre recursos |
| GET | `/admin/pbs/servers` | PBS |
| GET | `/admin/pbs/datastores` | Datastores |
| GET | `/admin/pbs/snapshots` | Snapshots |
| POST | `/admin/pbs/verify` | Verify |
| POST | `/admin/pbs/prune` | Prune |
| POST | `/admin/pbs/gc` | GC |
| GET | `/admin/verifications` | Verificaciones |
| GET | `/admin/alerts` | Alertas |
| POST | `/admin/alerts/{id}/ack` | Acuse |
| POST | `/admin/alerts/{id}/resolve` | Resolución |
| GET/POST | `/admin/alert-rules` | Reglas |
| GET | `/admin/audit` | Auditoría |
| GET | `/admin/audit/export` | Exportación |
| GET/PUT | `/admin/settings` | Configuración |
| GET | `/admin/system/health` | Salud |
| GET | `/admin/system/version` | Versión |

### 4.2 Ejemplo de restauración a Proxmox

```http
POST /api/v1/admin/restores/proxmox
Idempotency-Key: 8f14e45f-ceea-467f-9c1e-...
Content-Type: application/json

{
  "restore_point_id": "0192d...",
  "cluster_id": "0192e...",
  "node": "pve01",
  "storage": "local-zfs",
  "vmid": 210,
  "name": "srv01-restored",
  "cores": 8,
  "memory_mb": 32768,
  "bridge": "vmbr0",
  "firmware": "ovmf",
  "tpm": true,
  "secure_boot": true,
  "disk_bus": "sata",
  "nic_model": "e1000",
  "start": true,
  "test_mode": false
}
```

Respuesta `202 Accepted`:

```json
{
  "restore_id": "0192f...",
  "job_id": "0192g...",
  "state": "preflight"
}
```

La operación es asíncrona. El cliente consulta `/admin/restores/{id}` o escucha SSE.

### 4.3 Asignación de planes a agentes y grupos

```http
POST /api/v1/admin/backup-plans/{planId}/assignments
Idempotency-Key: 3b241101-e2bb-4255-8caf-4136c566a962

{
  "target_type": "group",
  "target_id": "0192h...",
  "enabled": true,
  "overrides": {
    "window_start": "01:00",
    "window_end": "05:00",
    "bandwidth_kbps": 50000
  }
}
```

Reglas:

- `target_type` ∈ `agent`, `group`; exactamente uno.
- Una asignación a agente prevalece sobre una asignación del mismo plan a grupo.
- Un agente recibe el plan una sola vez aunque esté en varios grupos; la respuesta incluye `effective` y `shadowed_by` cuando aplica.
- Un cambio de membresía recalcula `next_run_at`.
- Ciclos de grupos → `E_GROUP_CYCLE`.

### 4.4 Navegación y descarga de archivos

- `GET /admin/restore-points/{id}/files?path=` devuelve metadatos (nombre, tipo, tamaño, mtime), no contenido.
- `GET /admin/restore-points/{id}/files/content?path=` devuelve el contenido por streaming, con `Content-Length` si se conoce y soporte de `Range` cuando el backend lo permita.
- Ambas rutas exigen permiso de lectura y quedan auditadas; un archivo no se materializa completo en memoria.
- La ruta se normaliza y se valida contra el prefijo del snapshot para evitar traversal.

### 4.5 Acceso por pantalla (VNC)

Acceso al escritorio del equipo gestionado desde el panel (visor canvas
propio con protocolo de tiles estilo MeshCentral).
Mismo modelo que el canal de terminal remota: el agente mantiene una
conexión WebSocket **saliente** con el servidor (`GET /api/v1/agent/screen/connect`,
mTLS + API key) y el servidor solo autoriza y retransmite; nunca hay puertos
entrantes en el equipo. El servidor VNC local solo escucha en `127.0.0.1`.

Endpoints (caps `screen:*`):

```http
GET  /api/v1/admin/agents/{id}/screen                    (screen:read)
POST /api/v1/admin/agents/{id}/screen/sessions           (screen:connect)
GET  /api/v1/admin/screen/sessions                       (screen:read)
GET  /api/v1/admin/screen/sessions/{id}                  (screen:read)
POST /api/v1/admin/screen/sessions/{id}/close            (screen:terminate)
GET  /api/v1/admin/screen/sessions/{id}/stream           (screen:connect o ?ticket=…)
```

`POST …/screen/sessions` acepta `{ "quality": "low|medium|high",
"allow_clipboard": bool }` (ambos opcionales) y responde `201` con
`session_id`, `stream_url` y `stream_ticket` (solo en la creación: los
WebSockets del navegador no pueden enviar `Authorization`, así que presentan
el ticket como query param). `GET …/stream` es el WebSocket de datos:
transporta **mensajes del protocolo de tiles** (binarios: screen_info con el
tamaño, tiles JPEG de 64 px con su XY, y entrada de ratón/teclado del
operador — ver `packages/contracts/screenaccess.go`); el motivo de fin de
sesión viaja en el frame de cierre de WebSocket.

Límites y auditoría: máximo de 2 sesiones de pantalla simultáneas por
agente, TTL de sesión de 30 minutos, reaper de sesiones pendientes/expiradas
y auditoría `screen.session.created` / `screen.session.closed`. El cierre del
visor cierra la sesión. Backends por plataforma (sin instalar nada): Windows —
helper de sesión nativo del agente (captura GDI + SendInput); Linux — captura
X11 nativa (fallback automático a x11vnc solo-loopback si X11 no es accesible);
macOS — bridge contra el «Compartir pantalla» integrado del sistema.
Requisito en todas: **sesión gráfica activa** — un equipo headless (Proxmox,
servidor solo-TTY) no tiene escritorio que compartir y falla con error claro.
Diagnóstico Windows: `%ProgramData%\bdr\screenhelper.log`.

---

## 5. Ciclo de vida de jobs

```
pending → queued → leased → running → succeeded
                                   ↘ failed
                                   ↘ canceled
                                   ↘ expired
```

- `leased` implica un `lease_id` y un `lease_expires_at`.
- El agente debe enviar heartbeat/progreso antes de expirar; si no, el job vuelve a `queued`.
- `cancel_requested_at` se consulta en heartbeat/lease; la cancelación es cooperativa.
- Un job `failed` puede reintentarse si el error es transitorio; los errores permanentes no.
- `expired` indica que el lease se perdió; se reintenta si quedan intentos.

Estados de restauración: `preflight`, `staging`, `creating_vm`, `booting`, `verifying`, `succeeded`, `failed_boot`, `failed`, `canceled`.

---

## 6. Catálogo de errores

| Código | Significado | Reintentable |
|---|---|---|
| `E_AUTH_TOKEN` | Token inválido o caducado | No |
| `E_CERT_EXPIRED` | Certificado caducado | No (renovar) |
| `E_KOPIA_AUTH` | Credencial de repositorio inválida | No |
| `E_KOPIA_LOCK` | Repositorio bloqueado | Sí |
| `E_KOPIA_QUOTA` | Cuota o espacio | No |
| `E_KOPIA_STREAM` | Error de flujo | Sí |
| `E_KOPIA_CORRUPT` | Repositorio o segmento corrupto | No |
| `E_VSS_CREATE` | No se pudo crear el shadow set | No |
| `E_VSS_WRITER` | Writer VSS (SQL/AD) en estado de error | No |
| `E_VSS_DELTA` | Delta VSS por encima del umbral | No |
| `E_SNAPSHOT_UNSUPPORTED` | FS/volumen no soportado | No |
| `E_BITLOCKER_LOCKED` | BitLocker bloqueado sin clave custodiada | No |
| `E_DISK_LAYOUT_CHANGED` | Layout cambió durante la captura | Sí |
| `E_PVE_AUTH` | Token PVE inválido | No |
| `E_PVE_PRIV` | Permisos insuficientes | No |
| `E_PVE_IMPORT` | Fallo de `import-from` | No |
| `E_PVE_SPACE` | Espacio insuficiente | No |
| `E_BOOT_TIMEOUT` | La VM no arranca en el tiempo esperado | No |
| `E_DRIVER_MISSING` | Faltan drivers (VirtIO) | No |
| `E_MANIFEST_SCHEMA` | Manifiesto desconocido | No |
| `E_PLAN_CONFLICT` | Asignaciones contradictorias del mismo plan | No |
| `E_GROUP_CYCLE` | Jerarquía de grupos cíclica | No |
| `E_IDEMPOTENCY_CONFLICT` | Misma clave, cuerpo distinto | No |
| `E_RATE_LIMIT` | Límite de peticiones | Sí |
| `E_CANCELED` | Cancelado por el usuario | No |

---

## 7. Límites

| Ámbito | Límite |
|---|---|
| Agente | 120 req/min por certificado |
| Enrolamiento | 5/min por IP, 20/h global |
| Admin | 600 req/min por sesión |
| SSE | keepalive 15 s |
| Long-poll | 25 s |
| Body admin | 1 MiB |
| Body logs | 8 MiB |

`429` y `503` incluyen `Retry-After`.

---

## 8. SSE de eventos

`GET /admin/jobs/{id}/events` con `Accept: text/event-stream`.

Eventos:

```
event: progress
data: {"job_id":"...","percent":20,"bytes_done":...,"message":"segmento 5/25"}

event: state
data: {"job_id":"...","state":"running"}

event: log
data: {"level":"info","message":"..."}

event: done
data: {"job_id":"...","state":"succeeded"}
```

La UI no depende de la salida de Kopia; consume estos eventos.

---

## 9. Versionado y compatibilidad

- `/api/v1` estable durante la vida del producto; cambios aditivos permitidos.
- Campos nuevos opcionales; los clientes ignoran desconocidos.
- Eliminación o cambio de tipo → `/api/v2`.
- Los agentes declaran su versión en heartbeat; el servidor puede rechazar versiones incompatibles con `426 Upgrade Required`.
- El contrato se documentará en OpenAPI en la Fase 1; este documento es la fuente normativa inicial.

---

## 10. Preguntas abiertas

- ¿Se usa long-poll o WebSocket para agentes? Long-poll es más simple y suficiente.
- ¿La navegación de archivos se hace por proxy a Kopia o por un servicio de catálogo?
- ¿La aprobación de dos personas es obligatoria para toda restauración a VM o solo para producción?
- ¿Se expone API pública fuera de la LAN? No por defecto.

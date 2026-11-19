# Desarrollo

**Estado:** vertical slice funcional `0.5.0-dev` (Fase 1 + consolidación). Servidor, agente, enrolamiento con **mTLS obligatorio y revocación inmediata**, planes con programación, jobs con idempotencia y cancelación, persistencia en **fichero o PostgreSQL**, Kopia real con credenciales del repositorio (filesystem o **Kopia Repository Server con aislamiento por agente**), restore points con verificación, navegación, búsqueda y descarga de directorios, restauración de archivos, alertas con webhook/email con reintentos, auditoría encadenada, RBAC con sesiones y TOTP, y panel Next.js.

---

## 1. Requisitos

| Herramienta | Versión | Uso |
|---|---|---|
| Go | ≥ 1.25 | Servidor y agente |
| Kopia | ≥ 0.20 | Motor de backup (smoke real; opcional, hay fake) |
| curl | cualquiera | Smoke test y API |
| Python 3 | ≥ 3.8 | Parseo JSON del smoke test |
| Node.js | ≥ 20 | Panel Next.js |
| Docker | cualquiera | Opcional: smoke y tests de contrato con PostgreSQL |

En Windows, usa Git Bash o WSL para los scripts. `make` es opcional.

---

## 2. Compilar y probar

```bash
make build
make test
make vet
make smoke            # E2E con estado en fichero; kopia real si está en el PATH
make smoke-postgres   # el mismo E2E con el estado en PostgreSQL (docker)
make smoke-krs        # el mismo E2E con Kopia Repository Server (aislamiento por agente)
make smoke-web
./scripts/build-agents.sh     # agentes linux/windows + SHA256SUMS en dist/
./scripts/package-deb.sh amd64
```

Equivalente sin Make:

```bash
go build ./...
go test ./...
./scripts/smoke.sh
BDR_SMOKE_STORE=postgres ./scripts/smoke.sh
./scripts/smoke-web.sh
```

Los tests de contrato del store (`contract_test.go`) garantizan la paridad entre `memory`, `file` y `postgres`. Los de PostgreSQL se ejecutan solo con `BDR_TEST_POSTGRES` (DSN de administración, p. ej. `postgres://bdr:bdr@localhost:15432/postgres?sslmode=disable`); sin la variable se omiten.

El smoke test:

1. Compila `bin/bdr-control`, `bin/bdr-agent` y (si no hay kopia) un Kopia falso.
2. Crea el repositorio Kopia del plano de control (`kopia repository create filesystem`) como haría el operador.
3. Arranca el servidor en `127.0.0.1:18080` y comprueba que ve el repositorio.
4. Bootstrap del usuario admin de la API de sesiones.
5. Crea token, enrola el agente, crea plan con programación y exclusiones, asigna y lanza job con `Idempotency-Key`.
6. El agente ejecuta el backup real con Kopia y el job queda `succeeded` al 100 %.
7. Comprueba el restore point, lanza verificación estructural, navega el snapshot, descarga un archivo y compara el contenido.
8. Restaura el archivo en el agente (`RESTORE_FILE`) y compara de nuevo.
9. Provoca un fallo (kopia inexistente) y valida la alerta `job_failed`.
10. Prueba el endpoint SSE de eventos.
11. Reinicia el servidor: persisten agentes, jobs y restore points.
12. Verifica la cadena de auditoría (`/admin/audit/verify`) y el resumen.
13. Comprueba que el token de enrolamiento no se puede reutilizar (410).

---

## 3. Ejecución manual

### 3.1 Crear el repositorio del plano de control (una vez)

```bash
mkdir -p data/control data/repo
KOPIA_PASSWORD=una-clave-de-repo kopia repository create filesystem \
  --path ./data/repo --config-file ./data/control/kopia.config
```

### 3.2 Servidor

```bash
BDR_ADMIN_TOKEN=dev-admin-token \
BDR_KOPIA_PASSWORD=una-clave-de-repo \
BDR_KOPIA_KIND=filesystem \
BDR_KOPIA_REPO_PATH=./data/repo \
go run ./apps/server/cmd/bdr-control \
  --dev --addr 127.0.0.1:8080 --data-dir ./data/control
```

Con TLS para agentes (mTLS opcional):

```bash
... --agent-addr 127.0.0.1:8443 --agent-url https://127.0.0.1:8443 --agent-tls-names 127.0.0.1,localhost
```

Con TLS nativo para la API de administración (en producción, o Caddy delante — `deploy/docker-compose.prod.yml`):

```bash
... --tls-cert ./certs/api.crt --tls-key ./certs/api.key
```

Variables de entorno del servidor:

| Variable | Uso |
|---|---|
| `BDR_ADMIN_TOKEN` | Credencial de administración de arranque |
| `BDR_SECRET_KEY` | Clave AES-256-GCM (base64, 32 bytes) para cifrar secretos; si falta se genera `secret.key` |
| `BDR_DB_URL` | DSN de PostgreSQL para `--store postgres` (alternativa a `--db-url`) |
| `BDR_KOPIA_PASSWORD` | Contraseña que usa el plano de control y que se entrega a los agentes (modo filesystem) |
| `BDR_KOPIA_KIND` / `BDR_KOPIA_REPO_PATH` | Tipo y ruta del repositorio para `repository-credentials` |
| `BDR_KOPIA_SERVER_URL` / `BDR_KOPIA_SERVER_FP` | Alternativa: Kopia Repository Server (usuario por agente) |
| `BDR_ADMIN_EMAIL` / `BDR_ADMIN_PASSWORD` | Crea el primer usuario del panel al arrancar |

### 3.3 Estado en PostgreSQL

```bash
# Con Docker para pruebas; en producción usa un PostgreSQL gestionado.
docker run -d --name bdr-db -e POSTGRES_USER=bdr -e POSTGRES_PASSWORD=bdr \
  -e POSTGRES_DB=bdr -p 5432:5432 postgres:16-alpine

BDR_ADMIN_TOKEN=dev-admin-token \
BDR_DB_URL="postgres://bdr:bdr@localhost:5432/bdr?sslmode=disable" \
BDR_KOPIA_PASSWORD=una-clave-de-repo \
BDR_KOPIA_KIND=filesystem \
BDR_KOPIA_REPO_PATH=./data/repo \
go run ./apps/server/cmd/bdr-control \
  --store postgres --dev --addr 127.0.0.1:8080 --data-dir ./data/control
```

- Las migraciones (`control_state`, `audit_events`) se aplican al arrancar.
- Para importar un `state.json` existente: `--import-state ./data/control/state.json` una sola vez, sobre una base vacía.
- Copia de seguridad: `pg_dump -Fc bdr` / `pg_restore`; estado y auditoría viajan dentro. Los secretos cifrados siguen dependiendo de `BDR_SECRET_KEY`/`secret.key`.
- La decisión de diseño (documento JSONB transaccional + auditoría en tabla propia) está en `docs/adr/0007-store-postgres-jsonb.md`.

### 3.4 Enrolar un agente

```bash
TOKEN=$(curl -sS -X POST \
  -H "Authorization: Bearer dev-admin-token" \
  -H "Content-Type: application/json" \
  -d '{"expires_in_seconds":600}' \
  http://127.0.0.1:8080/api/v1/admin/enrollment-tokens \
  | python3 -c 'import sys,json;print(json.load(sys.stdin)["token"])')

go run ./apps/agent/cmd/bdr-agent register \
  --server http://127.0.0.1:8080 --token "$TOKEN" --data-dir ./data/agent

go run ./apps/agent/cmd/bdr-agent run --once --data-dir ./data/agent
```

Con HTTPS y pin de la CA (recomendado con listener TLS):

```bash
go run ./apps/agent/cmd/bdr-agent register \
  --server https://127.0.0.1:8443 \
  --server-fingerprint sha256:... \
  --token "$TOKEN" --data-dir ./data/agent
```

La huella aparece al arrancar con `--agent-addr` y también en la respuesta de creación de tokens (`server_fingerprint`) y en `GET /auth/me` de la sesión.

### 3.5 Crear un plan y lanzar un backup

```bash
curl -sS -X POST -H "Authorization: Bearer dev-admin-token" -H "Content-Type: application/json" \
  -d '{"name":"datos","kind":"file","paths":["./data"],"excludes":["./data/tmp"],
       "schedule":"0 3 * * *","timezone":"UTC","window":{"start":"01:00","end":"05:00"},
       "retention":{"keep_latest":7,"keep_daily":7,"keep_weekly":4,"keep_monthly":6}}' \
  http://127.0.0.1:8080/api/v1/admin/backup-plans

curl -sS -X POST -H "Authorization: Bearer dev-admin-token" -H "Idempotency-Key: prueba-1" \
  -H "Content-Type: application/json" -d '{}' \
  http://127.0.0.1:8080/api/v1/admin/agents/<AGENT_ID>/backup
```

### 3.6 Panel web (embebido en el binario)

El panel se compila a ficheros estáticos y se sirve desde el propio
`bdr-control` (mismo origen, sin Node en runtime ni CORS):

```bash
make web-build   # npm ci + export estático → apps/server/internal/webui/out
make build       # recompila bin/bdr-control con el panel embebido
```

En desarrollo (panel con recarga en :3000 y API en :8080):

```bash
cd apps/web
NEXT_PUBLIC_API_BASE=http://127.0.0.1:8080 npm run dev
# y el control con CORS de desarrollo:
BDR_ADMIN_TOKEN=dev-admin-token ./bin/bdr-control --dev --addr 127.0.0.1:8080 --cors-origins http://localhost:3000
```

Abre `http://127.0.0.1:3000` e introduce `BDR_ADMIN_TOKEN` (`dev-admin-token` en desarrollo). El panel cubre:

- Resumen con datos de `/admin/summary` (protección, éxito 24 h/7 d, próximos backups, alertas).
- Equipos con ficha, grupos, revocación y **Backup ahora**.
- Planes con programación, ventana, retención, validación y asignación a equipos o grupos.
- Puntos de recuperación con verificación, explorador de archivos y restauración.
- Trabajos con cancelación, reintento, detalle y eventos en vivo.
- Alertas con acuse y resolución.

El panel es un export estático de Next (`output: "export"`) embebido con
`go:embed` en `apps/server/internal/webui/out` y servido por el propio
binario: en producción no hay Node, ni proxy, ni CORS (mismo origen). El
detalle dinámico por equipo se resuelve con `?id=` (Suspense) por
compatibilidad con el export estático.

### 3.7 Usuarios del panel con 2FA (opcional)

```bash
# Primer usuario (solo si no existe ninguno):
curl -X POST http://127.0.0.1:8080/api/v1/auth/bootstrap \
  -H "Content-Type: application/json" \
  -d '{"email":"admin@empresa.test","password":"una-clave-larga"}'

# Login con cookie + CSRF:
curl -c cookies.txt -X POST http://127.0.0.1:8080/api/v1/auth/login \
  -H "Content-Type: application/json" \
  -d '{"email":"admin@empresa.test","password":"una-clave-larga"}'
# Alta de TOTP: POST /auth/totp/setup → secreto y otpauth://
# Confirmación: POST /auth/totp/enable {"code":"123456"} → códigos de recuperación
```

---

## 4. API implementada

### 4.1 Sesiones y usuarios

| Método | Ruta | Auth |
|---|---|---|
| POST | `/api/v1/auth/bootstrap` | solo si no hay usuarios |
| POST | `/api/v1/auth/login` / `/mfa` / `/logout` | credenciales / sesión |
| GET | `/api/v1/auth/me` | sesión, token bdt_ o bearer admin |
| POST | `/api/v1/auth/step-up` / `/password` | sesión (+TOTP) |
| POST | `/api/v1/auth/totp/setup` / `enable` / `disable` | sesión |
| GET/POST | `/api/v1/admin/users` | admin |
| PATCH/DELETE | `/api/v1/admin/users/{id}` | admin |
| GET/POST | `/api/v1/admin/api-tokens` | admin |
| DELETE | `/api/v1/admin/api-tokens/{id}` | admin |

### 4.2 Operación

| Método | Ruta | Auth |
|---|---|---|
| GET | `/api/v1/admin/summary` | lectura |
| GET | `/api/v1/admin/system/health` · `/system/version` | lectura |
| GET | `/api/v1/admin/repositories` | lectura |
| GET/POST | `/api/v1/admin/enrollment-tokens` | operate |
| DELETE | `/api/v1/admin/enrollment-tokens/{id}` | operate |
| GET | `/api/v1/admin/agents` · `/agents/{id}` | lectura |
| POST | `/api/v1/admin/agents/{id}/backup` (Idempotency-Key) | operate |
| POST | `/api/v1/admin/agents/{id}/revoke` | admin |
| GET | `/api/v1/admin/agents/{id}/repo-credentials` | read |
| POST | `/api/v1/admin/agents/{id}/repo-credentials/rotate` | admin |
| POST | `/api/v1/admin/installers` | operate |
| GET | `/api/v1/download/installer/{osarch}/{token}` | público (token en URL) |
| GET | `/api/v1/download/agent/{osarch}` | público |
| GET/POST | `/api/v1/admin/proxmox/instances` | read / admin |
| DELETE | `/api/v1/admin/proxmox/instances/{id}` | admin |
| GET | `/api/v1/admin/proxmox/instances/{id}/vms` | read |
| GET/POST | `/api/v1/admin/storage/targets` | lectura / operate |
| PUT/DELETE | `/api/v1/admin/storage/targets/{id}` | operate / admin |
| POST | `/api/v1/admin/storage/targets/{id}/verify` · `/{id}/sync` | operate |
| GET | `/api/v1/admin/agents/{id}/inventory` · `/effective-plans` | lectura |
| GET/POST | `/api/v1/admin/groups` · `/groups/{id}` · `/groups/{id}/members` | operate |
| GET/POST | `/api/v1/admin/backup-plans` | operate |
| GET/PATCH | `/api/v1/admin/backup-plans/{id}` | operate |
| DELETE | `/api/v1/admin/backup-plans/{id}` | admin |
| POST | `/admin/backup-plans/{id}/validate` | operate |
| GET/POST | `/admin/backup-plans/{id}/assignments` | operate |
| POST | `/admin/backup-plans/{id}/assign` (compatibilidad 0.2) | operate |
| GET | `/api/v1/admin/jobs` (filtros + cursor) · `/jobs/{id}` | lectura |
| POST | `/api/v1/admin/jobs/{id}/cancel` · `/retry` | operate |
| GET | `/api/v1/admin/jobs/{id}/events` (JSON o SSE) | lectura |
| GET | `/api/v1/admin/restore-points` · `/{id}` | lectura |
| POST | `/api/v1/admin/restore-points/{id}/verify` | operate |
| GET | `/api/v1/admin/restore-points/{id}/files?path=` (navegación) | lectura |
| GET | `/api/v1/admin/restore-points/{id}/files/search?q=&path=` | lectura |
| GET | `/api/v1/admin/restore-points/{id}/files/download?path=` (ZIP) | lectura |
| GET | `/api/v1/admin/restore-points/{id}/files/content?path=` (streaming) | lectura |
| POST | `/api/v1/admin/restores/files` (Idempotency-Key) | operate |
| GET | `/api/v1/admin/alerts` | lectura |
| POST | `/api/v1/admin/alerts/{id}/ack` · `/resolve` | operate |
| GET | `/api/v1/admin/audit` · `/audit/verify` · `/audit/export` | admin |
| GET/PUT | `/api/v1/admin/settings` | admin |
| POST | `/api/v1/admin/settings/test-notification` | admin |

### 4.3 Agente

| Método | Ruta | Auth |
|---|---|---|
| POST | `/api/v1/agent/enroll` | token de un solo uso |
| POST | `/api/v1/agent/renew-cert` | API key |
| POST | `/api/v1/agent/heartbeat` | API key |
| PUT | `/api/v1/agent/inventory` | API key |
| GET | `/api/v1/agent/me` · `/agent/plans` | API key |
| POST | `/api/v1/agent/jobs/lease` (long-poll) | API key |
| POST | `/api/v1/agent/jobs/{id}/progress` → `{cancel_requested}` | API key |
| POST | `/api/v1/agent/jobs/{id}/log` | API key |
| GET | `/api/v1/agent/jobs/{id}/cancel` | API key |
| POST | `/api/v1/agent/jobs/{id}/complete` (succeeded/failed/canceled) | API key |
| GET | `/api/v1/agent/repository-credentials` | API key |

Los errores usan `application/problem+json` con `code` y `request_id`. Estados de job: `pending`, `leased`, `running`, `succeeded`, `failed`, `canceled`, `expired`. Un lease caducado vuelve a `pending` y el programador decide el reintento (máx. 3 intentos).

---

## 5. Estructura

```
apps/server/cmd/bdr-control/     binario del servidor
apps/server/internal/control/    router, handlers, scheduler, kopia, notificaciones
apps/server/internal/store/      State tipado + transacciones + auditoría encadenada
apps/server/internal/store/migrations/        SQL versionado embebido (PostgreSQL)
apps/server/internal/store/contract_test.go   batería de contrato memory/file/postgres
apps/server/internal/auth/       tokens, PBKDF2, TOTP
apps/server/internal/pki/        CA interna y certificados de agentes
apps/server/internal/cron/       cron de 5 campos y ventanas
apps/server/internal/secrets/    AES-256-GCM para secretos en reposo
apps/agent/cmd/bdr-agent/        binario del agente
apps/agent/internal/agent/       ciclo: heartbeat concurrente, cancelación, logs
apps/agent/internal/client/      cliente HTTP (+mTLS)
apps/agent/internal/config/      configuración local 0600
apps/agent/internal/identity/    CSR y pin de CA
apps/agent/internal/inventory/   inventario Windows/Linux
apps/agent/internal/jobs/        ejecutor y wrapper de Kopia real
apps/web/                        panel Next.js (App Router + Tailwind)
packages/contracts/              tipos JSON compartidos
packages/logging/                logs JSON con redacción
packages/version/                versión
tools/fake-kopia/                Kopia falso para el smoke sin kopia real
scripts/build-agents.sh          agentes linux/windows + SHA256SUMS
scripts/package-deb.sh           .deb portátil (ar+tar, sin dpkg)
scripts/smoke.sh                 E2E servidor + agente + kopia real (fichero o postgres)
scripts/smoke-web.sh             E2E servidor + panel + proxy
deploy/agent/                    systemd, install.sh, install.ps1
deploy/docker-compose.yml        control + kopia-init + web (perfil postgres: db)
```

---

## 6. Decisiones y límites de esta iteración

- **Almacenamiento en fichero JSON o PostgreSQL, con transacciones.** Un documento `State` tipado con `View`/`Update` serializadas. `store.File` lo persiste en `state.json`; `store.Postgres` lo persiste como documento JSONB (`control_state`) con auditoría en tabla propia (`audit_events`) y migraciones automáticas (docs/adr/0007). El modelo relacional de docs/05 queda como evolución posterior. Batería de contrato compartida entre las tres implementaciones.
- **RBAC con tres roles** y token de administración de arranque para el panel y la automatización. Las sesiones con TOTP son el objetivo; el bearer sigue disponible como «token de servicio».
- **mTLS obligatorio para agentes.** El listener `--agent-addr` exige certificado de cliente válido y **vigente** en cada handshake (sin reutilización de sesión), verificado contra la política del plano de control: revocación o renovación surten efecto de inmediato, sin reinicio. La API key va ligada a la identidad del certificado (SAN `urn:bdr:agent:<id>`): la clave de un agente no funciona sobre el certificado de otro. `--insecure-agents` queda solo para migraciones. El smoke E2E completo transcurre por el listener TLS y comprueba renovación (`bdr-agent renew-cert`) y revocación inmediata.- **Repositorio filesystem compartido o Kopia Repository Server.** En filesystem todos los agentes usan la misma contraseña (modo laboratorio). Con `BDR_KOPIA_KIND=server` cada agente recibe usuario y contraseña propios, aprovisionados y rotados por el plano de control (`docs/adr/0002`); el aislamiento por usuario está verificado en `make smoke-krs`. Kopia 0.23 no liga el hostname en el handshake: las credenciales robadas solo exponen los datos del propio agente y se revocan rotando o revocando el agente.
- **Verificación nivel 1/2 con Kopia.** Estructural siempre disponible si el plano de control tiene conexión directa; contenido por muestreo (`--verify-files-percent`).
- **Un job a la vez por agente.** El heartbeat corre en paralelo al job (mejora de 0.2); el lease se renueva al reportar progreso y la cancelación es cooperativa.
- **Sin imagen, P2V ni ISO.** Cubre la Fase 1 del roadmap: archivos, control plane, panel mínimo y operación.

---

## 7. Siguiente vertical slice

1. Kopia Repository Server con usuario y ACL por agente.
2. mTLS obligatorio para agentes y revocación de certificados.
3. Cola River sobre PostgreSQL (ADR-0006) para el paso a multi-nodo.
4. Explorador con búsqueda y descarga de directorios.
5. Motor de imagen (Fase 2): manifiesto v1, VSS y agente PVE.

Cada paso debe mantener `make test`, `make smoke` y `make smoke-web` verdes.

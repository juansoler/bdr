# Changelog del producto

Formato basado en Keep a Changelog; versionado semántico mientras el producto no llegue a 1.0.

## [0.9.5] — 2026-11-20

### Añadido

- Auto-actualización de agentes coherente de una vez: la oferta lleva URL
  RELATIVA (el agente la resuelve contra el servidor con el que ya hace
  heartbeat — NetBird, LAN — sin depender de --public-url) y el SHA-256 se
  toma de agents.json: el manifiesto de SHAs de los binarios de agente de
  ESTA versión del control, generado en el release, en el build de Docker y
  escrito por el propio self-update junto al binario. La oferta solo se
  emite si el fichero servido coincide con ese SHA — fin del bucle de
  binarios desincronizados.

## [0.10.5] — 2026-11-20

### Añadido

- Destinos S3: campo Región (opcional) — el verify la pasa al cliente
  MinIO y la réplica sync-to ya la incluía. Útil en proveedores que exigen
  la región en la firma (hel1 en Hetzner).
- Mensaje de fallo de escritura con pista accionable cuando el Object Lock
  es la causa (bucket creado sin lock — InvalidRequest en Hetzner).

## [0.10.4] — 2026-11-20

### Corregido

- Destinos S3: validación del nombre de bucket — un "/" o espacios son
  inválidos en S3 y la escritura de prueba falla con InvalidRequest sin
  pista (en S3 no hay carpetas: usa buckets distintos o el campo prefix).

## [0.10.3] — 2026-11-20

### Añadido

- Soporte de agentes en Raspberry Pi de 32 bits: plataforma linux/arm
  (GOARM 7) en la matriz de release, en el manifiesto de agentes y en el
  selector del panel.

## [0.10.2] — 2026-11-20

### Corregido

- Instalador de macOS: verifica que el servicio responde tras el arranque
  (bootstrap + load heredado) y FALLA con el log del daemon si no — antes
  imprimía éxito aunque el control viejo siguiera corriendo.

## [0.10.1] — 2026-11-20

> Publicada sobre la v0.10.0 y sustituye también a la v0.9.5 (misma fecha,
> numeración incorrecta — este es el contenido real del fix de
> auto-actualización de agentes).

### Añadido

- Auto-actualización de agentes coherente de una vez: la oferta lleva URL
  RELATIVA (el agente la resuelve contra el servidor con el que ya hace
  heartbeat — NetBird, LAN — sin depender de --public-url) y el SHA-256 se
  toma de agents.json: el manifiesto de SHAs de los binarios de agente de
  ESTA versión del control, generado en el release, en el build de Docker y
  escrito por el propio self-update junto al binario. La oferta solo se
  emite si el fichero servido coincide con ese SHA — fin del bucle de
  binarios desincronizados.

## [0.10.0] — 2026-11-20

### Añadido

- Instalador de servidor interactivo: pregunta en qué interfaz publicar el
  servicio (LAN, NetBird… detectadas automáticamente) y si usar IP, hostname
  o un nombre DNS propio. Prioridad: --public-host > BDR_PUBLIC_HOST >
  pregunta (solo con terminal; sin terminal publica el hostname con aviso).
- Instalador de Windows: parámetro -PublicHost equivalente, y ahora fija
  --public-url y --agent-tls-names coherentes.

## [0.9.4] — 2026-11-20

### Corregido

- Instaladores de agente: detienen el `bdr-agent` en marcha ANTES de
  descargar el binario — Linux prohíbe reescribir el binario de un proceso
  en ejecución (`ETXTBSY`) y el curl fallaba con (23) escribiendo sobre él.
- Mensaje del fallo de descarga de binario: menciona también las causas
  locales (disco lleno, permisos del destino), no solo el servidor.

## [0.9.3] — 2026-11-20

### Corregido

- One-liner del panel en dos pasos (`curl … -o /tmp/bdr-install.sh && sudo
  bash /tmp/bdr-install.sh`): la sustitución de proceso con sudo daba
  "Bad file descriptor" en macOS (sudo cierra los fds extra).

## [0.9.2] — 2026-11-20

### Corregido

- La descarga del binario de agente ya no depende del estado del
  directorio de descargas del control: si el binario local no está, el
  endpoint REDIRIGE a la release pública de la misma versión, y el
  instalador generado tiene fallback explícito a GitHub. Fin de los
  E_NO_BINARY en los enrolamientos.

## [0.9.1] — 2026-11-20

### Corregido

- Instalador de agente Linux: al re-instalar sobre un agente ya activo,
  reinicia el servicio — sin esto el proceso seguía con la config vieja
  (heartbeats al control anterior) tras re-enrolar.

## [0.9.0] — 2026-11-20

### Añadido

- La actualización del servidor refresca también los binarios de agente del
  directorio de descargas: se descargan de la release firmada (verificación
  SHA-256) en staging ANTES de tocar el binario en marcha — los agentes que
  se enrolen después reciben la versión nueva sin pasos manuales y la oferta
  de actualización no entra en bucle.

### Corregido

- Panel embebido: `/agents` servía la portada del panel (fallback SPA) hasta
  que el router de cliente recuperaba la página — el handler resuelve ahora
  `<ruta>.html` (export plano de Next), reescribe la URL para
  `http.FileServer` y añade tests de regresión.
- Instaladores: mensajes accionables cuando falta el binario del agente en
  el servidor o falla el enrolamiento (antes se enmascaraban con `curl: (23)`).

### Cambios de CI

- `govulncheck` fijado a v1.10.0 (no reproducible con `@latest`).

### Versiones intermedias no publicadas con entrada propia

- 0.8.7: `--kopia-binary` en el launchd de macOS.
- 0.8.8: one-liner `bash <(curl …)` y mensajes accionables del instalador.

## [0.8.6] — 2026-11-19

### Corregido

- Instaladores nativos: inicializan el repositorio Kopia (kopia-init nativo,
  idempotente) y exportan BDR_KOPIA_KIND y BDR_KOPIA_REPO_PATH — sin esto
  los jobs fallaban con 502 E_REPO_PROVISION.
- Unidades: --kopia-binary /opt/bdr/bin/kopia (systemd no ve /opt/bdr/bin
  en el PATH).

## [0.8.5] — 2026-11-19

### Cambiado

- Layout nativo unificado con Docker: binarios en /opt/bdr/bin y agentes en
  /opt/bdr/agents, propiedad del usuario del servicio — la auto-actualización
  del servidor funciona sin ajustes manuales de permisos.

### Corregido

- selfupdate: error accionable cuando el directorio del binario no es
  escribible por el usuario del servicio.

## [0.8.4] — 2026-11-19

### Corregido

- Instaladores: la unidad systemd incluye --downloads-dir — los agentes
  para los one-liners del panel se sirven desde /usr/local/lib/bdr/agents
  sin pasos manuales.
- Bundle de release: incluye bdr-control.service (unidad completa) en lugar
  de la mínima de reserva.

## [0.8.3] — 2026-11-19

### Corregido

- Instaladores: patrón $( [[ ] && echo ) en el bucle de agentes —
  mataba el script en silencio con set -e en plataformas no-Windows.

## [0.8.2] — 2026-11-19

### Corregido

- CI de release: fijar GOOS y GOARCH en la compilación multi-plataforma — todos
  los binarios salían para linux/amd64 aunque el nombre dijera darwin/arm64/windows.

## [0.8.1] — 2026-11-19

### Corregido

- Instaladores: formato del URL de descarga de la release (`/releases/download/<tag>/`).
- Instaladores: los binarios de agente se descargan y verifican con SHA256SUMS.
- Instalador: extracción de la versión del canal con latest.json indentado.
- Build Docker: el panel se cruza entre fases con COPY --from; fuentes autoalojadas (builds herméticos).
- CI: permiso attestations: write.
- Dependencias: klauspost/compress v1.20.2 (GO-2026-6865).

## [0.8.0] — 2026-11-19 — primera versión pública

Almacenamiento flexible (S3 o ruta física) y eliminación de backups.

### Añadido

- **Selección de discos en planes de imagen**: los planes `kind: image`
  aceptan `disks` (índices de disco separados por comas en el panel, array
  en la API) para limitar la captura a discos concretos — p. ej. excluir un
  disco de datos y capturar solo el del sistema. Vacío = todos los discos
  fijos (comportamiento anterior). El filtro viaja en el payload del job
  (`BACKUP_IMAGE.disks`) y lo aplica el agente al enumerar discos; índices
  que no coinciden con ningún disco abortan el job con `E_VALIDATION`.
  PATCH con `disks: []` limpia la selección. La validación de planes
  (`/validate`) es ahora consciente del tipo: no exige rutas en imagen y
  avisa de qué discos quedan fuera.
- **Destinos SMB con autenticación AD**: las rutas físicas aceptan
  compartidas SMB (`//servidor/recurso[/subdir]` o `smb://…`) con cliente
  SMB nativo (go-smb2, sin montar ni root) y credenciales NTLM cifradas en
  el store (usuario, contraseña y dominio AD/workgroup). Válidas para
  backups de imagen, VM y LXC (subida directa por segmentos), verificación,
  borrado de backups y lectura de manifiestos desde el panel. La réplica
  del repositorio a SMB usa `kopia sync-to rclone` (requiere rclone
  instalado). Los planes de archivo no admiten destinos SMB (kopia no
  escribe en compartidas sin montar); S3 o ruta local sí.
- **Backup y restauración de contenedores LXC de Proxmox**: job
  `BACKUP_LXC` — `pct config` → `vzdump --stdout` (snapshot, con fallback a
  stop si el storage no soporta snapshots) → compresión zstd en el agente →
  subida por segmentos al destino → manifiesto con la config cruda del CT.
  Job `RESTORE_LXC`: descarga verificando SHA-256 por segmento y `pct
  restore` — en sitio (stop → destroy → restore → start, mismo VMID) o CT
  nuevo (VMID libre, hostname opcional, storage del manifiesto o elegido).
  Puntos de restauración `Kind: lxc`; panel Proxmox con botón Backup y
  restauración para LXC. Requiere agente actualizado en el nodo PVE.
- **Destinos de almacenamiento con ruta física**: además de buckets
  S3-compatible, un destino puede ser una ruta absoluta local o NAS montado
  (`Kind: filesystem`). Verificación (crea y borra un archivo de prueba),
  réplica del plano de control (`kopia repository sync-to filesystem`) y
  aprovisionamiento del repo Kopia del destino (`repository connect/create
  filesystem`, contraseña cifrada en el store). El prefijo actúa como
  subdirectorio de la ruta. Los planes de imagen aceptan ambos tipos.
- **Backups de imagen/VM sobre ruta física**: el destino de subida del agente
  (`newUploadDest`) acepta repositorios `filesystem`; los segmentos y
  manifiestos se escriben como ficheros (escritura atómica temporal+rename,
  reanudación incremental por listing de claves). Cada repo destino del
  agente usa su propia config de Kopia (`kopia-<id>.config`).
- **Eliminación de backups**: `DELETE /api/v1/admin/restore-points/{id}`
  (rol admin, auditado). Puntos de archivo (`file`): `kopia snapshot delete
  --delete` en el repo central o en el repo del destino. Puntos de
  imagen/VM: borra el manifiesto y sus segmentos del destino (S3 con bypass
  de Object Lock GOVERNANCE, ficheros en la ruta física). El registro sale
  del catálogo; los restore points de archivo registran ahora su destino
  (`TargetID`) para borrar en el repositorio correcto.
- Panel: página de Almacenamiento con selector de tipo (S3 / ruta física) y
  campos condicionales; botón **Eliminar** con confirmación en Puntos de
  recuperación; textos de planes y Proxmox actualizados a "S3 o ruta física".

---

## [0.7.0-dev] — 2026-11-19

Proxmox: backup y restauración de VMs desde el panel (Fase 2 — PVE).

### Añadido

- **Backup de VMs de Proxmox**: job `BACKUP_VM` ejecutado por el agente
  instalado en el nodo PVE. `qm config` → discos vía `pvesm path` →
  `qemu-nbd` read-only → subida en streaming por segmentos al bucket del
  destino (misma tubería que imagen: SHA-256, incremental, salto de ceros,
  Object Lock opcional). El manifiesto v1 lleva la configuración cruda de
  la VM (`Kind: vm`).
- **Restauración de VMs** (`RESTORE_VM`): en sitio (detiene la VM, reescribe
  los volúmenes vía NBD verificando SHA-256, arranca) o **instancia nueva**
  (`qm create` con la configuración del manifiesto —cores, RAM, BIOS,
  machine, red, discos con sus tamaños, efidisk y TPM— y escritura de los
  datos). VMID nuevo automático o elegido.
- Instancias Proxmox: **vinculación de agente** por instancia (PUT), que
  es quién ejecuta los jobs de esa instancia; endpoints
  `POST /instances/{id}/vms/{vmid}/backup`,
  `GET /instances/{id}/vms/{vmid}/restore-points` y
  `POST /admin/proxmox/restore`. Los backups de VM crean puntos de
  restauración (`Kind: vm`) con la clave del manifiesto y el destino.
- Panel Proxmox: selector de destino por instancia, **Backup** por VM,
  listado de puntos de restauración por VM y diálogo de restauración
  (en sitio / nueva VM con nombre). Edición de instancias con binding de
  agente. Guía en `docs/proxmox.md`.

---

## [0.6.0-dev] — 2026-11-18

Agente de Windows como servicio real, autoactualización de agentes y **planes de imagen completa (bare-metal)**.

### Añadido

- **Elección de almacenamiento por plan (repositorio Kopia S3 por destino)**:
  - Cada destino de la sección Almacenamiento puede albergar su **propio
    repositorio Kopia** (contraseña generada y cifrada; el repo se crea
    automáticamente en el bucket al primer uso, `repository connect/create
    s3` con config aislada por destino).
  - El plan elige su destino (`storage_target_id`, selector en el panel);
    por defecto sigue el repositorio central con sus réplicas.
  - El agente pide las credenciales del repo del destino
    (`/agent/repository-credentials?target=`) **solo si tiene un plan
    asignado que use ese destino**; su configuración Kopia por repo vive en
    `<data-dir>/kopia/kopia-<target>.config` sin pisar la primaria.
  - Imprescindible para equipos remotos: un agente Windows ya puede escribir
    imagen/archivos directamente en el bucket S3 del destino.
- **Planes de imagen completa del equipo (Fase 2)**: kind=image — captura
  íntegra de todos los discos fijos (tabla de particiones, ESP, sistema y
  datos) para desplegar el equipo en otro hardware que arranque igual:
  - El job `BACKUP_IMAGE` usa el motor de segmentos con journal y
    reanudación (G0.2): staging local + snapshot Kopia con manifiesto v1
    (particiones, roles, firmware, SHA-256 por segmento).
  - Enumeración de discos por plataforma: `lsblk` (Linux) y
    `Get-Disk/Get-Partition` (Windows). Coherencia crash-consistent.
  - **Despliegue**: `bdr-agent image-write --segments-dir DIR --target
    /dev/sdX --yes` reconstruye el disco byte a byte verificando el SHA-256
    de cada segmento contra el manifiesto. Runbook completo en
    `docs/recuperacion-bare-metal.md`.
  - Panel: selector de tipo de plan (Archivos / Imagen completa) con campos
    condicionales; los planes de imagen se listan sin rutas («equipo
    completo»).
  - El programador y el disparo manual generan `BACKUP_IMAGE` según el kind
    del plan; agentes sin soporte (≤ 0.5) reciben un error explícito de
    tipo de job, no un fallo genérico.
- **Autoactualización de agentes desde el servidor**:
  - El heartbeat del agente reporta su versión; si difiere de la del plano de
    control y hay binario publicado en el directorio de descargas, la
    respuesta incluye `upgrade` con URL y **SHA-256** del artefacto.
  - El agente solo se actualiza **sin un job en curso**: descarga, verifica
    el SHA-256 en streaming y reemplaza su ejecutable por renombrado atómico
    (el anterior queda como `<exe>.old` para rollback manual). Un fallo deja
    el binario intacto y se reintenta en el siguiente heartbeat.
  - Reinicio: en Unix re-ejecuta el binario nuevo in-place (`syscall.Exec`,
    mismo PID, systemd ni se entera); en Windows sale sin reportar `Stopped`
    y la recuperación del SCM lo levanta con el binario nuevo.
  - Agentes antiguos que no conocen el campo `upgrade` lo ignoran (JSON
    tolerante): la oferta no rompe nada.

### Corregido

- **Crear un plan inválido desde el panel devolvía 200 con cuerpo vacío**
  (el panel mostraba «Cannot read properties of undefined (reading 'name')»).
  `withIdempotency` no propagaba los errores de validación (`errProblem`):
  ahora responde 400 con problema RFC 7807 y no cachea el fallo.
- **El agente de Windows ya funciona como servicio**: el binario no hablaba
  el protocolo del Service Control Manager, así que Windows lo mataba al
  ~30 s (error 1053) — síntoma: «conectado» al instalar y `Stopped` minutos
  después. Ahora `bdr-agent run` detecta si lo arrancó el SCM (`svc.Run` con
  handler StartPending/Running/StopPending) y en consola sigue igual.
  En modo servicio los logs van a `<data-dir>\agent.log`.
- Instalador Windows: ruta del binario entre comillas en `BinaryPathName`
  (Program Files tiene espacios) y **acciones de recuperación** del servicio
  (`sc failure`, reinicio 5 s/10 s/5 min) para que un crash se recupere solo.
- `verify` de destinos de almacenamiento: la clave de prueba empezaba por
  `/` sin prefix (URL con doble barra → 307) y minio-go usaba addressing
  virtual-host; ahora path-style y clave limpia.

---

## [0.5.0-dev] — 2026-11-17

Estado del plano de control en PostgreSQL.

### Añadido

- **`store.Postgres`: estado duradero en PostgreSQL** (`--store postgres` con `--db-url` o `BDR_DB_URL`):
  - El documento `State` completo se persiste como JSONB en `control_state` (fila única transaccional); la auditoría encadenada, en la tabla append-only `audit_events` (docs/adr/0007).
  - Migraciones versionadas embebidas (`store/migrations/`) aplicadas al arrancar, con registro en `schema_migrations`.
  - `Update` persiste en la misma llamada; `UpdateLazy` difiere al flusher/`Close` (misma semántica que el fichero); `Flush` y `Close` vuelven lo pendiente.
  - `--import-state <path>` importa un `state.json` (formato 0.2 en adelante) sobre una base vacía; se niega si ya hay estado.
  - Copia de seguridad y restauración de todo el estado con `pg_dump`/`pg_restore`.
- **Batería de contrato del store** (`contract_test.go`): los comportamientos exigibles a todo `Store` (persistencia, error sin persistir, lazy/flush, auditoría que sobrevive al reinicio, continuidad de secuencias) se ejecutan contra `memory`, `file` y `postgres` sin cambios. Los de PostgreSQL se activan con `BDR_TEST_POSTGRES`.
- **Smoke E2E con PostgreSQL**: `make smoke-postgres` (o `BDR_SMOKE_STORE=postgres`) ejecuta el mismo E2E completo con el estado en una base efímera de Docker, incluidos reinicio con persistencia y verificación de la cadena de auditoría.
- **Compose**: servicio `db` (PostgreSQL 16 con healthcheck) bajo el perfil `postgres` y ejemplo de `command` para `control`; guía de migración y copia de seguridad en `deploy/README.md`.

- **Kopia Repository Server: aislamiento por agente** (`BDR_KOPIA_KIND=server` con `BDR_KOPIA_SERVER_URL`/`BDR_KOPIA_SERVER_FP`, docs/adr/0002):
  - Al entregar credenciales (`GET /agent/repository-credentials`) el plano de control **crea o actualiza el usuario del agente en el repositorio** (`kopia server users add/set`); la entrega es idempotente y converge si hubo deriva (`E_REPO_PROVISION` si el repositorio no acepta el usuario).
  - **Rotación sin re-enrolar**: `POST /api/v1/admin/agents/{id}/repo-credentials/rotate` genera contraseña nueva, incrementa la versión y la aplica en el repositorio; `GET .../repo-credentials` expone el estado sin contraseña (`PendingApply` si quedó pendiente de aplicar).
  - **Revocación**: al revocar un agente se borra su usuario del repositorio (best-effort).
  - Normalización a minúsculas del identificador `user@hostname` (exigida por el CLI de Kopia) con auto-curación de credenciales 0.4.
  - **Smoke `make smoke-krs`**: E2E completo contra un Kopia Repository Server real con TLS y pin por huella, más verificación de aislamiento (usuario↔usuario, credenciales robadas limitadas al propio agente) y rotación (la antigua deja de aceptarse, la nueva funciona).
  - Compose: servicio `kopia-server` (perfil `server`) y guía de certificado/huella en `deploy/README.md`.
- **Smoke más robusto**: el agente 2 del flujo de alertas se identifica por su ID de enrolamiento (antes se elegía por orden de lista, no estable); los asertos de conteo de agentes admiten el tercer agente del aislamiento.
- **«Añadir equipo»: instaladores preconfigurados** (POST /admin/installers):
  - El panel genera un instalador (bash para Linux, PowerShell para Windows) con
    la URL del servidor y un token de enrolamiento de un solo uso **embebidos**:
    al ejecutarlo en el equipo destino, el binario se descarga del servidor, el
    agente se auto-enrola y queda conectado como servicio (systemd/Windows) sin
    intervención. One-liner copiable y descarga directa desde el panel.
  - `GET /api/v1/download/agent/{osarch}` sirve los binarios (linux/windows,
    amd64/arm64) desde el directorio de descargas del servidor
    (`--downloads-dir`, por defecto `<data>/downloads`); colócalos con
    `scripts/build-agents.sh`.
  - Panel: modal «Añadir equipo» rediseñado con modo instalador (por defecto)
    y modo token manual.
- **Sección Proxmox en el panel** (nueva página): registro de instancias con
  token de API (secreto cifrado AES-256-GCM), verificación de conexión e
  inventario de VMs/LXC en vivo vía la API de PVE (`cluster/resources`),
  con estado, RAM, disco y cores por máquina. Endpoint
  `GET /api/v1/admin/proxmox/instances/{id}/vms`. La ejecución de
  backup/restore de VMs requiere el agente en el nodo PVE (siguiente slice).
- **Destinos de almacenamiento S3 desde el panel** (página «Almacenamiento»):
  alta de targets S3-compatible con credenciales cifradas (AES-256-GCM),
  Object Lock GOVERNANCE con retención en días, intervalo de réplica
  programada y verificación manual. El plano de control replica su repositorio
  Kopia con `kopia repository sync-to s3` (sin `--delete`: inmutabilidad) y
  verifica cada destino con minio-go (bucket, credenciales y escritura de
  prueba con lock). Endpoints CRUD + `verify` + `sync`; loop de réplica cada
  minuto para los destinos con intervalo > 0. El listado nunca expone el
  secreto; el PUT sin secret conserva el cifrado existente.
- **Explorador: búsqueda y descarga de directorios**:
  - `GET /admin/restore-points/{id}/files/search?q=&path=&limit=`: búsqueda recursiva por nombre (subcadena, sin mayúsculas/minúsculas) con límites de profundidad, directorios visitados y resultados (`Truncated` si se corta). Auditada.
  - `GET .../files/download?path=`: descarga del directorio (o raíz) como **ZIP en streaming**, con límites de ficheros y bytes. Auditada.
  - `FileEntry.path` se rellena ahora en el listado: la navegación de subdirectorios del panel funcionaba solo en la raíz.
  - Panel: caja de búsqueda en el explorador (con «volver al directorio»), botón **ZIP** para el directorio actual y descarga directa desde los resultados.
- **Notificaciones con reintentos**: webhook y email reintentan hasta 3 veces con backoff exponencial (2 s, 8 s; configurable para tests). Un fallo puntual de red ya no pierde la alerta.
- **Puerta «backup de la BD probado»**: en modo postgres, el smoke E2E ahora hace `pg_dump` → `pg_restore` a una base nueva y **reinicia el plano de control contra la base restaurada**, validando persistencia y cadena de auditoría sobre la copia.
- **Panel con TLS en Compose**: servicio `caddy` (perfil `tls`) con certificado interno y HSTS para el despliegue de referencia.
- **mTLS obligatorio y revocación inmediata de agentes** (puerta del roadmap: revocación efectiva < 5 min):
  - El listener de agentes (`--agent-addr`) pasa a **exigir certificado de cliente válido y vigente** en cada handshake: `RequireAndVerifyClientCert` + verificación de política (agente existe, no revocado y certificado el actual) sin reutilización de sesión TLS, de modo que revocar o renovar surte efecto en la conexión siguiente, sin reiniciar.
  - **Ligadura identidad ↔ API key** (docs/06 §2.2): el SAN `urn:bdr:agent:<id>` del certificado presentado debe coincidir con el agente de la API key; en caso contrario `401 E_AUTH_CERT`.
  - Transición: `--insecure-agents` acepta agentes sin certificado (con aviso); `--agent-mtls=false` está deprecado.
  - **`bdr-agent renew-cert`**: renovación bajo demanda de la identidad mTLS (la automática por heartbeat ya existía); útil para operación y para verificar el ciclo.
  - **Smoke E2E sobre el listener TLS**: el flujo completo (heartbeat, jobs, restore) transcurre por mTLS estricto; comprueba que el enrolamiento entrega certificado y `agent_url`, la renovación E2E y la revocación inmediata (el agente revocado pierde el acceso en la conexión siguiente).
  - Tests unitarios de la política end-to-end: listener TLS de pruebas con certificados de cliente (handshake válido, sin cert, ligadura, renovación que invalida el certificado antiguo, revocación instantánea).

### Cambiado

- El instalador Windows arranca el servicio con error visible: antes
  `Start-Service -ErrorAction SilentlyContinue` tragaba el fallo y el agente
  quedaba enrolado pero parado (síntoma: «conectado» al instalar y
  «desconectado» minutos después).
- `pgx/v5` como única dependencia externa nueva (Go ≥ 1.25; `Dockerfile.server` actualizado a `golang:1.25-alpine`).
- `AuditLog` generaliza su persistencia detrás de un sink (archivo, tabla PostgreSQL o memoria); si el sink falla, el evento no se registra ni en memoria, de modo que nunca divergen.
- El smoke sondea la alerta `job_failed` unos segundos en lugar de un `sleep` fijo (elimina un flake de timing).

### Limitaciones

- Un solo escritor por proceso: el documento JSONB da durabilidad y atomicidad, no HA multi-nodo (evolución en docs/adr/0007, cola River en docs/adr/0006).
- El esquema relacional de `docs/05-modelo-datos.md` sigue siendo el objetivo cuando haya consultas SQL reales.

---

## [0.4.0-dev] — 2026-11-17

Fase 1 del MVP: identidad, alertas, programación, restore points y restauración de archivos con Kopia real.

### Añadido

- **Identidad y seguridad del panel:**
  - Bootstrap del primer usuario administrador (`/auth/bootstrap`, o `BDR_ADMIN_EMAIL`/`BDR_ADMIN_PASSWORD`).
  - Sesiones con cookie `HttpOnly` + CSRF (`X-CSRF-Token`), caducidad configurable y revocación al cambiar contraseña o logout.
  - TOTP RFC 6238: alta con QR (`otpauth://`), verificación con tolerancia ±30 s, anti-replay, códigos de recuperación de un solo uso, step-up para acciones sensibles.
  - Contraseñas PBKDF2-SHA256 (600 000 iteraciones), política mínima de 12 caracteres.
  - Tokens de integración `bdt_*` con rol y caducidad; revocación inmediata.
  - RBAC con tres roles: `admin`, `operator`, `viewer`; capacidades verificadas en cada endpoint.
  - Secretos en reposo con AES-256-GCM (`BDR_SECRET_KEY` o `secret.key` 0600 en el directorio de datos).
- **Auditoría encadenada:** registro append-only (`audit.jsonl`) donde cada evento lleva el hash del anterior; `GET /admin/audit`, `/verify`, `/export` (NDJSON). Un hueco o una alteración rompe la cadena y se detecta.
- **Planes completos:** cron de 5 campos (parser propio con alias y tests), zona horaria, ventanas `HH:MM–HH:MM` con cruce de medianoche, retención GFS aplicada como política de Kopia, `rpo_hours`, validación, PATCH y DELETE.
- **Grupos con jerarquía:** miembros, grupos anidados con detección de ciclos (`E_GROUP_CYCLE`); la asignación a un grupo padre cubre a los miembros de los subgrupos.
- **Asignaciones a agente o grupo** (`target_type`/`target_id`, compatible con el `{agent_id}` antiguo); los planes efectivos muestran la vía (agent/group) y qué asignación de grupo queda sombreada por una directa.
- **Programador:** crea jobs vencidos respetando ventanas y ocupación del agente, recalcula `next_run_at`, auto-reintenta jobs con lease perdido mientras queden intentos y alerta cuando un equipo deja de reportar contacto.
- **Jobs ampliados:** idempotencia por `Idempotency-Key` (24 h, 409 si cambia el cuerpo), cancelación cooperativa visible en heartbeat/progreso/`GET cancel`, reintentos, eventos por job con SSE (`text/event-stream`) y JSON paginado, progreso con bytes y mensajes.
- **Puntos de recuperación:** registrados automáticamente al completar un backup (uno por snapshot de Kopia), con tamaño, ficheros, errores y estado de verificación; listado con filtros y cursor.
- **Verificación de restore points:** `kopia snapshot verify` estructural (nivel 1) o con lectura de contenido por porcentaje (nivel 2), en segundo plano; resultado y detalle en el propio punto.
- **Explorador de snapshots:** navegación de directorios vía `kopia show` (JSON interno de Kopia) con validación anti-traversal, y descarga de archivos por streaming (`kopia show` a la respuesta), auditadas ambas.
- **Restauración de archivos:** job `RESTORE_FILE` hacia el equipo de origen u otro agente (mismo SO), con destino absoluto o `in_place`, validación de rutas y resultado por elemento.
- **Alertas:** deduplicadas por clave, severidad (critical/high/medium/low), ack y resolución, re-notificación periódica configurable; webhook firmado con HMAC-SHA256 (`X-BDR-Signature`) y email por SMTP con STARTTLS; prueba de notificaciones.
- **Agente (Kopia real verificado):**
  - Conexión al repositorio desde `GET /agent/repository-credentials` (filesystem: contraseña del repositorio; server: usuario por agente); políticas de exclusión y retención; snapshot con resumen real (tamaño, ficheros, errores); restore.
  - Ciclo con heartbeat concurrente al job, cancelación cooperativa, logs por lotes (`POST /jobs/{id}/log`), heartbeat `RUNNING_BACKUP`/`RESTORING`.
  - mTLS: CSR ECDSA P-256 en el enrolamiento, CA interna propia, pin del servidor (`--server-fingerprint`), renovación de certificado; compatible con `agent.json` 0.2/0.3.
  - Inventario amplio: discos/particiones/LVM/LUKS (lsblk o /sys), red, firmware UEFI/Secure Boot, TPM, BitLocker por PowerShell; histórico por hash en el servidor.
- **Instalación de agentes:** `bdr-agent register`, instaladores Linux (systemd endurecido + `install.sh` + `.deb` generado sin dpkg) y Windows (`install.ps1` con ACL de datos, tarea programada como SYSTEM), `scripts/build-agents.sh` con sumas SHA-256.
- **Despliegue:** Compose con servicio `kopia-init` para crear el repositorio, documentación de secretos y mTLS en `deploy/README.md`.
- **Smoke E2E con Kopia real:** backup con exclusiones → restore point → verify estructural → navegación → descarga → restore de archivo con comparación de contenido → alerta de fallo provocado → SSE → reinicio con persistencia → cadena de auditoría → resumen.

### Cambiado

- `store` rediseñado: documento `State` tipado con transacciones `View`/`Update`; migración automática del `state.json` 0.2/0.3 (con copia `.v1.bak`).
- El plano de control puede conectarse directamente al repositorio Kopia (kopia.config en su directorio de datos) para navegar y verificar; si no, esas funciones se marcan como no disponibles en lugar de fallar.
- `os_family` acepta `darwin` para desarrollo.

### Limitaciones

- Sin PostgreSQL ni River; sin mTLS obligatorio (API key + TLS opcional por listener).
- El webhook/email no reintentan con backoff; los fallos quedan en el log.
- En modo repositorio filesystem todos los agentes comparten la contraseña; el aislamiento por agente exige el modo `server` (ya disponible, ver arriba). Kopia no liga el hostname en el handshake: las credenciales robadas se revocan rotando.
- La verificación de contenido (nivel 2) es muestreada por porcentaje.
- El panel se actualiza a las pantallas nuevas en `apps/web` (ver 0.4.0 del panel).

---

## [0.3.0-dev] — 2026-11-16

Panel web operativo.

### Añadido

- **Panel Next.js 15** en `apps/web` con App Router, TypeScript y Tailwind:
  - Login con token de administración validado contra la API y guardado solo en `sessionStorage`.
  - Resumen con cobertura, planes, trabajos recientes y accesos rápidos.
  - Equipos: estado, SO, hardware, último contacto, último job, búsqueda y **Backup ahora**.
  - Modal de instalación con token de un solo uso, caducidad, comandos Windows/Linux y avisos de seguridad.
  - Planes: creación con rutas/exclusiones y asignación a equipos.
  - Trabajos: filtros por estado, progreso, errores y refresco automático.
- **Proxy runtime** en route handlers de Next:
  - `/api/v1/[...path]` y `/healthz` reenvían a `BDR_API_URL` sin recompilar el panel.
  - Cabeceras de autenticación y errores RFC 7807 preservados.
- **Diseño visual propio**: consola industrial, tipografía Barlow Condensed + IBM Plex, rejilla técnica, acento lima y estados accesibles.
- **Docker**: `deploy/Dockerfile.web` y servicio `web` en Compose.
- **Smoke web**: `scripts/smoke-web.sh` arranca API y panel, valida el proxy y el HTML.
- **Makefile**: `web-install`, `web-build`, `web-dev`, `smoke-web`.

### Limitaciones

- Sin TOTP ni sesiones de servidor; el token vive en la pestaña.
- Sin HTTPS.
- Sin explorador de snapshots ni wizard de restauración.
- El panel no se ha probado en un navegador real automatizado; sí su build, proxy y HTML.

---

## [0.2.0-dev] — 2026-11-16

Planes, jobs y persistencia.

### Añadido

- **Almacenamiento persistente en fichero** (`store.File`):
  - Agentes, tokens, planes, asignaciones y jobs en `state.json`.
  - Escritura con temporal + `fsync` y permisos 0600.
  - Fallback no atómico en Windows cuando `Rename` sobre un destino existente falla.
  - Recarga al arrancar y reconstrucción de índices (hash de API key, asignaciones).
- **Planes de protección**:
  - `POST/GET /api/v1/admin/backup-plans`.
  - Rutas y exclusiones; `kind=file` por ahora.
- **Asignaciones**:
  - `POST /api/v1/admin/backup-plans/{id}/assign`, idempotente por plan y agente.
- **Jobs**:
  - `POST /api/v1/admin/agents/{id}/backup` crea un job `BACKUP_FILE`.
  - Lease con expiración y recuperación de jobs cuyo lease caduca.
  - Progreso y cierre (`succeeded`/`failed`) desde el agente.
  - Listado y detalle administrativo.
- **Ejecución en el agente**:
  - `kopia snapshot create <ruta> --json` por cada ruta del plan.
  - Códigos `E_KOPIA_MISSING`, `E_KOPIA_FAILED`, `E_KOPIA_OUTPUT`, `E_CANCELED`, `E_PLAN_EMPTY`.
  - Runner sustituible; el smoke usa un Kopia falso.
- **Smoke test ampliado**: enrolamiento, plan, asignación, job, ejecución, reinicio persistente y token reutilizado.
- **Tests**: store en memoria y fichero, lease/caducidad, ejecutor, wrapper ausente y flujo API de planes/jobs.

### Cambiado

- El servidor usa `--store file` por defecto (`state.json` en `--data-dir`); `--store memory` sigue disponible.
- `README.md` y `DEVELOPMENT.md` reflejan el estado real.

### Limitaciones

- Sin PostgreSQL ni cola River.
- El wrapper de Kopia no se ha probado contra un repositorio real.
- Un job por agente y heartbeat bloqueado durante la ejecución.
- Sin panel.

---

## [0.1.0-dev] — 2026-11-16

Primer vertical slice funcional.

### Añadido

- **Servidor `bdr-control`** en Go, sin dependencias externas.
  - `/healthz` y `/readyz`.
  - Logs JSON con redacción de secretos.
  - Errores `application/problem+json` (RFC 7807) con `code` y `request_id`.
  - Límites de cuerpo, timeouts y apagado ordenado.
- **Enrolamiento** con token de un solo uso:
  - El administrador crea el token; solo se guarda su SHA-256.
  - El agente recibe una API key una sola vez; solo se guarda su hash.
  - Caducidad, binding opcional a hostname y rechazo de reutilización (410).
- **Heartbeat e inventario**:
  - API key por agente.
  - Estado `pending`/`online`/`offline`/`revoked` calculado por último contacto.
  - Inventario mínimo: hostname, SO, arquitectura, CPU, RAM y disco del sistema.
- **Agente `bdr-agent`**:
  - Comandos `enroll`, `run --once`, `run`, `status`.
  - Configuración local `agent.json` con permisos 0600 y escritura atómica.
  - Inventario específico de Windows (kernel32) y Linux (`/proc`, `os-release`, `statfs`).
  - Compilación cruzada `GOOS=linux` verificada.
- **Tests**: control, store, cliente, configuración e inventario.
- **Smoke test E2E**: servidor + agente reales, token de un solo uso y listado online.
- **Despliegue**: Makefile, Dockerfile y Docker Compose de desarrollo.
- **Documentación**: `DEVELOPMENT.md` y estado de implementación en el README.

### Limitaciones conocidas

- Almacenamiento en memoria; se pierde al reiniciar.
- HTTP sin TLS y API key en lugar de mTLS.
- Sin panel Next.js.
- Sin Kopia, jobs, backup, imagen, P2V ni ISO.
- El inventario no incluye todavía discos/particiones completos, VSS ni BitLocker.

### Seguridad

- No usar fuera de localhost o una red de laboratorio.
- El token de administración se toma de `BDR_ADMIN_TOKEN`; `--dev` usa un valor fijo solo para desarrollo.

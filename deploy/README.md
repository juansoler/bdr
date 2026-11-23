# Despliegue

**Documentos:** runbook de producción en [`PRODUCTION.md`](PRODUCTION.md) · stack de referencia en [`docker-compose.prod.yml`](docker-compose.prod.yml) · laboratorio/develop en [`docker-compose.yml`](docker-compose.yml).

**Estado:** nodo único con PostgreSQL (o fichero para laboratorio), repositorio Kopia de archivos con opción de Kopia Repository Server (aislamiento por agente), agentes mTLS, panel con TLS y backup diario de la BD.

## Docker Compose

```bash
export BDR_ADMIN_TOKEN="$(openssl rand -base64 32)"
export BDR_KOPIA_PASSWORD="$(openssl rand -base64 24)"
# Opcional, recomendado: clave de cifrado de secretos fija
export BDR_SECRET_KEY="$(openssl rand -base64 32)"
# Opcional: primer usuario del panel (además del token de administración)
export BDR_ADMIN_EMAIL="admin@empresa.test"
export BDR_ADMIN_PASSWORD="una-contraseña-larga"

docker compose -f deploy/docker-compose.yml run --rm kopia-init   # crea el repositorio Kopia (una vez)
docker compose -f deploy/docker-compose.yml up --build -d
```

### Estado en PostgreSQL (opcional)

El despliegue por defecto usa el fichero `state.json`. Para usar PostgreSQL, levanta el perfil `postgres` y cambia el `command` del servicio `control` (el ejemplo está comentado en `docker-compose.yml`):

```bash
docker compose -f deploy/docker-compose.yml --profile postgres up --build -d db
# y en control:
#   command: ["--store","postgres","--db-url","postgres://bdr:bdr@db:5432/bdr?sslmode=disable","--addr","0.0.0.0:8080","--data-dir","/data"]
```

- Las migraciones se aplican al arrancar (`control_state` + `audit_events`).
- La auditoría encadenada se guarda en la tabla `audit_events`; la verificación (`/admin/audit/verify`) lee de ella.
- Para importar un `state.json` existente: añade `--import-state /data/state.json` una sola vez (la base debe estar vacía).
- Copia de seguridad de la base: `pg_dump -Fc bdr` y restauración con `pg_restore`; el documento de estado y la auditoría viajan dentro.

Con fichero o con base, los secretos cifrados (`BDR_SECRET_KEY` / `secret.key`) siguen siendo necesarios para descifrar credenciales.

### Repositorio Kopia Repository Server (opcional, aislamiento por agente)

En modo `filesystem` todos los agentes comparten la contraseña del repositorio. Para aislarlos, levanta el perfil `server`: un **Kopia Repository Server** sobre el mismo repositorio, donde cada agente recibe usuario y contraseña propios y solo ve sus snapshots. `bdr-control` gestiona los usuarios automáticamente (creación al entregar credenciales, actualización al rotar, borrado al revocar).

```bash
# 1) Repositorio y config del plano de control (una vez)
docker compose -f deploy/docker-compose.yml run --rm kopia-init

# 2) Certificado TLS del KRS y su huella (una vez)
docker compose -f deploy/docker-compose.yml run --rm kopia-init sh -c '
  openssl req -x509 -newkey rsa:2048 -nodes -keyout /data/krs.key -out /data/krs.crt \
    -days 825 -subj "/CN=kopia" -addext "subjectAltName=DNS:kopia-server,IP:127.0.0.1" &&
  echo "FP=sha256:$(openssl x509 -in /data/krs.crt -outform DER | sha256sum | cut -d" " -f1)"'

# 3) Arrancar con el perfil server y anunciar el KRS a los agentes
export BDR_KOPIA_KIND=server
export BDR_KOPIA_SERVER_URL=https://IP-DEL-SERVIDOR:51515
export BDR_KOPIA_SERVER_FP=sha256:...   # la huella del paso 2
docker compose -f deploy/docker-compose.yml --profile server up --build -d
```

Notas de seguridad verificadas con Kopia 0.23:

- El ACL por usuario funciona: un agente no ve los snapshots de otro (probado en `smoke-krs`).
- Kopia **no** liga el hostname en el handshake: quien roba usuario y contraseña de un agente accede solo a los datos de ese agente, desde cualquier hostname declarado. La revocación es la rotación (`POST /admin/agents/{id}/repo-credentials/rotate`) o la revocación del agente (borra su usuario).
- El servidor refresca su catálogo de usuarios cada `--refresh-interval` (15 s en Compose): un job justo tras la rotación puede fallar una vez y recuperarse en el siguiente intento.

- **API:** `http://127.0.0.1:8080`
- **Panel:** `http://127.0.0.1:3000` (introduce `BDR_ADMIN_TOKEN` al entrar)
- **Repositorio Kopia:** `/data/repo` dentro del volumen; el plano de control lo usa para navegar y verificar (estado «Server-side: sí» en la API).

Prueba:

```bash
curl http://127.0.0.1:8080/healthz
curl -H "Authorization: Bearer $BDR_ADMIN_TOKEN" http://127.0.0.1:8080/api/v1/admin/repositories
```

### Panel por HTTPS (opcional)

Con el perfil `tls`, Caddy sirve el panel en `https://localhost` con certificado interno y HSTS:

```bash
docker compose -f deploy/docker-compose.yml --profile tls up -d caddy
```

Para un dominio real, cambia `--from localhost:443` por tu dominio y quita `--internal-certs` (Caddy emitirá vía ACME/Let's Encrypt).

El panel es un export estático servido por el propio binario (panel embebido,
mismo origen: sin Node, sin proxy y sin CORS). TLS delante con Caddy incluido
en Compose.

## Agentes

Los agentes se instalan fuera de Docker, en cada equipo protegido:

```bash
# Linux (Debian)
sudo apt install ./dist/bdr-agent_*_amd64.deb
sudo bdr-agent register --server http://IP-DEL-SERVIDOR:8080 --token <TOKEN-DE-UN-USO>
sudo systemctl enable --now bdr-agent

# Windows (PowerShell como administrador)
.\install.ps1 -Server http://IP-DEL-SERVIDOR:8080 -Token <TOKEN-DE-UN-USO>
```

Compila los binarios y el `.deb` con `scripts/build-agents.sh` y `scripts/package-deb.sh`.

Para mTLS (recomendado en producción), levanta además el listener TLS del servidor con `--agent-addr 0.0.0.0:8443 --public-url http://IP:8080 --agent-url https://IP:8443 --agent-tls-names IP` (añádelo al `entrypoint` del servicio o monta tu propio Compose).

## Desinstalación

Procedimiento completo por SO (scripts + comandos manuales) en
[`uninstall/README.md`](uninstall/README.md). Los desinstaladores conservan
por defecto el estado y el repositorio Kopia; `-Purge` / `--purge` borra
todo con confirmación.

Para Compose:

```bash
docker compose -f deploy/docker-compose.prod.yml down     # contenedores
docker compose -f deploy/docker-compose.prod.yml down -v  # + volúmenes (borra las copias)
```

## Secretos y clave maestra

- `BDR_ADMIN_TOKEN`: credencial de administración de arranque (el «token del panel»).
- `BDR_SECRET_KEY`: clave AES-256-GCM que cifra secretos en `state.json` (contraseñas de usuarios se hashean aparte con PBKDF2; esta clave cifra TOTP y credenciales de repositorio). Si no se define, se genera `/data/secret.key`; **cópiala a un lugar seguro**: sin ella los secretos cifrados no se recuperan (docs/14).
- `BDR_KOPIA_PASSWORD`: contraseña del repositorio Kopia de archivos. Cámbiala por la del servidor de repositorio cuando migres a Kopia Repository Server (Fase 3).

## Limitaciones actuales

- **Persistencia en un solo fichero:** válida para un nodo; no soporta varias réplicas ni PostgreSQL.
- **Sin TLS en la API de administración:** el panel guarda el token en la sesión del navegador. No exponer fuera de localhost o una red de laboratorio sin un proxy TLS delante.
- **Repositorio de archivos:** en modo filesystem todos los agentes comparten la contraseña del repositorio; el aislamiento por agente (usuario + ACL por agente) llega con el Kopia Repository Server.
- **Wizard P2V / ISO:** fuera de Fase 1; el panel cubre archivos (explorador, descarga y restauración).

No usar este despliegue en producción sin revisar `docs/13-seguridad.md` (mTLS, TOTP activado para los usuarios del panel, red dedicada a backups).

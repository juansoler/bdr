# Despliegue en producción

Runbook del stack de producción de BDR Self-Hosted para el alcance actual:
**backup de archivos** con control plane durable (PostgreSQL), agentes mTLS,
aislamiento por agente (Kopia Repository Server), panel y TLS.

**Alcance y límites** (leer antes de operar): nodo único; backup de
archivos — imagen/P2V y réplica 3-2-1 S3 son fases posteriores del roadmap
(`docs/19-roadmap.md`); sin HA multi-nodo. Con esto cubre la **Fase 1 + Fase
0.5** del roadmap.

---

## 1. Requisitos

| Componente | Notas |
|---|---|
| Docker ≥ 24 + Compose v2 | host Linux con kernel reciente |
| 2 vCPU / 4 GB RAM / disco para `/data` | más espacio para el repositorio Kopia |
| DNS | un nombre: `bdr.<dominio>` apuntando al host (panel + API + descargas de agentes en el mismo origen) |
| Firewall | 80/443 (panel+API) y 8443 (agentes mTLS) abiertos al rango de gestión/agentes |

## 2. Secretos y variables

```bash
cd deploy
cat > .env <<'EOF'
BDR_ADMIN_TOKEN=$(openssl rand -base64 32)     # token de servicio/arranque
BDR_SECRET_KEY=$(openssl rand -base64 32)      # clave de cifrado de secretos
BDR_KOPIA_PASSWORD=<contraseña del repositorio>
BDR_KOPIA_CONTROL_PASSWORD=$(openssl rand -base64 24)  # API de control del KRS
BDR_DB_PASSWORD=$(openssl rand -base64 24)
BDR_PUBLIC_HOST=bdr.empresa.test               # host que alcanzan los agentes (8443)
BDR_DOMAIN=bdr.empresa.test                    # dominio único del panel+API (Caddy)
BDR_KOPIA_SERVER_URL=https://bdr.empresa.test:51515  # alcanzable DESDE LOS AGENTES
BDR_ADMIN_EMAIL=admin@empresa.test             # primer usuario del panel
BDR_ADMIN_PASSWORD=<contraseña larga, 12+ caracteres>
EOF
chmod 600 .env
```

- `BDR_SECRET_KEY` es la **clave maestra de secretos**: sin ella, las
  credenciales cifradas (TOTP, webhooks, SMTP, contraseñas de repositorio)
  son irrecuperables. Cópiala al gestor de secretos de la empresa.
- `BDR_ADMIN_TOKEN` queda como token de servicio para automatización; el
  acceso humano es por sesiones + TOTP.
- Certificados: Caddy usa su CA interna (`tls internal`) — aviso de navegador
  esperado. Para dominios públicos reales, quita `tls internal` del
  `Caddyfile` (ACME/Let's Encrypt) o pon un proxy corporativo delante.

## 3. Arranque

```bash
docker compose -f docker-compose.prod.yml run --rm kopia-init   # crea el repositorio (una vez)
docker compose -f docker-compose.prod.yml up -d
docker compose -f docker-compose.prod.yml ps
```

Verificación:

```bash
curl -k https://bdr.<dominio>/healthz            # {"status":"ok",...}
curl -kL https://bdr.<dominio>/                  # panel (login)
docker compose -f docker-compose.prod.yml logs control | grep migraciones
```

El control aplica las migraciones de PostgreSQL al arrancar y el servicio
`db-backup` genera `bdr-<fecha>.dump` diarios (retención 14) en el volumen
`bdr-db-backups`.

## 3.1 Opcional: aislamiento por agente (Kopia Repository Server)

El stack por defecto anuncia el KRS (`BDR_KOPIA_KIND=server`) para que cada
agente tenga usuario y contraseña propios. Genera el certificado del KRS y
arranca el perfil:

```bash
docker run --rm -v bdr_bdr-control-data:/data alpine sh -c '
  apk add --no-cache openssl >/dev/null 2>&1
  openssl req -x509 -newkey rsa:2048 -nodes -keyout /data/krs.key -out /data/krs.crt \
    -days 825 -subj "/CN=kopia" -addext "subjectAltName=DNS:kopia-server,IP:127.0.0.1"
  chown -R 10001:10001 /data
  echo "BDR_KOPIA_SERVER_FP=sha256:$(openssl x509 -in /data/krs.crt -outform DER | sha256sum | cut -d" " -f1)"'
docker compose -f docker-compose.prod.yml --profile server up -d kopia-server
# exporta BDR_KOPIA_SERVER_FP=<la huella impresa> en .env y:
docker compose -f docker-compose.prod.yml up -d control
```

Si prefieres el modo laboratorio compartido: `BDR_KOPIA_KIND=filesystem` en
`.env` (todos los agentes comparten contraseña de repositorio).

## 4. Altas de agentes

En cada equipo protegido (instaladores en `dist/`, ver `DEVELOPMENT.md §5`):

```bash
# 1. token de un solo uso desde el panel (Equipos → Instalar agente) o API
# 2. Linux:
sudo apt install ./dist/bdr-agent_*_amd64.deb
sudo bdr-agent register --server https://bdr.<dominio> \
  --token <TOKEN-DE-UN-USO> \
  --server-fingerprint <huella sha256 de la CA interna>
#    la huella se muestra en el panel o con:
#    docker compose -f docker-compose.prod.yml logs control | grep fingerprint
# 3. Windows (PowerShell como admin):
#    .\install.ps1 -Server https://bdr.<dominio> -Token <TOKEN> ...
```

El tráfico de agentes va por el listener **mTLS estricto** (8443): certificado
de cliente emitido en el enrolamiento, renovación automática y ligadura
certificado↔API key. Los agentes quedan aislados: solo ven sus propios
snapshots (modo KRS).

### Huella para enrolar a través de Caddy

El enrolamiento pasa por Caddy (CA interna de Caddy, no la del control), así
que el agente fija la huella del **certificado presentado por la API**:

```bash
FP=$(openssl s_client -connect bdr.<dominio>:443 -servername bdr.<dominio> </dev/null 2>/dev/null \
  | openssl x509 -outform DER | sha256sum | cut -d' ' -f1)
sudo bdr-agent register --server https://bdr.<dominio> --token <TOKEN> --server-fingerprint "sha256:$FP"
```

Tras enrolar, el agente cambia automáticamente al listener mTLS del control
(`agent_url`, puerto 8443) fijando la **CA interna del control** — esa huella
sí viaja en la respuesta de enrolamiento.

## 5. Operación

| Tarea | Comando |
|---|---|
| Estado | `docker compose -f docker-compose.prod.yml ps` |
| Logs | `docker compose -f docker-compose.prod.yml logs -f control` |
| Backup de la BD ahora | `./scripts/backup-control-db.sh` |
| Restaurar la BD | para `control` → `./scripts/backup-control-db.sh restore <fichero>` → levanta `control` |
| Verificar auditoría | panel → Auditoría → Verificar (o `GET /admin/audit/verify`) |
| Alertas offline/jobs fallidos | panel → Alertas; webhook/email en Configuración (con reintentos) |
| Verificación de copias | panel → Puntos de recuperación → Verificar (nivel 1/2 de Kopia) |

**Recomendado semanal**: restaurar el último `bdr-*.dump` en una base de
prueba y arrancar un control efímero contra ella — el smoke
(`BDR_SMOKE_STORE=postgres make smoke-postgres`) automatiza el mismo
procedimiento como referencia.

## 6. Actualización de versión

```bash
git pull && docker compose -f docker-compose.prod.yml build
docker compose -f docker-compose.prod.yml stop control web
docker compose -f docker-compose.prod.yml run --rm kopia-init sh -c \
  'true'   # (no-op; las migraciones aplica al arrancar el control)
./scripts/backup-control-db.sh            # copia ANTES de arrancar la nueva versión
docker compose -f docker-compose.prod.yml up -d
docker compose -f docker-compose.prod.yml logs control | grep -i migración
```

Las migraciones de la BD son incrementales y aplicadas al arrancar; si una
versión exigiera revertir, restaura el `.dump` previo (§5). Los agentes se
actualizan reinstalando el paquete (la configuración y la identidad mTLS se
conservan en el directorio de datos).

## 7. Recuperación ante desastres (alcance actual)

1. Host nuevo con Docker + DNS apuntado.
2. Restaura volúmenes: `bdr-control-data` (repositorio Kopia + PKI + claves)
   desde tu backup de volúmenes, y la BD con `backup-control-db.sh restore`.
   **Sin `bdr-control-data` (CA + repositorio) la BD sola no basta.**
3. `up -d`, verificar `/healthz`, auditoría intacta y un backup de prueba.

> Para el objetivo 3-2-1 completo (réplica S3 con Object Lock, verificación
> de arranque, HA) véase la Fase 3 del roadmap: pendiente.

## 8. Checklist de seguridad antes de producción

- [ ] `.env` con permisos 600 y fuera de Git; secretos en el gestor corporativo.
- [ ] `BDR_SECRET_KEY` copiada a custodia externa (docs/14-custodia-clave-maestra).
- [ ] Primer usuario del panel creado (`BDR_ADMIN_EMAIL/PASSWORD`) y TOTP activado.
- [ ] `BDR_ADMIN_TOKEN` restringido a la automatización que lo necesite.
- [ ] Firewall: 80/443 desde la red de gestión; 8443 solo desde los agentes.
- [ ] Certificados del panel/API válidos (o proxy corporativo con TLS real).
- [ ] `db-backup` generando dumps y una restauración de prueba hecha.
- [ ] Verificación de auditoría (`/admin/audit/verify`) en verde.
- [ ] Un backup + restore de ficheros E2E por sistema operativo crítico.

## 9. Acceso remoto por NetBird (o otra malla WireGuard)

El plano de control no necesita puertos expuestos a internet si solo se
alcanza por la malla. Dos reglas para que los one-liners del panel lleven
una dirección alcanzable:

- El `--public-url` del servicio manda sobre el Host de navegación: en
  macOS, el instalador fija `http://$(hostname -s):8080` (mDNS — solo
  resuelve en LAN). Para acceso remoto, sustitúyelo por la IP NetBird del
  servidor o un nombre DNS de la malla:
  ```bash
  sudo sed -i '' "s|http://$(hostname -s):8080|http://100.121.229.XX:8080|g" \
    /Library/LaunchDaemons/local.bdr.control.plist
  sudo launchctl bootout system/local.bdr.control 2>/dev/null || true
  sudo launchctl bootstrap system /Library/LaunchDaemons/local.bdr.control.plist
  ```
- Navega el panel por la dirección NetBird: los one-liners se generan con
  esa URL y los agentes remotos alcanzan el enrolamiento sin rutas extra.

Instalación con dirección fija desde el principio (sudo elimina las
variables de entorno normales, usa `env`):
```bash
sudo env BDR_PUBLIC_HOST=100.121.229.XX bash install-macos.sh
sudo env BDR_PUBLIC_HOST=100.121.229.XX bash install-linux.sh
```

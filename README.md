<div align="center">

# 🔐 BDR Self-Hosted

**Plataforma self-hosted de copias de seguridad y recuperación ante desastres**

Imágenes completas · archivos · VMs de Proxmox · SQL Server · PostgreSQL · P2V · cifrado en cliente · verificación de recuperabilidad

[![CI](https://github.com/juansoler/bdr/actions/workflows/ci.yml/badge.svg)](https://github.com/juansoler/bdr/actions/workflows/ci.yml)
[![Release](https://github.com/juansoler/bdr/actions/workflows/release.yml/badge.svg)](https://github.com/juansoler/bdr/releases)
[![GitHub release](https://img.shields.io/github/v/release/juansoler/bdr?include_prereleases)](https://github.com/juansoler/bdr/releases)
[![Go](https://img.shields.io/badge/Go-1.26-00ADD8?logo=go&logoColor=white)](https://go.dev)
[![Next.js](https://img.shields.io/badge/Next.js-15-black?logo=next.js)](https://nextjs.org)
[![License](https://img.shields.io/badge/License-Apache_2.0-blue.svg)](LICENSE)

**Servidor de binario único** (panel web embebido) · **Agentes para Linux, macOS y Windows** · **Sin coste por carga de trabajo**

</div>

---

## ¿Qué es BDR?

BDR es una plataforma de protección de datos construida sobre componentes open
source probados — **Kopia** para el repositorio de archivos, **Proxmox Backup
Server** para VMs, VSS/LVM/Btrfs/ZFS para consistencia y QEMU para
virtualización — con un plano de control propio que los orquesta como un solo
producto.

El objetivo es subconjunto deliberado de una suite completa de BDR, con tres
diferencias de diseño: **código 100 % auditable**, **estándares abiertos** y
**cero coste por carga de trabajo**.

```text
┌─────────────────────────────── tu servidor ───────────────────────────────┐
│                                                                          │
│   ┌──────────────────────── bdr-control (binario único) ──────────────┐  │
│   │  🌐 Panel web embebido   🔐 API REST + mTLS   ⏱ Programador        │  │
│   └────────┬───────────────────────┬───────────────────────┬──────────┘  │
│            │ :8080                 │ :8443 (mTLS)          │             │
│   ┌────────▼─────────┐    ┌────────▼─────────┐    ┌────────▼─────────┐   │
│   │  PostgreSQL      │    │ Repositorio      │    │ Kopia Repository │   │
│   │  (o state.json)  │    │ Kopia (NAS/S3)   │    │ Server (opcional)│   │
│   └──────────────────┘    └────────▲─────────┘    └──────────────────┘   │
└─────────────────────────────────────┼─────────────────────────────────────┘
                                      │ dedup + zstd + cifrado cliente
        ┌─────────────────────────────┼─────────────────────────────┐
        │                             │                             │
   ┌────┴─────┐                 ┌─────┴─────┐                ┌──────┴─────┐
   │ 🐧 Linux │                 │ 🪟 Windows│                │ 🍎 macOS   │
   │ bdr-agent│                 │ bdr-agent │                │ bdr-agent  │
   └──────────┘                 └───────────┘                └────────────┘
```

## Características

| Capacidad | Detalle | Estado |
|---|---|---|
| **Servidor** | Binario único con API REST, panel web embebido, mTLS y logs JSON | ✅ |
| **Backup de archivos** | Kopia real: incremental, dedup por contenido, compresión zstd, cifrado en cliente | ✅ |
| **Restore points** | Verificación de recuperabilidad nivel 1–2, explorador de archivos y restauración | ✅ |
| **SQL Server** | Backups nativos FULL/DIFF/LOG/COPY_ONLY con COMPRESSION+CHECKSUM, VERIFYONLY, DBCC CHECKDB, planes con `verify_cron`, cadenas por LSN con PITR (`STOPAT`), cifrado nativo con custodia de certificados, runbook de `master`, detección automática de instancias | ✅ |
| **PostgreSQL** | `pg_dump` custom comprimido con verificación `pg_restore --list`, restauración con `createdb`/`--clean`, inventario y detección automática de instancias | ✅ |
| **Acceso por pantalla** | Consola remota en el navegador (tiles JPEG estilo MeshCentral, helper nativo en Windows) | ✅ |
| **Agentes** | Enrolamiento con token de un solo uso, inventario (discos, TPM, BitLocker/LUKS), auto-actualización verificada | ✅ |
| **Programación** | Planes con cron, ventanas de horario, retención GFS, grupos con jerarquía | ✅ |
| **Seguridad** | mTLS obligatorio de agentes, RBAC + sesiones + TOTP, auditoría encadenada, secretos cifrados AES-256-GCM | ✅ |
| **Alertas** | Deduplicación, webhook firmado, email con reintentos | ✅ |
| **Actualizaciones** | Canal de releases firmado (Ed25519): el servidor **notifica** y tú decides | ✅ |
| **Persistencia** | PostgreSQL opcional con migraciones y auditoría duradera | ✅ |
| Backup de imagen / VSS / P2V / ISO | Fase 2 del roadmap | 🚧 |
| PITR físico de PostgreSQL (`pg_basebackup` + WAL) | Fase 2 del roadmap | 🚧 |
| Réplica 3-2-1 a S3 con Object Lock | Fase 3 | 🚧 |

## 🚀 Inicio rápido

### 1 · Instala el servidor (Linux / macOS)

```bash
curl -fsSL https://github.com/juansoler/bdr/releases/latest/download/install-linux.sh -o install.sh
less install.sh          # revisa siempre antes de ejecutar
sudo bash install.sh
```

<details>
<summary><strong>🪟 Windows (PowerShell como administrador)</strong></summary>

```powershell
irm https://github.com/juansoler/bdr/releases/latest/download/install-windows.ps1 -OutFile install-windows.ps1
notepad install-windows.ps1
.\install-windows.ps1
```

</details>

<details>
<summary>🐳 Docker / Compose</summary>

```bash
git clone https://github.com/juansoler/bdr.git && cd bdr/deploy
cat > .env <<'EOF'
BDR_ADMIN_TOKEN=$(openssl rand -base64 32)
BDR_SECRET_KEY=$(openssl rand -base64 32)
BDR_KOPIA_PASSWORD=$(openssl rand -base64 24)
BDR_DB_PASSWORD=$(openssl rand -base64 24)
BDR_DOMAIN=bdr.tudominio.com
BDR_PUBLIC_HOST=bdr.tudominio.com
EOF
docker compose -f docker-compose.prod.yml run --rm kopia-init   # una vez
docker compose -f docker-compose.prod.yml up -d
```

</details>

<details>
<summary>⚠️ ¿El <code>curl</code> devuelve 404?</summary>

Si el repositorio es **privado**, los assets de releases no se pueden descargar
sin autenticación. Opciones:

```bash
# Con gh CLI (más cómodo):
gh release download v0.8.1 -R juansoler/bdr -p install-linux.sh
sudo bash install.sh

# O con un token en el header:
curl -H "Authorization: Bearer $GITHUB_TOKEN" \
  -fsSL https://github.com/juansoler/bdr/releases/latest/download/install-linux.sh -o install.sh
```

</details>

### 2 · Entra en el panel

El instalador genera los secretos y arranca el servicio. Conéctate a
`http://<servidor>:8080` e introduce el token de
`/etc/bdr/bdr.env` (`BDR_ADMIN_TOKEN`):

```bash
sudo grep BDR_ADMIN_TOKEN /etc/bdr/bdr.env
```

> **Producción:** pon TLS delante (el stack de Compose incluye Caddy con
> Let's Encrypt) y sigue el checklist de
> [`deploy/PRODUCTION.md`](deploy/PRODUCTION.md#8-checklist-de-seguridad-antes-de-producción).

### 3 · Instala tu primer agente

En el panel: **Equipos → Añadir equipo** → genera el instalador para la
plataforma y copia el one-liner (el token de enrolamiento viaja embebido y es
de un solo uso):

```bash
# Linux — en el equipo protegido
# Linux — dos pasos: descarga a fichero y ejecuta (evita el clásico
# "Bad file descriptor" de sudo+fd y funciona con o sin sudo)
curl -fsSL "https://bdr.example/api/v1/download/installer/linux-amd64/<TOKEN>" -o /tmp/bdr-install.sh && sudo bash /tmp/bdr-install.sh
```

```powershell
# Windows — PowerShell como administrador
irm "https://bdr.example/api/v1/download/installer/windows-amd64/<TOKEN>.ps1" | iex
```

El agente se enrola con mTLS, publica heartbeat e inventario, y aparece en el
panel. Lanza un **backup ahora** y verifica que el restore point es
recuperable.

> Los instaladores son de un solo uso y **no re-ejecutables**: si el equipo ya
> tiene un agente enrolado, el instalador **pregunta** si quieres olvidar el
> servidor anterior y enrolar el equipo en este (al confirmar, borra la config
> y la identidad mTLS antiguas); sin terminal interactiva aborta para no crear
> un equipo duplicado (se fuerza con `BDR_FORCE_REENROLL=1` si de verdad
> quieres uno nuevo). Para recuperar un agente existente desconectado sin
> borrar nada usa **Reconectar** en su ficha: genera un token que lo reenlaza
> conservando su identidad, grupos, planes e historial.

### 4 · Actualiza cuando quieras

El servidor consulta el canal de releases firmado (Ed25519, clave pública
embebida en el binario) y te **notifica** en el panel: nada se instala sin
tu confirmación. Los agentes se actualizan solos cuando están ociosos,
verificando el SHA-256 del binario.

### 5 · Desinstala cuando quieras

El procedimiento por sistema operativo (agente y servidor, con scripts y
comandos manuales equivalentes) está en
[`deploy/uninstall/README.md`](deploy/uninstall/README.md). Por defecto los
desinstaladores solo detienen el servicio y eliminan los binarios: el estado,
los secretos y el repositorio Kopia **se conservan**; con `--purge` /
`-Purge` se borra todo (con confirmación, y las copias externas NAS/S3
quedan siempre fuera).

```bash
# Linux / macOS — en el equipo protegido
curl -fsSL https://github.com/juansoler/bdr/releases/latest/download/uninstall-agent.sh -o /tmp/uninstall-agent.sh
sudo bash /tmp/uninstall-agent.sh
```

## 🗄️ Bases de datos: SQL Server y PostgreSQL

El motor corre **dentro del agente instalado en el host de la base de datos**
y los jobs se orquestan desde el plano de control: backup nativo →
transferencia al destino (S3/SMB/local, con Object Lock GOVERNANCE
opcional) → catálogo con metadatos para la restauración. Al elegir el
agente en el panel, **las instancias se detectan automáticamente** y el
selector de bases se rellena solo.

### SQL Server (panel → SQL Server)

Requisitos en el host (Windows):

1. **Agente BDR** instalado (corre como LocalSystem → SSPI como
   `NT AUTHORITY\SYSTEM`).
2. **Login y permisos** en SQL Server — el conjunto mínimo validado:

   ```sql
   CREATE LOGIN [NT AUTHORITY\SYSTEM] FROM WINDOWS;  -- si no existe ya
   ALTER SERVER ROLE dbcreator     ADD MEMBER [NT AUTHORITY\SYSTEM]; -- VERIFYONLY/RESTORE
   -- por base: db_backupoperator (o sysadmin en laboratorio)
   ```

3. **Staging**: automático — el agente consulta el `BackupDirectory` de la
   instancia (`xp_instance_regread`). Sobreescribe con la variable de
   entorno del servicio:

   ```powershell
   reg add "HKLM\SYSTEM\CurrentControlSet\Services\bdr-agent" /v Environment /t REG_MULTI_SZ /d "BDR_SQL_STAGING=C:\bdr-sql-staging" /f
   ```

   (la carpeta debe ser escribible por la cuenta del servicio de SQL:
   `icacls C:\bdr-sql-staging /grant "NT Service\MSSQLSERVER:(OI)(CI)M" /t`)

Flujo en el panel: elige agente → **autodetección de instancias** →
elige base del selector → **Backup** (FULL/DIFF/LOG/COPY_ONLY, destino,
retención, VERIFYONLY) → planes programados con cron y `verify_cron` →
**CheckDB** (DBCC con alerta si hay corrupción) → **Restaurar** con
cadena calculada FULL→DIFF→LOG por continuidad de LSN, PITR con punto en
el tiempo y destino en otra instancia (WITH REPLACE / MOVE).

Extras: **cifrado nativo** (`WITH ENCRYPTION` por certificado custodiado —
la custodia `.cer/.pvk` se sella en el control y alerta si falta),
**runbook de master** con `bdr-sql runbook-master` (la restauración de
`master` exige single-user manual y el motor la rechaza a propósito), y
**CLI de laboratorio** `bdr-sql` (discover/inventory/backup/chain/restore/
health) publicado como asset de cada release.

### PostgreSQL (panel → PostgreSQL)

Requisitos en el host:

1. **Agente BDR** + **cliente PostgreSQL** en el PATH del servicio
   (`pg_dump`, `pg_restore`, `createdb` — paquete `postgresql-client`).
2. **Credenciales de conexión** — nunca viajan por la API; el agente usa
   su entorno (o `.pgpass`):

   ```bash
   # Linux (systemd):
   systemctl edit bdr-agent    # [Service] Environment=BDR_PG_PASSWORD=...
   systemctl restart bdr-agent
   ```

   ```powershell
   # Windows:
   reg add "HKLM\SYSTEM\CurrentControlSet\Services\bdr-agent" /v Environment /t REG_MULTI_SZ /d "BDR_PG_PASSWORD=..." /f
   Restart-Service bdr-agent
   ```

   Y un rol con permisos de lectura (p. ej. `GRANT pg_read_all_data TO
   bdr_backup;` — superusuario solo en laboratorio).

Flujo en el panel: elige agente → **autodetección** del puerto (servicios
`postgresql-*` en Windows, `pg_lsclusters`/`systemctl` en Linux) →
Inventario → Dump (`--format=custom`, comprimido, verificado con
`pg_restore --list` tras cada dump) → Restaurar con `createdb` (motor
limpio) o `--clean --if-exists` (reemplazo), destino en otra máquina
incluido.

### Variables de entorno del agente (motores de bases de datos)

| Variable | Motor | Uso |
|---|---|---|
| `BDR_SQL_STAGING` | SQL Server | Carpeta staging donde SQL Server escribe los `.bak` (default: `BackupDirectory` de la instancia) |
| `BDR_PG_PASSWORD` | PostgreSQL | Contraseña para pg_dump/pg_restore/inventario |
| `BDR_PG_STAGING` | PostgreSQL | Carpeta de dumps (default: temp del agente) |

### API de motores (resumen)

| Endpoint | Operación |
|---|---|
| `POST /api/v1/admin/sql/{backup,restore,verify,checkdb,inventory,discover}` | Operaciones SQL Server (el restore exige `confirm: true`) |
| `GET /api/v1/admin/sql/restore-points` · `POST /api/v1/admin/sql/restore-chain` | Catálogo SQL y cadena calculada por LSN |
| `POST /api/v1/admin/sql/{plans}` + `PUT/DELETE …/plans/{id}` | Planes SQL programados (cron + `verify_cron`) |
| `POST /api/v1/admin/sql/certbackup` | Custodia de certificados (cifrado nativo) |
| `POST /api/v1/admin/pg/{dump,restore,verify,inventory,discover}` · `GET …/restore-points` | Operaciones PostgreSQL |

## 🧩 Arquitectura técnica

| Componente | Elección |
|---|---|
| Servidor y agentes | Go (binario único, cross-platform) |
| Panel | Next.js 15 (export estático embebido con `go:embed`) |
| Base de datos | PostgreSQL (alternativa: `state.json` con cifrado en reposo) |
| Motor de archivos | Kopia (Apache-2.0), fijado por versión |
| SQL Server | T-SQL nativo vía `go-mssqldb` (SSPI), msdb para cadenas y LSNs |
| PostgreSQL | `pg_dump`/`pg_restore`/`createdb` (cliente oficial, formato custom) |
| VMs de Proxmox | Proxmox Backup Server orquestado por API |
| Repositorio | Kopia Repository Server: los agentes nunca reciben la clave maestra |
| Seguridad | mTLS, TOTP, PBKDF2, auditoría encadenada, canales firmados |

## 📚 Documentación

| Doc | Contenido |
|---|---|
| [`DEVELOPMENT.md`](DEVELOPMENT.md) | Guía de desarrollo completa, API implementada y límites |
| [`docs/04-arquitectura.md`](docs/04-arquitectura.md) | Componentes, flujos y modos de fallo |
| [`docs/13-seguridad.md`](docs/13-seguridad.md) | Modelo de amenazas y controles |
| [`deploy/PRODUCTION.md`](deploy/PRODUCTION.md) | Runbook de producción (PostgreSQL, mTLS, KRS, Caddy) |
| [`docs/19-roadmap.md`](docs/19-roadmap.md) | Fases y puertas de salida |
| [`docs/24-motor-sql-server.md`](docs/24-motor-sql-server.md) | Especificación del motor SQL Server (RF-40–46) |
| [`CONTRIBUTING.md`](CONTRIBUTING.md) | Cómo contribuir (DCO, tests, estilo) |
| [`SECURITY.md`](SECURITY.md) | Política de seguridad y cómo reportar vulnerabilidades |

## 🧪 Desarrollo

```bash
git clone https://github.com/juansoler/bdr.git && cd bdr
make build       # exporta el panel y compila bin/bdr-control + bin/bdr-agent
make test        # tests unitarios y de contrato
make smoke       # servidor + agente reales en localhost
make smoke-web   # panel embebido: /healthz, SPA, assets y API en el mismo origen
```

Requisitos: Go ≥ 1.26, Node 22 (solo para compilar el panel), curl y Python
para los smoke tests.

## 🗺️ Roadmap

| Fase | Alcance | Estado |
|---|---|---|
| 1 | MVP de archivos: agentes, planes, Kopia, panel, alertas | ✅ completa |
| 0.5 | PostgreSQL, mTLS estricto, Kopia Repository Server | ✅ completa |
| 2 | Imagen completa, VSS/LVM/Btrfs, restauración P2V a Proxmox, ISO de rescate | 🚧 |
| 2b | Motores de bases de datos: SQL Server (RF-40–46) y PostgreSQL | ✅ núcleo completo — lab y PITR-PG en curso |
| 3 | Réplica 3-2-1 a S3 con Object Lock, verificación de arranque, HA | ⏳ |
| 4 | CBT, instant restore, multitenancia | ⏳ |

## 🤝 Contribuir

Las contribuciones son bienvenidas bajo la **Apache License 2.0**. Lee
[`CONTRIBUTING.md`](CONTRIBUTING.md): DCO en cada commit, tests en verde y
sin marcas de terceros ni datos reales en el código.

## 🔐 Seguridad

Modelo de amenazas, controles y custodia de claves en
[`docs/13-seguridad.md`](docs/13-seguridad.md). Reporta vulnerabilidades en
privado siguiendo [`SECURITY.md`](SECURITY.md) — no abras un issue público.

## 📄 Licencias y atribución

- **BDR Self-Hosted**: [Apache License 2.0](LICENSE).
- **Terceros**: avisos de copyright y licencia de Kopia, dependencias Go,
  panel web y fuentes en
  [`THIRD_PARTY_NOTICES.md`](THIRD_PARTY_NOTICES.md) y [`NOTICE`](NOTICE).
- **Kopia**: Apache-2.0 — se redistribuye como binario auxiliar.
- **Next.js / React**: MIT — panel compilado a estáticos.
- **Fuentes del panel**: Barlow Condensed, IBM Plex Sans y IBM Plex Mono —
  SIL OFL 1.1 (ver [`THIRD_PARTY_NOTICES.md`](THIRD_PARTY_NOTICES.md) y
  [`NOTICE`](NOTICE)).
- **Proxmox VE / PBS**: AGPL-3.0 — se usan por API, no se redistribuye código.

Los detalles completos están en [`NOTICE`](NOTICE).

---

<div align="center">

Hecho con ❤️ y sin coste por carga de trabajo.

</div>

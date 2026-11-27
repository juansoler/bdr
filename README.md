[Español](README.es.md)

<div align="center">

# 🔐 BDR Self-Hosted

**Self-hosted backup and disaster recovery platform**

Full images · files · Proxmox VMs · SQL Server · PostgreSQL · P2V · client-side encryption · recoverability verification

[![CI](https://github.com/juansoler/bdr/actions/workflows/ci.yml/badge.svg)](https://github.com/juansoler/bdr/actions/workflows/ci.yml)
[![Release](https://github.com/juansoler/bdr/actions/workflows/release.yml/badge.svg)](https://github.com/juansoler/bdr/actions/workflows/release.yml)
[![GitHub release](https://img.shields.io/github/v/release/juansoler/bdr?include_prereleases)](https://github.com/juansoler/bdr/releases)
[![Go](https://img.shields.io/badge/Go-1.26-00ADD8?logo=go&logoColor=white)](https://go.dev)
[![Next.js](https://img.shields.io/badge/Next.js-15-black?logo=next.js)](https://nextjs.org)
[![License](https://img.shields.io/badge/License-Apache_2.0-blue.svg)](LICENSE)
[![Web](https://img.shields.io/badge/web-juansoler.github.io%2Fbdr-c8ff4d)](https://juansoler.github.io/bdr/)

**Single-binary server** (embedded web panel) · **Agents for Linux, macOS and Windows** · **No per-workload cost**

🌐 **[Project website](https://juansoler.github.io/bdr/)** — capabilities, security and installation on a single page.

</div>

---

## What is BDR?

BDR is a data protection platform built on proven open source components —
**Kopia** for the file repository, **Proxmox Backup Server** for VMs,
VSS/LVM/Btrfs/ZFS for consistency and QEMU for virtualization — with its own
control plane that orchestrates them as a single product.

The goal is a deliberate subset of a full BDR suite, with three design
differences: **100 % auditable code**, **open standards** and **zero per-
workload cost**.

```text
┌─────────────────────────────── your server ──────────────────────────────┐
│                                                                          │
│   ┌──────────────────────── bdr-control (single binary) ──────────────┐  │
│   │  🌐 Embedded web panel   🔐 REST API + mTLS   ⏱ Scheduler          │  │
│   └────────┬───────────────────────┬───────────────────────┬──────────┘  │
│            │ :8080                 │ :8443 (mTLS)          │             │
│   ┌────────▼─────────┐    ┌────────▼─────────┐    ┌────────▼─────────┐   │
│   │  PostgreSQL      │    │ Kopia Repository │    │ Kopia Repository │   │
│   │  (or state.json) │    │ (NAS/S3)         │    │ Server (optional)│   │
│   └──────────────────┘    └────────▲─────────┘    └──────────────────┘   │
└─────────────────────────────────────┼─────────────────────────────────────┘
                                      │ dedup + zstd + client-side encryption
        ┌─────────────────────────────┼─────────────────────────────┐
        │                             │                             │
   ┌────┴─────┐                 ┌─────┴─────┐                ┌──────┴─────┐
   │ 🐧 Linux │                 │ 🪟 Windows│                │ 🍎 macOS   │
   │ bdr-agent│                 │ bdr-agent │                │ bdr-agent  │
   └──────────┘                 └───────────┘                └────────────┘
```

## Features

| Capability | Detail | Status |
|---|---|---|
| **Server** | Single binary with REST API, embedded web panel, mTLS and JSON logs | ✅ |
| **File backup** | Real Kopia: incremental, content-defined dedup, zstd compression, client-side encryption | ✅ |
| **Restore points** | Level 1–2 recoverability verification, file browser and restore | ✅ |
| **SQL Server** | Native FULL/DIFF/LOG/COPY_ONLY backups with COMPRESSION+CHECKSUM, VERIFYONLY, DBCC CHECKDB, plans with `verify_cron`, LSN-based chains with PITR (`STOPAT`), native encryption with certificate custody, `master` runbook, automatic instance discovery | ✅ |
| **PostgreSQL** | Compressed custom `pg_dump` with `pg_restore --list` verification, restore with `createdb`/`--clean`, inventory and automatic instance discovery | ✅ |
| **Screen access** | Remote console in the browser (JPEG tiles à la MeshCentral, native helper on Windows) | ✅ |
| **Agents** | Single-use token enrollment, inventory (disks, TPM, BitLocker/LUKS), verified self-update | ✅ |
| **Scheduling** | Cron plans, schedule windows, GFS retention, groups with hierarchy | ✅ |
| **Security** | Mandatory agent mTLS, RBAC + sessions + TOTP, hash-chained audit log, AES-256-GCM encrypted secrets | ✅ |
| **Alerts** | Deduplication, signed webhook, email with retries | ✅ |
| **Updates** | Signed release channel (Ed25519): the server **notifies** and you decide | ✅ |
| **Persistence** | Optional PostgreSQL with migrations and durable audit | ✅ |
| Image backup / VSS / P2V / ISO | Phase 2 of the roadmap | 🚧 |
| PostgreSQL physical PITR (`pg_basebackup` + WAL) | Phase 2 of the roadmap | 🚧 |
| 3-2-1 replication to S3 with Object Lock | Phase 3 | 🚧 |

## 🚀 Quick start

### 1 · Install the server (Linux / macOS)

```bash
curl -fsSL https://github.com/juansoler/bdr/releases/latest/download/install-linux.sh -o install.sh
less install.sh          # always review before running
sudo bash install.sh
```

<details>
<summary><strong>🪟 Windows (PowerShell as administrator)</strong></summary>

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
cp .env.example .env && chmod 600 .env
# Generate the secrets (once) and paste them into the empty keys in .env
for v in BDR_ADMIN_TOKEN BDR_SECRET_KEY; do echo "$v=$(openssl rand -base64 32)"; done
for v in BDR_KOPIA_PASSWORD BDR_KOPIA_CONTROL_PASSWORD BDR_DB_PASSWORD; do echo "$v=$(openssl rand -hex 32)"; done
${EDITOR:-vi} .env    # paste the secrets and set your domain (BDR_DOMAIN, BDR_PUBLIC_HOST)
docker compose -f docker-compose.prod.yml config -q    # validates the variables without starting anything
docker compose -f docker-compose.prod.yml run --rm kopia-init   # once
docker compose -f docker-compose.prod.yml up -d
```

Each variable is explained in `deploy/.env.example`; the full runbook is in
[`deploy/PRODUCTION.md`](deploy/PRODUCTION.md).

</details>

<details>
<summary>⚠️ Is <code>curl</code> returning 404?</summary>

If the repository is **private**, release assets cannot be downloaded without
authentication. Options:

```bash
# With gh CLI (most convenient):
gh release download v0.8.1 -R juansoler/bdr -p install-linux.sh
sudo bash install.sh

# Or with a token in the header:
curl -H "Authorization: Bearer $GITHUB_TOKEN" \
  -fsSL https://github.com/juansoler/bdr/releases/latest/download/install-linux.sh -o install.sh
```

</details>

### 2 · Log into the panel

The installer generates the secrets and starts the service. Connect to
`http://<server>:8080` and enter the token from
`/etc/bdr/bdr.env` (`BDR_ADMIN_TOKEN`):

```bash
sudo grep BDR_ADMIN_TOKEN /etc/bdr/bdr.env
```

> **Production:** put TLS in front (the Compose stack includes Caddy with
> Let's Encrypt) and follow the checklist in
> [`deploy/PRODUCTION.md`](deploy/PRODUCTION.md).

### 3 · Install your first agent

In the panel: **Computers → Add computer** → generate the installer for the
platform and copy the one-liner (the enrollment token travels embedded and is
single-use):

```bash
# Linux — on the protected machine
# Two steps: download to a file and run (avoids the classic sudo+fd
# "Bad file descriptor" and works with or without sudo)
curl -fsSL "https://bdr.example/api/v1/download/installer/linux-amd64/<TOKEN>" -o /tmp/bdr-install.sh && sudo bash /tmp/bdr-install.sh
```

```powershell
# Windows — PowerShell as administrator
irm "https://bdr.example/api/v1/download/installer/windows-amd64/<TOKEN>.ps1" | iex
```

The agent enrolls over mTLS, publishes heartbeat and inventory, and appears in
the panel. Launch a **backup now** and verify that the restore point is
recoverable.

> Installers are single-use and **not re-runnable**: if the machine already
> has an enrolled agent, the installer **asks** whether you want to forget the
> previous server and enroll the machine with this one (if confirmed, it
> deletes the old config and mTLS identity); without an interactive terminal
> it aborts to avoid creating a duplicate computer (force it with
> `BDR_FORCE_REENROLL=1` if you really want a new one). To recover an
> existing disconnected agent without deleting anything use **Reconnect** on
> its card: it generates a token that re-links it while keeping its identity,
> groups, plans and history.

### 4 · Update whenever you want

The server queries the signed release channel (Ed25519, public key embedded
in the binary) and **notifies** you in the panel: nothing is installed without
your confirmation. Agents update themselves when idle, verifying the binary's
SHA-256.

### 5 · Uninstall whenever you want

The per-OS procedure (agent and server, with scripts and equivalent manual
commands) is in
[`deploy/uninstall/README.md`](deploy/uninstall/README.md). By default the
uninstallers only stop the service and remove the binaries: state, secrets
and the Kopia repository are **preserved**; with `--purge` / `-Purge`
everything is deleted (with confirmation, and external NAS/S3 backups always
stay out of reach).

```bash
# Linux / macOS — on the protected machine
curl -fsSL https://github.com/juansoler/bdr/releases/latest/download/uninstall-agent.sh -o /tmp/uninstall-agent.sh
sudo bash /tmp/uninstall-agent.sh
```

## 🗄️ Databases: SQL Server and PostgreSQL

The engine runs **inside the agent installed on the database host** and jobs
are orchestrated from the control plane: native backup → transfer to the
destination (S3/SMB/local, with optional GOVERNANCE Object Lock) → catalog
with metadata for restore. When you pick the agent in the panel, **instances
are discovered automatically** and the database selector fills itself in.

### SQL Server (panel → SQL Server)

Requirements on the host (Windows):

1. **BDR agent** installed (runs as LocalSystem → SSPI as
   `NT AUTHORITY\SYSTEM`).
2. **Login and permissions** in SQL Server — the minimum validated set:

   ```sql
   CREATE LOGIN [NT AUTHORITY\SYSTEM] FROM WINDOWS;  -- if not already present
   ALTER SERVER ROLE dbcreator     ADD MEMBER [NT AUTHORITY\SYSTEM]; -- VERIFYONLY/RESTORE
   -- per database: db_backupoperator (or sysadmin in the lab)
   ```

3. **Staging**: automatic — the agent queries the instance's `BackupDirectory`
   (`xp_instance_regread`). Override it with the service's environment
   variable:

   ```powershell
   reg add "HKLM\SYSTEM\CurrentControlSet\Services\bdr-agent" /v Environment /t REG_MULTI_SZ /d "BDR_SQL_STAGING=C:\bdr-sql-staging" /f
   ```

   (the folder must be writable by the SQL service account:
   `icacls C:\bdr-sql-staging /grant "NT Service\MSSQLSERVER:(OI)(CI)M" /t`)

Panel flow: pick agent → **automatic instance discovery** → pick database
from the selector → **Backup** (FULL/DIFF/LOG/COPY_ONLY, destination,
retention, VERIFYONLY) → scheduled plans with cron and `verify_cron` →
**CheckDB** (DBCC with alert on corruption) → **Restore** with the calculated
FULL→DIFF→LOG chain for LSN continuity, PITR with a point in time and
destination on another instance (WITH REPLACE / MOVE).

Extras: **native encryption** (`WITH ENCRYPTION` with a certificate under
custody — the `.cer/.pvk` custody is sealed in the control plane and alerts
if missing), the **master runbook** with `bdr-sql runbook-master` (restoring
`master` requires manual single-user mode and the engine refuses it on
purpose), and the **lab CLI** `bdr-sql`
(discover/inventory/backup/chain/restore/health) published as a release
asset.

### PostgreSQL (panel → PostgreSQL)

Requirements on the host:

1. **BDR agent** + **PostgreSQL client** on the service's PATH
   (`pg_dump`, `pg_restore`, `createdb` — the `postgresql-client` package).
2. **Connection credentials** — they never travel through the API; the agent
   uses its environment (or `.pgpass`):

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

   And a role with read permissions (e.g. `GRANT pg_read_all_data TO
   bdr_backup;` — superuser only in the lab).

Panel flow: pick agent → **automatic discovery** of the port
(`postgresql-*` services on Windows, `pg_lsclusters`/`systemctl` on Linux) →
Inventory → Dump (`--format=custom`, compressed, verified with
`pg_restore --list` after every dump) → Restore with `createdb` (clean
engine) or `--clean --if-exists` (replacement), including a destination on
another machine.

### Agent environment variables (database engines)

| Variable | Engine | Purpose |
|---|---|---|
| `BDR_SQL_STAGING` | SQL Server | Staging folder where SQL Server writes the `.bak` files (default: instance's `BackupDirectory`) |
| `BDR_PG_PASSWORD` | PostgreSQL | Password for pg_dump/pg_restore/inventory |
| `BDR_PG_STAGING` | PostgreSQL | Dump folder (default: agent temp) |

### Engine API (summary)

| Endpoint | Operation |
|---|---|
| `POST /api/v1/admin/sql/{backup,restore,verify,checkdb,inventory,discover}` | SQL Server operations (restore requires `confirm: true`) |
| `GET /api/v1/admin/sql/restore-points` · `POST /api/v1/admin/sql/restore-chain` | SQL catalog and LSN-calculated chain |
| `POST /api/v1/admin/sql/{plans}` + `PUT/DELETE …/plans/{id}` | Scheduled SQL plans (cron + `verify_cron`) |
| `POST /api/v1/admin/sql/certbackup` | Certificate custody (native encryption) |
| `POST /api/v1/admin/pg/{dump,restore,verify,inventory,discover}` · `GET …/restore-points` | PostgreSQL operations |

## 🧩 Technical architecture

| Component | Choice |
|---|---|
| Server and agents | Go (single binary, cross-platform) |
| Panel | Next.js 15 (static export embedded with `go:embed`) |
| Database | PostgreSQL (alternative: `state.json` with encryption at rest) |
| File engine | Kopia (Apache-2.0), version-pinned |
| SQL Server | Native T-SQL via `go-mssqldb` (SSPI), msdb for chains and LSNs |
| PostgreSQL | `pg_dump`/`pg_restore`/`createdb` (official client, custom format) |
| Proxmox VMs | Proxmox Backup Server orchestrated via API |
| Repository | Kopia Repository Server: agents never receive the master key |
| Security | mTLS, TOTP, PBKDF2, hash-chained audit, signed channels |

## 📚 Documentation

| Doc | Content |
|---|---|
| [`DEVELOPMENT.md`](DEVELOPMENT.md) | Full development guide, implemented API and limits |
| [`docs/04-arquitectura.md`](docs/04-arquitectura.md) | Components, flows and failure modes |
| [`docs/13-seguridad.md`](docs/13-seguridad.md) | Threat model and controls |
| [`deploy/PRODUCTION.md`](deploy/PRODUCTION.md) | Production runbook (PostgreSQL, mTLS, KRS, Caddy) |
| [`docs/19-roadmap.md`](docs/19-roadmap.md) | Phases and exit gates |
| [`docs/24-motor-sql-server.md`](docs/24-motor-sql-server.md) | SQL Server engine specification (RF-40–46) |
| [`CONTRIBUTING.md`](CONTRIBUTING.md) | How to contribute (DCO, tests, style) |
| [`SECURITY.md`](SECURITY.md) | Security policy and how to report vulnerabilities |

## 🧪 Development

```bash
git clone https://github.com/juansoler/bdr.git && cd bdr
make build       # exports the panel and builds bin/bdr-control + bin/bdr-agent
make test        # unit and contract tests
make smoke       # real server + agent on localhost
make smoke-web   # embedded panel: /healthz, SPA, assets and API on the same origin
```

Requirements: Go ≥ 1.26, Node 22 (only to build the panel), curl and Python
for the smoke tests.

## 🗺️ Roadmap

| Phase | Scope | Status |
|---|---|---|
| 1 | Files MVP: agents, plans, Kopia, panel, alerts | ✅ complete |
| 0.5 | PostgreSQL, strict mTLS, Kopia Repository Server | ✅ complete |
| 2 | Full image, VSS/LVM/Btrfs, P2V restore to Proxmox, rescue ISO | 🚧 |
| 2b | Database engines: SQL Server (RF-40–46) and PostgreSQL | ✅ core complete — lab and PG-PITR in progress |
| 3 | 3-2-1 replication to S3 with Object Lock, boot verification, HA | ⏳ |
| 4 | CBT, instant restore, multi-tenancy | ⏳ |

## 🤝 Contributing

Contributions are welcome under the **Apache License 2.0**. Read
[`CONTRIBUTING.md`](CONTRIBUTING.md): DCO on every commit, green tests and no
third-party trademarks or real data in the code.

## 🔐 Security

Threat model, controls and key custody in
[`docs/13-seguridad.md`](docs/13-seguridad.md). Report vulnerabilities
privately following [`SECURITY.md`](SECURITY.md) — do not open a public
issue.

## 📄 Licenses and attribution

- **BDR Self-Hosted**: [Apache License 2.0](LICENSE).
- **Third parties**: copyright and license notices for Kopia, Go
  dependencies, the web panel and fonts in
  [`THIRD_PARTY_NOTICES.md`](THIRD_PARTY_NOTICES.md) and [`NOTICE`](NOTICE).
- **Kopia**: Apache-2.0 — redistributed as a helper binary.
- **Next.js / React**: MIT — panel compiled to static assets.
- **Panel fonts**: Barlow Condensed, IBM Plex Sans and IBM Plex Mono —
  SIL OFL 1.1 (see [`THIRD_PARTY_NOTICES.md`](THIRD_PARTY_NOTICES.md) and
  [`NOTICE`](NOTICE)).
- **Proxmox VE / PBS**: AGPL-3.0 — used via API, no code redistributed.

Full details in [`NOTICE`](NOTICE).

---

<div align="center">

Made with ❤️ and no per-workload cost.

</div>

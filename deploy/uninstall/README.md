# Desinstalación

Procedimiento para retirar **bdr-agent** y **bdr-control** en todos los
sistemas operativos. Cada plataforma tiene un script que hace todo el
trabajo; junto a él se listan los comandos manuales equivalentes.

**Descarga los scripts de la release** (mismo canal que los instaladores):

```bash
# Linux / macOS
curl -fsSL https://github.com/juansoler/bdr/releases/latest/download/uninstall-agent.sh -o /tmp/uninstall-agent.sh
sudo bash /tmp/uninstall-agent.sh
```

```powershell
# Windows (PowerShell como administrador)
irm https://github.com/juansoler/bdr/releases/latest/download/uninstall-agent.ps1 -OutFile uninstall-agent.ps1
.\uninstall-agent.ps1
```

---

## ⚠️ Antes de empezar

1. **Las copias de seguridad NO se borran por defecto.** El desinstalador
   conserva el repositorio Kopia (`/opt/bdr/repo` en instalación nativa,
   el volumen `bdr-control-data` en Compose) y el estado del control.
   Solo `-Purge` / `--purge` los elimina, con confirmación explícita.
2. **Si el repositorio es externo (NAS/S3), ninguna purga local lo toca:**
   límpialo con el CLI de Kopia o desde el panel *antes* de purgar.
3. **Orden recomendado** cuando se retira todo el despliegue:
   revocar los agentes desde el panel → desinstalar agentes en los equipos
   → desinstalar el control → limpiar repositorio externo si procede.
4. **`secret.key` y los secretos**: si vas a reinstalar el control y quieres
   conservar los secretos cifrados (credenciales de repositorio, TOTP),
   NO purgues: sin `secret.key` (o `BDR_SECRET_KEY`) esos datos no se
   recuperan (docs/14).
5. Un agente desinstalado **sigue apareciendo en el panel** (estado
   desconectado) hasta que lo revoques desde *Equipos → Revocar*.

---

## Desinstalación de bdr-agent

### Linux (systemd)

```bash
# Script: hace todo lo de abajo (+ .deb y datos)
sudo bash uninstall-agent.sh            # conserva datos
sudo bash uninstall-agent.sh --purge    # borra también /var/lib/bdr-agent
```

Manual:

```bash
sudo systemctl disable --now bdr-agent
sudo rm -f /etc/systemd/system/bdr-agent.service
sudo systemctl daemon-reload
sudo rm -f /usr/local/bin/bdr-agent        # instalado por el panel/one-liner
# Si vino del paquete .deb (binario en /usr/bin):
sudo apt remove --purge bdr-agent          # o dpkg -r bdr-agent
# Datos y caché de Kopia (opcional):
sudo rm -rf /var/lib/bdr-agent
# Usuario del servicio (creado por .deb / install.sh):
sudo userdel bdr-agent
```

> El servicio del agente no lleva variables de entorno propias; si usaste
> `systemctl edit bdr-agent` para PostgreSQL (`BDR_PG_PASSWORD`), el drop-in
> queda en `/etc/systemd/system/bdr-agent.service.d/` — bórralo también:
> `sudo rm -rf /etc/systemd/system/bdr-agent.service.d`.

### macOS (launchd)

```bash
sudo bash uninstall-agent.sh               # conserva datos
sudo bash uninstall-agent.sh --purge       # borra agent.json y caché
```

Manual:

```bash
sudo launchctl bootout system/local.bdr.agent
sudo rm -f /Library/LaunchDaemons/local.bdr.agent.plist
sudo rm -f /usr/local/bin/bdr-agent
# Datos (opcional). El directorio por defecto se COMPARTE con bdr-control:
# borra solo los ficheros del agente, no el directorio entero.
sudo rm -rf "/Library/Application Support/BDR/agent.json" \
            "/Library/Application Support/BDR/agent.log" \
            "/Library/Application Support/BDR/kopia"
```

### Windows

```powershell
.\uninstall-agent.ps1                # conserva datos
.\uninstall-agent.ps1 -Purge         # borra también C:\ProgramData\BDR (parte del agente)
```

Cubre las dos variantes de instalación: el **servicio de Windows** (one-liner
del panel) y la **tarea programada "BDR Agent"** (instalador de
`deploy/agent/windows`). Manual:

```powershell
# Variante servicio (panel):
Stop-Service bdr-agent -Force
& sc.exe delete bdr-agent

# Variante tarea programada:
Stop-ScheduledTask "BDR Agent"
Unregister-ScheduledTask -TaskName "BDR Agent" -Confirm:$false

# Binario (el directorio se comparte con bdr-control):
Remove-Item "$env:ProgramFiles\BDR\bdr-agent.exe" -Force

# Datos (opcional; el directorio se comparte con bdr-control):
Remove-Item "$env:ProgramData\BDR\agent.json" -Force -ErrorAction SilentlyContinue
Remove-Item "$env:ProgramData\BDR\agent.log" -Force -ErrorAction SilentlyContinue
Remove-Item "$env:ProgramData\BDR\kopia" -Recurse -Force -ErrorAction SilentlyContinue

# Variables del motor SQL, si las configuraste (desaparecen con el servicio,
# pero los drop-ins de registro con BDR_PG_PASSWORD/BDR_SQL_STAGING también):
Remove-ItemProperty "HKLM:\SYSTEM\CurrentControlSet\Services\bdr-agent" -Name Environment -ErrorAction SilentlyContinue
```

---

## Desinstalación de bdr-control

### Linux (systemd)

```bash
sudo bash uninstall-control.sh                # conserva estado, secretos y repo
sudo bash uninstall-control.sh --purge        # borra TODO, INCLUIDOS LOS BACKUPS
```

Manual:

```bash
sudo systemctl disable --now bdr-control
sudo rm -f /etc/systemd/system/bdr-control.service
sudo systemctl daemon-reload
sudo rm -rf /opt/bdr/bin /opt/bdr/agents      # binarios + instaladores de agentes
# Se CONSERVA por defecto (bórralos solo si estás seguro):
#   /opt/bdr/repo    ← copias de seguridad
#   /var/lib/bdr     ← estado del control (equipos, planes, auditoría, secret.key)
#   /etc/bdr/bdr.env ← secretos (BDR_ADMIN_TOKEN, BDR_KOPIA_PASSWORD…)
# Purga completa:
sudo rm -rf /opt/bdr/repo /var/lib/bdr /etc/bdr
sudo userdel bdr      # solo si el instalador lo creó (home=/var/lib/bdr)
```

### macOS (launchd)

```bash
sudo bash uninstall-control.sh                # conserva estado, secretos y repo
sudo bash uninstall-control.sh --purge        # borra TODO, INCLUIDOS LOS BACKUPS
```

Manual:

```bash
sudo launchctl bootout system/local.bdr.control
sudo rm -f /Library/LaunchDaemons/local.bdr.control.plist
sudo rm -rf /opt/bdr/bin /opt/bdr/agents
# Se CONSERVA por defecto:
#   /opt/bdr/repo                                 ← copias de seguridad
#   "/Library/Application Support/BDR/control"    ← estado (state.json, secret.key, pki)
#   /etc/bdr/bdr.env                              ← secretos
# Purga completa:
sudo rm -rf /opt/bdr/repo /etc/bdr "/Library/Application Support/BDR"
```

### Windows

```powershell
.\uninstall-control.ps1                # conserva C:\ProgramData\BDR
.\uninstall-control.ps1 -Purge         # borra también estado y secretos
```

Manual:

```powershell
Stop-Service bdr-control -Force
& sc.exe delete bdr-control

# Secretos que el instalador dejó como variables de máquina:
foreach ($k in "BDR_ADMIN_TOKEN","BDR_SECRET_KEY","BDR_KOPIA_PASSWORD") {
  [Environment]::SetEnvironmentVariable($k, $null, "Machine")
}

# Reglas de firewall (si renombraste los puertos, búscalas con "BDR*"):
Remove-NetFirewallRule -DisplayName "BDR Control","BDR Agentes mTLS" -ErrorAction SilentlyContinue

# Binarios (el directorio se comparte con bdr-agent):
Remove-Item "$env:ProgramFiles\BDR\bdr-control.exe","$env:ProgramFiles\BDR\kopia.exe" -Force
Remove-Item "$env:ProgramFiles\BDR\agents.json" -Force
Remove-Item "$env:ProgramFiles\BDR\agents" -Recurse -Force

# Estado y secretos (opcional; se CONSERVAN por defecto):
Remove-Item "$env:ProgramData\BDR\control" -Recurse -Force
Remove-Item "$env:ProgramData\BDR\bdr.env" -Force
```

---

## Docker / Compose

El despliegue en Compose no instala servicios en el host: todo vive en
contenedores y volúmenes.

```bash
cd deploy
docker compose -f docker-compose.prod.yml down      # para y elimina contenedores

# Los datos (estado, BD y REPOSITORIO KOPIA con las copias) están en volúmenes:
docker volume ls | grep bdr
#   bdr-control-data   ← estado del control + repositorio Kopia (/data/repo)
#   bdr-db-data        ← PostgreSQL (estado y auditoría)
#   bdr-db-backups     ← backups diarios de la BD
#   caddy-data / caddy-config ← certificados TLS

# Borrarlos TODOS (irreversible: borra las copias de seguridad):
docker compose -f docker-compose.prod.yml down -v

# O selectivamente:
docker volume rm deploy_bdr-control-data deploy_bdr-db-data
```

Después, desinstala los **agentes** de cada equipo protegido con los
procedimientos de arriba (el agente nunca corre dentro de Compose).

---

## Qué conserva cada modo

| Modo | Servicio | Binarios | Estado y secretos | Repositorio (backups) |
|---|---|---|---|---|
| por defecto | ✅ elimina | ✅ elimina | ⛔ conserva | ⛔ conserva |
| `--purge` / `-Purge` | ✅ elimina | ✅ elimina | ✅ elimina | nativo: ✅ elimina · externo (NAS/S3): ⛔ no se toca |

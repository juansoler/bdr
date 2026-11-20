# AGENTS.md — memoria de sesión

Este fichero lo leen automáticamente las sesiones de OpenCode que trabajan en
este repositorio. Define el contexto mínimo y el **procedimiento obligatorio**
para que cualquier corrección o feature llegue publicada.

## Contexto esencial

- **Repo canónico:** `github.com/juansoler/bdr` (rama `main`), trabajo local en
  `~/Documents/bdr`.
- El directorio `~/Documents/backupTool` es un **ARCHIVO histórico**: no
  trabajes ahí, no hagas commits ahí.
- Producto: `bdr-control` (binario único Go con panel Next.js embebido en
  `apps/server/internal/webui/out`) + `bdr-agent` multi-plataforma.
- Canal de releases: `latest.json` firmado con Ed25519. Los servidores
  **notifican** las actualizaciones en el panel (nunca auto-instalan).
- El workflow **Release** está protegido por el entorno `release`: cada
  ejecución requiere aprobación humana (Actions → *Review deployments*).
- `make-release.sh` exige `BDR_RELEASE_KEY` (secreto del entorno) y necesita
  el binario del panel ya exportado si tocaste `apps/web`.

## PROCEDIMIENTO OBLIGATORIO tras corregir un error o añadir una feature

1. **Verifica en verde:** `go build ./... && go test ./...`. Si tocaste el
   panel: `make web-build && go test ./apps/server/internal/webui/`.
2. **Versiona siempre que vaya a publicarse:** bump en
   `packages/version/version.go` + entrada en `CHANGELOG.md` (Keep a
   Changelog). Un cambio sin versionar no es publicable.
3. **Commit** (conventional commits) + **push a `main`**.
4. **Tag nuevo** `vX.Y.Z` + push del tag. **Nunca reutilices ni force-pushes
   un tag ya publicado**: confunde a los clientes y re-pide aprobación del
   entorno. Para arreglos sobre una versión publicada, sube a la siguiente
   patch.
5. **Avisa al usuario** de que debe aprobar la ejecución en
   https://github.com/juansoler/bdr/actions (*Review deployments → Approve*).
6. **Tras el verde**, cierra el círculo: la release debe tener los assets
   (`SHA256SUMS`, `latest.json`, `latest.json.sig`, bundles, instaladores) y
   el canal debe reflejar la versión nueva:
   `curl -sL https://github.com/juansoler/bdr/releases/latest/download/latest.json`
7. **Si el fallo afecta a una versión ya desplegada**, documenta primero el
   workaround inmediato para el usuario (comandos concretos) y después la
   release que lo corrige de raíz.
8. Si la corrección es del CI/infra de release y el release no depende de
   ella, dilo explícitamente al usuario (workflows CI y Release son
   independientes).

## Lecciones aprendidas — no repetir

- La matriz de `release.yml` debe fijar **GOOS/GOARCH explícitos** con
  `GOOS=$os GOARCH=$arch CGO_ENABLED=0 go build …` — sin eso todos los
  binarios compilan para el host aunque el `-o` nombre otra plataforma.
- En scripts con `set -e`: **nunca `var=$( [ x ] && echo y )`** — el estado 1
  del `&&` aborta el script en silencio (asignaciones heredan el estado de la
  sustitución). Usa `if/fi`. Este patrón mordió dos veces (workflow e
  instalador).
- `curl` contra URLs de releases de GitHub necesita **`-L`** (redirect a S3);
  sin `-L` descarga ficheros de 0 bytes y todo lo que verifique contra ellos
  falla en falso.
- `shasum -c` resuelve los nombres **tal y como figuran en SHA256SUMS**:
  descarga los ficheros con su nombre real, no con un alias.
- Los one-liners del panel son **dos pasos**: `curl … -o /tmp/bdr-install.sh &&
  sudo bash /tmp/bdr-install.sh`. **Nunca `bash <(curl …)` con sudo** (sudo
  cierra los descriptores extra → "Bad file descriptor") ni `| bash` con
  sudo (enmascara errores con `curl: (23)`). Sistemas sin sudo (Debian
  minimal, pve): ejecutar directamente como root.
- El instalador nativo debe: inicializar el repositorio Kopia una vez
  (idempotente), exportar `BDR_KOPIA_KIND`/`BDR_KOPIA_REPO_PATH`, y en la
  unidad pasar `--kopia-binary` y `--downloads-dir` (systemd/launchd no ven
  `/opt/bdr/bin` en el PATH).
- Layout `/opt/bdr/bin` + `/opt/bdr/agents` con **chown al usuario del
  servicio**: la auto-actualización renombra ficheros dentro del directorio
  del binario (no basta con permisos del fichero).
- Docker multi-stage: para cruzar artefactos entre fases usa `COPY --from`
  — un `cp` de `RUN` no ve el filesystem de otra fase.
- `govulncheck` en CI debe ir **fijado a versión** (no `@latest`): no
  reproducible y candidato a fallos esporádicos.
- **NUNCA publiques una versión menor que la última tag publicada**: antes
  de numerar, revisa `git tag --sort=-creatordate | head -3`. Si el fix va
  sobre v0.10.0, es v0.10.1 — no v0.9.x. Numerar a la baja deja el fix fuera
  del canal (la release "latest" es la última publicada) y los servidores
  actualizados no vuelven atrás.
- NetBird: el primer "NetBird IP" de `netbird status --detail` es el del
  **servidor de gestión** (primer peer listado), NO el de la máquina propia.
  La identidad propia está en `netbird status` (líneas `FQDN:` y
  `NetBird IP:`). Para `--public-url` usa el FQDN propio de NetBird
  (`<host>.netbird.cloud`) — lo resuelve el DNS de la malla y es estable.
- La instalación del agente Windows genera servicio con recuperación
  automática (SCM); la de Linux systemd y la de macOS launchd — no mezclar
  tareas programadas con servicios.

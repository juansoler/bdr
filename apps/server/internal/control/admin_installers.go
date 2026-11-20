// Instaladores preconfigurados tipo «Añadir equipo»: el panel genera un
// instalador con token de enrolamiento embebido; al ejecutarlo en el equipo
// destino, el binario se descarga del servidor, se auto-enrola y el agente
// queda conectado sin intervención del usuario.
package control

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/juansoler/bdr/apps/server/internal/auth"
	"github.com/juansoler/bdr/apps/server/internal/store"
	"github.com/juansoler/bdr/packages/contracts"
)

var installerOSArch = map[string]string{
	"linux-amd64":   "bdr-agent",
	"linux-arm64":   "bdr-agent",
	"linux-arm":     "bdr-agent",
	"windows-amd64": "bdr-agent.exe",
	"darwin-amd64":  "bdr-agent", // macOS Intel
	"darwin-arm64":  "bdr-agent", // macOS Apple Silicon (M1/M2/M3/…)
}

// handleDownloads sirve las descargas públicas: binarios del agente y
// instaladores preconfigurados (el token en la URL es el secreto).
func (s *Server) handleDownloads(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/api/v1/download/")
	parts := strings.Split(strings.Trim(rest, "/"), "/")
	switch {
	case len(parts) == 2 && parts[0] == "agent":
		s.downloadAgentBinary(w, r, parts[1])
	case len(parts) == 3 && parts[0] == "installer":
		s.downloadInstaller(w, r, parts[1], parts[2])
	default:
		s.problem(w, r, http.StatusNotFound, "E_NOT_FOUND", "Ruta de descarga no válida")
	}
}

func validOSArch(osarch string) (osName, arch string, ok bool) {
	switch osarch {
	case "linux-amd64":
		return "linux", "amd64", true
	case "linux-arm64":
		return "linux", "arm64", true
	case "linux-arm":
		return "linux", "arm", true
	case "windows-amd64":
		return "windows", "amd64", true
	case "darwin-amd64":
		return "darwin", "amd64", true
	case "darwin-arm64":
		return "darwin", "arm64", true
	}
	return "", "", false
}

// githubAgentURL construye la URL pública del binario de agente en la
// release de GitHub correspondiente a la versión de ESTE control: el
// fallback cuando el directorio de descargas local no puede servir el
// binario (repo público, misma versión garantizada).
func (s *Server) githubAgentURL(osarch string) string {
	name := "bdr-agent-" + osarch
	if strings.HasPrefix(osarch, "windows") {
		name += ".exe"
	}
	return "https://github.com/juansoler/bdr/releases/download/v" + s.Version + "/" + name
}

// downloadAgentBinary sirve el binario del agente desde el directorio de
// descargas (bdr-agent-{osarch}); lo publica el operador con
// scripts/build-agents.sh o colocando los binarios compilados.
// Si el binario local no está (directorio vacío tras una actualización,
// instalación a medias…), NO devolvemos 404 E_NO_BINARY: redirigimos a la
// release pública de la MISMA versión — los one-liners del panel nunca
// quedan bloqueados por el estado del directorio de descargas.
func (s *Server) downloadAgentBinary(w http.ResponseWriter, r *http.Request, osarch string) {
	if !allowMethod(w, r, http.MethodGet) {
		return
	}
	if _, _, ok := validOSArch(osarch); !ok {
		s.problem(w, r, http.StatusBadRequest, "E_VALIDATION", "Plataforma no soportada; usa linux-amd64, linux-arm64, windows-amd64, darwin-amd64 o darwin-arm64")
		return
	}
	if s.DownloadsDir == "" {
		http.Redirect(w, r, s.githubAgentURL(osarch), http.StatusFound)
		return
	}
	name := "bdr-agent-" + osarch
	if strings.HasPrefix(osarch, "windows") {
		name += ".exe"
	}
	path := filepath.Join(s.DownloadsDir, filepath.Base(name))
	b, err := os.ReadFile(path)
	if err != nil {
		// Auditoría del fallback: el control no puede servir el binario.
		s.audit(r, nil, "download.fallback", "agent_binary", osarch, "ok",
			map[string]string{"dir": s.DownloadsDir})
		http.Redirect(w, r, s.githubAgentURL(osarch), http.StatusFound)
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Disposition", `attachment; filename="`+filepath.Base(name)+`"`)
	_, _ = w.Write(b)
}

// validateEnrollToken busca un token de enrolamiento válido por su valor en
// claro (el instalador lo lleva embebido en la URL).
func (s *Server) validateEnrollToken(token string) (*store.EnrollmentToken, bool) {
	token = strings.TrimSpace(token)
	if token == "" {
		return nil, false
	}
	var t *store.EnrollmentToken
	_ = s.Store.View(context.Background(), func(st *store.State) error {
		t = st.TokenByHash(auth.Hash(token))
		return nil
	})
	if t == nil {
		return nil, false
	}
	now := s.Now().UTC()
	if t.RevokedAt != nil || now.After(t.ExpiresAt) {
		return nil, false
	}
	return t, true
}

// downloadInstaller genera el instalador (bash o PowerShell) con la URL del
// servidor y el token embebidos. Público: el token en la URL ES el secreto.
func (s *Server) downloadInstaller(w http.ResponseWriter, r *http.Request, osarch, token string) {
	if !allowMethod(w, r, http.MethodGet) {
		return
	}
	osName, _, ok := validOSArch(osarch)
	if !ok {
		s.problem(w, r, http.StatusBadRequest, "E_VALIDATION", "Plataforma no soportada")
		return
	}
	// La URL del instalador Windows lleva extensión .ps1: se elimina antes de
	// validar el token.
	if osName == "windows" && strings.HasSuffix(token, ".ps1") {
		token = strings.TrimSuffix(token, ".ps1")
	}
	if _, valid := s.validateEnrollToken(token); !valid {
		s.problem(w, r, http.StatusNotFound, "E_TOKEN_INVALID",
			"Token de enrolamiento inexistente, caducado o revocado")
		return
	}
	serverURL := strings.TrimRight(s.publicURLFor(r), "/")
	var body, ctype string
	switch osName {
	case "windows":
		body = s.windowsInstallerScript(serverURL, osarch, token)
		ctype = "application/octet-stream"
	case "darwin":
		body = s.darwinInstallerScript(serverURL, osarch, token)
		ctype = "text/x-shellscript; charset=utf-8"
	default:
		body = s.linuxInstallerScript(serverURL, osarch, token)
		ctype = "text/x-shellscript; charset=utf-8"
	}
	w.Header().Set("Content-Type", ctype)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"bdr-installer-%s%s\"", osarch, map[string]string{"windows": ".ps1"}[osName]))
	_, _ = w.Write([]byte(body))
}

func (s *Server) publicURLFor(r *http.Request) string {
	if s.PublicURL != "" {
		return s.PublicURL
	}
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	return scheme + "://" + r.Host
}

// linuxInstallerScript genera el instalador bash: descarga el binario, lo
// registra (auto-enrol con el token embebido) e instala el servicio systemd
// si existe; sin systemd imprime el comando manual.
func (s *Server) linuxInstallerScript(serverURL, osarch, token string) string {
	return strings.ReplaceAll("#!/bin/sh\n" +
		"# Instalador generado por BDR Control — configura y conecta el agente sin intervención.\n" +
		"# El token de enrolamiento es de un solo uso: este instalador no es re-ejecutable.\n" +
		"set -e\n" +
		`SERVER="` + serverURL + `"` + "\n" +
		`TOKEN="` + token + `"` + "\n" +
		`OSARCH="` + osarch + `"` + "\n" +
		`BIN_DIR="${BDR_INSTALL_DIR:-/usr/local/bin}"` + "\n" +
		`DATA_DIR="${BDR_DATA_DIR:-/var/lib/bdr-agent}"` + "\n" +
		`
command -v curl >/dev/null 2>&1 || { echo "falta curl" >&2; exit 1; }
# Si hay un bdr-agent corriendo desde $BIN_DIR, hay que detenerlo ANTES de
# descargar: Linux prohíbe reescribir (ETXTBSY) el binario de un proceso en
# ejecución — y por eso el curl fallaba con (23) al escribir.
if command -v systemctl >/dev/null 2>&1 && systemctl is-active --quiet bdr-agent; then
  echo "deteniendo el bdr-agent en marcha para actualizarlo…" >&2
  systemctl stop bdr-agent
fi
mkdir -p "$BIN_DIR" "$DATA_DIR"
if curl -fsSL "$SERVER/api/v1/download/agent/$OSARCH" -o "$BIN_DIR/bdr-agent"; then
  :
else
  echo "aviso: el control no sirvió el binario — descargando de la release pública…" >&2
  curl -fsSL "https://github.com/juansoler/bdr/releases/download/v{VER}/bdr-agent-$OSARCH" -o "$BIN_DIR/bdr-agent" || { echo "no se pudo descargar el binario ni del control ni de GitHub (¿disco lleno? ¿permisos de $BIN_DIR?)" >&2; exit 1; }
fi
chmod 0755 "$BIN_DIR/bdr-agent"
"$BIN_DIR/bdr-agent" register --server "$SERVER" --token "$TOKEN" --data-dir "$DATA_DIR" || { echo "el enrolamiento falló — revisa el token y la conexión" >&2; exit 1; }
if command -v systemctl >/dev/null 2>&1 && [ -z "${BDR_NO_SERVICE:-}" ]; then
cat > /etc/systemd/system/bdr-agent.service <<UNIT
[Unit]
Description=BDR Agent
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
ExecStart=$BIN_DIR/bdr-agent run --data-dir $DATA_DIR
Restart=always
RestartSec=15

[Install]
WantedBy=multi-user.target
UNIT
  systemctl daemon-reload
  # Re-instalación sobre un agente vivo: register ya reescribió la config
  # en el data-dir; sin el restart el proceso seguiría con la config vieja
  # (heartbeats al control antiguo) y el panel no lo vería nunca online.
  if systemctl is-active --quiet bdr-agent; then
    systemctl restart bdr-agent
  else
    systemctl enable --now bdr-agent
  fi
  echo "servicio bdr-agent instalado y en marcha"
else
  echo "systemd no disponible; arranca el agente con:"
  echo "  $BIN_DIR/bdr-agent run --data-dir $DATA_DIR"
fi
`, "{VER}", s.Version)
}

// windowsInstallerScript genera el instalador PowerShell: descarga el exe,
// lo registra y crea el servicio de Windows.
func (s *Server) windowsInstallerScript(serverURL, osarch, token string) string {
	return "# Instalador generado por BDR Control — conecta el agente sin intervención.\r\n" +
		"$ErrorActionPreference = \"Stop\"\r\n" +
		`$Server = "` + serverURL + `"` + "\r\n" +
		`$Token = "` + token + `"` + "\r\n" +
		`$OsArch = "` + osarch + `"` + "\r\n" +
		`$Dir = "$env:ProgramFiles\BDR"` + "\r\n" +
		`$DataDir = "$env:ProgramData\BDR"` + "\r\n" +
		`
New-Item -ItemType Directory -Force -Path $Dir, $DataDir | Out-Null
Invoke-WebRequest -Uri "$Server/api/v1/download/agent/$OsArch" -OutFile "$Dir\bdr-agent.exe" -UseBasicParsing
& "$Dir\bdr-agent.exe" register --server $Server --token $Token --data-dir $DataDir
if (-not (Get-Service bdr-agent -ErrorAction SilentlyContinue)) {
` +
		// PowerShell escapa comillas con backtick; fuera del raw string de Go.
		"  New-Service -Name bdr-agent -DisplayName \"BDR Agent\" -BinaryPathName \"`\"$Dir\\bdr-agent.exe`\" run --data-dir `\"$DataDir`\"\" -StartupType Automatic | Out-Null\r\n" +
		`}
# Recuperación automática: si el proceso muere (p. ej. tras una
# autoactualización) el SCM lo reinicia con el binario nuevo.
sc.exe failure bdr-agent reset= 86400 actions= restart/5000/restart/10000/restart/300000 | Out-Null
sc.exe description bdr-agent "Agente BDR: heartbeat, copias de seguridad y restauración" | Out-Null
try {
  Start-Service bdr-agent -ErrorAction Stop
  Write-Host "agente BDR instalado y conectado a $Server"
} catch {
  Write-Warning "el servicio no arrancó: $_"
  Write-Host "intenta manualmente: Start-Service bdr-agent"
}
`
}

// darwinInstallerScript genera el instalador bash para macOS: descarga el
// binario, lo registra (auto-enrol con el token embebido) e instala el
// servicio launchd (/Library/LaunchDaemons) para que arranque en el boot.
func (s *Server) darwinInstallerScript(serverURL, osarch, token string) string {
	return strings.ReplaceAll("#!/bin/sh\n" +
		"# Instalador generado por BDR Control — configura y conecta el agente sin intervención.\n" +
		"# El token de enrolamiento es de un solo uso: este instalador no es re-ejecutable.\n" +
		"# Requiere sudo (macOS Intel darwin-amd64 o Apple Silicon darwin-arm64).\n" +
		"set -e\n" +
		`SERVER="` + serverURL + `"` + "\n" +
		`TOKEN="` + token + `"` + "\n" +
		`OSARCH="` + osarch + `"` + "\n" +
		`BIN_DIR="${BDR_INSTALL_DIR:-/usr/local/bin}"` + "\n" +
		`DATA_DIR="${BDR_DATA_DIR:-/Library/Application Support/BDR}"` + "\n" +
		`
command -v curl >/dev/null 2>&1 || { echo "falta curl" >&2; exit 1; }
# Si hay un bdr-agent corriendo desde $BIN_DIR, hay que detenerlo ANTES de
# descargar: Linux prohíbe reescribir (ETXTBSY) el binario de un proceso en
# ejecución — y por eso el curl fallaba con (23) al escribir.
if command -v systemctl >/dev/null 2>&1 && systemctl is-active --quiet bdr-agent; then
  echo "deteniendo el bdr-agent en marcha para actualizarlo…" >&2
  systemctl stop bdr-agent
fi
mkdir -p "$BIN_DIR" "$DATA_DIR"
if curl -fsSL "$SERVER/api/v1/download/agent/$OSARCH" -o "$BIN_DIR/bdr-agent"; then
  :
else
  echo "aviso: el control no sirvió el binario — descargando de la release pública…" >&2
  curl -fsSL "https://github.com/juansoler/bdr/releases/download/v{VER}/bdr-agent-$OSARCH" -o "$BIN_DIR/bdr-agent" || { echo "no se pudo descargar el binario ni del control ni de GitHub (¿disco lleno? ¿permisos de $BIN_DIR?)" >&2; exit 1; }
fi
chmod 0755 "$BIN_DIR/bdr-agent"
"$BIN_DIR/bdr-agent" register --server "$SERVER" --token "$TOKEN" --data-dir "$DATA_DIR" || { echo "el enrolamiento falló — revisa el token y la conexión" >&2; exit 1; }
if [ -z "${BDR_NO_SERVICE:-}" ]; then
cat > /Library/LaunchDaemons/local.bdr.agent.plist <<PLIST
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key><string>local.bdr.agent</string>
  <key>ProgramArguments</key>
  <array>
    <string>$BIN_DIR/bdr-agent</string>
    <string>run</string>
    <string>--data-dir</string>
    <string>$DATA_DIR</string>
  </array>
  <key>RunAtLoad</key><true/>
  <key>KeepAlive</key><true/>
  <key>StandardOutPath</key><string>$DATA_DIR/agent.log</string>
  <key>StandardErrorPath</key><string>$DATA_DIR/agent.log</string>
</dict>
</plist>
PLIST
  chown root:wheel /Library/LaunchDaemons/local.bdr.agent.plist
  chmod 0644 /Library/LaunchDaemons/local.bdr.agent.plist
  launchctl bootout system/local.bdr.agent 2>/dev/null || true
  launchctl bootstrap system /Library/LaunchDaemons/local.bdr.agent.plist \
    || launchctl load -w /Library/LaunchDaemons/local.bdr.agent.plist
  echo "servicio bdr-agent instalado y en marcha (launchd)"
else
  echo "arranca el agente con:"
  echo "  $BIN_DIR/bdr-agent run --data-dir \"$DATA_DIR\""
fi
`, "{VER}", s.Version)
}

// handleInstallers crea instaladores preconfigurados («Añadir equipo»).
// POST /admin/installers — genera token de un solo uso + URLs.
func (s *Server) handleInstallers(w http.ResponseWriter, r *http.Request) {
	if !allowMethod(w, r, http.MethodPost) {
		return
	}
	r, a, ok := s.authorize(w, r, "operate")
	if !ok {
		return
	}
	s.withIdempotency(w, r, a, func() (int, any) {
		var req contracts.InstallerRequest
		if !decodeJSON(w, r, &req, maxBodyAdmin) {
			return 0, nil
		}
		osName, arch, ok := validOSArch(strings.ToLower(strings.TrimSpace(req.OS)) + "-" + strings.ToLower(strings.TrimSpace(req.Arch)))
		if !ok {
			s.problem(w, r, http.StatusBadRequest, "E_VALIDATION", "os/arch no soportados; usa linux|windows|darwin con amd64|arm64")
			return 0, nil
		}
		osarch := osName + "-" + arch
		ttl := time.Hour
		if req.ExpiresInSeconds > 0 {
			ttl = time.Duration(req.ExpiresInSeconds) * time.Second
		}
		if ttl > maxTokenTTL {
			ttl = maxTokenTTL
		}
		if ttl < time.Minute {
			ttl = time.Minute
		}
		token, err := auth.NewSecret(32)
		if err != nil {
			return 0, errInternal{err}
		}
		id, err := newID()
		if err != nil {
			return 0, errInternal{err}
		}
		now := s.Now().UTC()
		rec := store.EnrollmentToken{
			ID: id, TokenHash: auth.Hash(token),
			ExpectedHostname: strings.TrimSpace(req.ExpectedHostname),
			CreatedAt:        now, ExpiresAt: now.Add(ttl),
			Note: strings.TrimSpace(req.Note), CreatedBy: a.auditName(),
		}
		if err := s.Store.Update(r.Context(), func(st *store.State) error {
			st.Tokens[id] = &rec
			return nil
		}); err != nil {
			return 0, errInternal{err}
		}
		s.audit(r, a, "installer.created", "enrollment_token", id, "ok",
			map[string]string{"os": osName, "arch": arch})

		base := strings.TrimRight(s.publicURLFor(r), "/")
		instURL := base + "/api/v1/download/installer/" + osarch + "/" + token
		binURL := base + "/api/v1/download/agent/" + osarch
		ext := ""
		one := ""
		switch osName {
		case "windows":
			ext = ".ps1"
			one = "irm " + instURL + ext + " | iex"
			instURL += ext
		case "darwin":
			// Dos pasos: sudo cierra los descriptores extra, así que la
			// sustitución de proceso (bash <(curl …)) falla con "Bad file
			// descriptor". Descargar a fichero y ejecutar después funciona
			// en macOS, Linux con sudo y Linux sin sudo.
			one = "curl -fsSL " + instURL + " -o /tmp/bdr-install.sh && sudo bash /tmp/bdr-install.sh"
		default:
			one = "curl -fsSL " + instURL + " -o /tmp/bdr-install.sh && sudo bash /tmp/bdr-install.sh"
		}
		return http.StatusCreated, contracts.InstallerResponse{
			OS: osName, Arch: arch, Token: token, ExpiresAt: rec.ExpiresAt,
			InstallerURL: instURL, InstallerPath: "/api/v1/download/installer/" + osarch + "/" + token + ext,
			BinaryURL: binURL, OneLiner: one,
			AgentFilename: installerOSArch[osarch],
		}
	})
}

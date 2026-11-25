package control_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/juansoler/bdr/apps/server/internal/control"
	"github.com/juansoler/bdr/apps/server/internal/store"
	"github.com/juansoler/bdr/packages/contracts"
	"github.com/juansoler/bdr/packages/logging"
)

// newInstallersTestAPI monta la API con caja de secretos y directorio de
// descargas propio.
func newInstallersTestAPI(t *testing.T, downloadsDir string) *httptest.Server {
	t.Helper()
	api := control.New(store.NewMemory(), adminToken, logging.NewWithWriter(io.Discard, "error"), "test")
	api.Box = testBox(t)
	api.DownloadsDir = downloadsDir
	ts := httptest.NewServer(api.Handler())
	t.Cleanup(ts.Close)
	return ts
}

// TestInstallersFlow: POST /admin/installers genera token de un solo uso y
// URLs; el instalador descargado lleva la URL del servidor y el token
// embebidos; la descarga del binario sale del directorio configurado.
func TestInstallersFlow(t *testing.T) {
	dir := t.TempDir()
	ts := newInstallersTestAPI(t, dir)

	status, body := doJSON(t, http.MethodPost, ts.URL+"/api/v1/admin/installers", adminToken,
		contracts.InstallerRequest{OS: "linux", Arch: "amd64", ExpiresInSeconds: 600, Note: "installer"})
	if status != http.StatusCreated {
		t.Fatalf("crear instalador: %d %s", status, body)
	}
	var inst contracts.InstallerResponse
	if err := json.Unmarshal(body, &inst); err != nil {
		t.Fatalf("json: %v", err)
	}
	if inst.Token == "" || inst.InstallerPath == "" || !strings.Contains(inst.OneLiner, "bash") {
		t.Fatalf("respuesta incompleta: %+v", inst)
	}

	// Descarga del instalador con token válido: script con server y token.
	status, body = doJSON(t, http.MethodGet, ts.URL+inst.InstallerPath, "", nil)
	if status != http.StatusOK {
		t.Fatalf("descargar instalador: %d %s", status, body)
	}
	if !strings.Contains(string(body), "register --server") || !strings.Contains(string(body), inst.Token) {
		t.Fatal("el instalador no incluye el registro ni el token")
	}

	// Token inválido → 404.
	status, body = doJSON(t, http.MethodGet, ts.URL+"/api/v1/download/installer/linux-amd64/token-inventado", "", nil)
	if status != http.StatusNotFound || !strings.Contains(string(body), "E_TOKEN_INVALID") {
		t.Fatalf("token inválido: esperaba 404, got %d %s", status, body)
	}

	// Plataforma no soportada → 400.
	status, _ = doJSON(t, http.MethodPost, ts.URL+"/api/v1/admin/installers", adminToken,
		contracts.InstallerRequest{OS: "solaris", Arch: "sparc"})
	if status != http.StatusBadRequest {
		t.Fatalf("os inválido: %d", status)
	}

	// Windows: URL con extensión .ps1 y contenido PowerShell.
	status, body = doJSON(t, http.MethodPost, ts.URL+"/api/v1/admin/installers", adminToken,
		contracts.InstallerRequest{OS: "windows", Arch: "amd64"})
	if status != http.StatusCreated {
		t.Fatalf("instalador windows: %d %s", status, body)
	}
	var instW contracts.InstallerResponse
	_ = json.Unmarshal(body, &instW)
	if !strings.HasSuffix(instW.InstallerURL, ".ps1") {
		t.Fatalf("instalador windows sin .ps1: %s", instW.InstallerURL)
	}
	status, body = doJSON(t, http.MethodGet, ts.URL+instW.InstallerPath, "", nil)
	if status != http.StatusOK || !strings.Contains(string(body), "Invoke-WebRequest") {
		t.Fatalf("script windows: %d", status)
	}

	// macOS Intel: instalador bash con launchd.
	status, body = doJSON(t, http.MethodPost, ts.URL+"/api/v1/admin/installers", adminToken,
		contracts.InstallerRequest{OS: "darwin", Arch: "amd64"})
	if status != http.StatusCreated {
		t.Fatalf("instalador darwin-amd64: %d %s", status, body)
	}
	var instM contracts.InstallerResponse
	_ = json.Unmarshal(body, &instM)
	if instM.OS != "darwin" || instM.Arch != "amd64" || instM.AgentFilename != "bdr-agent" {
		t.Fatalf("respuesta darwin-amd64 incompleta: %+v", instM)
	}
	status, body = doJSON(t, http.MethodGet, ts.URL+instM.InstallerPath, "", nil)
	if status != http.StatusOK {
		t.Fatalf("descargar instalador darwin-amd64: %d %s", status, body)
	}
	if !strings.Contains(string(body), "LaunchDaemons") || !strings.Contains(string(body), "darwin-amd64") {
		t.Fatal("instalador darwin sin launchd o con arquitectura incorrecta")
	}

	// macOS Apple Silicon: mismo flujo con darwin-arm64.
	status, body = doJSON(t, http.MethodPost, ts.URL+"/api/v1/admin/installers", adminToken,
		contracts.InstallerRequest{OS: "darwin", Arch: "arm64"})
	if status != http.StatusCreated {
		t.Fatalf("instalador darwin-arm64: %d %s", status, body)
	}
	var instM2 contracts.InstallerResponse
	_ = json.Unmarshal(body, &instM2)
	status, body = doJSON(t, http.MethodGet, ts.URL+instM2.InstallerPath, "", nil)
	if status != http.StatusOK || !strings.Contains(string(body), "darwin-arm64") {
		t.Fatalf("instalador darwin-arm64: %d %s", status, body)
	}

	// El token de instalador es de un solo uso y aparece en la lista de
	// enrolamiento (para el panel).
	status, body = doJSON(t, http.MethodGet, ts.URL+"/api/v1/admin/enrollment-tokens", adminToken, nil)
	if status != http.StatusOK || !strings.Contains(string(body), "installer") {
		t.Fatalf("token de instalador no listado: %d", status)
	}
}

// TestInstallerReenrollPrompt: el instalador de alta, al detectar una config
// existente, pregunta interactivamente si se quiere olvidar el servidor
// anterior y enrolar el equipo en este, borrando también la identidad mTLS
// (register solo la reescribe si el nuevo servidor emite certificado). Sin
// terminal interactiva aborta como antes y BDR_FORCE_REENROLL=1 sigue
// apareciendo como vía de fuerza. El instalador de RECONEXIÓN no pregunta:
// su propósito es reenlazar la identidad existente.
func TestInstallerReenrollPrompt(t *testing.T) {
	dir := t.TempDir()
	ts := newInstallersTestAPI(t, dir)

	download := func(osName, arch string) string {
		t.Helper()
		status, body := doJSON(t, http.MethodPost, ts.URL+"/api/v1/admin/installers", adminToken,
			contracts.InstallerRequest{OS: osName, Arch: arch})
		if status != http.StatusCreated {
			t.Fatalf("crear instalador %s/%s: %d %s", osName, arch, status, body)
		}
		var inst contracts.InstallerResponse
		_ = json.Unmarshal(body, &inst)
		status, body = doJSON(t, http.MethodGet, ts.URL+inst.InstallerPath, "", nil)
		if status != http.StatusOK {
			t.Fatalf("descargar instalador %s/%s: %d %s", osName, arch, status, body)
		}
		return string(body)
	}

	// Linux y macOS: prompt + borrado de la identidad mTLS + aborto seguro
	// sin terminal. (El guard va duplicado en los dos generadores bash.)
	for _, osarch := range [][2]string{{"linux", "amd64"}, {"darwin", "arm64"}} {
		script := download(osarch[0], osarch[1])
		for _, want := range []string{
			"olvidar el servidor antiguo",
			`rm -f "$DATA_DIR/agent.json" "$DATA_DIR/agent.key" "$DATA_DIR/agent.crt" "$DATA_DIR/ca.crt"`,
			"entrada no interactiva",
			"BDR_FORCE_REENROLL",
		} {
			if !strings.Contains(script, want) {
				t.Fatalf("instalador %s/%s sin %q", osarch[0], osarch[1], want)
			}
		}
	}

	// Windows: mismo prompt en PowerShell.
	windows := download("windows", "amd64")
	for _, want := range []string{"Olvidar el servidor antiguo", "agent.key", "IsInputRedirected"} {
		if !strings.Contains(windows, want) {
			t.Fatalf("instalador windows sin %q", want)
		}
	}

	// Reconexión (token ligado a un agente): sin prompt — reenlaza la
	// identidad existente en lugar de olvidarla.
	enrolled := enrollAgent(t, ts, createToken(t, ts, "lab-re").Token, "lab-re")
	status, body := doJSON(t, http.MethodPost, ts.URL+"/api/v1/admin/agents/"+enrolled.AgentID+"/reconnect-token", adminToken, nil)
	if status != http.StatusCreated {
		t.Fatalf("reconnect-token: %d %s", status, body)
	}
	var rt contracts.ReconnectTokenResponse
	_ = json.Unmarshal(body, &rt)
	status, body = doJSON(t, http.MethodGet, ts.URL+"/api/v1/download/installer/linux-amd64/"+rt.Token, "", nil)
	if status != http.StatusOK {
		t.Fatalf("installer reconnect: %d %s", status, body)
	}
	if strings.Contains(string(body), "olvidar el servidor antiguo") {
		t.Fatal("installer de reconexión con prompt de olvido: reenlaza, no olvida")
	}
}

// TestAgentBinaryDownload: el binario se sirve desde DownloadsDir; si falta,
// 302 a la release pública (nunca un 404 que rompa los one-liners).
func TestAgentBinaryDownload(t *testing.T) {
	dir := t.TempDir()
	ts := newInstallersTestAPI(t, dir)

	// Cliente que NO sigue redirects: verificamos el 302 y el destino.
	nofollow := &http.Client{CheckRedirect: func(req *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	resp, err := nofollow.Get(ts.URL + "/api/v1/download/agent/linux-amd64")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("sin binario: esperaba 302 (redirect a la release pública), got %d", resp.StatusCode)
	}
	if loc := resp.Header.Get("Location"); !strings.Contains(loc, "github.com/juansoler/bdr/releases/download/") || !strings.Contains(loc, "bdr-agent-linux-amd64") {
		t.Fatalf("Location inesperado: %q", loc)
	}
	if err := os.WriteFile(filepath.Join(dir, "bdr-agent-linux-amd64"), []byte("BINARIO-FAKE"), 0o755); err != nil {
		t.Fatal(err)
	}
	status, body := doJSON(t, http.MethodGet, ts.URL+"/api/v1/download/agent/linux-amd64", "", nil)
	if status != http.StatusOK || !strings.Contains(string(body), "BINARIO-FAKE") {
		t.Fatalf("con binario: %d %s", status, body)
	}

	status, _ = doJSON(t, http.MethodGet, ts.URL+"/api/v1/download/agent/plan9-9k1", "", nil)
	if status != http.StatusBadRequest {
		t.Fatalf("plataforma inválida: %d", status)
	}
}

// Autoactualización de agentes: cuando el binario publicado en el directorio
// de descargas no coincide con la versión que reporta el agente, el
// heartbeat responde con la instrucción de actualización (URL + SHA-256).
// El agente decide cuándo aplicarla (sin jobs en curso).
package control

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/juansoler/bdr/packages/contracts"
	"github.com/juansoler/bdr/packages/version"
)

// agentOSArch deriva el artefacto de descarga del inventario reportado
// (runtime.GOOS/GOARCH del agente).
func agentOSArch(osFamily, arch string) string {
	switch strings.ToLower(strings.TrimSpace(osFamily)) + "/" + strings.ToLower(strings.TrimSpace(arch)) {
	case "windows/amd64", "windows/x86_64":
		return "windows-amd64"
	case "linux/amd64", "linux/x86_64":
		return "linux-amd64"
	case "linux/arm64", "linux/aarch64":
		return "linux-arm64"
	case "linux/arm", "linux/armv7l":
		return "linux-arm"
	case "darwin/amd64", "darwin/x86_64":
		return "darwin-amd64"
	case "darwin/arm64", "darwin/aarch64":
		return "darwin-arm64"
	}
	return ""
}

// shaCache guarda el SHA-256 de los binarios publicados, invalidado por
// (tamaño, mtime): el operador reemplaza el fichero y la suma se recalcula.
type shaCache struct {
	mu    sync.Mutex
	sums  map[string]string
	stamp map[string]string
}

func newSHACache() *shaCache {
	return &shaCache{sums: map[string]string{}, stamp: map[string]string{}}
}

func (c *shaCache) fileSHA(path string) (string, bool) {
	st, err := os.Stat(path)
	if err != nil || st.IsDir() {
		return "", false
	}
	stamp := fmt.Sprintf("%d:%d", st.Size(), st.ModTime().UnixNano())
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.stamp[path] == stamp {
		return c.sums[path], true
	}
	f, err := os.Open(path)
	if err != nil {
		return "", false
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", false
	}
	sum := hex.EncodeToString(h.Sum(nil))
	c.stamp[path] = stamp
	c.sums[path] = sum
	return sum, true
}

// agentBinaryPath devuelve la ruta del binario del agente para un osarch.
func (s *Server) agentBinaryPath(osarch string) string {
	if s.DownloadsDir == "" {
		return ""
	}
	name, ok := installerOSArch[osarch]
	if !ok {
		return ""
	}
	// Los binarios se publican como bdr-agent-{osarch}[.exe] o con el nombre
	// corto; probamos ambos.
	candidates := []string{
		filepath.Join(s.DownloadsDir, "bdr-agent-"+osarch+extFor(osarch)),
		filepath.Join(s.DownloadsDir, name),
	}
	for _, p := range candidates {
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p
		}
	}
	return ""
}

func extFor(osarch string) string {
	if strings.HasPrefix(osarch, "windows") {
		return ".exe"
	}
	return ""
}

// structureMinVersion es la versión mínima del agente para ejecutar
// backups de VM/LXC/imagen: a partir de 0.16.29 los segmentos se guardan
// con la estructura backup/<máquina>/<fecha>/ (0.16.31 añadió el nivel de
// nodo Proxmox). Agentes anteriores los guardarían sin estructura.
const structureMinVersion = "0.16.29"

// jobRequiresStructure indica si el tipo de job exige el layout nuevo.
func jobRequiresStructure(jobType string) bool {
	switch jobType {
	case contracts.JobTypeBackupVM, contracts.JobTypeBackupLXC, contracts.JobTypeBackupImage:
		return true
	}
	return false
}

// agentUpgradeOffer construye la instrucción de actualización para un agente
// cuya versión difiere de la del plano de control.
//
// La URL prefiere la del listener público (--public-url): los agentes ya
// desplegados descargan con un cliente sin TLS personalizado, y por HTTP
// claro no necesitan la CA del control. La integridad no depende del
// transporte: la oferta viaja por el canal mTLS autenticado y trae el
// SHA-256 del binario. Si no hay --public-url, la URL es RELATIVA y el
// agente (0.11.10+) la resuelve contra su listener mTLS con la CA embebida.
//
// El SHA-256 se toma de agents.json (junto al binario del control): el
// registro de los binarios de agente de ESTA versión del control, escrito en
// el release, en el build de Docker y por el propio self-update. La oferta
// solo se emite si el fichero servido coincide con ese SHA — así nunca se
// ofrece un binario desincronizado de la versión del control (bucle).
func (s *Server) agentUpgradeOffer(osFamily, arch, reported string) *contracts.AgentUpgrade {
	if reported == version.Version {
		return nil
	}
	osarch := agentOSArch(osFamily, arch)
	if osarch == "" {
		return nil
	}
	offerURL := func() string {
		u := "/api/v1/download/agent/" + osarch
		if s.PublicURL != "" {
			u = strings.TrimRight(s.PublicURL, "/") + u
		}
		return u
	}

	// 1) Con agents.json: oferta con SHA del manifiesto (coherente con esta
	//    versión del control), servido por el propio control.
	if sum := s.agentsManifestSHA(osarch); sum != "" {
		path := s.agentBinaryPath(osarch)
		if path == "" {
			return nil
		}
		local, ok := s.binarySHA.fileSHA(path)
		if !ok || !strings.EqualFold(local, sum) {
			// El fichero servido no coincide con la versión del control:
			// no ofrecer (evita el bucle de descarga de binarios viejos).
			return nil
		}
		return &contracts.AgentUpgrade{
			Version: version.Version,
			URL:     offerURL(),
			SHA256:  sum,
		}
	}

	// 2) Sin agents.json (instalaciones previas a 0.9.5): oferta desde el
	//    canal firmado (último manifiesto verificado). El SHA del binario
	//    servido debe coincidir con el del canal — sin bucles aunque el
	//    manifiesto local no exista.
	if m := updateState.lastManifest.Load(); m != nil && m.Version == version.Version {
		if ag, ok := m.Agents[osarch]; ok && len(ag.SHA256) == 64 {
			path := s.agentBinaryPath(osarch)
			if path != "" {
				if local, ok := s.binarySHA.fileSHA(path); ok && strings.EqualFold(local, ag.SHA256) {
					return &contracts.AgentUpgrade{
						Version: version.Version,
						URL:     offerURL(),
						SHA256:  ag.SHA256,
					}
				}
			}
		}
	}

	// 3) Último recurso: la URL local solo si el control conoce su propia
	//    dirección pública.
	if s.PublicURL == "" || s.DownloadsDir == "" {
		return nil
	}
	path := s.agentBinaryPath(osarch)
	if path == "" {
		return nil
	}
	sum, ok := s.binarySHA.fileSHA(path)
	if !ok {
		return nil
	}
	return &contracts.AgentUpgrade{
		Version: version.Version,
		URL:     strings.TrimRight(s.PublicURL, "/") + "/api/v1/download/agent/" + osarch,
		SHA256:  sum,
	}
}

// agentsManifestPath devuelve la ruta del manifiesto de SHAs de agentes
// (agents.json) junto al binario del control en marcha.
func (s *Server) agentsManifestPath() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	return filepath.Join(filepath.Dir(exe), "agents.json")
}

// agentsManifestSHA devuelve el SHA-256 registrado para una plataforma
// (cadena vacía si no hay manifiesto o la plataforma no figura).
func (s *Server) agentsManifestSHA(osarch string) string {
	data, err := os.ReadFile(s.agentsManifestPath())
	if err != nil {
		return ""
	}
	var m map[string]string
	if err := json.Unmarshal(data, &m); err != nil {
		return ""
	}
	return strings.ToLower(strings.TrimSpace(m[osarch]))
}

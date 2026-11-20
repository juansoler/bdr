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

// agentUpgradeOffer construye la instrucción de actualización para un agente
// cuya versión difiere de la del plano de control.
//
// La URL es RELATIVA: el agente la resuelve contra el servidor con el que ya
// hace heartbeat (NetBird, LAN…) — sin depender de --public-url.
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
			URL:     "/api/v1/download/agent/" + osarch,
			SHA256:  sum,
		}
	}

	// 2) Sin agents.json (instalaciones previas, Docker): la URL local solo
	//    si el control conoce su propia dirección pública.
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

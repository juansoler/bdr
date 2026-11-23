// Package selfupdate verifica y aplica la actualización del propio
// bdr-control desde el canal de releases firmado.
//
// Flujo (modo notificación): un bucle en el plano de control consulta
// latest.json (URL fija de la release), verifica la firma Ed25519 con la
// clave pública embebida y compara versiones. Si hay versión nueva, se
// registra en auditoría y el panel muestra una notificación; NADA se aplica
// sin que el administrador pulse «Actualizar ahora» (POST install), que
// descarga el bundle, verifica el SHA-256, sustituye el binario (con .old
// para rollback) y re-ejecuta el proceso in-place (Unix). En Windows la
// instalación queda fuera del binario: el administrador ejecuta el
// instalador firmado con checksum.
package selfupdate

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// ManifestURL es la URL por defecto del canal estable: la última release.
// Fijar el fichero concreto evita el límite de peticiones de la API de
// GitHub y hace el canal inspeccionable.
const ManifestURL = "https://github.com/juansoler/bdr/releases/latest/download/latest.json"

// Manifest es el contenido de latest.json.
type Manifest struct {
	Version    string           `json:"version"`
	Channel    string           `json:"channel"`
	ReleasedAt time.Time        `json:"released_at"`
	NotesURL   string           `json:"notes_url,omitempty"`
	Server     map[string]Asset `json:"server"`
	Agents     map[string]Asset `json:"agents,omitempty"`
	MinFrom    string           `json:"min_upgradable_from,omitempty"`
}

// Asset describe un artefacto descargable del release.
type Asset struct {
	URL    string `json:"url"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size,omitempty"`
}

// Check descarga y verifica el manifiesto del canal. La firma es Ed25519
// sobre los bytes exactos de latest.json; el fichero .sig contiene la firma
// en base64 estándar. Sin clave pública verificable no hay resultado.
func Check(ctx context.Context, client *http.Client, manifestURL, publicKeyBase64 string) (*Manifest, error) {
	if publicKeyBase64 == "" {
		return nil, fmt.Errorf("canal de actualizaciones sin clave pública embebida: deshabilitado")
	}
	pub, err := base64.StdEncoding.DecodeString(publicKeyBase64)
	if err != nil || len(pub) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("clave pública del canal inválida")
	}
	if client == nil {
		client = http.DefaultClient
	}
	raw, err := get(ctx, client, manifestURL)
	if err != nil {
		return nil, err
	}
	sig, err := get(ctx, client, manifestURL+".sig")
	if err != nil {
		return nil, err
	}
	sigB64 := strings.TrimSpace(string(sig))
	sigBytes, err := base64.StdEncoding.DecodeString(sigB64)
	if err != nil || len(sigBytes) != ed25519.SignatureSize {
		return nil, fmt.Errorf("firma del canal inválida")
	}
	if !ed25519.Verify(ed25519.PublicKey(pub), raw, sigBytes) {
		return nil, fmt.Errorf("la firma del manifiesto no corresponde a la clave pública embebida")
	}
	var m Manifest
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, fmt.Errorf("manifiesto inválido: %w", err)
	}
	if m.Version == "" || len(m.Server) == 0 {
		return nil, fmt.Errorf("manifiesto sin versión o sin artefactos de servidor")
	}
	return &m, nil
}

func get(ctx context.Context, c *http.Client, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("descarga de %s: HTTP %d", url, resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 4<<20))
}

// PlatformKey devuelve la clave de plataforma del host según los nombres del
// manifiesto (p. ej. "linux-amd64").
func PlatformKey() string {
	osArch := runtime.GOOS + "-" + runtime.GOARCH
	if runtime.GOOS == "windows" {
		return osArch
	}
	return osArch
}

// CompareVersion devuelve -1 si a<b, 0 si iguales y 1 si a>b, comparando
// versiones semver (con o sin "v", sufijos -dev/-rc tratados como
// anteriores a la versión final del mismo triple).
func CompareVersion(a, b string) int {
	na, sa := splitVer(a)
	nb, sb := splitVer(b)
	for i := 0; i < 3; i++ {
		if na[i] != nb[i] {
			if na[i] < nb[i] {
				return -1
			}
			return 1
		}
	}
	// Mismo triple: sin sufijo > con sufijo ("0.8.0" > "0.8.0-rc1").
	switch {
	case sa == "" && sb == "":
		return 0
	case sa == "":
		return 1
	case sb == "":
		return -1
	default:
		if sa < sb {
			return -1
		}
		if sa == sb {
			return 0
		}
		return 1
	}
}

// splitVer separa el triple numérico del sufijo de pre-release/dev.
func splitVer(v string) ([3]int, string) {
	var n [3]int
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	if i := strings.IndexAny(v, "-+"); i >= 0 {
		suffix := v[i:]
		if strings.HasPrefix(suffix, "+") {
			suffix = ""
		}
		v = v[:i]
		return n, suffix
	}
	parts := strings.Split(v, ".")
	for i := 0; i < 3 && i < len(parts); i++ {
		n[i], _ = strconv.Atoi(parts[i])
	}
	return n, ""
}

// Newer devuelve el asset de servidor para esta plataforma si el manifiesto
// anuncia una versión posterior a current; nil si no hay nada que aplicar.
func (m *Manifest) Newer(current string) (*Asset, bool) {
	if CompareVersion(m.Version, current) <= 0 {
		return nil, false
	}
	asset, ok := m.Server[PlatformKey()]
	return &asset, ok
}

// Apply descarga el bundle de servidor, verifica el SHA-256, extrae los
// binarios y sustituye el ejecutable en marcha (con .old para rollback).
// Devuelve la ruta del binario nuevo ya en su sitio; el llamante debe
// re-ejecutar (restartFn) o salir para que el gestor de servicios reinicie.
// Solo aplica a la plataforma del host.
//
// agentsDir: si no es vacío, los binarios de agentes del manifiesto se
// refrescan allí ANTES de tocar el binario en marcha — así los agentes que
// se enrolen después reciben la versión nueva y no entra en bucle la oferta
// de actualización. El swap del binario es lo último: si la descarga de
// agentes falla, se devuelve error sin haber alterado nada.
func Apply(ctx context.Context, client *http.Client, manifest *Manifest, exePath string, agentsDir string) (string, error) {
	if runtime.GOOS == "windows" {
		return "", fmt.Errorf("la actualización en Windows se aplica ejecutando el instalador de la release (el binario en ejecución no puede reemplazarse a sí mismo)")
	}
	if client == nil {
		client = http.DefaultClient
	}
	asset, ok := manifest.Server[PlatformKey()]
	if !ok {
		return "", fmt.Errorf("el manifiesto no trae bundle de servidor para %s", PlatformKey())
	}

	// Los temporales van junto al binario: el propio self-update ya renombra
	// ficheros en ese directorio, así que es escribible, y /tmp puede ser de
	// solo lectura en despliegues endurecidos (ProtectSystem=strict,
	// contenedores con root read-only). Si tampoco se puede escribir ahí,
	// cae al /tmp del sistema.
	base := writableTempDir(exePath)
	if base == "" {
		return "", fmt.Errorf("no hay directorio temporal escribible (ni junto al binario ni /tmp)")
	}

	tmp, err := os.CreateTemp(base, "bdr-bundle-*.tar.gz")
	if err != nil {
		return "", err
	}
	defer os.Remove(tmp.Name())
	sum, err := download(ctx, client, asset.URL, tmp)
	if err != nil {
		tmp.Close()
		return "", err
	}
	if err := tmp.Close(); err != nil {
		return "", err
	}
	want := strings.ToLower(strings.TrimSpace(asset.SHA256))
	if want == "" || len(want) != 64 {
		return "", fmt.Errorf("el manifiesto no trae SHA-256 válido para %s", PlatformKey())
	}
	if !strings.EqualFold(sum, want) {
		return "", fmt.Errorf("el bundle descargado no coincide con el SHA-256 del manifiesto")
	}

	staging, err := os.MkdirTemp(base, "bdr-stage-*")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(staging)
	if err := extractBundle(tmp.Name(), staging); err != nil {
		return "", err
	}

	dir := filepath.Dir(exePath)

	// Agentes: se descargan y verifican ANTES de tocar el binario en
	// marcha. Si falla algo, se devuelve error sin haber alterado nada.
	agentFiles := map[string]string{} // plat → fichero en staging
	if agentsDir != "" && len(manifest.Agents) > 0 {
		for plat, ag := range manifest.Agents {
			if ag.URL == "" || len(ag.SHA256) != 64 {
				continue
			}
			name := "bdr-agent-" + plat
			if strings.HasPrefix(plat, "windows") {
				name += ".exe"
			}
			dst := filepath.Join(staging, name)
			f, err := os.Create(dst)
			if err != nil {
				return "", fmt.Errorf("staging del agente %s: %w", plat, err)
			}
			got, derr := download(ctx, client, ag.URL, f)
			f.Close()
			if derr != nil {
				return "", fmt.Errorf("refresco del agente %s: %w", plat, derr)
			}
			if !strings.EqualFold(got, ag.SHA256) {
				return "", fmt.Errorf("refresco del agente %s: el binario no coincide con el SHA-256 del manifiesto", plat)
			}
			agentFiles[plat] = dst
		}
	}

	// Reemplazo del control (con respaldo .old para rollback manual).
	newCtl := filepath.Join(staging, "bdr-control")
	if _, err := os.Stat(newCtl); err != nil {
		return "", fmt.Errorf("bundle sin bdr-control: %w", err)
	}
	old := exePath + ".old"
	_ = os.Remove(old)
	if err := os.Rename(exePath, old); err != nil {
		// El renombrado exige permiso de escritura en el DIRECTORIO del
		// binario, no en el fichero: si el servicio corre como usuario
		// no privilegiado, el directorio debe ser suyo (layout /opt/bdr).
		return "", fmt.Errorf(
			"respaldo del binario actual: %w — el directorio %s debe ser del usuario del servicio para auto-actualizarse; reinstala con el instalador v0.8.5+ (layout /opt/bdr/bin)",
			err, dir)
	}
	placed := filepath.Join(dir, filepath.Base(exePath))
	if err := copyFile(newCtl, placed); err != nil {
		_ = os.Rename(old, exePath)
		return "", fmt.Errorf("colocación del binario nuevo: %w", err)
	}
	if err := os.Chmod(placed, 0o755); err != nil {
		_ = os.Rename(old, exePath)
		return "", fmt.Errorf("permisos del binario nuevo: %w", err)
	}
	// agents.json del release: junto al binario, alimenta las ofertas de
	// auto-actualización de agentes de esta versión del control.
	if newAgents := filepath.Join(staging, "agents.json"); fileExists(newAgents) {
		dst := filepath.Join(dir, "agents.json")
		if err := copyFile(newAgents, dst); err == nil {
			_ = os.Chmod(dst, 0o644)
		}
	}
	// Kopia: si el bundle lo trae y kopia vive junto al binario, se
	// actualiza con la versión fijada del release.
	if newKopia := filepath.Join(staging, "kopia"); fileExists(newKopia) {
		kopiaPath := filepath.Join(dir, "kopia")
		if fileExists(kopiaPath) || kopiaIsLocal(dir) {
			oldK := kopiaPath + ".old"
			_ = os.Remove(oldK)
			_ = os.Rename(kopiaPath, oldK)
			if err := copyFile(newKopia, kopiaPath); err == nil {
				_ = os.Chmod(kopiaPath, 0o755)
			}
		}
	}
	// Agentes: colocación final en el directorio de descargas del control.
	if agentsDir != "" && len(agentFiles) > 0 {
		if err := os.MkdirAll(agentsDir, 0o755); err != nil {
			return "", fmt.Errorf("directorio de descargas %s: %w", agentsDir, err)
		}
		for plat, src := range agentFiles {
			name := "bdr-agent-" + plat
			if strings.HasPrefix(plat, "windows") {
				name += ".exe"
			}
			dst := filepath.Join(agentsDir, name)
			// El destino puede existir con un dueño previo ajeno al usuario del
			// servicio (p. ej. ficheros root de una instalación previa): truncar
			// un fichero existente exige permiso de escritura sobre el propio
			// fichero y falla con permission denied, así que se elimina antes de
			// escribir el binario nuevo.
			if err := os.Remove(dst); err != nil && !os.IsNotExist(err) {
				return "", fmt.Errorf("colocación del agente %s: %w", name, err)
			}
			if err := copyFile(src, dst); err != nil {
				return "", fmt.Errorf("colocación del agente %s: %w", name, err)
			}
			if err := os.Chmod(dst, 0o755); err != nil {
				return "", fmt.Errorf("permisos del agente %s: %w", name, err)
			}
		}
	}
	return placed, nil
}

// mustCreate se eliminó: las descargas en staging se gestionan con
// os.Create + cierre explícito en el bucle de Apply.

// kopiaIsLocal evita pisar una instalación de kopia gestionada por el
// sistema (docker: /usr/local/bin/kopia de la imagen es la misma ubicación
// que el binario del control, así que se actualiza con el bundle).
func kopiaIsLocal(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, "kopia"))
	return err == nil
}

func fileExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && !st.IsDir()
}

// download escribe la respuesta en w devolviendo el SHA-256 en hex.
func download(ctx context.Context, c *http.Client, url string, w io.Writer) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	resp, err := c.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("descarga de %s: HTTP %d", url, resp.StatusCode)
	}
	h := sha256.New()
	if _, err := io.Copy(io.MultiWriter(w, h), resp.Body); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// extractBundle despliega los ficheros del tar.gz plano en dest.
func extractBundle(src, dest string) error {
	f, err := os.Open(src)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return fmt.Errorf("bundle no es tar.gz: %w", err)
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		// Solo ficheros del primer nivel: los paquetes no anidan rutas.
		name := filepath.Clean(hdr.Name)
		if strings.Contains(name, "/") || hdr.Typeflag != tar.TypeReg {
			continue
		}
		if name == ".." || strings.HasPrefix(name, "/") {
			continue
		}
		out, err := os.OpenFile(filepath.Join(dest, name), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, os.FileMode(hdr.Mode))
		if err != nil {
			return err
		}
		if _, err := io.Copy(out, tr); err != nil {
			out.Close()
			return err
		}
		if out.Close(); err != nil {
			return err
		}
	}
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, in)
	return err
}

// writableTempDir devuelve el primer directorio en el que este proceso
// puede crear ficheros entre el del binario y el /tmp del sistema; "" si
// ninguno lo es.
func writableTempDir(exePath string) string {
	candidates := []string{}
	if exePath != "" {
		candidates = append(candidates, filepath.Dir(exePath))
	}
	candidates = append(candidates, os.TempDir())
	for _, d := range candidates {
		if d == "" {
			continue
		}
		probe, err := os.CreateTemp(d, ".bdr-write-probe-*")
		if err != nil {
			continue
		}
		name := probe.Name()
		_ = probe.Close()
		_ = os.Remove(name)
		return d
	}
	return ""
}

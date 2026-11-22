// Paquete upgrade: autoactualización del binario del agente.
//
// El servidor ofrece en la respuesta del heartbeat un binario nuevo con su
// SHA-256. El agente descarga, verifica y reemplaza su propio ejecutable
// solo cuando no tiene un job en curso; el reinicio lo decide la plataforma
// (re-ejecución in-place en Unix, reinicio por el gestor de servicios en
// Windows).
package upgrade

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/juansoler/bdr/packages/contracts"
	"github.com/juansoler/bdr/packages/logging"
)

// maxBinarySize límite de descarga (el binario del agente ronda 10-15 MB).
const maxBinarySize = 512 << 20

// Errores esperables: el llamante los registra y reintenta en el siguiente
// heartbeat; nunca dejan el binario en estado intermedio.
var (
	ErrNoChange = errors.New("la versión ofrecida coincide con la local")
	ErrBadSHA   = errors.New("el binario descargado no coincide con el SHA-256 del servidor")
)

// Replace descarga u.URL, verifica u.SHA256 y sustituye el ejecutable en
// marcha por el nuevo. El binario anterior queda como <exe>.old para
// rollback manual. Al terminar llama a restart(exePath), que se encarga del
// relevo: re-ejecución in-place en Unix; en Windows, parada limpia del
// servicio y arranque por un helper (service-restart) en 1-2 s. La
// sustitución es atómica por renombrado: si algo falla antes del restart,
// el binario actual sigue intacto.
//
// hc es el cliente HTTP de la descarga; nil usa http.DefaultClient. La URL
// de la oferta suele ser relativa al listener mTLS del control: para esas
// descargas el agente debe pasar un cliente con la CA del control y su
// certificado de cliente (el mismo par que usa en el heartbeat) — un
// http.DefaultClient falla la verificación TLS del listener de agentes.
func Replace(ctx context.Context, log *logging.Logger, u contracts.AgentUpgrade, restart func(exe string) error, hc *http.Client) error {
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("ruta del ejecutable: %w", err)
	}
	return ReplaceAt(ctx, log, exe, u, restart, hc)
}

// replaceAt hace el trabajo sobre una ruta concreta (exportada para tests).
func ReplaceAt(ctx context.Context, log *logging.Logger, exe string, u contracts.AgentUpgrade, restart func(exe string) error, hc *http.Client) error {
	if err := os.MkdirAll(filepath.Dir(exe), 0o755); err != nil {
		return err
	}

	// Descarga a un temporal junto al binario (mismo volumen → renombrado
	// atómico) y verificación streaming del SHA-256.
	tmp, err := os.CreateTemp(filepath.Dir(exe), "bdr-agent.new-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	cleanup := func() { _ = os.Remove(tmpName) }
	if _, err := downloadVerified(ctx, hc, u.URL, u.SHA256, tmp); err != nil {
		_ = tmp.Close()
		cleanup()
		return err
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return err
	}
	log.Info("binario nuevo verificado", "sha256", u.SHA256, "version", u.Version)

	// Preserva el binario actual para rollback y coloca el nuevo.
	old := exe + ".old"
	_ = os.Remove(old)
	if err := os.Rename(exe, old); err != nil {
		cleanup()
		return fmt.Errorf("respaldo del binario actual: %w", err)
	}
	if err := os.Rename(tmpName, exe); err != nil {
		// Restaura: el agente sigue con el binario que estaba en marcha.
		_ = os.Rename(old, exe)
		cleanup()
		return fmt.Errorf("colocación del binario nuevo: %w", err)
	}
	if err := os.Chmod(exe, 0o755); err != nil {
		log.Warn("permisos del binario nuevo", "error", err.Error())
	}
	log.Info("binario reemplazado; reiniciando", "old", old)
	return restart(exe)
}

// downloadVerified vuelca la respuesta HTTP en w comprobando límite de
// tamaño y SHA-256 en streaming.
func downloadVerified(ctx context.Context, hc *http.Client, url, wantSHA string, w io.Writer) (string, error) {
	if hc == nil {
		hc = http.DefaultClient
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	resp, err := hc.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("descarga: HTTP %d", resp.StatusCode)
	}
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(w, h), io.LimitReader(resp.Body, maxBinarySize+1))
	if err != nil {
		return "", err
	}
	if n > maxBinarySize {
		return "", fmt.Errorf("descarga supera el límite de %d MB", maxBinarySize>>20)
	}
	got := hex.EncodeToString(h.Sum(nil))
	if !strings.EqualFold(got, wantSHA) {
		return "", ErrBadSHA
	}
	return got, nil
}

// OfferVálida comprueba si la oferta aplica a este proceso.
func OfferValid(u *contracts.AgentUpgrade, currentVersion string) bool {
	return u != nil && u.URL != "" && len(u.SHA256) == 64 && u.Version != currentVersion
}

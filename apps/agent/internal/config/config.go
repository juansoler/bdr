// Package config gestiona la configuración local del agente.
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

const fileName = "agent.json"

// Config es la configuración persistida del agente.
//
// Compatibilidad: los archivos antiguos solo traen server_url, agent_id,
// api_key, heartbeat_seconds y enrolled_at. Los campos nuevos son opcionales.
type Config struct {
	ServerURL         string     `json:"server_url"`
	AgentURL          string     `json:"agent_url,omitempty"`
	AgentID           string     `json:"agent_id"`
	APIKey            string     `json:"api_key"`
	Hostname          string     `json:"hostname,omitempty"`
	HeartbeatSeconds  int        `json:"heartbeat_seconds"`
	EnrolledAt        time.Time  `json:"enrolled_at"`
	ServerFingerprint string     `json:"server_fingerprint,omitempty"`
	CertExpiresAt     *time.Time `json:"certificate_expires_at,omitempty"`
}

// Endpoint devuelve la URL que debe usar el agente: el listener mTLS si el
// servidor lo anunció al enrolar, o el plano de control en caso contrario.
func (c Config) Endpoint() string {
	if c.AgentURL != "" {
		return c.AgentURL
	}
	return c.ServerURL
}

// Path devuelve la ruta del archivo de configuración.
func Path(dataDir string) string {
	return filepath.Join(dataDir, fileName)
}

// Load lee la configuración del directorio de datos.
func Load(dataDir string) (Config, error) {
	b, err := os.ReadFile(Path(dataDir))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Config{}, fmt.Errorf("el agente no está enrolado en %s", dataDir)
		}
		return Config{}, err
	}
	var c Config
	if err := json.Unmarshal(b, &c); err != nil {
		return Config{}, fmt.Errorf("configuración corrupta: %w", err)
	}
	if c.AgentID == "" || c.APIKey == "" || c.Endpoint() == "" {
		return Config{}, errors.New("configuración incompleta")
	}
	return c, nil
}

// Save escribe la configuración de forma atómica y con permisos 0600.
func Save(dataDir string, c Config) error {
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dataDir, ".agent-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)

	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(b); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, Path(dataDir))
}

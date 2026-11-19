package config

import (
	"os"
	"testing"
	"time"
)

func TestSaveLoad(t *testing.T) {
	dir := t.TempDir()
	want := Config{
		ServerURL:        "http://127.0.0.1:8080",
		AgentID:          "agent-1",
		APIKey:           "secret",
		HeartbeatSeconds: 30,
		EnrolledAt:       time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC),
	}
	if err := Save(dir, want); err != nil {
		t.Fatalf("save: %v", err)
	}
	got, err := Load(dir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if got.AgentID != want.AgentID || got.APIKey != want.APIKey || got.ServerURL != want.ServerURL {
		t.Fatalf("configuración distinta: %+v", got)
	}
	if _, err := os.Stat(Path(dir)); err != nil {
		t.Fatalf("stat: %v", err)
	}
}

func TestLoadMissing(t *testing.T) {
	if _, err := Load(t.TempDir()); err == nil {
		t.Fatal("esperaba error sin configuración")
	}
}

// TestBackwardCompatibility comprueba que un agent.json antiguo (sin campos
// mTLS ni agent_url) sigue siendo válido y usa server_url como endpoint.
func TestBackwardCompatibility(t *testing.T) {
	dir := t.TempDir()
	old := `{
  "server_url": "http://127.0.0.1:8080",
  "agent_id": "agent-viejo",
  "api_key": "clave",
  "heartbeat_seconds": 30,
  "enrolled_at": "2026-01-02T03:04:05Z"
}`
	if err := os.WriteFile(Path(dir), []byte(old), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(dir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.Endpoint() != "http://127.0.0.1:8080" {
		t.Fatalf("endpoint = %q", cfg.Endpoint())
	}
	if cfg.AgentURL != "" || cfg.ServerFingerprint != "" || cfg.CertExpiresAt != nil {
		t.Fatalf("campos nuevos deberían quedar vacíos: %+v", cfg)
	}
}

func TestEndpointPriorizaAgentURL(t *testing.T) {
	cfg := Config{
		ServerURL: "http://127.0.0.1:8080",
		AgentURL:  "https://127.0.0.1:8443",
		AgentID:   "a", APIKey: "k",
	}
	if cfg.Endpoint() != "https://127.0.0.1:8443" {
		t.Fatalf("endpoint = %q", cfg.Endpoint())
	}
}

func TestLoadIncompleta(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(Path(dir), []byte(`{"server_url":"http://x","agent_id":""}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(dir); err == nil {
		t.Fatal("esperaba error de configuración incompleta")
	}
}

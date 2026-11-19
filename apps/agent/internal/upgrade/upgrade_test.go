package upgrade_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/juansoler/bdr/apps/agent/internal/upgrade"
	"github.com/juansoler/bdr/packages/contracts"
	"github.com/juansoler/bdr/packages/logging"
)

// Reemplazo completo: servidor que sirve el binario nuevo, SHA-256
// correcto → el ejecutable se sustituye y restart recibe su ruta. El
// binario anterior queda como .old para rollback.
func TestReplaceOK(t *testing.T) {
	newBin := []byte("nuevo binario del agente")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(newBin)
	}))
	t.Cleanup(srv.Close)

	// El "ejecutable" en marcha.
	dir := t.TempDir()
	exe := filepath.Join(dir, "bdr-agent")
	if err := os.WriteFile(exe, []byte("binario antiguo"), 0o755); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(newBin)

	restarted := ""
	err := upgrade.ReplaceAt(context.Background(), logging.New("error"),
		exe,
		contracts.AgentUpgrade{URL: srv.URL, SHA256: hex.EncodeToString(sum[:]), Version: "9.9.9"},
		func(exePath string) error { restarted = exePath; return nil })
	if err != nil {
		t.Fatal(err)
	}
	if restarted != exe {
		t.Fatalf("restart con %q, esperaba %q", restarted, exe)
	}
	got, err := os.ReadFile(exe)
	if err != nil || string(got) != string(newBin) {
		t.Fatalf("binario reemplazado: %v %q", err, got)
	}
	old, err := os.ReadFile(exe + ".old")
	if err != nil || string(old) != "binario antiguo" {
		t.Fatalf("rollback .old: %v %q", err, old)
	}
}

// SHA-256 incorrecto → no se toca el binario ni se reinicia.
func TestReplaceBadSHA(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("binario malicioso"))
	}))
	t.Cleanup(srv.Close)

	dir := t.TempDir()
	exe := filepath.Join(dir, "bdr-agent")
	if err := os.WriteFile(exe, []byte("binario antiguo"), 0o755); err != nil {
		t.Fatal(err)
	}

	called := false
	err := upgrade.ReplaceAt(context.Background(), logging.New("error"),
		exe,
		contracts.AgentUpgrade{URL: srv.URL, SHA256: "0000000000000000000000000000000000000000000000000000000000000000"},
		func(exePath string) error { called = true; return nil })
	if !errors.Is(err, upgrade.ErrBadSHA) {
		t.Fatalf("esperaba ErrBadSHA, got %v", err)
	}
	if called {
		t.Fatal("no debe reiniciar con un binario no verificado")
	}
	got, _ := os.ReadFile(exe)
	if string(got) != "binario antiguo" {
		t.Fatalf("el binario debe quedar intacto: %q", got)
	}
	if _, err := os.Stat(exe + ".old"); !os.IsNotExist(err) {
		t.Fatalf("no debe quedar .old de un intento fallido: %v", err)
	}
}

// OfferValid: URL vacía, SHA corto o misma versión → sin actualización.
func TestOfferValid(t *testing.T) {
	if upgrade.OfferValid(nil, "1") {
		t.Fatal("nil no es oferta")
	}
	if upgrade.OfferValid(&contracts.AgentUpgrade{Version: "2", SHA256: "abc"}, "1") {
		t.Fatal("sin URL no es oferta")
	}
	if upgrade.OfferValid(&contracts.AgentUpgrade{Version: "1", URL: "http://x", SHA256: "abc"}, "1") {
		t.Fatal("sha incompleto no es oferta")
	}
	if upgrade.OfferValid(&contracts.AgentUpgrade{Version: "1", URL: "http://x", SHA256: "abc"}, "1") {
		t.Fatal("misma versión no requiere actualización")
	}
	if !upgrade.OfferValid(&contracts.AgentUpgrade{Version: "2", URL: "http://x",
		SHA256: "0000000000000000000000000000000000000000000000000000000000000000"}, "1") {
		t.Fatal("oferta válida rechazada")
	}
}

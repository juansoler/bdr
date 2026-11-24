package agent

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/juansoler/bdr/apps/agent/internal/client"
	"github.com/juansoler/bdr/apps/agent/internal/config"
	"github.com/juansoler/bdr/apps/agent/internal/identity"
	"github.com/juansoler/bdr/packages/contracts"
	"github.com/juansoler/bdr/packages/logging"
)

// selfSignedIdentity genera un par clave/certificado autofirmado válido para
// identity.Save (updateEndpoint exige un X509KeyPair parseable).
func selfSignedIdentity(t *testing.T) (keyPEM, certPEM []byte) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "lab-agent"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		DNSNames:     []string{"lab-agent"},
	}
	certDER, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}),
		pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})
}

// heartbeatServer es un plano de control mínimo: sirve heartbeats y cuenta
// las peticiones recibidas.
func heartbeatServer(t *testing.T, agentURL string, hits *int32) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/agent/heartbeat" {
			http.NotFound(w, r)
			return
		}
		if hits != nil {
			atomic.AddInt32(hits, 1)
		}
		_ = json.NewEncoder(w).Encode(contracts.HeartbeatResponse{
			ServerTime: time.Now(),
			AgentURL:   agentURL,
		})
	}))
}

func newTestAgent(t *testing.T, endpoint, serverURL, dataDir string) *Agent {
	t.Helper()
	c, err := client.NewWithOptions(client.Options{BaseURL: endpoint, APIKey: "k", Timeout: 2 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	return New(Options{
		Client:  c,
		Config:  config.Config{ServerURL: serverURL, AgentURL: endpoint, APIKey: "k", HeartbeatSeconds: 30},
		DataDir: dataDir,
		Log:     logging.NewWithWriter(io.Discard, "error"),
	})
}

// Tras N heartbeats fallidos contra el endpoint, el agente debe reintentar
// contra la URL de enrolamiento y adoptar la URL de agentes anunciada.
func TestBeatEndpointRecovery(t *testing.T) {
	keyPEM, certPEM := selfSignedIdentity(t)
	dataDir := t.TempDir()
	if err := identity.Save(dataDir, keyPEM, certPEM, nil); err != nil {
		t.Fatal(err)
	}

	var fbHits int32
	const recovered = "http://recovered.example:8443"
	fb := heartbeatServer(t, recovered, &fbHits)
	defer fb.Close()

	// Endpoint muerto: conexión rechazada en cada heartbeat.
	a := newTestAgent(t, "http://127.0.0.1:1", fb.URL, dataDir)
	for i := 0; i < 5; i++ {
		a.beat(context.Background())
	}
	if atomic.LoadInt32(&fbHits) != 1 {
		t.Fatalf("fallback: %d heartbeats contra la URL de enrolamiento, quiero 1", fbHits)
	}
	a.cfgMu.Lock()
	got := a.cfg.AgentURL
	a.cfgMu.Unlock()
	if got != recovered {
		t.Fatalf("AgentURL = %q, quiero %q", got, recovered)
	}
	if a.client.BaseURL() != recovered {
		t.Fatalf("cliente no actualizado: %q", a.client.BaseURL())
	}

	// Cooldown: otro heartbeat fallido no dispara un segundo fallback inmediato.
	a.beat(context.Background())
	if atomic.LoadInt32(&fbHits) != 1 {
		t.Fatalf("fallback sin cooldown: %d hits", fbHits)
	}
}

// Sin AgentURL distinta de ServerURL no hay nada que recuperar: el agente
// no debe intentar ningún fallback.
func TestBeatNoFallbackWhenServerURLEqualsEndpoint(t *testing.T) {
	dataDir := t.TempDir()
	// Config sin AgentURL: Endpoint() == ServerURL.
	a := newTestAgent(t, "http://127.0.0.1:1", "http://127.0.0.1:1", dataDir)
	a.cfgMu.Lock()
	a.cfg.AgentURL = ""
	a.cfgMu.Unlock()
	for i := 0; i < 8; i++ {
		a.beat(context.Background())
	}
	a.cfgMu.Lock()
	got := a.cfg.AgentURL
	a.cfgMu.Unlock()
	if got != "" {
		t.Fatalf("AgentURL cambió sin fallback posible: %q", got)
	}
}

// Un heartbeat exitoso resetea el contador de fallos.
func TestBeatResetsFailureCounter(t *testing.T) {
	var hits int32
	ok := heartbeatServer(t, "", &hits)
	defer ok.Close()

	a := newTestAgent(t, "http://127.0.0.1:1", "http://127.0.0.1:1", t.TempDir())
	a.beat(context.Background())
	a.beat(context.Background())
	if a.hbFailures != 2 {
		t.Fatalf("hbFailures = %d, quiero 2", a.hbFailures)
	}
	// Endpoint sano: el siguiente heartbeat tiene éxito y resetea.
	c, err := client.NewWithOptions(client.Options{BaseURL: ok.URL, APIKey: "k", Timeout: 2 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	a.mu.Lock()
	a.client = c
	a.mu.Unlock()
	a.beat(context.Background())
	if a.hbFailures != 0 {
		t.Fatalf("hbFailures = %d tras éxito, quiero 0", a.hbFailures)
	}
}

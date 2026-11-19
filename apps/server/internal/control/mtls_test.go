package control_test

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/juansoler/bdr/apps/server/internal/auth"
	"github.com/juansoler/bdr/apps/server/internal/control"
	"github.com/juansoler/bdr/apps/server/internal/pki"
	"github.com/juansoler/bdr/apps/server/internal/store"
	"github.com/juansoler/bdr/packages/logging"
)

// mtlsEnv monta un control plane con listener TLS estricto (igual que
// --agent-addr sin --insecure-agents) para probar la política de certificados
// end-to-end: handshake, revocación, renovación y ligadura identidad↔API key.
type mtlsEnv struct {
	ts  *httptest.Server
	st  store.Store
	api *control.Server
	ca  *pki.CA
}

func newMTLSEnv(t *testing.T) *mtlsEnv {
	t.Helper()
	dir := t.TempDir()
	ca, err := pki.LoadOrCreate(dir)
	if err != nil {
		t.Fatal(err)
	}
	st := store.NewMemory()
	api := control.New(st, adminToken, logging.NewWithWriter(io.Discard, "error"), "test")
	api.CA = ca
	serverPair, err := ca.ServerTLS([]string{"127.0.0.1", "localhost"})
	if err != nil {
		t.Fatal(err)
	}
	cfg := &tls.Config{
		Certificates: []tls.Certificate{serverPair},
		MinVersion:   tls.VersionTLS12,
		// Igual que en main.go: mTLS estricto, sin reutilización de sesión.
		ClientAuth:             tls.RequireAndVerifyClientCert,
		ClientCAs:              ca.Pool(),
		SessionTicketsDisabled: true,
		VerifyPeerCertificate: func(rawCerts [][]byte, _ [][]*x509.Certificate) error {
			if len(rawCerts) == 0 {
				return errNoClientCert
			}
			leaf, err := x509.ParseCertificate(rawCerts[0])
			if err != nil {
				return err
			}
			agentID, serial, ok := pki.AgentIdentity(leaf)
			if !ok {
				return errNoClientCert
			}
			return api.VerifyAgentHandshake(agentID, serial)
		},
	}
	ts := httptest.NewUnstartedServer(api.Handler())
	ts.TLS = cfg
	ts.StartTLS()
	t.Cleanup(ts.Close)
	return &mtlsEnv{ts: ts, st: st, api: api, ca: ca}
}

var errNoClientCert = errStr("sin certificado de cliente")

type errStr string

func (e errStr) Error() string { return string(e) }

// makeClientCert genera un par clave+CSR, lo firma como certificado de agente
// y devuelve el tls.Certificate listo para el cliente.
func (e *mtlsEnv) makeClientCert(t *testing.T, agentID string) (tls.Certificate, string) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.CertificateRequest{
		Subject:            pkix.Name{CommonName: agentID},
		SignatureAlgorithm: x509.ECDSAWithSHA256,
	}
	csrDER, err := x509.CreateCertificateRequest(rand.Reader, tmpl, key)
	if err != nil {
		t.Fatal(err)
	}
	csrPEM := string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: csrDER}))
	issued, err := e.ca.SignAgentCSR(csrPEM, agentID, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	pair, err := tls.X509KeyPair(
		[]byte(issued.PEM),
		pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}),
	)
	if err != nil {
		t.Fatal(err)
	}
	return pair, issued.Serial
}

// seedAgent registra el agente con su API key y certificado vigente.
func (e *mtlsEnv) seedAgent(t *testing.T, id, apiKey, serial string) {
	t.Helper()
	now := time.Now().UTC()
	err := e.st.Update(t.Context(), func(s *store.State) error {
		s.Agents[id] = &store.Agent{
			ID: id, Hostname: "mtls-lab", OSFamily: "linux", Arch: "amd64",
			Status: "online", EnrolledAt: now,
			APIKeyHash: auth.Hash(apiKey), CertSerial: serial,
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func (e *mtlsEnv) get(t *testing.T, cert *tls.Certificate, apiKey string) (*http.Response, string) {
	t.Helper()
	cl := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{
		RootCAs:      e.ca.Pool(),
		Certificates: certOrEmpty(cert),
		MinVersion:   tls.VersionTLS12,
	}}}
	req, err := http.NewRequest(http.MethodGet, e.ts.URL+"/api/v1/agent/me", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	resp, err := cl.Do(req)
	if err != nil {
		return nil, "handshake-error: " + err.Error()
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return resp, string(body)
}

func certOrEmpty(c *tls.Certificate) []tls.Certificate {
	if c == nil {
		return nil
	}
	return []tls.Certificate{*c}
}

func TestMTLSHandshakePolicy(t *testing.T) {
	env := newMTLSEnv(t)

	certA, serialA := env.makeClientCert(t, "agent-a")
	env.seedAgent(t, "agent-a", "key-a", serialA)

	// 1. Certificado vigente + API key correcta → 200.
	resp, body := env.get(t, &certA, "key-a")
	if resp == nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("agente válido debía autenticar: %v %s", resp, body)
	}

	// 2. Sin certificado de cliente → handshake rechazado.
	resp, _ = env.get(t, nil, "key-a")
	if resp != nil {
		t.Fatal("sin certificado el handshake debía fallar")
	}

	// 3. API key de otro agente con cert válido → el handshake pasa, pero la
	// ligadura HTTP lo rechaza (E_AUTH_CERT).
	certB, serialB := env.makeClientCert(t, "agent-b")
	env.seedAgent(t, "agent-b", "key-b", serialB)
	_ = certB
	resp, body = env.get(t, &certA, "key-b")
	if resp == nil || resp.StatusCode != http.StatusUnauthorized || !strings.Contains(body, "E_AUTH_CERT") {
		t.Fatalf("ligadura identidad: esperaba 401 E_AUTH_CERT, got %v %s", resp, body)
	}

	// 4. Renovación: el serial antiguo deja de ser vigente de inmediato.
	certA2, serialA2 := env.makeClientCert(t, "agent-a")
	err := env.st.Update(t.Context(), func(s *store.State) error {
		if a := s.Agents["agent-a"]; a != nil {
			a.CertSerial = serialA2
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	resp, _ = env.get(t, &certA, "key-a")
	if resp != nil {
		t.Fatal("el certificado antiguo debía rechazarse tras la renovación")
	}
	resp, body = env.get(t, &certA2, "key-a")
	if resp == nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("el certificado renovado debía funcionar: %v %s", resp, body)
	}

	// 5. Revocación: efecto inmediato en el siguiente handshake (sin reinicio).
	err = env.st.Update(t.Context(), func(s *store.State) error {
		if a := s.Agents["agent-a"]; a != nil {
			now := time.Now().UTC()
			a.RevokedAt = &now
			a.RevokedReason = "prueba"
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	resp, _ = env.get(t, &certA2, "key-a")
	if resp != nil {
		t.Fatal("el certificado de un agente revocado debía rechazarse en el handshake")
	}
}

// TestMTLSMeView valida el cuerpo del /agent/me tras la ligadura mTLS.
func TestMTLSMeView(t *testing.T) {
	env := newMTLSEnv(t)
	certA, serialA := env.makeClientCert(t, "agent-a")
	env.seedAgent(t, "agent-a", "key-a", serialA)
	resp, body := env.get(t, &certA, "key-a")
	if resp == nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("me: %v %s", resp, body)
	}
	var me map[string]any
	if err := json.Unmarshal([]byte(body), &me); err != nil {
		t.Fatalf("json: %v (%s)", err, body)
	}
	if me["id"] != "agent-a" {
		t.Fatalf("id: %v", me["id"])
	}
	if !strings.Contains(body, "mtls-lab") {
		t.Fatalf("falta el hostname: %s", body)
	}
}

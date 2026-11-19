package pki

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"path/filepath"
	"testing"
	"time"
)

func makeCSR(t *testing.T) (csrPEM, keyPEM string) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.CertificateRequest{
		Subject:            pkix.Name{CommonName: "lo-que-sea"},
		SignatureAlgorithm: x509.ECDSAWithSHA256,
	}
	der, err := x509.CreateCertificateRequest(rand.Reader, tmpl, key)
	if err != nil {
		t.Fatal(err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: der})),
		"-----BEGIN EC PRIVATE KEY-----"
}

func TestCALoadOrCreateAndSign(t *testing.T) {
	dir := t.TempDir()
	ca, err := LoadOrCreate(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !ca.Cert.IsCA {
		t.Fatal("la CA no es CA")
	}
	if len(ca.Fingerprint()) != len("sha256:")+64 {
		t.Fatalf("huella: %s", ca.Fingerprint())
	}
	ca2, err := LoadOrCreate(dir)
	if err != nil {
		t.Fatal(err)
	}
	if ca2.Fingerprint() != ca.Fingerprint() {
		t.Fatal("la CA cambió al reabrir")
	}

	csrPEM, _ := makeCSR(t)
	issued, err := ca.SignAgentCSR(csrPEM, "agent-123", time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if !(time.Until(issued.NotAfter) < AgentCertValidity+time.Minute) {
		t.Fatal("validez del certificado incorrecta")
	}
	cert := parsePEMCert(t, issued.PEM)
	if cert.Subject.CommonName != "agent-123" {
		t.Fatalf("CN=%s", cert.Subject.CommonName)
	}
	id, serial, ok := AgentIdentity(cert)
	if !ok || id != "agent-123" || serial != issued.Serial {
		t.Fatalf("identidad: %q %q %v", id, serial, ok)
	}
	// Verificable contra la CA.
	chains, err := cert.Verify(x509.VerifyOptions{Roots: ca.Pool(), KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}})
	if err != nil || len(chains) == 0 {
		t.Fatalf("no encadena: %v", err)
	}

	// CSR inválidos.
	if _, err := ca.SignAgentCSR("no es pem", "x", time.Now()); err == nil {
		t.Fatal("esperaba error con PEM inválido")
	}
	if _, err := ca.SignAgentCSR("-----BEGIN CERTIFICATE-----\nAAAA\n-----END CERTIFICATE-----\n", "x", time.Now()); err == nil {
		t.Fatal("esperaba error con tipo PEM incorrecto")
	}
}

func TestServerTLSRegenerates(t *testing.T) {
	dir := t.TempDir()
	ca, err := LoadOrCreate(dir)
	if err != nil {
		t.Fatal(err)
	}
	pair1, err := ca.ServerTLS([]string{"127.0.0.1", "localhost"})
	if err != nil {
		t.Fatal(err)
	}
	if err := pair1.Leaf.VerifyHostname("localhost"); err != nil {
		t.Fatal("el certificado no cubre localhost")
	}
	// Mismos hosts: reutiliza.
	pair2, err := ca.ServerTLS([]string{"127.0.0.1", "localhost"})
	if err != nil {
		t.Fatal(err)
	}
	if string(pair2.Certificate[0]) != string(pair1.Certificate[0]) {
		t.Fatal("regeneró sin necesidad")
	}
	// Host nuevo: regenera cubriendo ambos.
	pair3, err := ca.ServerTLS([]string{"127.0.0.1", "localhost", "bdr.example"})
	if err != nil {
		t.Fatal(err)
	}
	if err := pair3.Leaf.VerifyHostname("bdr.example"); err != nil {
		t.Fatal("el certificado nuevo no cubre bdr.example")
	}
	if err := pair3.Leaf.VerifyHostname("localhost"); err != nil {
		t.Fatal("el certificado nuevo no cubre localhost")
	}
	if filepath.Join(dir, "server.key") == "" {
		t.Fatal("imposible")
	}
}

func parsePEMCert(t *testing.T, pemStr string) *x509.Certificate {
	t.Helper()
	block, _ := pem.Decode([]byte(pemStr))
	if block == nil {
		t.Fatal("PEM inválido")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	return cert
}

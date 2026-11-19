package identity

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/x509"
	"encoding/pem"
	"os"
	"testing"
)

func TestGenerateCSR(t *testing.T) {
	kp, err := GenerateCSR("equipo-01")
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	block, _ := pem.Decode(kp.CSRPEM)
	if block == nil || block.Type != "CERTIFICATE REQUEST" {
		t.Fatalf("bloque PEM inesperado: %+v", block)
	}
	csr, err := x509.ParseCertificateRequest(block.Bytes)
	if err != nil {
		t.Fatalf("parse csr: %v", err)
	}
	if err := csr.CheckSignature(); err != nil {
		t.Fatalf("firma de la CSR inválida: %v", err)
	}
	if csr.Subject.CommonName != "equipo-01" {
		t.Fatalf("CN = %q", csr.Subject.CommonName)
	}
	pub, ok := csr.PublicKey.(*ecdsa.PublicKey)
	if !ok || pub.Curve != elliptic.P256() {
		t.Fatalf("la CSR no usa ECDSA P-256")
	}
}

func TestNewCSRReutilizaClave(t *testing.T) {
	kp, err := GenerateCSR("equipo-01")
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	csrPEM, err := NewCSR(kp.KeyPEM, "equipo-02")
	if err != nil {
		t.Fatalf("new csr: %v", err)
	}
	block, _ := pem.Decode(csrPEM)
	csr, err := x509.ParseCertificateRequest(block.Bytes)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if csr.Subject.CommonName != "equipo-02" {
		t.Fatalf("CN = %q", csr.Subject.CommonName)
	}
}

func TestSaveLoadPermissions(t *testing.T) {
	dir := t.TempDir()
	kp, err := GenerateCSR("equipo-01")
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if err := Save(dir, kp.KeyPEM, []byte("cert"), []byte("ca")); err != nil {
		t.Fatalf("save: %v", err)
	}
	key, cert, ca, err := Load(dir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(key) == 0 || string(cert) != "cert" || string(ca) != "ca" {
		t.Fatalf("contenido inesperado")
	}
	keyPath, certPath, caPath := Paths(dir)
	for _, p := range []string{keyPath, certPath, caPath} {
		info, err := os.Stat(p)
		if err != nil {
			t.Fatalf("stat %s: %v", p, err)
		}
		if info.Mode().Perm() != 0o600 {
			t.Fatalf("%s tiene permisos %o", p, info.Mode().Perm())
		}
	}
}

// Package pki gestiona la CA interna que emite certificados mTLS a los agentes.
//
// Los archivos viven en <data-dir>/pki: ca.key/ca.crt (CA ECDSA P-256, 10
// años) y server.key/server.crt (certificado del listener de agentes, 2 años,
// regenerado si cambian los nombres). La huella publicada a los agentes es
// SHA-256 del DER de la CA: "sha256:<hex>".
package pki

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// AgentCertValidity es la validez de los certificados de agente.
const AgentCertValidity = 90 * 24 * time.Hour

// AgentURIPrefix es el prefijo del SAN URI que identifica al agente.
const AgentURIPrefix = "urn:bdr:agent:"

// CA es la autoridad interna.
type CA struct {
	Cert    *x509.Certificate
	CertPEM []byte
	key     *ecdsa.PrivateKey
	dir     string
}

// LoadOrCreate abre la CA de dir o la crea.
func LoadOrCreate(dir string) (*CA, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	certPath, keyPath := filepath.Join(dir, "ca.crt"), filepath.Join(dir, "ca.key")
	if _, err := os.Stat(certPath); err == nil {
		certPEM, err := os.ReadFile(certPath)
		if err != nil {
			return nil, err
		}
		keyPEM, err := os.ReadFile(keyPath)
		if err != nil {
			return nil, err
		}
		cert, err := parseCert(certPEM)
		if err != nil {
			return nil, err
		}
		key, err := parseKey(keyPEM)
		if err != nil {
			return nil, err
		}
		return &CA{Cert: cert, CertPEM: certPEM, key: key, dir: dir}, nil
	}

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	tmpl := &x509.Certificate{
		SerialNumber:          randSerial(),
		Subject:               pkix.Name{CommonName: "BDR Internal CA", Organization: []string{"BDR Self-Hosted"}},
		NotBefore:             now.Add(-5 * time.Minute),
		NotAfter:              now.AddDate(10, 0, 0),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign | x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
		IsCA:                  true,
		MaxPathLenZero:        true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return nil, err
	}
	cert, _ := x509.ParseCertificate(der)
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	if err := writeKey(keyPath, key); err != nil {
		return nil, err
	}
	if err := os.WriteFile(certPath, certPEM, 0o644); err != nil {
		return nil, err
	}
	return &CA{Cert: cert, CertPEM: certPEM, key: key, dir: dir}, nil
}

// Fingerprint devuelve "sha256:<hex>" del DER de la CA.
func (c *CA) Fingerprint() string { return Fingerprint(c.Cert.Raw) }

// Fingerprint calcula la huella de un DER.
func Fingerprint(der []byte) string {
	sum := sha256.Sum256(der)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// ServerTLS devuelve el certificado del listener de agentes para hosts
// (nombres DNS o IP). Se regenera si faltan nombres o caduca en menos de 30 días.
func (c *CA) ServerTLS(hosts []string) (tls.Certificate, error) {
	certPath, keyPath := filepath.Join(c.dir, "server.crt"), filepath.Join(c.dir, "server.key")
	if pair, err := tls.LoadX509KeyPair(certPath, keyPath); err == nil {
		leaf, err := x509.ParseCertificate(pair.Certificate[0])
		if err == nil && covers(leaf, hosts) && time.Until(leaf.NotAfter) > 30*24*time.Hour {
			pair.Leaf = leaf
			return pair, nil
		}
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return tls.Certificate{}, err
	}
	now := time.Now().UTC()
	tmpl := &x509.Certificate{
		SerialNumber: randSerial(),
		Subject:      pkix.Name{CommonName: "bdr-control"},
		NotBefore:    now.Add(-5 * time.Minute),
		NotAfter:     now.AddDate(2, 0, 0),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	for _, h := range hosts {
		h = strings.TrimSpace(h)
		if h == "" {
			continue
		}
		if ip := net.ParseIP(h); ip != nil {
			tmpl.IPAddresses = append(tmpl.IPAddresses, ip)
		} else {
			tmpl.DNSNames = append(tmpl.DNSNames, h)
		}
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, c.Cert, &key.PublicKey, c.key)
	if err != nil {
		return tls.Certificate{}, err
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	if err := writeKey(keyPath, key); err != nil {
		return tls.Certificate{}, err
	}
	if err := os.WriteFile(certPath, certPEM, 0o644); err != nil {
		return tls.Certificate{}, err
	}
	pair, err := tls.LoadX509KeyPair(certPath, keyPath)
	if err != nil {
		return tls.Certificate{}, err
	}
	pair.Leaf, _ = x509.ParseCertificate(der)
	return pair, nil
}

func covers(leaf *x509.Certificate, hosts []string) bool {
	for _, h := range hosts {
		h = strings.TrimSpace(h)
		if h == "" {
			continue
		}
		if ip := net.ParseIP(h); ip != nil {
			if !slices.ContainsFunc(leaf.IPAddresses, func(x net.IP) bool { return x.Equal(ip) }) {
				return false
			}
		} else if !slices.Contains(leaf.DNSNames, h) {
			return false
		}
	}
	return true
}

// IssuedCert es un certificado de agente emitido.
type IssuedCert struct {
	PEM        string
	Serial     string
	NotAfter   time.Time
	Thumbprint string
}

// SignAgentCSR valida el CSR y emite un certificado de cliente para agentID.
// El sujeto y los SAN del CSR se ignoran: la identidad la fija el servidor.
func (c *CA) SignAgentCSR(csrPEM, agentID string, now time.Time) (IssuedCert, error) {
	block, _ := pem.Decode([]byte(csrPEM))
	if block == nil || block.Type != "CERTIFICATE REQUEST" {
		return IssuedCert{}, errors.New("csr_pem no contiene un CERTIFICATE REQUEST")
	}
	csr, err := x509.ParseCertificateRequest(block.Bytes)
	if err != nil {
		return IssuedCert{}, fmt.Errorf("CSR inválido: %w", err)
	}
	if err := csr.CheckSignature(); err != nil {
		return IssuedCert{}, fmt.Errorf("firma del CSR inválida: %w", err)
	}
	switch pub := csr.PublicKey.(type) {
	case *ecdsa.PublicKey:
		if pub.Curve.Params().BitSize < 256 {
			return IssuedCert{}, errors.New("clave ECDSA demasiado corta")
		}
	default:
		if csr.PublicKeyAlgorithm != x509.RSA {
			return IssuedCert{}, errors.New("solo se admiten claves ECDSA o RSA")
		}
	}
	u, _ := url.Parse(AgentURIPrefix + agentID)
	serial := randSerial()
	tmpl := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: agentID, Organization: []string{"BDR Agent"}},
		NotBefore:    now.Add(-5 * time.Minute),
		NotAfter:     now.Add(AgentCertValidity),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
		URIs:         []*url.URL{u},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, c.Cert, csr.PublicKey, c.key)
	if err != nil {
		return IssuedCert{}, err
	}
	return IssuedCert{
		PEM:        string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})),
		Serial:     serial.Text(16),
		NotAfter:   tmpl.NotAfter,
		Thumbprint: Fingerprint(der),
	}, nil
}

// Pool devuelve un pool con la CA.
func (c *CA) Pool() *x509.CertPool {
	p := x509.NewCertPool()
	p.AddCert(c.Cert)
	return p
}

// AgentIdentity extrae el ID de agente y el serial de un certificado verificado.
func AgentIdentity(cert *x509.Certificate) (agentID, serial string, ok bool) {
	for _, u := range cert.URIs {
		s := u.String()
		if strings.HasPrefix(s, AgentURIPrefix) {
			return strings.TrimPrefix(s, AgentURIPrefix), cert.SerialNumber.Text(16), true
		}
	}
	return "", "", false
}

func randSerial() *big.Int {
	n, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 127))
	if err != nil {
		panic(err)
	}
	return n.Add(n, big.NewInt(1))
}

func parseCert(b []byte) (*x509.Certificate, error) {
	block, _ := pem.Decode(b)
	if block == nil {
		return nil, errors.New("PEM de certificado inválido")
	}
	return x509.ParseCertificate(block.Bytes)
}

func parseKey(b []byte) (*ecdsa.PrivateKey, error) {
	block, _ := pem.Decode(b)
	if block == nil {
		return nil, errors.New("PEM de clave inválido")
	}
	return x509.ParseECPrivateKey(block.Bytes)
}

func writeKey(path string, key *ecdsa.PrivateKey) error {
	der, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return err
	}
	return os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: der}), 0o600)
}

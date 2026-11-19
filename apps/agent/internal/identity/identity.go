// Package identity genera y persiste la identidad mTLS del agente.
package identity

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// Nombres de los archivos de identidad dentro del directorio de datos.
const (
	KeyFileName  = "agent.key"
	CertFileName = "agent.crt"
	CAFileName   = "ca.crt"
)

// KeyPair es una clave privada y su solicitud de certificado en PEM.
type KeyPair struct {
	KeyPEM []byte
	CSRPEM []byte
}

// GenerateCSR crea una clave ECDSA P-256 y una CSR firmada con ella.
// El nombre común y el SAN DNS se rellenan con commonName.
func GenerateCSR(commonName string) (KeyPair, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return KeyPair{}, fmt.Errorf("no se pudo generar la clave ECDSA: %w", err)
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return KeyPair{}, fmt.Errorf("no se pudo serializar la clave: %w", err)
	}
	csr, err := csrFor(key, commonName)
	if err != nil {
		return KeyPair{}, err
	}
	return KeyPair{
		KeyPEM: pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}),
		CSRPEM: csr,
	}, nil
}

// NewCSR reutiliza una clave privada existente para pedir un certificado nuevo.
func NewCSR(keyPEM []byte, commonName string) ([]byte, error) {
	block, _ := pem.Decode(keyPEM)
	if block == nil {
		return nil, errors.New("la clave privada no es PEM válido")
	}
	key, err := x509.ParseECPrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("clave privada ECDSA inválida: %w", err)
	}
	return csrFor(key, commonName)
}

func csrFor(key *ecdsa.PrivateKey, commonName string) ([]byte, error) {
	if commonName == "" {
		commonName = "bdr-agent"
	}
	tmpl := &x509.CertificateRequest{
		Subject:            pkix.Name{CommonName: commonName, Organization: []string{"BDR"}},
		DNSNames:           []string{commonName},
		SignatureAlgorithm: x509.ECDSAWithSHA256,
	}
	der, err := x509.CreateCertificateRequest(rand.Reader, tmpl, key)
	if err != nil {
		return nil, fmt.Errorf("no se pudo crear la CSR: %w", err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: der}), nil
}

// Save escribe la clave, el certificado y la CA en el directorio con permisos 0600.
// Los tres archivos son opcionales; los vacíos se omiten.
func Save(dir string, keyPEM, certPEM, caPEM []byte) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	files := []struct {
		name string
		data []byte
	}{
		{KeyFileName, keyPEM},
		{CertFileName, certPEM},
		{CAFileName, caPEM},
	}
	for _, f := range files {
		if len(f.data) == 0 {
			continue
		}
		if err := writeFile(filepath.Join(dir, f.name), f.data); err != nil {
			return err
		}
	}
	return nil
}

// Load lee la identidad guardada. Devuelve error si no hay clave o certificado.
func Load(dir string) (keyPEM, certPEM, caPEM []byte, err error) {
	keyPEM, err = readFile(filepath.Join(dir, KeyFileName))
	if err != nil {
		return nil, nil, nil, fmt.Errorf("clave del agente no disponible: %w", err)
	}
	certPEM, err = readFile(filepath.Join(dir, CertFileName))
	if err != nil {
		return nil, nil, nil, fmt.Errorf("certificado del agente no disponible: %w", err)
	}
	// La CA es opcional: si falta se usarán las raíces del sistema.
	caPEM, _ = readFile(filepath.Join(dir, CAFileName))
	return keyPEM, certPEM, caPEM, nil
}

// Paths devuelve las rutas de la identidad en el directorio de datos.
func Paths(dir string) (keyPath, certPath, caPath string) {
	return filepath.Join(dir, KeyFileName), filepath.Join(dir, CertFileName), filepath.Join(dir, CAFileName)
}

func writeFile(path string, data []byte) error {
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return err
	}
	// os.WriteFile no cambia los permisos de un archivo existente.
	return os.Chmod(path, 0o600)
}

func readFile(path string) ([]byte, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if len(b) == 0 {
		return nil, fmt.Errorf("%s está vacío", path)
	}
	return b, nil
}

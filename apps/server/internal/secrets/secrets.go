// Package secrets cifra secretos en reposo con AES-256-GCM.
//
// La clave maestra del plano de control se toma de BDR_SECRET_KEY (32 bytes
// en base64) o, si no existe, de <data-dir>/secret.key (se genera en el primer
// arranque con permisos 0600). Esa clave debe custodiarse aparte del
// state.json: sin ella los secretos almacenados no se pueden descifrar
// (docs/14-custodia-clave-maestra.md).
package secrets

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const prefix = "v1:"

// ErrDecrypt indica que el secreto no se pudo descifrar (clave errónea o dato alterado).
var ErrDecrypt = errors.New("no se pudo descifrar el secreto")

// Box cifra y descifra secretos.
type Box struct {
	aead cipher.AEAD
}

// New crea una caja con una clave de 32 bytes.
func New(key []byte) (*Box, error) {
	if len(key) != 32 {
		return nil, fmt.Errorf("la clave debe tener 32 bytes, tiene %d", len(key))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &Box{aead: aead}, nil
}

// Load obtiene la clave de env (base64) o del archivo; genera el archivo si no existe.
// Devuelve también si la clave se ha generado ahora.
func Load(envValue, path string) (*Box, bool, error) {
	if v := strings.TrimSpace(envValue); v != "" {
		key, err := base64.StdEncoding.DecodeString(v)
		if err != nil {
			return nil, false, fmt.Errorf("BDR_SECRET_KEY no es base64 válido: %w", err)
		}
		b, err := New(key)
		return b, false, err
	}
	data, err := os.ReadFile(path)
	if err == nil {
		key, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(data)))
		if err != nil {
			return nil, false, fmt.Errorf("clave corrupta en %s: %w", path, err)
		}
		b, err := New(key)
		return b, false, err
	}
	if !errors.Is(err, os.ErrNotExist) {
		return nil, false, err
	}
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return nil, false, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, false, err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return nil, false, err
	}
	if _, err := f.WriteString(base64.StdEncoding.EncodeToString(key) + "\n"); err != nil {
		_ = f.Close()
		return nil, false, err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return nil, false, err
	}
	if err := f.Close(); err != nil {
		return nil, false, err
	}
	b, err := New(key)
	return b, true, err
}

// Seal cifra un texto. La cadena vacía se conserva vacía.
func (b *Box) Seal(plain string) (string, error) {
	if plain == "" {
		return "", nil
	}
	nonce := make([]byte, b.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	out := b.aead.Seal(nonce, nonce, []byte(plain), []byte(prefix))
	return prefix + base64.RawStdEncoding.EncodeToString(out), nil
}

// MustSeal cifra y entra en pánico solo si falla el generador aleatorio.
func (b *Box) MustSeal(plain string) string {
	s, err := b.Seal(plain)
	if err != nil {
		panic(err)
	}
	return s
}

// Open descifra un texto producido por Seal.
func (b *Box) Open(sealed string) (string, error) {
	if sealed == "" {
		return "", nil
	}
	if !strings.HasPrefix(sealed, prefix) {
		return "", ErrDecrypt
	}
	raw, err := base64.RawStdEncoding.DecodeString(strings.TrimPrefix(sealed, prefix))
	if err != nil {
		return "", ErrDecrypt
	}
	ns := b.aead.NonceSize()
	if len(raw) < ns {
		return "", ErrDecrypt
	}
	plain, err := b.aead.Open(nil, raw[:ns], raw[ns:], []byte(prefix))
	if err != nil {
		return "", ErrDecrypt
	}
	return string(plain), nil
}

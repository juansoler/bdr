package secrets

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSealOpenRoundTrip(t *testing.T) {
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}
	b, err := New(key)
	if err != nil {
		t.Fatal(err)
	}
	secret := "password-con-acentos-áéí y símbolos !$%"
	sealed, err := b.Seal(secret)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(sealed, "v1:") {
		t.Fatal("falta el prefijo de versión")
	}
	got, err := b.Open(sealed)
	if err != nil || got != secret {
		t.Fatalf("got=%q err=%v", got, err)
	}
	if s2, _ := b.Seal(secret); s2 == sealed {
		t.Fatal("el nonce debe ser distinto cada vez")
	}
	if got, _ := b.Open(""); got != "" {
		t.Fatal("vacío debe seguir vacío")
	}
}

func TestOpenWithWrongKey(t *testing.T) {
	b1, _ := New(make([]byte, 32))
	b2, _ := New([]byte{9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9})
	sealed, _ := b1.Seal("dato")
	if _, err := b2.Open(sealed); err == nil {
		t.Fatal("otra clave no debe descifrar")
	}
	if _, err := b2.Open("v1:no-es-base64!!"); err == nil {
		t.Fatal("esperaba error de formato")
	}
}

func TestLoadFromFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "secret.key")
	b, generated, err := Load("", path)
	if err != nil || !generated {
		t.Fatalf("generated=%v err=%v", generated, err)
	}
	if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("permisos: %v", info.Mode())
	}
	b2, generated2, err := Load("", path)
	if err != nil || generated2 {
		t.Fatalf("no debía regenerar: %v %v", generated2, err)
	}
	sealed, _ := b.Seal("x")
	if _, err := b2.Open(sealed); err != nil {
		t.Fatal("la clave recargada debe descifrar")
	}
	if _, _, err := Load("no-es-base64!", path); err == nil {
		t.Fatal("env inválido debe fallar")
	}
	if _, _, err := Load("", filepath.Join(dir, "otro.key")); err != nil {
		t.Fatal(err)
	}
}

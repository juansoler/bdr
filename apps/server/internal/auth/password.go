package auth

import (
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"
)

// PasswordIterations es el número de iteraciones PBKDF2-SHA256 (OWASP 2023:
// 600 000). Los tests lo reducen.
var PasswordIterations = 600_000

// MinPasswordLength es la longitud mínima de contraseña.
const MinPasswordLength = 12

// ErrWeakPassword indica que la contraseña no cumple la política.
var ErrWeakPassword = fmt.Errorf("la contraseña debe tener al menos %d caracteres", MinPasswordLength)

// ValidatePassword aplica la política mínima de contraseñas.
func ValidatePassword(pw string) error {
	if utf8.RuneCountInString(pw) < MinPasswordLength {
		return ErrWeakPassword
	}
	if len(pw) > 1024 {
		return errors.New("la contraseña es demasiado larga")
	}
	return nil
}

// HashPassword devuelve "pbkdf2-sha256$iter$salt$hash".
func HashPassword(pw string) (string, error) {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	key, err := pbkdf2.Key(sha256.New, pw, salt, PasswordIterations, 32)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("pbkdf2-sha256$%d$%s$%s", PasswordIterations,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(key)), nil
}

// CheckPassword compara en tiempo constante.
func CheckPassword(encoded, pw string) bool {
	parts := strings.Split(encoded, "$")
	if len(parts) != 4 || parts[0] != "pbkdf2-sha256" {
		return false
	}
	iter, err := strconv.Atoi(parts[1])
	if err != nil || iter < 1 {
		return false
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[2])
	if err != nil {
		return false
	}
	want, err := base64.RawStdEncoding.DecodeString(parts[3])
	if err != nil {
		return false
	}
	got, err := pbkdf2.Key(sha256.New, pw, salt, iter, len(want))
	if err != nil {
		return false
	}
	return subtle.ConstantTimeCompare(got, want) == 1
}

// DummyPasswordCheck consume un tiempo similar a CheckPassword para no
// revelar si un usuario existe.
func DummyPasswordCheck(pw string) {
	_, _ = pbkdf2.Key(sha256.New, pw, []byte("bdr-dummy-salt!!"), PasswordIterations, 32)
}

package auth

import (
	"strings"
	"testing"
	"time"
)

func TestPasswordHashCheck(t *testing.T) {
	PasswordIterations = 1000
	t.Cleanup(func() { PasswordIterations = 600_000 })

	enc, err := HashPassword("una-clave-larga-1234")
	if err != nil {
		t.Fatal(err)
	}
	if !CheckPassword(enc, "una-clave-larga-1234") {
		t.Fatal("clave correcta rechazada")
	}
	if CheckPassword(enc, "otra-clave-larga-999") {
		t.Fatal("clave incorrecta aceptada")
	}
	if CheckPassword("basura", "x") || CheckPassword("pbkdf2-sha256$abc$x$y", "x") {
		t.Fatal("formatos inválidos aceptados")
	}
}

func TestValidatePassword(t *testing.T) {
	if err := ValidatePassword("corta123"); err == nil {
		t.Fatal("esperaba rechazo")
	}
	if err := ValidatePassword("clave-suficientemente-larga"); err != nil {
		t.Fatal(err)
	}
}

func TestTOTPRoundTrip(t *testing.T) {
	sec, err := NewTOTPSecret()
	if err != nil {
		t.Fatal(err)
	}
	if len(sec) != 32 {
		t.Fatalf("longitud secreto=%d", len(sec))
	}
	now := time.Unix(1_700_000_000, 0)
	step := TOTPStep(now)
	code, err := TOTPCode(sec, step)
	if err != nil || len(code) != 6 {
		t.Fatalf("code=%q err=%v", code, err)
	}
	got, ok := VerifyTOTP(sec, code, now, step-10)
	if !ok || got != step {
		t.Fatalf("código actual rechazado: %d %v", got, ok)
	}
	// ±30 s de tolerancia.
	next, _ := TOTPCode(sec, step+1)
	if _, ok := VerifyTOTP(sec, next, now.Add(30*time.Second), step-10); !ok {
		t.Fatal("código del paso siguiente rechazado")
	}
	// No reutilizar el mismo paso.
	if _, ok := VerifyTOTP(sec, code, now.Add(30*time.Second), step); ok {
		t.Fatal("código reutilizado aceptado")
	}
	if _, ok := VerifyTOTP(sec, "12345", now, 0); ok {
		t.Fatal("código de 5 dígitos aceptado")
	}
	if _, err := TOTPCode("no-base32!!", 1); err == nil {
		t.Fatal("secreto inválido aceptado")
	}
	if !strings.Contains(TOTPURL("BDR", "a@b.c", sec), "issuer=BDR") {
		t.Fatal("URL otpauth mal formada")
	}
}

func TestRecoveryCodes(t *testing.T) {
	codes, err := NewRecoveryCodes(10)
	if err != nil || len(codes) != 10 {
		t.Fatalf("codes=%v err=%v", codes, err)
	}
	seen := map[string]bool{}
	for _, c := range codes {
		if len(c) != 11 || c[5] != '-' {
			t.Fatalf("formato: %q", c)
		}
		if seen[c] {
			t.Fatal("código repetido")
		}
		seen[c] = true
	}
	if got := NormalizeRecoveryCode("ABCDE-FGHIJ"); got != "abcde-fghij" {
		t.Fatalf("normalizado: %q", got)
	}
	if got := NormalizeRecoveryCode("abcde fghij"); got != "abcde-fghij" {
		t.Fatalf("normalizado con espacios: %q", got)
	}
}

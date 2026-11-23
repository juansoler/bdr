package kopiacli

import (
	"runtime"
	"testing"
)

// TestCredentialFlags: --no-persist-credentials va siempre; --no-use-keychain
// solo en macOS (el CLI de kopia registra las flags del keychain únicamente
// en darwin — setupOSSpecificKeychainFlags en cli/app.go).
func TestCredentialFlags(t *testing.T) {
	flags := CredentialFlags()
	var hasKeychain, hasPersist bool
	for _, f := range flags {
		switch f {
		case "--no-use-keychain":
			hasKeychain = true
		case "--no-persist-credentials":
			hasPersist = true
		}
	}
	if !hasPersist {
		t.Fatal("falta --no-persist-credentials")
	}
	if hasKeychain != (runtime.GOOS == "darwin") {
		t.Fatalf("--no-use-keychain=%v con GOOS=%s (el binario de kopia de ese SO %s el flag)",
			hasKeychain, runtime.GOOS,
			map[bool]string{true: "sí tiene", false: "no tiene"}[runtime.GOOS == "darwin"])
	}
}

// Package kopiacli construye argumentos comunes para invocar el CLI de
// kopia desde el servidor y el agente.
package kopiacli

import "runtime"

// CredentialFlags devuelve las flags de persistencia de credenciales que
// acepta el binario de kopia del sistema en curso.
//
// --no-use-keychain SOLO existe en los builds de kopia para macOS: el CLI
// la registra en setupOSSpecificKeychainFlags (cli/app.go) únicamente en
// darwin, así que en Linux hay que omitirla o kopia falla con
// "unknown long flag '--no-use-keychain'".
// --no-persist-credentials es un flag global y existe en todos los SO.
func CredentialFlags() []string {
	if runtime.GOOS == "darwin" {
		return []string{"--no-use-keychain", "--no-persist-credentials"}
	}
	return []string{"--no-persist-credentials"}
}

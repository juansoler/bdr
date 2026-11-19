// update-sign firma y genera claves para el canal de actualizaciones.
//
// latest.json se firma con Ed25519 (clave privada offline en el entorno de
// release); el binario verifica con la clave pública embebida en
// packages/updatekey. Sin clave pública embebida el chequeo de actualizaciones
// queda deshabilitado (fallo cerrado).
//
// Uso:
//
//	go run ./tools/update-sign keygen -out clave.private.pem   # imprimirá la pública
//	go run ./tools/update-sign sign -key clave.private.pem -in latest.json
//	  → escribe latest.json.sig (firma Ed25519 en base64)
package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"flag"
	"fmt"
	"os"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	switch os.Args[1] {
	case "keygen":
		cmdKeygen(os.Args[2:])
	case "sign":
		cmdSign(os.Args[2:])
	case "help", "-h", "--help":
		usage()
	default:
		fmt.Fprintf(os.Stderr, "subcomando desconocido: %s\n", os.Args[1])
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, `update-sign — firma del canal de actualizaciones

  keygen -out CLAVE.private.pem        genera Ed25519 y guarda PKCS8; imprime la pública
  sign   -key CLAVE.private.pem -in latest.json
                                       escribe latest.json.sig (Ed25519 b64)`)
}

func cmdKeygen(args []string) {
	fs := flag.NewFlagSet("keygen", flag.ExitOnError)
	out := fs.String("out", "", "ruta del fichero de clave privada PKCS8 (no se publica)")
	_ = fs.Parse(args)
	if *out == "" {
		fmt.Fprintln(os.Stderr, "falta -out")
		fs.Usage()
		os.Exit(2)
	}
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		fatal(err)
	}
	der, err := x509.MarshalPKCS8PrivateKey(priv)
	if err != nil {
		fatal(err)
	}
	pemBlock := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})
	if err := os.WriteFile(*out, pemBlock, 0o600); err != nil {
		fatal(err)
	}
	fmt.Println("# clave privada:", *out)
	fmt.Println("# pubícula (base64) para packages/updatekey/key.go:")
	fmt.Println(base64.StdEncoding.EncodeToString(pub))
}

func cmdSign(args []string) {
	fs := flag.NewFlagSet("sign", flag.ExitOnError)
	key := fs.String("key", "", "clave privada PKCS8 Ed25519")
	in := fs.String("in", "", "fichero a firmar (latest.json)")
	out := fs.String("out", "", "firma de salida (por defecto <in>.sig)")
	_ = fs.Parse(args)
	if *key == "" || *in == "" {
		fmt.Fprintln(os.Stderr, "faltan -key o -in")
		fs.Usage()
		os.Exit(2)
	}
	if *out == "" {
		*out = *in + ".sig"
	}
	pemBytes, err := os.ReadFile(*key)
	if err != nil {
		fatal(err)
	}
	block, _ := pem.Decode(pemBytes)
	if block == nil {
		fatal(fmt.Errorf("PEM inválido en %s", *key))
	}
	generic, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		fatal(err)
	}
	priv, ok := generic.(ed25519.PrivateKey)
	if !ok {
		fatal(fmt.Errorf("la clave no es Ed25519"))
	}
	data, err := os.ReadFile(*in)
	if err != nil {
		fatal(err)
	}
	sig := ed25519.Sign(priv, data)
	if err := os.WriteFile(*out, []byte(base64.StdEncoding.EncodeToString(sig)+"\n"), 0o644); err != nil {
		fatal(err)
	}
	fmt.Println("firma:", *out)
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "error:", err)
	os.Exit(1)
}

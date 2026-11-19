// Package updatekey contiene la clave pública que verifica el canal de
// actualizaciones (latest.json firmado con Ed25519).
//
// La clave privada vive OFFLINE en el entorno de release (nunca en este
// repositorio ni en los servidores). Se genera y firma con:
//
//	go run ./tools/update-sign keygen  -out clave.private.pem
//	go run ./tools/update-sign sign   -key clave.private.pem -in latest.json
//
// Si sustituyes el par de claves, actualiza la constante y publica la
// primera versión firmada ANTES de dejar de aceptar la anterior (los
// binarios viejos solo verifican con la clave que llevan embebida).
package updatekey

// PublicKeyBase64 es la clave pública Ed25519 (32 bytes, base64 estándar).
// Con cadena vacía el chequeo de actualizaciones queda deshabilitado:
// los binarios nunca aplican ni notifican manifiestos sin firma verificable.
const PublicKeyBase64 = "bvwhVRfhnL18ZD6uKTy6bsKGP3Msk7GBJX/luYXao34="

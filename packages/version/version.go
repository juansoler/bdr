// Package version contiene la versión del producto.
// Se puede sobrescribir en compilación con:
//
//	go build -ldflags "-X github.com/juansoler/bdr/packages/version.Version=1.2.3" ...
package version

// Version es la versión del servidor y del agente.
var Version = "0.16.9"

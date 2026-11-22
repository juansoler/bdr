//go:build windows

// vnc_windows.go atiende el escritorio local para sesiones de pantalla en
// Windows. Windows no trae servidor VNC de serie: el equipo debe tener uno
// instalado y configurado para escuchar SOLO en localhost (TigerVNC,
// UltraVNC, TightVNC…). El agente nunca abre el puerto hacia la red: todo el
// tráfico va por el túnel saliente del agente.
package remoteaccess

import (
	"errors"
	"net"
)

// vncAddr es la dirección del servidor VNC local (solo loopback).
const vncAddr = "127.0.0.1:5900"

// openLocalVNC conecta con el servidor VNC local del equipo.
func openLocalVNC(allowClipboard bool) (*localVNC, error) {
	_ = allowClipboard // sincronización de portapapeles gestionada por el visor

	conn, err := net.DialTimeout("tcp", vncAddr, vncDialTimeout)
	if err != nil {
		return nil, errors.New(
			"no hay servidor VNC en " + vncAddr + ": instala un servidor VNC " +
				"(TigerVNC, UltraVNC…) configurado para escuchar solo en localhost")
	}
	return &localVNC{conn: conn}, nil
}

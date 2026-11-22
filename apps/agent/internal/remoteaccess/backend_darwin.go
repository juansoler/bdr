//go:build darwin

// backend_darwin.go selecciona el backend de pantalla en macOS: bridge cliente
// VNC contra el «Compartir pantalla» integrado del sistema (127.0.0.1:5900).
// macOS no exige instalar nada: activar el toggle del sistema basta. La
// captura nativa (CoreGraphics) exigiría CGO o permisos TCC de grabación para
// el binario del servicio — se documenta como futura mejora.
package remoteaccess

// newScreenBackend crea el backend para una sesión de pantalla en macOS.
func newScreenBackend() (screenBackend, error) {
	return newRFBBridgeBackend(vncLocalAddr)
}

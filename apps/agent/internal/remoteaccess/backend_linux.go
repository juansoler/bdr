//go:build linux

// backend_linux.go selecciona el backend de pantalla en Linux:
//  1. Captura X11 nativa (XGetImage + XTEST vía jezek/xgb, sin CGO) — no
//     requiere instalar nada en equipos con sesión X11.
//  2. Fallback: lanzar x11vnc bajo demanda (solo loopback) y puentearlo con
//     el cliente RFB — cubre configuraciones donde X11 no es accesible para
//     el agente (XAUTHORITY raro,Wayland con x11vnc sobre XWayland…).
package remoteaccess

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// newScreenBackend crea el backend para una sesión de pantalla en Linux.
func newScreenBackend() (screenBackend, error) {
	b, err := newX11Backend()
	if err == nil {
		return b, nil
	}
	// Fallback: x11vnc + bridge (reutiliza el puente de macOS).
	proc, serr := startX11VNC()
	if serr != nil {
		return nil, fmt.Errorf("sin captura X11 (%v) y sin x11vnc: %w", err, serr)
	}
	bridge, berr := newRFBBridgeBackend(vncLocalAddr)
	if berr != nil {
		stopProcess(proc)
		return nil, fmt.Errorf("x11vnc no abrió %s a tiempo: %w", vncLocalAddr, berr)
	}
	return &bridgedBackend{rfbBridgeBackend: bridge, stopVNC: func() { stopProcess(proc) }}, nil
}

// bridgedBackend añade la limpieza del helper x11vnc al puente.
type bridgedBackend struct {
	*rfbBridgeBackend
	stopVNC func()
}

// Close termina también el x11vnc lanzado.
func (b *bridgedBackend) Close() {
	b.rfbBridgeBackend.Close()
	b.stopVNC()
}

// startX11VNC lanza x11vnc en modo solo-loopback y sin contraseña (el canal
// del agente ya autentica extremo a extremo).
func startX11VNC() (*os.Process, error) {
	bin, err := exec.LookPath("x11vnc")
	if err != nil {
		return nil, fmt.Errorf("x11vnc no está instalado: %w", err)
	}
	display := os.Getenv("DISPLAY")
	if display == "" {
		display = ":0"
	}
	cmd := exec.Command(bin,
		"-display", display,
		"-localhost", // solo loopback: el túnel del agente es la única vía de entrada
		"-nopw",      // sin contraseña VNC: autentica el canal del agente
		"-shared",
		"-forever",
		"-noxdamage",
		"-quiet",
	)
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("no se pudo lanzar x11vnc: %w", err)
	}
	return cmd.Process, nil
}

// stopProcess termina un helper lanzado por el agente.
func stopProcess(p *os.Process) {
	if p == nil {
		return
	}
	_ = p.Kill()
	_, _ = p.Wait()
}

// xauthorityCandidates devuelve rutas habituales del cookie X para el agente
// corriendo como servicio (sin XAUTHORITY en el entorno).
func xauthorityCandidates() []string {
	var out []string
	if v := os.Getenv("XAUTHORITY"); v != "" {
		out = append(out, v)
	}
	out = append(out, "/root/.Xauthority")
	if home, err := os.UserHomeDir(); err == nil && home != "" && home != "/root" {
		out = append(out, filepath.Join(home, ".Xauthority"))
	}
	matches, _ := filepath.Glob("/home/*/.Xauthority")
	out = append(out, matches...)
	out = append(out,
		"/var/lib/gdm/.Xauthority",
		"/var/lib/lightdm/.Xauthority",
		"/run/user/1000/.mutter-Xauthority",
		"/run/lightdm.pid/xauthority",
	)
	return out
}

// errNoX11 indica que no hay servidor X accesible.
var errNoX11 = errors.New("no hay servidor X accesible (¿Wayland o sesión cerrada?)")

//go:build !windows

// vnc_unix.go atiende el escritorio local para sesiones de pantalla en
// Linux y macOS:
//
//   - macOS: usa el «Compartir pantalla» nativo (servidor VNC integrado del
//     sistema). Si no está activado, error con instrucción clara.
//   - Linux: si ya hay un VNC en localhost lo reutiliza; si no, lanza x11vnc
//     bajo demanda contra la sesión X activa (solo loopback, sin contraseña:
//     el canal agente-servidor ya está autenticado con mTLS + API key).
//
// Nunca se escucha en la interfaz de red: todo el tráfico va por el túnel
// saliente del agente.
package remoteaccess

import (
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"runtime"
	"time"
)

// vncAddr es la dirección del servidor VNC local (solo loopback).
const vncAddr = "127.0.0.1:5900"

// vncHelperWait es el tiempo máximo de espera a que el helper abra el puerto.
const vncHelperWait = 8 * time.Second

// openLocalVNC conecta con un servidor VNC local, lanzando un helper si hace
// falta y la plataforma lo permite.
func openLocalVNC(allowClipboard bool) (*localVNC, error) {
	_ = allowClipboard // sincronización de portapapeles gestionada por el visor

	// 1. ¿Ya hay un VNC en localhost? (Compartir pantalla de macOS, x11vnc
	//    arrancado por el usuario, VNC de administración…)
	if conn, err := net.DialTimeout("tcp", vncAddr, vncDialTimeout); err == nil {
		return &localVNC{conn: conn}, nil
	}

	if runtime.GOOS == "linux" {
		// 2. Linux: lanzar x11vnc contra la sesión X activa.
		helper, err := startX11VNC()
		if err != nil {
			return nil, fmt.Errorf(
				"no hay servidor VNC en %s y no se pudo lanzar x11vnc: %w", vncAddr, err)
		}
		conn, err := waitVNCPort(vncHelperWait)
		if err != nil {
			stopProcess(helper)
			return nil, fmt.Errorf(
				"x11vnc no abrió %s a tiempo (¿hay sesión gráfica activa?): %w", vncAddr, err)
		}
		return &localVNC{
			conn: conn,
			stop: func() { stopProcess(helper) },
		}, nil
	}

	// 3. macOS: sin helper automático; el VNC integrado hay que activarlo.
	return nil, errors.New(
		"no hay servidor VNC en " + vncAddr + ": activa «Compartir pantalla» " +
			"en Ajustes del Sistema → General → Compartir y reintenta")
}

// startX11VNC lanza x11vnc en modo solo-loopback y sin contraseña (el canal
// del agente ya autentica extremo a extremo). Devuelve el proceso para poder
// terminarlo al cerrar la sesión.
func startX11VNC() (*os.Process, error) {
	bin, err := exec.LookPath("x11vnc")
	if err != nil {
		return nil, fmt.Errorf("x11vnc no está instalado: %w", err)
	}
	display := os.Getenv("DISPLAY")
	if display == "" {
		display = ":0" // sesión gráfica típica del usuario de consola
	}
	cmd := exec.Command(bin,
		"-display", display,
		"-localhost", // solo loopback: el túnel del agente es la única vía de entrada
		"-nopw",      // sin contraseña VNC: autentica el canal del agente, no el puerto
		"-shared",
		"-forever", // sigue atendiendo si el visor se reconecta
		"-noxdamage",
		"-quiet",
	)
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("no se pudo lanzar x11vnc: %w", err)
	}
	return cmd.Process, nil
}

// waitVNCPort reintenta la conexión contra el VNC local hasta que abre.
func waitVNCPort(max time.Duration) (net.Conn, error) {
	deadline := time.Now().Add(max)
	for {
		conn, err := net.DialTimeout("tcp", vncAddr, vncDialTimeout)
		if err == nil {
			return conn, nil
		}
		if time.Now().After(deadline) {
			return nil, err
		}
		time.Sleep(250 * time.Millisecond)
	}
}

// stopProcess termina un helper lanzado por el agente.
func stopProcess(p *os.Process) {
	if p == nil {
		return
	}
	_ = p.Kill()
	_, _ = p.Wait()
}

//go:build unix

// Package remoteaccess implementa la terminal Unix (Linux y macOS) usando
// creack/pty. Crea una pseudoterminal, inicia el shell especificado y expone
// operaciones de lectura/escritura sobre el descriptor maestro.
package remoteaccess

import (
	"os"
	"os/exec"
	"syscall"

	"github.com/creack/pty"
)

// ShellPath devuelve la ruta del binario del shell según la plataforma y el
// nombre lógico recibido del servidor.
func ShellPath(shell string) (string, bool) {
	switch shell {
	case "bash", "":
		if p, err := exec.LookPath("bash"); err == nil {
			return p, true
		}
		return "/bin/bash", true
	case "zsh":
		if p, err := exec.LookPath("zsh"); err == nil {
			return p, true
		}
		return "/bin/zsh", true
	case "sh":
		if p, err := exec.LookPath("sh"); err == nil {
			return p, true
		}
		return "/bin/sh", true
	default:
		return "", false
	}
}

// StartShell abre una pseudoterminal con las dimensiones dadas y arranca el
// shell especificado. La sesión resultante expone PTY/In/Out (el maestro de
// la PTY sirve para ambas direcciones), Resize y el comando en Cmd.
func StartShell(shell string, cols, rows uint16) (*PTYSession, error) {
	if cols == 0 {
		cols = 80
	}
	if rows == 0 {
		rows = 24
	}

	path, ok := ShellPath(shell)
	if !ok {
		// Intenta con el shell por defecto del sistema.
		path = os.Getenv("SHELL")
		if path == "" {
			path = "/bin/sh"
		}
	}

	cmd := exec.Command(path)
	cmd.Env = append(os.Environ(),
		"TERM=xterm-256color",
	)

	ws := &pty.Winsize{Rows: rows, Cols: cols}
	master, err := pty.StartWithSize(cmd, ws)
	if err != nil {
		return nil, err
	}

	return &PTYSession{
		PTY: master,
		In:  master,
		Out: master,
		Cmd: cmd,
		Resize: func(c, r uint16) {
			_ = ResizeTerminal(master, c, r)
		},
	}, nil
}

// ResizeTerminal cambia las dimensiones de la PTY indicada.
func ResizeTerminal(master *os.File, cols, rows uint16) error {
	if cols == 0 {
		cols = 80
	}
	if rows == 0 {
		rows = 24
	}
	return pty.Setsize(master, &pty.Winsize{Rows: rows, Cols: cols})
}

// SignalToString mapea un nombre de señal a su equivalente unix.
func SignalToString(sig string) string {
	switch sig {
	case "INT":
		return "SIGINT"
	case "TERM":
		return "SIGTERM"
	case "KILL":
		return "SIGKILL"
	case "HUP":
		return "SIGHUP"
	case "USR1":
		return "SIGUSR1"
	case "USR2":
		return "SIGUSR2"
	default:
		return sig
	}
}

// signalFromName traduce un nombre de señal a su equivalente unix.
func signalFromName(name string) os.Signal {
	switch name {
	case "INT":
		return syscall.SIGINT
	case "TERM":
		return syscall.SIGTERM
	case "KILL":
		return syscall.SIGKILL
	case "HUP":
		return syscall.SIGHUP
	case "USR1":
		return syscall.SIGUSR1
	case "USR2":
		return syscall.SIGUSR2
	default:
		return nil
	}
}

// Package remoteaccess implementa la terminal Unix (Linux y macOS) usando
// creack/pty. Crea una pseudoterminal, inicia el shell especificado y expone
// operaciones de lectura/escritura sobre el descriptor maestro.
package remoteaccess

import (
	"os"
	"os/exec"

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
// shell especificado. Devuelve el descriptor maestro de la PTY (lectura/escritura)
// y el comando del proceso.
func StartShell(shell string, cols, rows uint16) (*os.File, *exec.Cmd, error) {
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
		return nil, nil, err
	}

	return master, cmd, nil
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
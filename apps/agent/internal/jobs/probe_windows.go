//go:build windows

package jobs

import "syscall"

// probeSysProcAttr oculta la ventana de consola de los sondeos PowerShell.
func probeSysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{HideWindow: true}
}

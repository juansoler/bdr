//go:build !windows

package jobs

import "syscall"

func probeSysProcAttr() *syscall.SysProcAttr {
	return nil
}

//go:build windows

// terminal_windows.go implementa la terminal remota en Windows con ConPTY
// (CreatePseudoConsole, kernel32): pipes de entrada/salida del pseudoconsole,
// shell dentro de un proceso con el atributo PSEUDOCONSOLE, resize en vivo y
// código de salida del shell.
//
// El servicio corre en sesión 0: el shell de la terminal es SYSTEM en esa
// sesión (equivalente al shell root de los agentes Linux — operación
// administrativa por diseño).
package remoteaccess

import (
	"fmt"
	"os"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	kernel32DLL              = windows.NewLazySystemDLL("kernel32.dll")
	procCreatePseudoConsole  = kernel32DLL.NewProc("CreatePseudoConsole")
	procResizePseudoConsole  = kernel32DLL.NewProc("ResizePseudoConsole")
	procClosePseudoConsole   = kernel32DLL.NewProc("ClosePseudoConsole")
	procInitProcThreadAttr   = kernel32DLL.NewProc("InitializeProcThreadAttributeList")
	procUpdateProcThreadAttr = kernel32DLL.NewProc("UpdateProcThreadAttribute")
	procDeleteProcThreadAttr = kernel32DLL.NewProc("DeleteProcThreadAttributeList")
)

const (
	procThreadAttrPseudoConsole = 0x00020016
	extendedStartupInfoPresent  = 0x00080000
	infinite                    = 0xFFFFFFFF
)

// startupInfoEx es STARTUPINFOEX de Win32 (Go mirror: STARTUPINFO + puntero
// de lista de atributos; el tamaño lo calcula unsafe.Sizeof).
type startupInfoEx struct {
	si       windows.StartupInfo
	attrList *byte
}

// coord es COORD de Win32 (dimensiones del pseudoconsole).
type coord struct{ X, Y int16 }

// ShellPath devuelve la línea de comando del shell según el nombre lógico.
// En Windows, la consola de operación es PowerShell (cmd también disponible).
func shellCommandLine(shell string) (string, bool) {
	switch shell {
	case "powershell", "default", "":
		return "powershell.exe -NoLogo", true
	case "cmd":
		return "cmd.exe", true
	default:
		return "", false
	}
}

// StartShell crea un pseudoconsole ConPTY con las dimensiones dadas y lanza
// el shell dentro de él. La sesión resultante expone In/Out (pipes del
// pseudoconsole), Resize, WaitExit y Terminate.
func StartShell(shell string, cols, rows uint16) (*PTYSession, error) {
	if cols == 0 {
		cols = 120
	}
	if rows == 0 {
		rows = 36
	}
	cmdline, ok := shellCommandLine(shell)
	if !ok {
		return nil, fmt.Errorf("shell %q no disponible en Windows (usa default, powershell o cmd)", shell)
	}
	cmd16, err := windows.UTF16PtrFromString(cmdline)
	if err != nil {
		return nil, err
	}

	// 1. Pipes del pseudoconsole: escribimos la entrada del operador en
	//    ptyInWrite y leemos la salida del shell de ptyOutRead.
	var ptyInRead, ptyInWrite, ptyOutRead, ptyOutWrite windows.Handle
	if err := windows.CreatePipe(&ptyInRead, &ptyInWrite, nil, 0); err != nil {
		return nil, fmt.Errorf("ConPTY: pipe de entrada: %w", err)
	}
	if err := windows.CreatePipe(&ptyOutRead, &ptyOutWrite, nil, 0); err != nil {
		_ = windows.CloseHandle(ptyInRead)
		_ = windows.CloseHandle(ptyInWrite)
		return nil, fmt.Errorf("ConPTY: pipe de salida: %w", err)
	}

	// 2. CreatePseudoConsole se queda sus propias referencias a los extremos
	//    del hijo; los nuestros sobran.
	var hpc windows.Handle
	size := coord{X: int16(cols), Y: int16(rows)}
	r1, _, errPC := procCreatePseudoConsole.Call(
		uintptr(uint16(size.X))|uintptr(uint16(size.Y))<<16,
		uintptr(ptyInRead), uintptr(ptyOutWrite), 0, uintptr(unsafe.Pointer(&hpc)))
	_ = windows.CloseHandle(ptyInRead)
	_ = windows.CloseHandle(ptyOutWrite)
	if r1 != 0 || hpc == 0 {
		_ = windows.CloseHandle(ptyInWrite)
		_ = windows.CloseHandle(ptyOutRead)
		return nil, fmt.Errorf("ConPTY: CreatePseudoConsole falló: %v", errPC)
	}

	// 3. Lista de atributos con el pseudoconsole (obligatorio para que el
	//    hijo nazca atado a él).
	var attrSize uintptr
	procInitProcThreadAttr.Call(0, 1, 0, uintptr(unsafe.Pointer(&attrSize)))
	attr := make([]byte, attrSize)
	if r2, _, err := procInitProcThreadAttr.Call(
		uintptr(unsafe.Pointer(&attr[0])), 1, 0, uintptr(unsafe.Pointer(&attrSize))); r2 == 0 {
		procClosePseudoConsole.Call(uintptr(hpc))
		_ = windows.CloseHandle(ptyInWrite)
		_ = windows.CloseHandle(ptyOutRead)
		return nil, fmt.Errorf("ConPTY: InitializeProcThreadAttributeList: %v", err)
	}
	siEx := startupInfoEx{si: windows.StartupInfo{Cb: uint32(unsafe.Sizeof(startupInfoEx{}))}}
	siEx.attrList = &attr[0]
	if r3, _, err := procUpdateProcThreadAttr.Call(
		uintptr(unsafe.Pointer(&attr[0])), 0, procThreadAttrPseudoConsole,
		uintptr(hpc), unsafe.Sizeof(hpc), 0, 0); r3 == 0 {
		procDeleteProcThreadAttr.Call(uintptr(unsafe.Pointer(&attr[0])))
		procClosePseudoConsole.Call(uintptr(hpc))
		_ = windows.CloseHandle(ptyInWrite)
		_ = windows.CloseHandle(ptyOutRead)
		return nil, fmt.Errorf("ConPTY: UpdateProcThreadAttribute: %v", err)
	}

	// 4. El shell, atado al pseudoconsole.
	pi := &windows.ProcessInformation{}
	if err := windows.CreateProcess(nil, cmd16, nil, nil, false,
		extendedStartupInfoPresent, nil, nil, &siEx.si, pi); err != nil {
		procDeleteProcThreadAttr.Call(uintptr(unsafe.Pointer(&attr[0])))
		procClosePseudoConsole.Call(uintptr(hpc))
		_ = windows.CloseHandle(ptyInWrite)
		_ = windows.CloseHandle(ptyOutRead)
		return nil, fmt.Errorf("ConPTY: CreateProcess(%s): %w", cmdline, err)
	}
	_ = windows.CloseHandle(pi.Thread)

	ps := &PTYSession{
		In:  &handleWriter{h: ptyInWrite},
		Out: &handleReader{h: ptyOutRead},
		Resize: func(c, r uint16) {
			procResizePseudoConsole.Call(uintptr(hpc),
				uintptr(uint16(c))|uintptr(uint16(r))<<16)
		},
		WaitExit: func() int {
			_, _ = windows.WaitForSingleObject(pi.Process, infinite)
			var code uint32
			_ = windows.GetExitCodeProcess(pi.Process, &code)
			_ = windows.CloseHandle(pi.Process)
			procDeleteProcThreadAttr.Call(uintptr(unsafe.Pointer(&attr[0])))
			return int(int32(code))
		},
		Terminate: func() {
			_ = windows.TerminateProcess(pi.Process, 1)
			procClosePseudoConsole.Call(uintptr(hpc))
		},
	}
	return ps, nil
}

// signalFromName mapea un nombre de señal a su equivalente en Windows.
// Solo INT (console Ctrl-C) está soportada; el resto devuelve nil y el
// emisor lo ignora (KILL/TERM van por PTYSession.Terminate).
func signalFromName(name string) os.Signal {
	if name == "INT" {
		return os.Interrupt
	}
	return nil
}

// rfbclient.go implementa un cliente RFB (VNC) mínimo, usado como *bridge*:
// el agente se conecta a un servidor VNC local ya existente (macOS «Compartir
// pantalla», o x11vnc como fallback de Linux), mantiene el framebuffer en
// memoria con codificación RAW (rápido por loopback) y lo re-codifica en
// tiles JPEG para el navegador. La entrada del operador se traduce a eventos
// RFB (que usan keysyms nativamente).
//
// Solo se implementan los security types 1 (None) y 2 (VNC auth DES) — los
// tipos propietarios de Apple (30/35) quedan fuera: la única vía soportada es
// el VNC clásico en loopback.
package remoteaccess

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"time"
)

// rfbClient es un cliente RFB conectado y negociado.
type rfbClient struct {
	conn       net.Conn
	w, h       int
	fb         []byte // framebuffer BGRA (w*h*4)
	serverName string
}

// dialRFB conecta y negocia la sesión (RFB 3.8, RAW, 32bpp BGRX).
func dialRFB(addr string, timeout time.Duration) (*rfbClient, error) {
	conn, err := net.DialTimeout("tcp", addr, timeout)
	if err != nil {
		return nil, err
	}
	_ = conn.SetDeadline(time.Now().Add(10 * time.Second))

	// 1. ProtocolVersion: el servidor manda "RFB xxx.yyy\n".
	ver := make([]byte, 12)
	if _, err := io.ReadFull(conn, ver); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("RFB: sin saludo: %w", err)
	}
	if _, err := conn.Write([]byte("RFB 003.008\n")); err != nil {
		_ = conn.Close()
		return nil, err
	}

	// 2. Security types (3.8): [u8 n][n tipos].
	var nSec [1]byte
	if _, err := io.ReadFull(conn, nSec[:]); err != nil {
		_ = conn.Close()
		return nil, err
	}
	if nSec[0] == 0 {
		// error del servidor: [u32 len][motivo]
		var l [4]byte
		_, _ = io.ReadFull(conn, l[:])
		msg := make([]byte, binary.BigEndian.Uint32(l[:]))
		_, _ = io.ReadFull(conn, msg)
		_ = conn.Close()
		return nil, fmt.Errorf("RFB: servidor rechazó la conexión: %s", msg)
	}
	secs := make([]byte, nSec[0])
	if _, err := io.ReadFull(conn, secs); err != nil {
		_ = conn.Close()
		return nil, err
	}
	var chosen byte
	for _, s := range secs {
		if s == 1 { // None
			chosen = 1
			break
		}
	}
	if chosen == 0 {
		for _, s := range secs {
			if s == 2 { // VNC auth (sin contraseña no funciona; error claro)
				_ = conn.Close()
				return nil, errors.New(
					"el servidor VNC pide contraseña y el agente no tiene credenciales: " +
						"en macOS desactiva «los visores VNC pueden controlar con contraseña» o " +
						"conecta por un usuario local; en Linux usa x11vnc -nopw")
			}
		}
		_ = conn.Close()
		return nil, fmt.Errorf("RFB: solo security types no soportados (%v)", secs)
	}
	if _, err := conn.Write([]byte{chosen}); err != nil {
		_ = conn.Close()
		return nil, err
	}
	if chosen == 2 {
		// VNC auth: [16 challenge] → responder 16 bytes DES con la contraseña.
		_ = conn.Close()
		return nil, errors.New("VNC auth con contraseña no soportado en el bridge (usa None en loopback)")
	}
	// SecurityResult (solo si >= RFB 3.8 y type != None).
	var res [4]byte
	if _, err := io.ReadFull(conn, res[:]); err != nil {
		_ = conn.Close()
		return nil, err
	}
	if binary.BigEndian.Uint32(res[:]) != 0 {
		_ = conn.Close()
		return nil, errors.New("RFB: fallo de autenticación")
	}

	// 3. ClientInit (shared=true).
	if _, err := conn.Write([]byte{1}); err != nil {
		_ = conn.Close()
		return nil, err
	}

	// 4. ServerInit: [u16 w][u16 h][PIXELFORMAT 16][u32 nameLen][name].
	hdr := make([]byte, 24)
	if _, err := io.ReadFull(conn, hdr); err != nil {
		_ = conn.Close()
		return nil, err
	}
	c := &rfbClient{conn: conn}
	c.w = int(binary.BigEndian.Uint16(hdr[0:2]))
	c.h = int(binary.BigEndian.Uint16(hdr[2:4]))
	nameLen := binary.BigEndian.Uint32(hdr[20:24])
	name := make([]byte, min(int(nameLen), 4096))
	if _, err := io.ReadFull(conn, name); err != nil {
		_ = conn.Close()
		return nil, err
	}
	c.serverName = string(name)
	if c.w <= 0 || c.h <= 0 || c.w > 16384 || c.h > 16384 {
		_ = conn.Close()
		return nil, fmt.Errorf("RFB: tamaño de framebuffer inválido %dx%d", c.w, c.h)
	}
	c.fb = make([]byte, c.w*c.h*4)

	// 5. SetPixelFormat: pedimos BGRX 32bpp (igual que GDI/X11: coincide con
	//    TileSource sin conversión).
	pf := make([]byte, 16)
	pf[0] = 32                                   // bitsPerPixel
	pf[1] = 24                                   // depth
	pf[2] = 0                                    // bigEndian=false
	pf[3] = 1                                    // trueColor
	binary.BigEndian.PutUint16(pf[4:6], 255)     // redMax
	binary.BigEndian.PutUint16(pf[6:8], 255)     // greenMax
	binary.BigEndian.PutUint16(pf[8:10], 255)    // blueMax
	pf[10] = 16                                  // redShift
	pf[11] = 8                                   // greenShift
	pf[12] = 0                                   // blueShift
	if err := c.sendMessage(0, pf); err != nil { // SetPixelFormat = 0
		_ = conn.Close()
		return nil, err
	}

	// 6. SetEncodings: solo RAW (rápido en loopback).
	enc := make([]byte, 6)
	binary.BigEndian.PutUint16(enc[0:2], 1)       // número de encodings
	binary.BigEndian.PutUint32(enc[2:6], 0)       // Raw = 0
	if err := c.sendMessage(2, enc); err != nil { // SetEncodings = 2
		_ = conn.Close()
		return nil, err
	}

	_ = conn.SetDeadline(time.Time{}) // a partir de aquí, sin deadline
	return c, nil
}

// sendMessage envía [u8 tipo][payload] con cabecera de 4 bytes RFB.
func (c *rfbClient) sendMessage(msgType byte, payload []byte) error {
	hdr := []byte{msgType, 0, 0, 0}
	if msgType == 2 { // SetEncodings lleva u16 de padding distinto
		hdr = []byte{msgType, 0}
	}
	if _, err := c.conn.Write(hdr); err != nil {
		return err
	}
	_, err := c.conn.Write(payload)
	return err
}

// RequestUpdate pide un update (full=true la primera vez, luego incremental).
func (c *rfbClient) RequestUpdate(full bool) error {
	req := make([]byte, 10)
	req[0] = 3 // FramebufferUpdateRequest
	if full {
		req[1] = 0
	} else {
		req[1] = 1
	}
	binary.BigEndian.PutUint16(req[2:4], 0)
	binary.BigEndian.PutUint16(req[4:6], uint16(c.w))
	binary.BigEndian.PutUint16(req[6:8], uint16(c.h))
	_, err := c.conn.Write(req)
	return err
}

// ReadUpdate lee un FramebufferUpdate y aplica los rects RAW al framebuffer.
func (c *rfbClient) ReadUpdate() error {
	hdr := make([]byte, 4)
	if _, err := io.ReadFull(c.conn, hdr); err != nil {
		return err
	}
	if hdr[0] != 0 { // FramebufferUpdate = 0
		return fmt.Errorf("RFB: mensaje inesperado tipo %d", hdr[0])
	}
	nRects := binary.BigEndian.Uint16(hdr[2:4])
	for i := 0; i < int(nRects); i++ {
		rect := make([]byte, 12)
		if _, err := io.ReadFull(c.conn, rect); err != nil {
			return err
		}
		rx := int(binary.BigEndian.Uint16(rect[0:2]))
		ry := int(binary.BigEndian.Uint16(rect[2:4]))
		rw := int(binary.BigEndian.Uint16(rect[4:6]))
		rh := int(binary.BigEndian.Uint16(rect[6:8]))
		enc := binary.BigEndian.Uint32(rect[8:12])
		if enc != 0 {
			return fmt.Errorf("RFB: encoding %d no soportado (esperado RAW)", enc)
		}
		if rw == 0 || rh == 0 || rx+rw > c.w || ry+rh > c.h {
			return errors.New("RFB: rect fuera de rango")
		}
		for yy := 0; yy < rh; yy++ {
			row := make([]byte, rw*4)
			if _, err := io.ReadFull(c.conn, row); err != nil {
				return err
			}
			dst := (ry+yy)*c.w*4 + rx*4
			copy(c.fb[dst:dst+rw*4], row)
		}
	}
	return nil
}

// SendKey envía un KeyEvent RFB (keysym nativo del protocolo).
func (c *rfbClient) SendKey(down bool, keysym uint32) error {
	m := make([]byte, 8)
	m[0] = 4 // KeyEvent
	if down {
		m[1] = 1
	}
	binary.BigEndian.PutUint32(m[4:8], keysym)
	_, err := c.conn.Write(m)
	return err
}

// SendPointer envía un PointerEvent RFB.
func (c *rfbClient) SendPointer(buttons byte, x, y uint16) error {
	m := make([]byte, 6)
	m[0] = 5 // PointerEvent
	m[1] = buttons
	binary.BigEndian.PutUint16(m[2:4], x)
	binary.BigEndian.PutUint16(m[4:6], y)
	_, err := c.conn.Write(m)
	return err
}

// Close cierra la conexión.
func (c *rfbClient) Close() error { return c.conn.Close() }

// Package remoteaccess implementa el gestor de sesiones de terminal remota
// del plano de control: registro de agentes conectados, ciclo de vida de
// sesiones, relay WebSocket bidireccional y auditoría.
//
// El agente siempre inicia la conexión (canal saliente autenticado con mTLS y
// API key); el servidor solo autoriza y retransmite. No hay puertos
// entrantes en los equipos protegidos.
package remoteaccess

import (
	"encoding/binary"
	"errors"
	"fmt"
)

// sessionPrefixLen es el tamaño del prefijo de ID de sesión (uint16 BE) en un
// frame binario del canal agente-servidor.
const sessionPrefixLen = 2

// maxSessionIDLen acota el ID de sesión aceptado en un frame binario.
const maxSessionIDLen = 64

// errMalformedFrame indica un frame binario del canal agente con prefijo
// inválido.
var errMalformedFrame = errors.New("frame binario malformado")

// encodeAgentData construye un frame binario del canal agente-servidor:
//
//	[2 bytes BE longitud del session id][session id UTF-8][payload]
func encodeAgentData(sessionID string, payload []byte) ([]byte, error) {
	if len(sessionID) == 0 || len(sessionID) > maxSessionIDLen {
		return nil, fmt.Errorf("session id inválido (longitud %d)", len(sessionID))
	}
	if len(payload) > maxAgentDataLen {
		return nil, fmt.Errorf("payload demasiado grande (%d bytes)", len(payload))
	}
	frame := make([]byte, sessionPrefixLen+len(sessionID)+len(payload))
	binary.BigEndian.PutUint16(frame[:sessionPrefixLen], uint16(len(sessionID)))
	copy(frame[sessionPrefixLen:], sessionID)
	copy(frame[sessionPrefixLen+len(sessionID):], payload)
	return frame, nil
}

// decodeAgentData separa el ID de sesión y el payload de un frame binario.
func decodeAgentData(frame []byte) (sessionID string, payload []byte, err error) {
	if len(frame) < sessionPrefixLen {
		return "", nil, errMalformedFrame
	}
	n := int(binary.BigEndian.Uint16(frame[:sessionPrefixLen]))
	if n <= 0 || n > maxSessionIDLen || len(frame) < sessionPrefixLen+n {
		return "", nil, errMalformedFrame
	}
	return string(frame[sessionPrefixLen : sessionPrefixLen+n]),
		frame[sessionPrefixLen+n:], nil
}

// maxAgentDataLen es el máximo de bytes de datos de terminal por frame. Va
// alineado con contracts.MaxTerminalOutputBytes.
const maxAgentDataLen = 1 << 20

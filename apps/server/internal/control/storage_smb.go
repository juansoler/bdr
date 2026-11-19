// Cliente SMB nativo (go-smb2) para destinos de almacenamiento filesystem
// con compartidas SMB: verificación, lectura y borrado de objetos del plano
// de control sin montar el recurso ni requerir root. La autenticación es
// NTLM con usuario, contraseña y dominio (AD o workgroup).
package control

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"os"
	"path"
	"strings"
	"time"

	"github.com/juansoler/bdr/apps/server/internal/store"

	smb2 "github.com/cloudsoda/go-smb2"
)

// smbSession agrupa la sesión SMB y el recurso montado.
type smbSession struct {
	sess  *smb2.Session
	share *smb2.Share
}

// defaultSMBPort añade el puerto 445 al host si no lo trae (admite [IPv6]).
func defaultSMBPort(host string) string {
	if _, _, err := net.SplitHostPort(host); err == nil {
		return host
	}
	if strings.Contains(host, ":") && !strings.HasPrefix(host, "[") {
		// IPv6 literal sin corchetes ni puerto.
		return "[" + host + "]:445"
	}
	return host + ":445"
}

// smbConnect abre sesión SMB contra el destino y monta el recurso.
func (s *Server) smbConnect(ctx context.Context, t *store.StorageTarget) (*smbSession, error) {
	info := t.SMBPath()
	if info.Share == "" {
		return nil, fmt.Errorf("ruta SMB inválida: %q", t.Path)
	}
	var pass string
	if t.SMBPasswordEnc != "" {
		var err error
		pass, err = s.Box.Open(t.SMBPasswordEnc)
		if err != nil {
			return nil, fmt.Errorf("no se pudo descifrar la contraseña SMB del destino")
		}
	}
	dialer := &smb2.Dialer{
		Initiator: &smb2.NTLMInitiator{
			User: t.SMBUser, Password: pass, Domain: t.SMBDomain,
		},
	}
	dctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	sess, err := dialer.Dial(dctx, defaultSMBPort(info.Host))
	if err != nil {
		return nil, fmt.Errorf("no se pudo conectar al servidor SMB %s: %s", info.Host, firstLines(err.Error(), 1))
	}
	sm := &smbSession{sess: sess}
	cleanup := func() { sm.close() }
	if sm.share, err = sm.sess.Mount(info.Share); err != nil {
		cleanup()
		return nil, fmt.Errorf("no se pudo montar el recurso %q: %s", info.Share, firstLines(err.Error(), 1))
	}
	return sm, nil
}

func (sm *smbSession) close() {
	if sm.share != nil {
		_ = sm.share.Umount()
	}
	if sm.sess != nil {
		_ = sm.sess.Logoff()
	}
}

// smbObjectPath une el subdirectorio raíz del destino con la clave del
// objeto (claves validadas con validObjectKey: sin ".." ni absolutas).
func smbObjectPath(t *store.StorageTarget, key string) string {
	root := t.SMBRoot()
	if root == "" {
		return key
	}
	return root + "/" + key
}

// smbReadObject lee un objeto del destino SMB.
func (s *Server) smbReadObject(ctx context.Context, t *store.StorageTarget, key string) ([]byte, error) {
	sm, err := s.smbConnect(ctx, t)
	if err != nil {
		return nil, err
	}
	defer sm.close()
	f, err := sm.share.Open(smbObjectPath(t, key))
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return io.ReadAll(f)
}

// smbDeleteObjects borra objetos del destino SMB. Los ficheros ausentes no
// se consideran error.
func (s *Server) smbDeleteObjects(ctx context.Context, t *store.StorageTarget, keys []string) (int, []string) {
	sm, err := s.smbConnect(ctx, t)
	if err != nil {
		return 0, []string{"conexión: " + err.Error()}
	}
	defer sm.close()
	var deleted int
	var failures []string
	for _, k := range keys {
		if err := sm.share.Remove(smbObjectPath(t, k)); err != nil && !errors.Is(err, fs.ErrNotExist) && !errors.Is(err, os.ErrNotExist) {
			failures = append(failures, k+": "+firstLines(err.Error(), 1))
			continue
		}
		deleted++
	}
	return deleted, failures
}

// storageCheckSMB comprueba conexión, credenciales y escritura real en el
// recurso SMB. Devuelve un texto para el panel: "ok …" o "error: …".
func (s *Server) storageCheckSMB(ctx context.Context, t *store.StorageTarget) string {
	sm, err := s.smbConnect(ctx, t)
	if err != nil {
		return "error: " + err.Error()
	}
	defer sm.close()
	probe := path.Join(smbObjectPath(t, ""), "verify", "ok.txt")
	if dir := path.Dir(probe); dir != "" && dir != "." {
		if err := sm.share.MkdirAll(dir, 0o755); err != nil {
			return "error: no se pudo crear el directorio de prueba: " + firstLines(err.Error(), 1)
		}
	}
	f, err := sm.share.Create(probe)
	if err != nil {
		return "error: escritura de prueba fallida (" + firstLines(err.Error(), 1) + ")"
	}
	if _, err := io.WriteString(f, "verificación BDR"); err != nil {
		_ = f.Close()
		return "error: escritura de prueba fallida (" + firstLines(err.Error(), 1) + ")"
	}
	if err := f.Close(); err != nil {
		return "error: escritura de prueba fallida (" + firstLines(err.Error(), 1) + ")"
	}
	if err := sm.share.Remove(probe); err != nil {
		return "ok: recurso SMB accesible; el archivo de prueba no se pudo borrar (" + firstLines(err.Error(), 1) + ")"
	}
	return fmt.Sprintf("ok: recurso SMB accesible y escribible (//%s/%s)", t.SMBPath().Host, t.SMBPath().Share)
}

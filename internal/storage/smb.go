package storage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	smb2 "github.com/cloudsoda/go-smb2"

	"github.com/juansoler/bdr/packages/contracts"
)

// smbTransferer transfiere a una compartida SMB (NAS) con el cliente nativo
// go-smb2, sin montaje del sistema. La escritura es atómica (temporal +
// rename) y el contenido va en streaming del disco al share: los backups
// SQL pueden pesar gigas y nunca se cargan en memoria.
type smbTransferer struct {
	sess      *smb2.Session
	share     *smb2.Share
	host      string // host[:puerto] para las URIs UNC
	shareName string
	root      string // subdirectorio dentro del recurso ("" = raíz del share)
}

// defaultSMBPort añade el puerto 445 al host si no lo trae (admite [IPv6]).
func defaultSMBPort(host string) string {
	if _, _, err := net.SplitHostPort(host); err == nil {
		return host
	}
	if strings.Contains(host, ":") && !strings.HasPrefix(host, "[") {
		return "[" + host + "]:445"
	}
	return host + ":445"
}

func newSMBTransferer(ctx context.Context, creds *contracts.RepositoryCredentials) (*smbTransferer, error) {
	info, ok := contracts.ParseSMBPath(creds.Path)
	if !ok || info.Share == "" {
		return nil, fmt.Errorf("la ruta SMB %q no es válida (//servidor/recurso[/ruta] o smb://…)", creds.Path)
	}
	dialer := &smb2.Dialer{
		Initiator: &smb2.NTLMInitiator{
			User:     creds.SMBUser,
			Password: creds.SMBPassword,
			Domain:   creds.SMBDomain,
		},
	}
	dctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	sess, err := dialer.Dial(dctx, defaultSMBPort(info.Host))
	if err != nil {
		return nil, fmt.Errorf("conexión SMB a %s: %w", info.Host, err)
	}
	share, err := sess.Mount(info.Share)
	if err != nil {
		_ = sess.Logoff()
		return nil, fmt.Errorf("no se pudo montar el recurso %s: %w", info.Share, err)
	}
	return &smbTransferer{
		sess:      sess,
		share:     share,
		host:      info.Host,
		shareName: info.Share,
		root:      strings.Trim(info.Subdir, "/"),
	}, nil
}

// smbJoin une componentes de ruta SMB con "/" (el cliente normaliza a "\").
func smbJoin(parts ...string) string {
	nonEmpty := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.Trim(p, "/"); p != "" {
			nonEmpty = append(nonEmpty, p)
		}
	}
	return strings.Join(nonEmpty, "/")
}

// destPath resuelve la ruta destino dentro del share para un fichero local.
func (s *smbTransferer) destPath(srcPath string) (string, error) {
	name := filepath.Base(srcPath)
	if name == "" || name == "." || name == ".." || strings.ContainsRune(name, 0) {
		return "", fmt.Errorf("nombre de fichero inválido: %q", name)
	}
	return smbJoin(s.root, name), nil
}

// Transfer copia el fichero local al share de forma atómica.
// Devuelve la URI UNC \\host\recurso\ruta.
func (s *smbTransferer) Transfer(ctx context.Context, srcPath string) (string, error) {
	dest, err := s.destPath(srcPath)
	if err != nil {
		return "", err
	}
	if dir := path.Dir(dest); dir != "" && dir != "." {
		if err := s.share.MkdirAll(dir, 0o755); err != nil {
			return "", fmt.Errorf("crear directorio en el recurso: %w", err)
		}
	}

	src, err := os.Open(srcPath)
	if err != nil {
		return "", fmt.Errorf("abrir %s: %w", srcPath, err)
	}
	defer src.Close()

	tmp := dest + ".bdr-tmp"
	out, err := s.share.Create(tmp)
	if err != nil {
		return "", fmt.Errorf("crear %s en el recurso: %w", tmp, err)
	}
	if _, err := io.Copy(out, src); err != nil {
		_ = out.Close()
		_ = s.share.Remove(tmp)
		return "", fmt.Errorf("copiar contenido a %s: %w", tmp, err)
	}
	if err := out.Close(); err != nil {
		_ = s.share.Remove(tmp)
		return "", fmt.Errorf("cerrar %s: %w", tmp, err)
	}
	if err := s.share.Rename(tmp, dest); err != nil {
		_ = s.share.Remove(tmp)
		return "", fmt.Errorf("renombrar %s -> %s: %w", tmp, dest, err)
	}

	return s.uri(dest), nil
}

// uri construye la URI UNC de una ruta relativa al root.
func (s *smbTransferer) uri(relPath string) string {
	full := smbJoin(s.root, relPath)
	return `\\` + s.host + `\` + s.shareName + `\` + strings.ReplaceAll(full, "/", `\`)
}

// List lista las URIs UNC bajo el prefijo relativo dado ("" = todo el root).
func (s *smbTransferer) List(ctx context.Context, prefix string) ([]string, error) {
	prefix = strings.Trim(prefix, `/ \`)
	var out []string
	var walk func(dir string) error
	walk = func(dir string) error {
		entries, err := s.share.ReadDir(smbJoin(s.root, dir))
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) || errors.Is(err, os.ErrNotExist) {
				return nil
			}
			return err
		}
		for _, e := range entries {
			name := e.Name()
			if name == "." || name == ".." {
				continue
			}
			p := name
			if dir != "" {
				p = dir + "/" + name
			}
			if e.IsDir() {
				if err := walk(p); err != nil {
					return err
				}
				continue
			}
			if prefix != "" && !strings.HasPrefix(strings.ToLower(p), strings.ToLower(prefix)) {
				continue
			}
			out = append(out, s.uri(p))
		}
		return nil
	}
	if err := walk(""); err != nil {
		return nil, fmt.Errorf("listar recurso SMB: %w", err)
	}
	return out, nil
}

// Cleanup elimina un objeto del share (solo emergencia). La ruta debe estar
// dentro del root del destino.
func (s *smbTransferer) Cleanup(ctx context.Context, uri string) error {
	p := `\\` + s.host + `\` + s.shareName + `\`
	if !strings.HasPrefix(uri, p) {
		return fmt.Errorf("URI %s no pertenece al recurso \\\\%s\\%s", uri, s.host, s.shareName)
	}
	rel := strings.ReplaceAll(strings.TrimPrefix(uri, p), `\`, "/")
	if s.root != "" && !strings.HasPrefix(rel, s.root+"/") {
		return fmt.Errorf("ruta %s fuera del subdirectorio del destino", rel)
	}
	return s.share.Remove(rel)
}

// Close desmonta el recurso y cierra la sesión SMB.
func (s *smbTransferer) Close() error {
	err := s.share.Umount()
	if lerr := s.sess.Logoff(); err == nil {
		err = lerr
	}
	return err
}

var _ Transferer = (*smbTransferer)(nil)

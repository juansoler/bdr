// Destino de subida compartido por los motores de imagen y de VMs: bucket
// S3 (minio-go) o ruta física local, prefijo por namespace, reanudación
// incremental (listado de objetos existentes) y Object Lock opcional (solo
// aplica en S3; en filesystem la retención la gestiona la política del repo).
package jobs

import (
	"bytes"
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

	"github.com/juansoler/bdr/packages/contracts"

	smb2 "github.com/cloudsoda/go-smb2"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// uploadDest es el destino de los objetos de una captura (imagen o VM):
// bucket S3 del destino de almacenamiento o directorio en una ruta física.
type uploadDest interface {
	// Prefix devuelve el prefijo de claves del namespace, con barra final
	// ("image/" | "vm/", opcionalmente bajo el prefijo del destino).
	Prefix() string
	// putSegment sube un segmento si no existe ya (reanudación incremental).
	putSegment(ctx context.Context, key string, data []byte, now time.Time) (string, error)
	// putRaw sube un objeto arbitrario (manifiestos) siempre.
	putRaw(ctx context.Context, key string, data []byte, now time.Time) error
	// getManifest descarga y parsea un objeto manifiesto.
	getManifest(ctx context.Context, key string) ([]byte, error)
	// openObject abre un objeto para lectura (restauración).
	openObject(ctx context.Context, key string) (io.ReadCloser, error)
}

// newUploadDest prepara el destino para un namespace ("image" | "vm" | "lxc")
// según el tipo de repo de las credenciales: S3 directo, ruta física local o
// compartida SMB. Machine y date (ya normalizados con sanitizeMachine y
// dateFormat) estructuran el destino como
// backup/<máquina>/<fecha>/<namespace>/; si llegan vacíos (restauraciones,
// que leen por clave completa) el prefijo queda como siempre.
func newUploadDest(ctx context.Context, creds *contracts.RepositoryCredentials, ns string,
	machine, date string, retentionDays int) (uploadDest, error) {
	if creds == nil {
		return nil, &CodeError{Code: "E_VALIDATION",
			Message: "esta operación requiere un destino de almacenamiento (elige destino en el plan o en la acción)"}
	}
	switch creds.Kind {
	case contracts.RepoKindFilesystem:
		if contracts.IsSMBPath(creds.Path) {
			return newSMBUploadDest(ctx, creds, ns, machine, date)
		}
		return newFSUploadDest(creds, ns, machine, date)
	case contracts.RepoKindS3, "":
		return newS3UploadDest(ctx, creds, ns, machine, date, retentionDays)
	default:
		return nil, &CodeError{Code: "E_VALIDATION",
			Message: "el destino no soporta upload directo de imagen/VM/LXC (tipo " + creds.Kind + ")"}
	}
}

func uploadPrefix(creds *contracts.RepositoryCredentials, ns, machine, date string) string {
	parts := []string{}
	if creds != nil {
		if p := strings.Trim(creds.Prefix, "/"); p != "" {
			parts = append(parts, p)
		}
	}
	if machine != "" && date != "" {
		parts = append(parts, backupRoot, machine, date)
	}
	parts = append(parts, ns)
	return strings.Join(parts, "/") + "/"
}

// fsKeyPath resuelve la ruta local de una clave y rechaza recorridos de
// directorio (.., /absoluta) para no salir del destino.
func fsKeyPath(root, key string) (string, error) {
	if key == "" || strings.HasPrefix(key, "/") || strings.ContainsRune(key, 0) {
		return "", fmt.Errorf("clave de objeto inválida: %q", key)
	}
	for _, part := range strings.Split(key, "/") {
		if part == "" || part == "." || part == ".." {
			return "", fmt.Errorf("clave de objeto inválida: %q", key)
		}
	}
	return filepath.Join(root, filepath.FromSlash(key)), nil
}

// --- Destino filesystem (ruta física) ---

type fsUploadDest struct {
	root     string // ruta base del destino
	prefix   string // con barra final
	existing map[string]bool
}

func newFSUploadDest(creds *contracts.RepositoryCredentials, ns, machine, date string) (*fsUploadDest, error) {
	if creds.Path == "" {
		return nil, &CodeError{Code: "E_VALIDATION",
			Message: "las credenciales del destino no incluyen la ruta física"}
	}
	root := filepath.Clean(creds.Path)
	if err := os.MkdirAll(root, 0o755); err != nil {
		return nil, &CodeError{Code: "E_IMAGE_UPLOAD", Message: "preparar la ruta del destino: " + err.Error()}
	}
	prefix := uploadPrefix(creds, ns, machine, date)
	existing, err := listFSKeys(root)
	if err != nil {
		return nil, &CodeError{Code: "E_IMAGE_UPLOAD", Message: "listar destino: " + err.Error()}
	}
	return &fsUploadDest{root: root, prefix: prefix, existing: existing}, nil
}

// listFSKeys recorre el árbol del destino y devuelve las claves existentes
// (rutas relativas con "/" como separador). Si la ruta aún no existe, el
// destino está vacío.
func listFSKeys(root string) (map[string]bool, error) {
	out := map[string]bool{}
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, rerr := filepath.Rel(root, path)
		if rerr != nil {
			return rerr
		}
		out[filepath.ToSlash(rel)] = true
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (u *fsUploadDest) Prefix() string { return u.prefix }

// putSegment escribe el segmento si no existe ya (reanudación incremental).
func (u *fsUploadDest) putSegment(ctx context.Context, key string, data []byte, now time.Time) (string, error) {
	if _, ok := u.existing[key]; ok {
		return key, nil
	}
	if err := u.writeFS(key, data); err != nil {
		return "", err
	}
	u.existing[key] = true
	return key, nil
}

func (u *fsUploadDest) putRaw(ctx context.Context, key string, data []byte, now time.Time) error {
	return u.writeFS(key, data)
}

// writeFS escribe un objeto en la ruta de forma atómica (temporal + rename):
// evita ficheros a medias si el job muere en mitad de la escritura.
func (u *fsUploadDest) writeFS(key string, data []byte) error {
	p, err := fsKeyPath(u.root, key)
	if err != nil {
		return err
	}
	dir := filepath.Dir(p)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".bdr-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	if err := os.Chmod(tmpName, 0o644); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	if err := os.Rename(tmpName, p); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	return nil
}

func (u *fsUploadDest) getManifest(ctx context.Context, key string) ([]byte, error) {
	p, err := fsKeyPath(u.root, key)
	if err != nil {
		return nil, fmt.Errorf("manifiesto: %w", err)
	}
	data, err := os.ReadFile(p)
	if err != nil {
		return nil, fmt.Errorf("manifiesto: %w", err)
	}
	return data, nil
}

func (u *fsUploadDest) openObject(ctx context.Context, key string) (io.ReadCloser, error) {
	p, err := fsKeyPath(u.root, key)
	if err != nil {
		return nil, err
	}
	return os.Open(p)
}

// --- Destino S3 (minio-go) ---

type s3UploadDest struct {
	mc         *minio.Client
	bucket     string
	prefix     string // con barra final
	retain     bool
	retainDays int
	existing   map[string]bool
	// newClient crea un cliente S3 nuevo (misma configuración): los
	// reintentos lo usan para descartar conexiones keep-alive envenenadas.
	newClient func() (*minio.Client, error)
}

func newS3UploadDest(ctx context.Context, creds *contracts.RepositoryCredentials, ns string,
	machine, date string, retentionDays int) (*s3UploadDest, error) {
	if creds.Endpoint == "" || creds.Bucket == "" {
		return nil, &CodeError{Code: "E_VALIDATION",
			Message: "las credenciales del destino S3 no incluyen endpoint o bucket"}
	}
	region := creds.Region
	if region == "" {
		region = contracts.DetectS3Region(creds.Endpoint)
	}
	clientFactory := func() (*minio.Client, error) {
		return minio.New(creds.Endpoint, &minio.Options{
			Creds:        credentials.NewStaticV4(creds.AccessKey, creds.SecretKey, ""),
			Secure:       !creds.NoTLS,
			Region:       region,
			BucketLookup: minio.BucketLookupPath,
		})
	}
	mc, err := clientFactory()
	if err != nil {
		return nil, &CodeError{Code: "E_VALIDATION", Message: "endpoint del destino inválido"}
	}
	prefix := uploadPrefix(creds, ns, machine, date)
	existing, err := listKeys(ctx, mc, creds.Bucket, prefix)
	if err != nil {
		return nil, &CodeError{Code: "E_IMAGE_UPLOAD", Message: "listar destino: " + err.Error()}
	}
	return &s3UploadDest{
		mc: mc, bucket: creds.Bucket, prefix: prefix,
		retain: retentionDays > 0, retainDays: retentionDays, existing: existing,
		newClient: clientFactory,
	}, nil
}

func listKeys(ctx context.Context, mc *minio.Client, bucket, prefix string) (map[string]bool, error) {
	out := map[string]bool{}
	objects := mc.ListObjects(ctx, bucket, minio.ListObjectsOptions{Prefix: prefix, Recursive: true})
	for obj := range objects {
		if obj.Err != nil {
			return nil, obj.Err
		}
		out[obj.Key] = true
	}
	return out, nil
}

func (u *s3UploadDest) Prefix() string { return u.prefix }

// putOpts aplica Object Lock GOVERNANCE si el destino lo pide.
func (u *s3UploadDest) putOpts(now time.Time) minio.PutObjectOptions {
	opts := minio.PutObjectOptions{}
	if u.retain {
		opts.Mode = minio.Governance
		opts.RetainUntilDate = now.UTC().AddDate(0, 0, u.retainDays)
	}
	return opts
}

// esRetryableS3 decide si un error de la API S3 admite reintentar la
// subida: siempre los de transporte (sin ErrorResponse) y los 5xx; además
// los 400 InvalidRequest sin mensaje que emiten de forma esporádica los
// backends Ceph de proveedores como Hetzner Object Storage bajo carga
// (documentado: sus guías piden reintentar en el cliente). El RequestID no
// se usa para filtrar: S3 siempre lo asigna, el fallo flaky trae el código
// con Message vacío.
func esRetryableS3(err error) bool {
	if err == nil {
		return false
	}
	var resp minio.ErrorResponse
	if !errors.As(err, &resp) {
		return true // fallo de transporte (conexión, TLS, timeout…)
	}
	if resp.StatusCode >= 500 {
		return true
	}
	switch resp.Code {
	case "SlowDown", "InternalError", "ServiceUnavailable", "RequestTimeout":
		return true
	case "InvalidRequest":
		return resp.Message == ""
	}
	return false
}

// putObject sube un objeto al destino S3 con reintentos y espera creciente:
// el PUT es idempotente (clave y contenido fijos), así que reintentar no
// puede dejar objetos duplicados ni corruptos. Cada reintento usa un
// CLIENTE S3 NUEVO: una conexión keep-alive envenenada (NAT, proxy o front
// sobrecargado) falla siempre con el mismo 400, y reintentar sobre la misma
// conexión hereda el fallo.
func (u *s3UploadDest) putObject(ctx context.Context, key string, data []byte, now time.Time) error {
	var lastErr error
	for intento := 0; intento < 4; intento++ {
		if intento > 0 {
			if u.newClient != nil {
				if nc, cerr := u.newClient(); cerr == nil {
					u.mc = nc
				}
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(time.Duration(intento) * 3 * time.Second):
			}
		}
		_, err := u.mc.PutObject(ctx, u.bucket, key, bytes.NewReader(data),
			int64(len(data)), u.putOpts(now))
		if err == nil {
			return nil
		}
		if !esRetryableS3(err) {
			return s3ErrDetail(err)
		}
		lastErr = err
	}
	return s3ErrDetail(lastErr)
}

// s3ErrDetail añade al error los detalles del ErrorResponse del backend
// (código HTTP, mensaje, RequestID/HostID) para poder diagnosticar fallos
// de S3 compatibles que devuelven mensajes vacíos.
func s3ErrDetail(err error) error {
	if err == nil {
		return err
	}
	var resp minio.ErrorResponse
	if !errors.As(err, &resp) {
		return err
	}
	return fmt.Errorf("%w (HTTP %d, código %s, mensaje %q, request %q, host %q)",
		err, resp.StatusCode, resp.Code, resp.Message, resp.RequestID, resp.HostID)
}

// putSegment sube un segmento si no existe ya (reanudación incremental).
// Devuelve la clave del objeto.
func (u *s3UploadDest) putSegment(ctx context.Context, key string, data []byte, now time.Time) (string, error) {
	if _, ok := u.existing[key]; ok {
		return key, nil
	}
	if err := u.putObject(ctx, key, data, now); err != nil {
		return "", err
	}
	u.existing[key] = true
	return key, nil
}

// putRaw sube un objeto arbitrario (manifiestos) siempre.
func (u *s3UploadDest) putRaw(ctx context.Context, key string, data []byte, now time.Time) error {
	return u.putObject(ctx, key, data, now)
}

// getManifest descarga y parsea un objeto manifiesto.
func (u *s3UploadDest) getManifest(ctx context.Context, key string) ([]byte, error) {
	obj, err := u.mc.GetObject(ctx, u.bucket, key, minio.GetObjectOptions{})
	if err != nil {
		return nil, fmt.Errorf("manifiesto: %w", err)
	}
	defer obj.Close()
	data, err := io.ReadAll(obj)
	if err != nil {
		return nil, fmt.Errorf("manifiesto: %w", err)
	}
	return data, nil
}

func (u *s3UploadDest) openObject(ctx context.Context, key string) (io.ReadCloser, error) {
	return u.mc.GetObject(ctx, u.bucket, key, minio.GetObjectOptions{})
}

// --- Destino SMB (recurso compartido, cliente nativo sin montaje) ---

type smbUploadDest struct {
	sess     *smb2.Session
	share    *smb2.Share
	root     string // subdirectorio dentro del recurso (subdir + prefijo)
	prefix   string // con barra final
	existing map[string]bool
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

func newSMBUploadDest(ctx context.Context, creds *contracts.RepositoryCredentials, ns, machine, date string) (*smbUploadDest, error) {
	info, ok := contracts.ParseSMBPath(creds.Path)
	if !ok || info.Share == "" {
		return nil, &CodeError{Code: "E_VALIDATION",
			Message: "la ruta SMB del destino no es válida (//servidor/recurso o smb://servidor/recurso)"}
	}
	dialer := &smb2.Dialer{
		Initiator: &smb2.NTLMInitiator{
			User: creds.SMBUser, Password: creds.SMBPassword, Domain: creds.SMBDomain,
		},
	}
	dctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	sess, err := dialer.Dial(dctx, defaultSMBPort(info.Host))
	if err != nil {
		return nil, &CodeError{Code: "E_IMAGE_UPLOAD",
			Message: "conexión SMB a " + info.Host + ": " + err.Error()}
	}
	share, err := sess.Mount(info.Share)
	if err != nil {
		_ = sess.Logoff()
		return nil, &CodeError{Code: "E_IMAGE_UPLOAD",
			Message: "no se pudo montar el recurso " + info.Share + ": " + err.Error()}
	}
	root := info.Subdir
	if p := strings.Trim(creds.Prefix, "/"); p != "" {
		if root != "" {
			root += "/" + p
		} else {
			root = p
		}
	}
	existing, err := listSMBKeys(share, root)
	if err != nil {
		_ = share.Umount()
		_ = sess.Logoff()
		return nil, &CodeError{Code: "E_IMAGE_UPLOAD", Message: "listar destino SMB: " + err.Error()}
	}
	return &smbUploadDest{
		sess: sess, share: share, root: root,
		prefix: uploadPrefix(creds, ns, machine, date), existing: existing,
	}, nil
}

// smbPath une el root del destino con la clave (claves ya validadas por
// fsKeyPath en los métodos de escritura; aquí se revalida por defensa).
func (u *smbUploadDest) smbPath(key string) (string, error) {
	return fsKeyPath("/", key) // valida la clave…
}

// listSMBKeys recorre el árbol del recurso y devuelve las claves existentes
// como rutas relativas con "/" (vacío si el root aún no existe).
func listSMBKeys(share *smb2.Share, root string) (map[string]bool, error) {
	out := map[string]bool{}
	var walk func(dir string) error
	walk = func(dir string) error {
		entries, err := share.ReadDir(smbJoin(dir))
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
			out[p] = true
		}
		return nil
	}
	if err := walk(root); err != nil {
		return nil, err
	}
	return out, nil
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

func (u *smbUploadDest) Prefix() string { return u.prefix }

func (u *smbUploadDest) objectPath(key string) (string, error) {
	if _, err := fsKeyPath("/", key); err != nil {
		return "", err
	}
	return smbJoin(u.root, key), nil
}

// writeSMB escribe un objeto de forma atómica dentro del recurso (temporal
// + rename) para no dejar ficheros a medias si el job muere.
func (u *smbUploadDest) writeSMB(key string, data []byte) error {
	obj, err := u.objectPath(key)
	if err != nil {
		return err
	}
	if dir := path.Dir(obj); dir != "" && dir != "." {
		if err := u.share.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	tmp := obj + ".bdr-tmp"
	f, err := u.share.Create(tmp)
	if err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		_ = u.share.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		_ = u.share.Remove(tmp)
		return err
	}
	if err := u.share.Rename(tmp, obj); err != nil {
		_ = u.share.Remove(tmp)
		return err
	}
	return nil
}

func (u *smbUploadDest) putSegment(ctx context.Context, key string, data []byte, now time.Time) (string, error) {
	if _, ok := u.existing[key]; ok {
		return key, nil
	}
	if err := u.writeSMB(key, data); err != nil {
		return "", err
	}
	u.existing[key] = true
	return key, nil
}

func (u *smbUploadDest) putRaw(ctx context.Context, key string, data []byte, now time.Time) error {
	return u.writeSMB(key, data)
}

func (u *smbUploadDest) getManifest(ctx context.Context, key string) ([]byte, error) {
	obj, err := u.objectPath(key)
	if err != nil {
		return nil, fmt.Errorf("manifiesto: %w", err)
	}
	f, err := u.share.Open(obj)
	if err != nil {
		return nil, fmt.Errorf("manifiesto: %w", err)
	}
	defer f.Close()
	data, err := io.ReadAll(f)
	if err != nil {
		return nil, fmt.Errorf("manifiesto: %w", err)
	}
	return data, nil
}

func (u *smbUploadDest) openObject(ctx context.Context, key string) (io.ReadCloser, error) {
	obj, err := u.objectPath(key)
	if err != nil {
		return nil, err
	}
	return u.share.Open(obj)
}

// backupRoot es el primer componente de la estructura de backups: los
// segmentos y manifiestos de imagen/VM/LXC se agrupan como
// backup/<máquina>/<fecha>/<namespace>/... (sin barra inicial para que sea
// válida como clave de objeto: fsKeyPath rechaza claves absolutas).
const backupRoot = "backup"

// dateFormat es el formato de fecha usado para agrupar backups por día.
const dateFormat = "2006-01-02"

// guestMachine construye el componente de máquina para un guest de Proxmox
// como <nodo>/<guest>: el primer nivel identifica el servidor Proxmox donde
// vive la VM/CT y el segundo el propio guest, dando la estructura
// backup/<nodo>/<guest>/<fecha>/<namespace>/. Si el payload no trae nodo
// (o sanitiza a vacío) la ruta queda solo con el guest.
func guestMachine(node, guest string) string {
	n, g := sanitizeMachine(node), sanitizeMachine(guest)
	switch {
	case n != "" && g != "":
		return n + "/" + g
	case g != "":
		return g
	default:
		return n
	}
}

// sanitizeMachine normaliza el nombre de la máquina para usarlo como
// componente de ruta en el destino (S3, ruta física o SMB): sustituye todo
// lo que no sea letra, dígito, punto, guion o guion bajo por "_", colapsa
// separadores repetidos y recorta bordes. Puede devolver "" si el nombre
// era vacío o no tenía caracteres válidos (el llamador debe usar un
// fallback: p. ej. "vm105").
func sanitizeMachine(name string) string {
	name = strings.TrimSpace(name)
	out := strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			return r
		case r == '-' || r == '_' || r == '.':
			return r
		default:
			return '_'
		}
	}, name)
	for strings.Contains(out, "__") {
		out = strings.ReplaceAll(out, "__", "_")
	}
	return strings.Trim(out, "_.-")
}

// Motor de almacenamiento: verificación de destinos S3 (minio-go), rutas
// físicas y compartidas SMB (go-smb2), réplica del repositorio del plano de
// control con `kopia repository sync-to` (s3, filesystem o rclone-smb) y
// aprovisionamiento del repo Kopia de cada destino.
package control

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/juansoler/bdr/apps/server/internal/auth"
	"github.com/juansoler/bdr/apps/server/internal/store"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// storageCheck verifica un destino: credenciales y bucket en S3 (minio-go,
// con prueba de Object Lock si aplica) o accesibilidad de escritura de la
// ruta física. Devuelve un texto para el panel: "ok …" o "error: …".
func (s *Server) storageCheck(ctx context.Context, t *store.StorageTarget) string {
	if t.IsSMB() {
		return s.storageCheckSMB(ctx, t)
	}
	if t.IsFilesystem() {
		return storageCheckFilesystem(t)
	}

	sk, err := s.Box.Open(t.SecretKeyEnc)
	if err != nil {
		return "error: no se pudo descifrar el secreto (¿cambió secret.key?)"
	}
	endpoint, secure := t.Endpoint, !t.NoTLS
	mc, err := minio.New(endpoint, &minio.Options{
		Creds:        credentials.NewStaticV4(t.AccessKey, sk, ""),
		Secure:       secure,
		Region:       t.Region,
		BucketLookup: minio.BucketLookupPath,
	})
	if err != nil {
		return "error: endpoint inválido"
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	exists, err := mc.BucketExists(ctx, t.Bucket)
	if err != nil {
		return "error: sin acceso al bucket (" + firstLines(err.Error(), 1) + ")"
	}
	if !exists {
		return "error: el bucket no existe"
	}

	result := "ok: credenciales y bucket correctos"
	key := "verify/ok.txt"
	if p := strings.Trim(t.Prefix, "/"); p != "" {
		key = p + "/verify/ok.txt"
	}
	opts := minio.PutObjectOptions{}
	if t.ObjectLock && t.RetentionDays > 0 {
		opts.Mode = minio.Governance
		opts.RetainUntilDate = time.Now().UTC().AddDate(0, 0, t.RetentionDays)
		result = "ok: credenciales, bucket y Object Lock (GOVERNANCE) correctos"
	}
	_, err = mc.PutObject(ctx, t.Bucket, key, strings.NewReader("verificación BDR"), 16, opts)
	if err != nil {
		msg := "error: escritura de prueba fallida (" + firstLines(err.Error(), 1) + ")"
		if t.ObjectLock && t.RetentionDays > 0 && strings.Contains(err.Error(), "InvalidRequest") {
			// Un PUT con cabeceras de lock sobre un bucket creado sin lock
			// da InvalidRequest (Hetzner: el lock solo se habilita al crear
			// el bucket). Damos la pista accionable.
			msg += " — con Object Lock activo, el bucket debe haberse creado con Object Lock en el proveedor; crea el bucket con lock o desactiva el Object Lock en el destino"
		}
		return msg
	}
	err = mc.RemoveObject(ctx, t.Bucket, key, minio.RemoveObjectOptions{
		GovernanceBypass: t.ObjectLock,
	})
	if err != nil {
		result += "; objeto de verificación retenido por lock (normal)"
	}
	return result
}

// storageCheckFilesystem comprueba que la ruta física existe y es escribible.
func storageCheckFilesystem(t *store.StorageTarget) string {
	dir := t.RepoPath()
	fi, err := os.Stat(dir)
	switch {
	case err != nil:
		return "error: la ruta no existe (" + dir + ")"
	case !fi.IsDir():
		return "error: la ruta no es un directorio (" + dir + ")"
	}
	probe := filepath.Join(dir, "verify", "ok.txt")
	if err := os.MkdirAll(filepath.Dir(probe), 0o755); err != nil {
		return "error: no se pudo escribir en la ruta: " + err.Error()
	}
	if err := os.WriteFile(probe, []byte("verificación BDR"), 0o644); err != nil {
		return "error: escritura de prueba fallida (" + firstLines(err.Error(), 1) + ")"
	}
	if err := os.Remove(probe); err != nil {
		return "ok: ruta accesible; el archivo de prueba no se pudo borrar (" + firstLines(err.Error(), 1) + ")"
	}
	return "ok: ruta física accesible y escribible (" + dir + ")"
}

// storageRunSync ejecuta la réplica del repositorio del plano de control:
// `kopia repository sync-to s3` con las credenciales del destino S3, o
// `sync-to filesystem` hacia la ruta física. Devuelve (estado, ok).
func (s *Server) storageRunSync(ctx context.Context, t *store.StorageTarget) (string, bool) {
	if s.Kopia == nil || !s.Kopia.ServerSide() {
		return "error: el plano de control no tiene conexión al repositorio", false
	}
	var args []string
	var extraEnv []string
	if t.IsFilesystem() {
		if t.IsSMB() {
			var err error
			if args, extraEnv, err = s.storageSyncArgsRclone(ctx, t); err != nil {
				return "error: " + err.Error(), false
			}
		} else {
			if err := os.MkdirAll(t.RepoPath(), 0o755); err != nil {
				return "error: no se pudo crear la ruta: " + err.Error(), false
			}
			args = []string{"repository", "sync-to", "filesystem", "--path=" + t.RepoPath()}
		}
	} else {
		sk, err := s.Box.Open(t.SecretKeyEnc)
		if err != nil {
			return "error: no se pudo descifrar el secreto", false
		}
		endpoint, _ := parseEndpoint(t.Endpoint)
		args = []string{
			"repository", "sync-to", "s3",
			"--endpoint=" + endpoint,
			"--bucket=" + t.Bucket,
		}
		if t.Prefix != "" {
			args = append(args, "--prefix="+strings.Trim(t.Prefix, "/")+"/")
		}
		if t.Region != "" {
			args = append(args, "--region="+t.Region)
		}
		if t.AccessKey != "" {
			args = append(args,
				"--access-key="+t.AccessKey,
				"--secret-access-key="+sk)
		}
		if t.NoTLS {
			args = append(args, "--disable-tls")
		}
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Minute)
	defer cancel()
	if _, err := s.Kopia.RunRawWithEnv(ctx, 28*time.Minute, extraEnv, args...); err != nil {
		return "error: " + firstLines(err.Error(), 2), false
	}
	return "ok", true
}

// storageSyncArgsRclone construye la réplica `kopia repository sync-to
// rclone` hacia una compartida SMB: configura el remote smb de rclone vía
// variables de entorno (host, usuario, contraseña ofuscada y dominio AD) y
// apunta al recurso+subdirectorio del destino. Requiere rclone en el PATH.
func (s *Server) storageSyncArgsRclone(ctx context.Context, t *store.StorageTarget) ([]string, []string, error) {
	if _, err := exec.LookPath("rclone"); err != nil {
		return nil, nil, fmt.Errorf("rclone no está instalado: instálalo (p. ej. brew install rclone o apt install rclone) para replicar a rutas SMB")
	}
	info := t.SMBPath()
	host, port := info.Host, ""
	if h, p, err := net.SplitHostPort(info.Host); err == nil {
		host, port = h, p
	}
	name := "bdrsmb" + strings.ReplaceAll(strings.ReplaceAll(strings.ToLower(t.ID), "-", ""), "_", "")
	var pass string
	if t.SMBPasswordEnc != "" {
		var err error
		if pass, err = s.Box.Open(t.SMBPasswordEnc); err != nil {
			return nil, nil, fmt.Errorf("no se pudo descifrar la contraseña SMB del destino")
		}
	}
	// rclone obscure: la contraseña de configuración va ofuscada en el env.
	var obscured string
	if pass != "" {
		out, err := exec.CommandContext(ctx, "rclone", "obscure", pass).Output()
		if err != nil {
			return nil, nil, fmt.Errorf("rclone obscure falló: %s", firstLines(err.Error(), 1))
		}
		obscured = strings.TrimSpace(string(out))
	}
	p := func(opt, val string) string {
		return "RCLONE_CONFIG_" + name + "_" + opt + "=" + val
	}
	env := []string{p("TYPE", "smb"), p("HOST", host)}
	if port != "" {
		env = append(env, p("PORT", port))
	}
	if t.SMBUser != "" {
		env = append(env, p("USER", t.SMBUser))
	}
	if obscured != "" {
		env = append(env, p("PASS", obscured))
	}
	if t.SMBDomain != "" {
		env = append(env, p("DOMAIN", t.SMBDomain))
	}
	// Reconexión rápida ante cortes de red (evita reintentos de 5 min por defecto).
	env = append(env,
		"RCLONE_SMB_TIMEOUT=30s",
		"RCLONE_RETRIES=3",
		"RCLONE_RETRIES_SLEEP=5s",
	)
	root := t.SMBRoot()
	remote := name + ":" + info.Share
	if root != "" {
		remote += "/" + root
	}
	args := []string{"repository", "sync-to", "rclone", "--remote=" + remote}
	return args, env, nil
}

// storageEnsureRepo garantiza que el destino tiene un repositorio Kopia
// inicializado: genera su contraseña (cifrada en el store), intenta
// conectar y, si el bucket aún no es un repo, lo crea. Idempotente.
// Devuelve la contraseña del repo (para entregarla al agente).
func (s *Server) storageEnsureRepo(ctx context.Context, t *store.StorageTarget) (string, error) {
	if t.IsSMB() {
		return "", fmt.Errorf("los destinos SMB no admiten repositorio Kopia directo (kopia no escribe en compartidas sin montar); usa S3 o una ruta local/NFS para planes de archivo")
	}
	sk := ""
	if !t.IsFilesystem() {
		var err error
		sk, err = s.Box.Open(t.SecretKeyEnc)
		if err != nil {
			return "", fmt.Errorf("secreto del destino: %w", err)
		}
	}
	pw := ""
	if t.RepoPasswordEnc != "" {
		var err error
		pw, err = s.Box.Open(t.RepoPasswordEnc)
		if err != nil {
			return "", fmt.Errorf("contraseña del repo: %w", err)
		}
	} else {
		var err error
		pw, err = auth.NewSecret(24)
		if err != nil {
			return "", err
		}
		t.RepoPasswordEnc = s.Box.MustSeal(pw)
	}
	if t.RepoReady {
		return pw, nil
	}
	if s.Kopia == nil || !s.Kopia.ServerSide() {
		return "", fmt.Errorf("el plano de control no tiene conexión Kopia para aprovisionar repos")
	}
	// Config de kopia por destino: no debe tocar la del repo primario.
	cfgFile := filepath.Join(filepath.Dir(s.Kopia.ConfigFile), "target-"+t.ID+".config")
	var connect []string
	if t.IsFilesystem() {
		if err := os.MkdirAll(t.RepoPath(), 0o755); err != nil {
			return "", fmt.Errorf("crear la ruta %s: %w", t.RepoPath(), err)
		}
		connect = []string{"repository", "connect", "filesystem", "--path=" + t.RepoPath()}
	} else {
		connect = []string{"repository", "connect", "s3",
			"--endpoint=" + t.Endpoint, "--bucket=" + t.Bucket}
		if p := strings.Trim(t.Prefix, "/"); p != "" {
			connect = append(connect, "--prefix="+p+"/")
		}
		connect = append(connect, "--access-key="+t.AccessKey, "--secret-access-key="+sk)
		if t.NoTLS {
			connect = append(connect, "--disable-tls")
		}
	}
	if _, err := s.Kopia.RunRepoOp(ctx, 2*time.Minute, cfgFile, pw, connect...); err != nil {
		// No era un repo: se crea (formatea el destino con el layout).
		create := append([]string{}, connect...)
		create[1] = "create"
		if _, cerr := s.Kopia.RunRepoOp(ctx, 2*time.Minute, cfgFile, pw, create...); cerr != nil {
			msg := firstLines(cerr.Error(), 2)
			if strings.Contains(strings.ToLower(cerr.Error()), "found existing data") {
				if t.IsFilesystem() {
					return "", fmt.Errorf("la ruta %s ya contiene datos de otro repositorio o réplica: usa una ruta vacía o define un prefijo distinto en el destino", t.RepoPath())
				}
				return "", fmt.Errorf("el bucket %s ya contiene datos de otro repositorio o réplica: usa un bucket vacío o define un prefijo distinto en el destino", t.Bucket)
			}
			if t.IsFilesystem() {
				return "", fmt.Errorf("crear repo en %s: %s", t.RepoPath(), msg)
			}
			return "", fmt.Errorf("crear repo en %s/%s: %s", t.Endpoint, t.Bucket, msg)
		}
	}
	t.RepoReady = true
	return pw, nil
}

// storageSyncLoop lanza las réplicas programadas cada minuto.
func (s *Server) storageSyncLoop(ctx context.Context) {
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			s.syncDueStorage(ctx)
		}
	}
}

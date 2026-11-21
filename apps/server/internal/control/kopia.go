package control

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/juansoler/bdr/packages/contracts"
)

// KopiaControl ejecuta Kopia con la conexión directa del plano de control
// (kopia.config en el directorio de datos). Con acceso directo (repositorio
// filesystem) puede navegar, verificar y medir; con solo conexión de servidor
// esas funciones quedan marcadas como no disponibles.
type KopiaControl struct {
	Binary     string
	ConfigFile string
	Password   string
	// Campos de repositorio para repository-credentials del agente.
	KindName          string // "" | filesystem | server
	RepoPath          string
	ServerURL         string
	ServerFingerprint string
	// Credenciales de la API de control del Kopia Repository Server: al
	// aprovisionar un usuario, el control fuerza `kopia server refresh`
	// para que el cambio sea visible de inmediato (si no, el servidor lo
	// recoge en su siguiente refresco, 15 s por defecto).
	ServerControlUser string
	ServerControlPass string
	// serverSide indica si hay conexión directa usable.
	serverSide bool
	// LastConnError conserva el fallo real de la comprobación de conexión
	// (`kopia repository status`) para poder depurar el "sin conexión".
	LastConnError string
}

// NewKopiaControl crea el wrapper; determina si hay conexión directa probando
// `kopia repository status`.
func NewKopiaControl(binary, configFile, password, kind, repoPath, serverURL, serverFP, serverControlUser, serverControlPass string) *KopiaControl {
	if binary == "" {
		binary = "kopia"
	}
	k := &KopiaControl{
		Binary: binary, ConfigFile: configFile, Password: password,
		KindName: kind, RepoPath: repoPath, ServerURL: serverURL, ServerFingerprint: serverFP,
		ServerControlUser: serverControlUser, ServerControlPass: serverControlPass,
	}
	if configFile != "" {
		if _, err := os.Stat(configFile); err == nil {
			if _, err := k.run(context.Background(), 20*time.Second, "repository", "status"); err == nil {
				k.serverSide = true
			} else {
				// El detalle del fallo (contraseña, repo inexistente…) es
				// lo que permite depurar la conexión del plano de control.
				k.LastConnError = err.Error()
			}
		}
	}
	return k
}

// ServerSide indica si el plano de control tiene acceso directo al repositorio.
func (k *KopiaControl) ServerSide() bool { return k != nil && k.serverSide }

// RunRaw ejecuta kopia con argumentos arbitrarios sobre la conexión del
// plano de control (usado por la réplica a destinos S3).
func (k *KopiaControl) RunRaw(ctx context.Context, timeout time.Duration, args ...string) (string, error) {
	return k.run(ctx, timeout, args...)
}

// repoUserID devuelve el identificador de usuario del Kopia Repository Server:
// usuario@hostname, que además delimita el dominio visible del agente.
// El CLI exige minúsculas.
func repoUserID(username, hostname string) string {
	return strings.ToLower(username + "@" + hostname)
}

// repoUserExists comprueba si el usuario existe en el repositorio (los
// usuarios del Kopia Repository Server viven en el propio repositorio, así
// que una conexión administrativa basta para gestionarlos).
func (k *KopiaControl) repoUserExists(ctx context.Context, id string) (bool, error) {
	out, err := k.run(ctx, 30*time.Second, "server", "users", "list", "--json")
	if err != nil {
		return false, err
	}
	var users []struct {
		Username string `json:"username"`
	}
	if err := json.Unmarshal([]byte(out), &users); err != nil {
		// Listado no parseable: probablemente una versión sin --json. Caemos
		// a buscar el identificador en la salida.
		return strings.Contains(out, `"`+id+`"`), nil
	}
	for _, u := range users {
		if u.Username == id {
			return true, nil
		}
	}
	return false, nil
}

// ProvisionRepoUser crea o actualiza el usuario del agente en el repositorio.
// Idempotente: si existe, actualiza la contraseña. La llamada es segura
// aunque el servidor esté en marcha: lo recoge en su siguiente refresco.
func (k *KopiaControl) ProvisionRepoUser(ctx context.Context, username, hostname, password string) error {
	id := repoUserID(username, hostname)
	exists, err := k.repoUserExists(ctx, id)
	if err != nil {
		return err
	}
	if exists {
		_, err := k.run(ctx, 30*time.Second, "server", "users", "set", id, "--user-password="+password)
		if err != nil {
			return fmt.Errorf("no se pudo actualizar el usuario del repositorio: %w", err)
		}
	} else {
		if _, err := k.run(ctx, 30*time.Second, "server", "users", "add", id, "--user-password="+password); err != nil {
			return fmt.Errorf("no se pudo crear el usuario del repositorio: %w", err)
		}
	}
	k.refreshServerUsers(ctx)
	return nil
}

// refreshServerUsers pide al Kopia Repository Server que recargue su
// catálogo de usuarios. Best-effort: si no hay credenciales de control o
// falla, el servidor lo recogerá en su siguiente refresco.
func (k *KopiaControl) refreshServerUsers(ctx context.Context) {
	if k.ServerURL == "" || k.ServerControlUser == "" {
		return
	}
	args := []string{"server", "refresh", "--address=" + k.ServerURL}
	if fp := strings.TrimPrefix(k.ServerFingerprint, "sha256:"); fp != "" {
		args = append(args, "--server-cert-fingerprint="+fp)
	}
	args = append(args,
		"--server-control-username="+k.ServerControlUser,
		"--server-control-password="+k.ServerControlPass)
	if _, err := k.run(ctx, 20*time.Second, args...); err != nil {
		// No es fatal: el refresco periódico del servidor lo recogerá.
		return
	}
}

// DeleteRepoUser elimina el usuario del agente (revocación). Ignora el caso
// de que no exista.
func (k *KopiaControl) DeleteRepoUser(ctx context.Context, username, hostname string) error {
	id := repoUserID(username, hostname)
	if _, err := k.run(ctx, 30*time.Second, "server", "users", "delete", id); err != nil {
		// Si el usuario no existe, nada que hacer.
		if exists, e := k.repoUserExists(ctx, id); e == nil && !exists {
			return nil
		}
		return fmt.Errorf("no se pudo eliminar el usuario del repositorio: %w", err)
	}
	return nil
}

// Kind devuelve el tipo de repositorio configurado ("" si no hay).
func (k *KopiaControl) Kind() string {
	if k == nil {
		return ""
	}
	return k.KindName
}

func (k *KopiaControl) baseEnv() []string {
	env := append(os.Environ(), "KOPIA_CHECK_FOR_UPDATES=false", "KOPIA_PASSWORD="+k.Password)
	return env
}

// run ejecuta un comando de Kopia y devuelve stdout limpio (sin líneas de log).
// RunRepoOp ejecuta kopia con un fichero de configuración y contraseña
// propios (aprovisionamiento de repositorios en destinos S3), sin tocar la
// conexión del repo primario.
func (k *KopiaControl) RunRepoOp(ctx context.Context, timeout time.Duration, configFile, password string, args ...string) (string, error) {
	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	full := append([]string{"--config-file", configFile}, args...)
	cmd := exec.CommandContext(cctx, k.Binary, full...)
	cmd.Env = append(k.baseEnv(), "KOPIA_PASSWORD="+password)
	var out, errb bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errb
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(errb.String())
		if cctx.Err() != nil {
			return "", fmt.Errorf("kopia %s: tiempo agotado", args[0])
		}
		if msg == "" {
			msg = err.Error()
		}
		// Sin truncar agresivo: el motivo real del fallo (credenciales,
		// región SigV4, lock del proveedor, cuota…) viene en líneas
		// posteriores al banner de inicialización. Los callers aplican su
		// propio límite con firstLines.
		return "", fmt.Errorf("kopia %s: %s", args[0], msg)
	}
	return filterKopiaJSON(out.String()), nil
}

func (k *KopiaControl) run(ctx context.Context, timeout time.Duration, args ...string) (string, error) {
	return k.runEnv(ctx, timeout, nil, args...)
}

// RunRawWithEnv ejecuta kopia con variables de entorno adicionales
// (configuración de remotes rclone para la réplica a rutas SMB).
func (k *KopiaControl) RunRawWithEnv(ctx context.Context, timeout time.Duration, extraEnv []string, args ...string) (string, error) {
	return k.runEnv(ctx, timeout, extraEnv, args...)
}

func (k *KopiaControl) runEnv(ctx context.Context, timeout time.Duration, extraEnv []string, args ...string) (string, error) {
	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	full := append([]string{"--config-file", k.ConfigFile}, args...)
	cmd := exec.CommandContext(cctx, k.Binary, full...)
	cmd.Env = append(k.baseEnv(), extraEnv...)
	var out, errb bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errb
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(errb.String())
		if cctx.Err() != nil {
			return "", fmt.Errorf("kopia %s: tiempo agotado", args[0])
		}
		if msg == "" {
			msg = err.Error()
		}
		return "", fmt.Errorf("kopia %s: %s", args[0], firstLines(msg, 3))
	}
	return filterKopiaJSON(out.String()), nil
}

// filterKopiaJSON elimina las líneas de log que Kopia escribe en stdout.
func filterKopiaJSON(s string) string {
	lines := strings.Split(s, "\n")
	var keep []string
	for _, l := range lines {
		t := strings.TrimSpace(l)
		if t == "" {
			continue
		}
		if strings.HasPrefix(t, "{") || strings.HasPrefix(t, "[") || strings.HasPrefix(t, "]") ||
			strings.HasPrefix(t, "\"") || t == "}" {
			keep = append(keep, l)
		}
	}
	if len(keep) == 0 {
		return strings.TrimSpace(s)
	}
	return strings.Join(keep, "\n")
}

func firstLines(s string, n int) string {
	lines := strings.Split(s, "\n")
	if len(lines) > n {
		lines = lines[:n]
	}
	out := strings.Join(lines, " · ")
	if len(out) > 300 {
		out = out[:300] + "…"
	}
	return out
}

// Status comprueba el repositorio y devuelve métricas.
func (k *KopiaControl) Status(ctx context.Context) (ok bool, detail string, snapshotCount int, logical, stored int64) {
	if !k.ServerSide() {
		return false, "sin conexión directa al repositorio", 0, 0, 0
	}
	out, err := k.run(ctx, 30*time.Second, "snapshot", "list", "--all", "--json")
	if err != nil {
		return false, firstLines(err.Error(), 1), 0, 0, 0
	}
	var snaps []struct {
		Source struct {
			Path string `json:"path"`
		} `json:"source"`
		Stats *struct {
			TotalSize int64 `json:"totalSize"`
		} `json:"stats"`
	}
	if err := json.Unmarshal([]byte(out), &snaps); err == nil {
		snapshotCount = len(snaps)
		for _, sn := range snaps {
			logical += sn.Stats.TotalSize
		}
	}
	if cs, err := k.contentStats(ctx); err == nil {
		stored = cs
	}
	return true, "", snapshotCount, logical, stored
}

func (k *KopiaControl) contentStats(ctx context.Context) (int64, error) {
	out, err := k.run(ctx, 60*time.Second, "content", "stats", "--raw")
	if err != nil {
		return 0, err
	}
	sc := bufio.NewScanner(strings.NewReader(out))
	for sc.Scan() {
		line := sc.Text()
		if strings.HasPrefix(line, "Total Packed:") {
			f := strings.Fields(line)
			for _, x := range f[2:] {
				if v, err := strconv.ParseInt(x, 10, 64); err == nil {
					return v, nil
				}
			}
		}
	}
	return 0, fmt.Errorf("salida no reconocida")
}

// VerifySnapshot verifica la estructura (y opcionalmente un % del contenido).
func (k *KopiaControl) VerifySnapshot(ctx context.Context, snapshotID string, filesPercent int) (string, error) {
	args := []string{"snapshot", "verify"}
	if filesPercent > 0 {
		args = append(args, fmt.Sprintf("--verify-files-percent=%d", filesPercent))
	}
	args = append(args, snapshotID)
	out, err := k.run(ctx, 10*time.Minute, args...)
	if err != nil {
		return firstLines(err.Error(), 2), err
	}
	sc := bufio.NewScanner(strings.NewReader(out))
	var processed, errs int
	var last string
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if strings.HasPrefix(line, "Processed") || strings.HasPrefix(line, "Finished") {
			last = line
			processed++
		}
		if strings.Contains(strings.ToLower(line), "error") {
			errs++
		}
	}
	detail := "estructura verificada"
	if filesPercent > 0 {
		detail = fmt.Sprintf("contenido verificado (%d %% de archivos)", filesPercent)
	}
	if last != "" {
		detail += ": " + firstLines(last, 1)
	}
	if errs > 0 {
		detail += fmt.Sprintf("; %d líneas con error", errs)
	}
	return detail, nil
}

// ListDir lee un directorio del repositorio vía `kopia show` (JSON interno).
func (k *KopiaControl) ListDir(ctx context.Context, objectPath string) ([]contracts.FileEntry, error) {
	out, err := k.run(ctx, 60*time.Second, "show", objectPath)
	if err != nil {
		return nil, err
	}
	var dir struct {
		Stream  string `json:"stream"`
		Entries []struct {
			Name  string `json:"name"`
			Type  string `json:"type"`
			Mode  string `json:"mode"`
			Size  int64  `json:"size"`
			MTime string `json:"mtime"`
			Obj   string `json:"obj"`
			Summ  *struct {
				Size  int64 `json:"size"`
				Files int64 `json:"files"`
			} `json:"summ"`
		} `json:"entries"`
	}
	if err := json.Unmarshal([]byte(out), &dir); err != nil {
		return nil, fmt.Errorf("salida de kopia show no reconocida: %w", err)
	}
	if dir.Stream != "kopia:directory" {
		return nil, fmt.Errorf("la ruta no es un directorio")
	}
	entries := make([]contracts.FileEntry, 0, len(dir.Entries))
	for _, e := range dir.Entries {
		mt, _ := time.Parse(time.RFC3339Nano, e.MTime)
		size := e.Size
		files := int64(0)
		if e.Summ != nil {
			size = e.Summ.Size
			files = e.Summ.Files
		}
		typ := e.Type
		if typ == "f" {
			typ = "file"
		} else if typ == "d" {
			typ = "dir"
		} else if typ == "l" {
			typ = "symlink"
		} else {
			typ = "other"
		}
		entries = append(entries, contracts.FileEntry{
			Name: e.Name, Type: typ, Size: size, MTime: mt, Mode: e.Mode, Files: files,
		})
	}
	return entries, nil
}

// OpenObject abre un objeto del repositorio como lectura en streaming
// (`kopia show`). El llamador debe cerrar el lector.
func (k *KopiaControl) OpenObject(ctx context.Context, objPath string) (io.ReadCloser, error) {
	cctx, cancel := context.WithCancel(ctx)
	cmd := exec.CommandContext(cctx, k.Binary, "--config-file", k.ConfigFile, "show", objPath)
	cmd.Env = k.baseEnv()
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		cancel()
		return nil, err
	}
	return &objectReader{ReadCloser: stdout, cancel: cancel, cmd: cmd}, nil
}

// objectReader cierra el proceso subyacente al terminar la lectura.
type objectReader struct {
	io.ReadCloser
	cancel context.CancelFunc
	cmd    *exec.Cmd
}

func (o *objectReader) Close() error {
	err := o.ReadCloser.Close()
	werr := o.cmd.Wait()
	o.cancel()
	if err != nil {
		return err
	}
	return werr
}

// StreamObject escribe el contenido de un objeto en la respuesta.
func (k *KopiaControl) StreamObject(w http.ResponseWriter, r *http.Request, objPath string) {
	rc, err := k.OpenObject(r.Context(), objPath)
	if err != nil {
		sproblem(w, http.StatusInternalServerError, "E_INTERNAL", "No se pudo iniciar la descarga")
		return
	}
	defer rc.Close()
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	// Los errores tardíos (stderr de Kopia) no pueden cambiar las cabeceras
	// ya enviadas; la descarga simplemente queda truncada.
	_, _ = io.Copy(w, rc)
}

func sproblem(w http.ResponseWriter, status int, code, detail string) {
	w.Header().Set("Content-Type", problemMediaType)
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(contracts.Problem{
		Type: "about:blank", Title: http.StatusText(status), Status: status, Code: code, Detail: detail,
	})
}

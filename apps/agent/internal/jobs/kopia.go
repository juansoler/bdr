package jobs

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/juansoler/bdr/packages/contracts"
	"github.com/juansoler/bdr/packages/logging"
)

// KopiaOptions configura el runner real de Kopia.
type KopiaOptions struct {
	// Binary es la ruta o nombre del binario (por defecto "kopia").
	Binary string
	// LegacyConfigFile se usa tal cual cuando el servidor no entrega
	// credenciales de repositorio (comportamiento antiguo --kopia-config).
	LegacyConfigFile string
	// DataDir aloja la configuración y la caché de Kopia (DataDir/kopia).
	DataDir string
	Log     *logging.Logger
}

// KopiaRunner ejecuta el binario de Kopia.
type KopiaRunner struct {
	binary       string
	legacyConfig string
	dataDir      string
	log          *logging.Logger

	mu           sync.Mutex
	connectedSig string

	vmu     sync.Mutex
	version string
}

// NewKopiaRunner crea un runner compatible con la firma antigua.
func NewKopiaRunner(binary, configFile string) *KopiaRunner {
	return NewKopiaRunnerWithOptions(KopiaOptions{Binary: binary, LegacyConfigFile: configFile})
}

// NewKopiaRunnerWithOptions crea el runner real.
func NewKopiaRunnerWithOptions(opts KopiaOptions) *KopiaRunner {
	binary := opts.Binary
	if binary == "" {
		binary = "kopia"
	}
	return &KopiaRunner{
		binary:       binary,
		legacyConfig: opts.LegacyConfigFile,
		dataDir:      opts.DataDir,
		log:          opts.Log,
	}
}

// Snapshot captura una ruta aplicando exclusiones y retención como política.
func (r *KopiaRunner) Snapshot(ctx context.Context, req SnapshotRequest) (SnapshotResult, error) {
	if _, err := exec.LookPath(r.binary); err != nil {
		return SnapshotResult{}, &CodeError{
			Code:    "E_KOPIA_MISSING",
			Message: "no se encontró el binario de Kopia: " + r.binary,
		}
	}
	if err := r.ensureConnected(ctx, req.Credentials); err != nil {
		return SnapshotResult{}, err
	}
	if err := r.applyPolicy(ctx, req.Credentials, req.Path, req.Excludes, req.Retention); err != nil {
		return SnapshotResult{}, err
	}

	args := []string{"snapshot", "create", req.Path, "--json"}
	desc := "bdr:job=" + req.JobID
	if req.PlanID != "" {
		desc += " plan=" + req.PlanID
		args = append(args, "--tags=bdr:plan="+req.PlanID)
	}
	args = append(args, "--description="+desc)
	args = append(args, r.commandArgs(req.Credentials)...)

	out, stderr, err := r.runConnected(ctx, req.Credentials, args)
	if err != nil {
		return SnapshotResult{}, r.classify(ctx, err, stderr, req.Credentials, "capturar "+req.Path)
	}

	snap, err := parseSnapshotJSON(out)
	if err != nil {
		return SnapshotResult{}, &CodeError{
			Code:    "E_KOPIA_OUTPUT",
			Message: "la salida de Kopia no es JSON válido: " + err.Error(),
		}
	}

	stats := r.snapshotStats(ctx, req.Credentials, req.Path, snap.ID)
	errorCount := snap.RootEntry.Summ.NumFailed
	if errorCount == 0 {
		errorCount = stats.ErrorCount
	}
	summary := contracts.SnapshotSummary{
		Path:          req.Path,
		ID:            snap.ID,
		RootObjectID:  snap.RootEntry.Obj,
		SourceHost:    snap.Source.Host,
		SourceUser:    snap.Source.UserName,
		StartTime:     snap.StartTime,
		EndTime:       snap.EndTime,
		SizeBytes:     snap.RootEntry.Summ.Size,
		Files:         snap.RootEntry.Summ.Files,
		Dirs:          snap.RootEntry.Summ.Dirs,
		ErrorCount:    errorCount,
		ExcludedFiles: stats.ExcludedFileCount,
		ExcludedDirs:  stats.ExcludedDirCount,
	}
	return SnapshotResult{
		Summary:      summary,
		KopiaVersion: r.Version(ctx),
		Raw:          append(json.RawMessage(nil), out...),
	}, nil
}

// Restore restaura un snapshot o un elemento concreto en un destino.
func (r *KopiaRunner) Restore(ctx context.Context, spec RestoreSpec) (RestoreResult, error) {
	if strings.TrimSpace(spec.Source) == "" || strings.TrimSpace(spec.Destination) == "" {
		return RestoreResult{}, &CodeError{Code: "E_PAYLOAD", Message: "origen y destino de restauración son obligatorios"}
	}
	if _, err := exec.LookPath(r.binary); err != nil {
		return RestoreResult{}, &CodeError{
			Code:    "E_KOPIA_MISSING",
			Message: "no se encontró el binario de Kopia: " + r.binary,
		}
	}
	if err := r.ensureConnected(ctx, spec.Credentials); err != nil {
		return RestoreResult{}, err
	}

	args := []string{"snapshot", "restore", spec.Source, spec.Destination}
	if spec.Overwrite {
		args = append(args, "--overwrite-files", "--overwrite-directories", "--overwrite-symlinks")
	} else {
		// Aunque el executor ya comprueba el destino, estos flags evitan
		// cualquier sobrescritura si el destino aparece en carrera.
		args = append(args, "--no-overwrite-files", "--no-overwrite-directories", "--no-overwrite-symlinks")
	}
	args = append(args, r.commandArgs(spec.Credentials)...)

	_, stderr, err := r.runConnected(ctx, spec.Credentials, args)
	if err != nil {
		return RestoreResult{}, r.classify(ctx, err, stderr, spec.Credentials, "restaurar "+spec.Source)
	}

	files, dirs, bytesRestored := collectRestoreStats(spec.Destination)
	return RestoreResult{
		Files:        files,
		Dirs:         dirs,
		Bytes:        bytesRestored,
		KopiaVersion: r.Version(ctx),
	}, nil
}

// Version devuelve la versión de Kopia (cacheada), o "" si no se pudo obtener.
func (r *KopiaRunner) Version(ctx context.Context) string {
	r.vmu.Lock()
	v := r.version
	r.vmu.Unlock()
	if v != "" {
		return v
	}
	out, _, err := r.runRaw(ctx, nil, []string{"--version"})
	if err != nil {
		return ""
	}
	fields := strings.Fields(string(out))
	if len(fields) == 0 {
		return ""
	}
	v = fields[0]
	r.vmu.Lock()
	r.version = v
	r.vmu.Unlock()
	return v
}

// ConnectArgs construye los argumentos de `kopia repository connect`.
// La contraseña nunca forma parte de los argumentos: se pasa por KOPIA_PASSWORD.
func ConnectArgs(creds contracts.RepositoryCredentials, configFile, cacheDir string) ([]string, error) {
	kind := creds.Kind
	if kind == "" {
		if creds.URL != "" {
			kind = contracts.RepoKindServer
		} else {
			kind = contracts.RepoKindFilesystem
		}
	}
	var args []string
	switch kind {
	case contracts.RepoKindServer:
		if creds.URL == "" {
			return nil, &CodeError{Code: "E_KOPIA_AUTH", Message: "las credenciales del repositorio no incluyen URL"}
		}
		args = []string{"repository", "connect", "server", "--url=" + creds.URL}
		if fp := normalizeFingerprint(creds.Fingerprint); fp != "" {
			args = append(args, "--server-cert-fingerprint="+fp)
		}
		if creds.Username != "" {
			args = append(args, "--override-username="+creds.Username)
		}
		if creds.Hostname != "" {
			args = append(args, "--override-hostname="+creds.Hostname)
		}
	case contracts.RepoKindFilesystem:
		if creds.Path == "" {
			return nil, &CodeError{Code: "E_KOPIA_AUTH", Message: "las credenciales del repositorio no incluyen ruta"}
		}
		args = []string{"repository", "connect", "filesystem", "--path=" + creds.Path}
	case contracts.RepoKindS3:
		if creds.Endpoint == "" || creds.Bucket == "" {
			return nil, &CodeError{Code: "E_KOPIA_AUTH", Message: "las credenciales del repositorio S3 no incluyen endpoint o bucket"}
		}
		args = []string{"repository", "connect", "s3",
			"--endpoint=" + creds.Endpoint, "--bucket=" + creds.Bucket}
		if creds.Prefix != "" {
			args = append(args, "--prefix="+strings.Trim(creds.Prefix, "/")+"/")
		}
		if creds.AccessKey != "" {
			args = append(args,
				"--access-key="+creds.AccessKey,
				"--secret-access-key="+creds.SecretKey)
		}
		if creds.NoTLS {
			args = append(args, "--disable-tls")
		}
	default:
		return nil, &CodeError{Code: "E_KOPIA_AUTH", Message: "tipo de repositorio no soportado: " + kind}
	}
	if configFile != "" {
		args = append(args, "--config-file="+configFile)
	}
	if cacheDir != "" {
		args = append(args, "--cache-directory="+cacheDir)
	}
	args = append(args, "--no-use-keychain", "--no-persist-credentials")
	return args, nil
}

// ensureConnected conecta al repositorio si las credenciales cambiaron.
// Con creds == nil se mantiene el comportamiento antiguo: se usa el archivo
// de configuración indicado (o el de Kopia por defecto) sin conectar.
func (r *KopiaRunner) ensureConnected(ctx context.Context, creds *contracts.RepositoryCredentials) error {
	if creds == nil {
		return nil
	}
	sig := repoSignature(creds)
	r.mu.Lock()
	if r.connectedSig == sig {
		r.mu.Unlock()
		return nil
	}
	r.mu.Unlock()

	if !creds.ExpiresAt.IsZero() && time.Now().After(creds.ExpiresAt) {
		return &CodeError{Code: "E_KOPIA_AUTH", Message: "las credenciales del repositorio han caducado"}
	}
	cfg := r.activeConfigFile(creds)
	if cfg == "" {
		return &CodeError{Code: "E_KOPIA_AUTH", Message: "no hay configuración de Kopia para conectar"}
	}
	if err := os.MkdirAll(filepath.Dir(cfg), 0o700); err != nil {
		return &CodeError{Code: "E_KOPIA_FAILED", Message: "no se pudo crear el directorio de configuración de Kopia"}
	}
	cache := ""
	if r.dataDir != "" {
		cache = filepath.Join(r.dataDir, "kopia", "cache")
		if err := os.MkdirAll(cache, 0o700); err != nil {
			return &CodeError{Code: "E_KOPIA_FAILED", Message: "no se pudo crear la caché de Kopia"}
		}
	}
	args, err := ConnectArgs(*creds, cfg, cache)
	if err != nil {
		return err
	}
	_, stderr, err := r.runRaw(ctx, creds, args)
	if err != nil {
		// Una conexión fallida puede dejar un archivo a medias.
		_ = os.Remove(cfg)
		return r.classify(ctx, err, stderr, creds, "conectar con el repositorio")
	}
	r.mu.Lock()
	r.connectedSig = sig
	r.mu.Unlock()
	if r.log != nil {
		r.log.Info("repositorio de Kopia conectado", "kind", creds.Kind, "version", creds.Version)
	}
	return nil
}

func (r *KopiaRunner) resetConnected() {
	r.mu.Lock()
	r.connectedSig = ""
	r.mu.Unlock()
}

// applyPolicy fija las exclusiones y la retención como política de Kopia.
// `--clear-ignore` y `--add-ignore` no se pueden combinar en una sola
// invocación en Kopia 0.23.1 (clear gana), por eso se ejecutan por separado.
func (r *KopiaRunner) applyPolicy(ctx context.Context, creds *contracts.RepositoryCredentials, snapshotPath string, excludes []string, retention *contracts.Retention) error {
	patterns := make([]string, 0, len(excludes))
	for _, ex := range excludes {
		if p, ok := IgnorePattern(ex, snapshotPath); ok {
			patterns = append(patterns, p)
		} else if r.log != nil {
			r.log.Debug("exclusión ignorada por quedar fuera de la ruta", "exclude", ex, "path", snapshotPath)
		}
	}

	if len(patterns) > 0 {
		clearArgs := append([]string{"policy", "set", snapshotPath, "--clear-ignore"}, r.commandArgs(creds)...)
		if _, stderr, err := r.runConnected(ctx, creds, clearArgs); err != nil {
			return r.classify(ctx, err, stderr, creds, "limpiar exclusiones de "+snapshotPath)
		}
	}
	if len(patterns) == 0 && retention == nil {
		return nil
	}

	args := []string{"policy", "set", snapshotPath}
	for _, p := range patterns {
		args = append(args, "--add-ignore="+p)
	}
	if retention != nil {
		args = append(args, retentionArgs(*retention)...)
	}
	args = append(args, r.commandArgs(creds)...)
	if _, stderr, err := r.runConnected(ctx, creds, args); err != nil {
		return r.classify(ctx, err, stderr, creds, "aplicar política a "+snapshotPath)
	}
	return nil
}

func retentionArgs(r contracts.Retention) []string {
	return []string{
		fmt.Sprintf("--keep-latest=%d", r.KeepLatest),
		fmt.Sprintf("--keep-hourly=%d", r.KeepHourly),
		fmt.Sprintf("--keep-daily=%d", r.KeepDaily),
		fmt.Sprintf("--keep-weekly=%d", r.KeepWeekly),
		fmt.Sprintf("--keep-monthly=%d", r.KeepMonthly),
		fmt.Sprintf("--keep-annual=%d", r.KeepAnnual),
	}
}

// snapshotStats obtiene contadores de excluidos/errores de `snapshot list`.
// La salida de `snapshot create` no los incluye. Es best-effort.
func (r *KopiaRunner) snapshotStats(ctx context.Context, creds *contracts.RepositoryCredentials, snapshotPath, id string) kopiaStats {
	args := append([]string{"snapshot", "list", snapshotPath, "--json"}, r.commandArgs(creds)...)
	out, _, err := r.runConnected(ctx, creds, args)
	if err != nil {
		return kopiaStats{}
	}
	var list []kopiaSnapshotJSON
	if err := json.Unmarshal(out, &list); err != nil {
		return kopiaStats{}
	}
	for _, s := range list {
		if s.ID == id && s.Stats != nil {
			return *s.Stats
		}
	}
	return kopiaStats{}
}

func (r *KopiaRunner) activeConfigFile(creds *contracts.RepositoryCredentials) string {
	if creds != nil && r.dataDir != "" {
		// Cada repositorio destino (S3 o filesystem con ID de destino propio)
		// usa su propia config para no pisar la del repo primario del agente.
		if creds.RepositoryID != "" && creds.RepositoryID != "primary" {
			return filepath.Join(r.dataDir, "kopia", "kopia-"+creds.RepositoryID+".config")
		}
		return filepath.Join(r.dataDir, "kopia", "kopia.config")
	}
	return r.legacyConfig
}

func (r *KopiaRunner) commandArgs(creds *contracts.RepositoryCredentials) []string {
	var args []string
	if cfg := r.activeConfigFile(creds); cfg != "" {
		args = append(args, "--config-file="+cfg)
	}
	args = append(args, "--no-use-keychain")
	return args
}

// runConnected garantiza la conexión y reintenta una vez si Kopia perdió la
// conexión (por ejemplo, si el archivo de configuración desapareció).
func (r *KopiaRunner) runConnected(ctx context.Context, creds *contracts.RepositoryCredentials, args []string) ([]byte, []byte, error) {
	if err := r.ensureConnected(ctx, creds); err != nil {
		return nil, nil, err
	}
	out, stderr, err := r.runRaw(ctx, creds, args)
	if err == nil {
		return out, stderr, nil
	}
	if ctx.Err() != nil {
		return out, stderr, &CodeError{Code: "E_CANCELED", Message: "operación cancelada"}
	}
	if creds != nil && strings.Contains(strings.ToLower(string(stderr)), "not connected") {
		r.resetConnected()
		if cerr := r.ensureConnected(ctx, creds); cerr != nil {
			return nil, nil, cerr
		}
		return r.runRaw(ctx, creds, args)
	}
	return out, stderr, err
}

// RunRaw ejecuta kopia con argumentos arbitrarios sobre la conexión del
// agente (creds); usado por la captura de imágenes. Devuelve stdout y error
// con la primera línea de stderr.
func (r *KopiaRunner) RunRaw(ctx context.Context, creds *contracts.RepositoryCredentials, timeout time.Duration, args ...string) (string, error) {
	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	if creds != nil {
		if err := r.ensureConnected(cctx, creds); err != nil {
			return "", err
		}
		args = append(r.commandArgs(creds), args...)
	}
	out, stderr, err := r.runRaw(cctx, creds, args)
	if err != nil {
		msg := strings.TrimSpace(string(stderr))
		if msg == "" {
			msg = strings.TrimSpace(string(out))
		}
		return string(out), fmt.Errorf("kopia: %s", msg)
	}
	return string(out), nil
}

func (r *KopiaRunner) runRaw(ctx context.Context, creds *contracts.RepositoryCredentials, args []string) ([]byte, []byte, error) {
	if _, err := exec.LookPath(r.binary); err != nil {
		return nil, nil, &CodeError{
			Code:    "E_KOPIA_MISSING",
			Message: "no se encontró el binario de Kopia: " + r.binary,
		}
	}
	cmd := exec.CommandContext(ctx, r.binary, args...)
	cmd.Env = kopiaEnv(creds)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err != nil {
		if ctx.Err() != nil {
			return nil, stderr.Bytes(), &CodeError{Code: "E_CANCELED", Message: "operación cancelada"}
		}
		if errors.Is(err, exec.ErrNotFound) {
			return nil, stderr.Bytes(), &CodeError{Code: "E_KOPIA_MISSING", Message: "no se encontró Kopia"}
		}
	}
	return stdout.Bytes(), stderr.Bytes(), err
}

func (r *KopiaRunner) classify(ctx context.Context, err error, stderr []byte, creds *contracts.RepositoryCredentials, action string) error {
	if ctx.Err() != nil {
		return &CodeError{Code: "E_CANCELED", Message: "job cancelado"}
	}
	var ce *CodeError
	if errors.As(err, &ce) {
		return ce
	}
	password := ""
	if creds != nil {
		password = creds.Password
	}
	detail := sanitizeKopia(stderr, password)
	if detail == "" && err != nil {
		detail = sanitizeKopia([]byte(err.Error()), password)
	}
	code := "E_KOPIA_FAILED"
	low := strings.ToLower(detail)
	switch {
	case strings.Contains(low, "invalid repository password"),
		strings.Contains(low, "invalid credentials"),
		strings.Contains(low, "access denied"),
		strings.Contains(low, "authentication failed"),
		strings.Contains(low, "not authorized"),
		strings.Contains(low, "unauthorized"),
		strings.Contains(low, "repository is not connected"),
		strings.Contains(low, "not connected"):
		code = "E_KOPIA_AUTH"
	case strings.Contains(low, "repository is locked"), strings.Contains(low, "repository locked"):
		code = "E_KOPIA_LOCK"
	case strings.Contains(low, "out of space"), strings.Contains(low, "quota exceeded"),
		strings.Contains(low, "no space left"):
		code = "E_KOPIA_QUOTA"
	}
	return &CodeError{Code: code, Message: action + ": " + detail}
}

func kopiaEnv(creds *contracts.RepositoryCredentials) []string {
	env := append(os.Environ(), "KOPIA_CHECK_FOR_UPDATES=false")
	if creds != nil && creds.Password != "" {
		// La contraseña solo viaja por entorno, nunca por argv.
		env = append(env, "KOPIA_PASSWORD="+creds.Password)
	}
	return env
}

func repoSignature(creds *contracts.RepositoryCredentials) string {
	sum := sha256.Sum256([]byte(creds.Password))
	return fmt.Sprintf("%s|%d|%s|%s|%s|%s|%s|%s|%s|%s",
		creds.RepositoryID, creds.Version, creds.Kind, creds.URL, creds.Path,
		creds.Endpoint, creds.Bucket, creds.Prefix, creds.AccessKey, hex.EncodeToString(sum[:]))
}

func normalizeFingerprint(s string) string {
	s = strings.TrimSpace(strings.ToLower(s))
	s = strings.TrimPrefix(s, "sha256:")
	s = strings.ReplaceAll(s, ":", "")
	return s
}

func sanitizeKopia(raw []byte, password string) string {
	s := string(raw)
	if password != "" {
		s = strings.ReplaceAll(s, password, "***")
	}
	s = strings.Join(strings.Fields(s), " ")
	if s == "" {
		return "sin detalle"
	}
	if len(s) > 500 {
		s = s[:500] + "…"
	}
	return s
}

func collectRestoreStats(dest string) (files, dirs, bytesRestored int64) {
	info, err := os.Lstat(dest)
	if err != nil {
		return 0, 0, 0
	}
	if !info.IsDir() {
		return 1, 0, info.Size()
	}
	_ = filepath.Walk(dest, func(p string, fi os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if p == dest {
			return nil
		}
		if fi.IsDir() {
			dirs++
			return nil
		}
		files++
		if fi.Mode().IsRegular() {
			bytesRestored += fi.Size()
		}
		return nil
	})
	return files, dirs, bytesRestored
}

// --- Parseo de la salida JSON de Kopia ---

type kopiaSnapshotJSON struct {
	ID          string      `json:"id"`
	Description string      `json:"description"`
	Source      kopiaSource `json:"source"`
	StartTime   time.Time   `json:"startTime"`
	EndTime     time.Time   `json:"endTime"`
	RootEntry   struct {
		Name string `json:"name"`
		Type string `json:"type"`
		Obj  string `json:"obj"`
		Summ struct {
			Size      int64 `json:"size"`
			Files     int64 `json:"files"`
			Symlinks  int64 `json:"symlinks"`
			Dirs      int64 `json:"dirs"`
			NumFailed int   `json:"numFailed"`
		} `json:"summ"`
	} `json:"rootEntry"`
	Stats *kopiaStats `json:"stats,omitempty"`
}

type kopiaSource struct {
	Host     string `json:"host"`
	UserName string `json:"userName"`
	Path     string `json:"path"`
}

type kopiaStats struct {
	TotalSize         int64 `json:"totalSize"`
	FileCount         int64 `json:"fileCount"`
	DirCount          int64 `json:"dirCount"`
	ExcludedFileCount int64 `json:"excludedFileCount"`
	ExcludedDirCount  int64 `json:"excludedDirCount"`
	ErrorCount        int   `json:"errorCount"`
}

func parseSnapshotJSON(data []byte) (kopiaSnapshotJSON, error) {
	var out kopiaSnapshotJSON
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 {
		return out, errors.New("salida vacía")
	}
	// Por si el binario imprime alguna línea de progreso antes del JSON.
	if i := bytes.IndexByte(trimmed, '{'); i > 0 {
		trimmed = trimmed[i:]
	}
	if err := json.Unmarshal(trimmed, &out); err != nil {
		return out, err
	}
	if out.ID == "" {
		return out, errors.New("falta el campo id")
	}
	return out, nil
}

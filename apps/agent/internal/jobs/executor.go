// Package jobs ejecuta los trabajos que recibe el agente.
package jobs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/juansoler/bdr/packages/contracts"
	"github.com/juansoler/bdr/packages/logging"
)

// CodeError asocia un error a un código del catálogo de la API.
type CodeError struct {
	Code    string
	Message string
}

func (e *CodeError) Error() string { return e.Message }

// ErrorCode devuelve el código de un CodeError o E_JOB_FAILED.
func ErrorCode(err error) string {
	var ce *CodeError
	if errors.As(err, &ce) && ce.Code != "" {
		return ce.Code
	}
	return "E_JOB_FAILED"
}

// SnapshotRequest describe una captura de una ruta.
type SnapshotRequest struct {
	Path        string
	JobID       string
	PlanID      string
	Excludes    []string
	Retention   *contracts.Retention
	Credentials *contracts.RepositoryCredentials
}

// SnapshotResult es el resultado de una captura.
type SnapshotResult struct {
	Summary      contracts.SnapshotSummary
	KopiaVersion string
	Raw          json.RawMessage `json:"raw,omitempty"`
}

// RestoreSpec describe una restauración de Kopia.
type RestoreSpec struct {
	Source      string // id de snapshot o id/elemento
	Destination string
	Overwrite   bool
	Credentials *contracts.RepositoryCredentials
}

// RestoreResult es el resultado medido de una restauración.
type RestoreResult struct {
	Files        int64
	Dirs         int64
	Bytes        int64
	KopiaVersion string
}

// Runner abstrae el motor de backup (Kopia en producción, fake en tests).
type Runner interface {
	Snapshot(ctx context.Context, req SnapshotRequest) (SnapshotResult, error)
	Restore(ctx context.Context, spec RestoreSpec) (RestoreResult, error)
	Version(ctx context.Context) string
}

// Progress es una actualización de progreso de un job.
type Progress struct {
	Percent    int
	Message    string
	BytesDone  int64
	BytesTotal int64
}

// RunOptions reúne las dependencias de una ejecución de job.
type RunOptions struct {
	Credentials *contracts.RepositoryCredentials
	Progress    func(Progress)
	Log         func(level, message string)
}

// Executor ejecuta jobs BACKUP_FILE y RESTORE_FILE.
type Executor struct {
	Runner Runner
	Log    *logging.Logger

	imageRunner ImageCapturer
	vmRunner    VMOperator
	lxcRunner   LXOperator
	sqlRunner   SQLRunner
}

// NewExecutor crea un ejecutor.
func NewExecutor(r Runner, log *logging.Logger) *Executor {
	return &Executor{Runner: r, Log: log}
}

// Execute procesa un job y devuelve el resultado JSON.
func (e *Executor) Execute(ctx context.Context, job contracts.JobView, opts RunOptions) (json.RawMessage, error) {
	if e.Runner == nil {
		return nil, &CodeError{Code: "E_INTERNAL", Message: "runner no configurado"}
	}
	switch job.Type {
	case contracts.JobTypeBackupFile:
		return e.executeBackup(ctx, job, opts)
	case contracts.JobTypeRestoreFile:
		return e.executeRestore(ctx, job, opts)
	case contracts.JobTypeBackupImage:
		return e.executeBackupImage(ctx, job, opts)
	case contracts.JobTypeBackupVM:
		return e.executeBackupVM(ctx, job, opts)
	case contracts.JobTypeRestoreVM:
		return e.executeRestoreVM(ctx, job, opts)
	case contracts.JobTypeBackupLXC:
		return e.executeBackupLXC(ctx, job, opts)
	case contracts.JobTypeRestoreLXC:
		return e.executeRestoreLXC(ctx, job, opts)
	case contracts.JobTypeSQLBackup:
		return e.executeSQLBackup(ctx, job, opts)
	case contracts.JobTypeSQLRestore:
		return e.executeSQLRestore(ctx, job, opts)
	case contracts.JobTypeSQLVerify:
		return e.executeSQLVerify(ctx, job, opts)
	case contracts.JobTypeSQLInventory:
		return e.executeSQLInventory(ctx, job, opts)
	default:
		return nil, &CodeError{Code: "E_JOB_TYPE", Message: "tipo de job no soportado: " + job.Type}
	}
}

// ImageCapturer captura imágenes completas de disco (BACKUP_IMAGE); opcional
// para que los tests y agentes antiguos sin soporte sigan funcionando.
type ImageCapturer interface {
	Capture(ctx context.Context, creds *contracts.RepositoryCredentials,
		payload contracts.ImageJobPayload, progress func(percent int, msg string, bytesDone int64)) (json.RawMessage, error)
}

// SetImageRunner inyecta el runner de imágenes.
func (e *Executor) SetImageRunner(r ImageCapturer) { e.imageRunner = r }

// VMOperator ejecuta backup/restore de VMs de Proxmox en el nodo PVE.
type VMOperator interface {
	BackupVM(ctx context.Context, creds *contracts.RepositoryCredentials,
		payload contracts.BackupVMJobPayload, progress func(percent int, msg string, bytesDone int64)) (json.RawMessage, error)
	RestoreVM(ctx context.Context, creds *contracts.RepositoryCredentials,
		payload contracts.RestoreVMJobPayload, progress func(percent int, msg string, bytesDone int64)) (json.RawMessage, error)
}

// SetVMRunner inyecta el runner de VMs.
func (e *Executor) SetVMRunner(r VMOperator) { e.vmRunner = r }

// LXOperator ejecuta backup/restore de contenedores LXC de Proxmox en el
// nodo PVE (opcional: agentes antiguos sin soporte responden E_JOB_TYPE).
type LXOperator interface {
	BackupLXC(ctx context.Context, creds *contracts.RepositoryCredentials,
		payload contracts.BackupLXCJobPayload, progress func(percent int, msg string, bytesDone int64)) (json.RawMessage, error)
	RestoreLXC(ctx context.Context, creds *contracts.RepositoryCredentials,
		payload contracts.RestoreLXCJobPayload, progress func(percent int, msg string, bytesDone int64)) (json.RawMessage, error)
}

// SetLXCRunner inyecta el runner de contenedores LXC.
func (e *Executor) SetLXCRunner(r LXOperator) { e.lxcRunner = r }

// SQLRunner ejecuta los jobs del motor SQL Server (RF-40/A-11): backup,
// restauración, verificación e inventario sobre la instancia del host.
// Opcional: los agentes sin soporte SQL responden E_JOB_TYPE, como el resto
// de runners.
type SQLRunner interface {
	Backup(ctx context.Context, creds *contracts.RepositoryCredentials,
		payload contracts.SQLBackupJobPayload, progress func(percent int, msg string, bytesDone int64)) (json.RawMessage, error)
	Restore(ctx context.Context, creds *contracts.RepositoryCredentials,
		payload contracts.SQLRestoreJobPayload, progress func(percent int, msg string, bytesDone int64)) (json.RawMessage, error)
	Verify(ctx context.Context, creds *contracts.RepositoryCredentials,
		payload contracts.SQLVerifyJobPayload, progress func(percent int, msg string, bytesDone int64)) (json.RawMessage, error)
	Inventory(ctx context.Context, payload contracts.SQLInventoryJobPayload) (json.RawMessage, error)
}

// SetSQLRunner inyecta el runner del motor SQL Server.
func (e *Executor) SetSQLRunner(r SQLRunner) { e.sqlRunner = r }

func (e *Executor) executeBackupLXC(ctx context.Context, job contracts.JobView, opts RunOptions) (json.RawMessage, error) {
	if e.lxcRunner == nil {
		return nil, &CodeError{Code: "E_JOB_TYPE",
			Message: "este agente no soporta backup de contenedores LXC (actualiza el agente en el nodo PVE)"}
	}
	var payload contracts.BackupLXCJobPayload
	if err := json.Unmarshal(job.Payload, &payload); err != nil {
		return nil, &CodeError{Code: "E_PAYLOAD", Message: "payload del job inválido"}
	}
	return e.lxcRunner.BackupLXC(ctx, opts.Credentials, payload, func(percent int, msg string, bytesDone int64) {
		e.progress(opts, Progress{Percent: percent, Message: msg, BytesDone: bytesDone, BytesTotal: bytesDone})
		e.log(opts, "info", msg)
	})
}

func (e *Executor) executeRestoreLXC(ctx context.Context, job contracts.JobView, opts RunOptions) (json.RawMessage, error) {
	if e.lxcRunner == nil {
		return nil, &CodeError{Code: "E_JOB_TYPE",
			Message: "este agente no soporta restore de contenedores LXC (actualiza el agente en el nodo PVE)"}
	}
	var payload contracts.RestoreLXCJobPayload
	if err := json.Unmarshal(job.Payload, &payload); err != nil {
		return nil, &CodeError{Code: "E_PAYLOAD", Message: "payload del job inválido"}
	}
	return e.lxcRunner.RestoreLXC(ctx, opts.Credentials, payload, func(percent int, msg string, bytesDone int64) {
		e.progress(opts, Progress{Percent: percent, Message: msg, BytesDone: bytesDone, BytesTotal: bytesDone})
		e.log(opts, "info", msg)
	})
}

func (e *Executor) executeSQLBackup(ctx context.Context, job contracts.JobView, opts RunOptions) (json.RawMessage, error) {
	if e.sqlRunner == nil {
		return nil, &CodeError{Code: "E_JOB_TYPE",
			Message: "este agente no soporta el motor SQL Server (instala el agente en el host SQL, versión >= 0.14.0)"}
	}
	var payload contracts.SQLBackupJobPayload
	if err := json.Unmarshal(job.Payload, &payload); err != nil {
		return nil, &CodeError{Code: "E_PAYLOAD", Message: "payload del job inválido"}
	}
	return e.sqlRunner.Backup(ctx, opts.Credentials, payload, func(percent int, msg string, bytesDone int64) {
		e.progress(opts, Progress{Percent: percent, Message: msg, BytesDone: bytesDone, BytesTotal: bytesDone})
		e.log(opts, "info", msg)
	})
}

func (e *Executor) executeSQLRestore(ctx context.Context, job contracts.JobView, opts RunOptions) (json.RawMessage, error) {
	if e.sqlRunner == nil {
		return nil, &CodeError{Code: "E_JOB_TYPE",
			Message: "este agente no soporta el motor SQL Server (instala el agente en el host SQL, versión >= 0.14.0)"}
	}
	var payload contracts.SQLRestoreJobPayload
	if err := json.Unmarshal(job.Payload, &payload); err != nil {
		return nil, &CodeError{Code: "E_PAYLOAD", Message: "payload del job inválido"}
	}
	return e.sqlRunner.Restore(ctx, opts.Credentials, payload, func(percent int, msg string, bytesDone int64) {
		e.progress(opts, Progress{Percent: percent, Message: msg, BytesDone: bytesDone, BytesTotal: bytesDone})
		e.log(opts, "info", msg)
	})
}

func (e *Executor) executeSQLVerify(ctx context.Context, job contracts.JobView, opts RunOptions) (json.RawMessage, error) {
	if e.sqlRunner == nil {
		return nil, &CodeError{Code: "E_JOB_TYPE",
			Message: "este agente no soporta el motor SQL Server (instala el agente en el host SQL, versión >= 0.14.0)"}
	}
	var payload contracts.SQLVerifyJobPayload
	if err := json.Unmarshal(job.Payload, &payload); err != nil {
		return nil, &CodeError{Code: "E_PAYLOAD", Message: "payload del job inválido"}
	}
	return e.sqlRunner.Verify(ctx, opts.Credentials, payload, func(percent int, msg string, bytesDone int64) {
		e.progress(opts, Progress{Percent: percent, Message: msg, BytesDone: bytesDone, BytesTotal: bytesDone})
		e.log(opts, "info", msg)
	})
}

func (e *Executor) executeSQLInventory(ctx context.Context, job contracts.JobView, opts RunOptions) (json.RawMessage, error) {
	if e.sqlRunner == nil {
		return nil, &CodeError{Code: "E_JOB_TYPE",
			Message: "este agente no soporta el motor SQL Server (instala el agente en el host SQL, versión >= 0.14.0)"}
	}
	var payload contracts.SQLInventoryJobPayload
	if err := json.Unmarshal(job.Payload, &payload); err != nil {
		return nil, &CodeError{Code: "E_PAYLOAD", Message: "payload del job inválido"}
	}
	return e.sqlRunner.Inventory(ctx, payload)
}

func (e *Executor) executeBackupVM(ctx context.Context, job contracts.JobView, opts RunOptions) (json.RawMessage, error) {
	if e.vmRunner == nil {
		return nil, &CodeError{Code: "E_JOB_TYPE",
			Message: "este agente no soporta backup de VMs (instálalo en el nodo PVE con agente >= 0.7.0)"}
	}
	var payload contracts.BackupVMJobPayload
	if err := json.Unmarshal(job.Payload, &payload); err != nil {
		return nil, &CodeError{Code: "E_PAYLOAD", Message: "payload del job inválido"}
	}
	return e.vmRunner.BackupVM(ctx, opts.Credentials, payload, func(percent int, msg string, bytesDone int64) {
		e.progress(opts, Progress{Percent: percent, Message: msg, BytesDone: bytesDone, BytesTotal: bytesDone})
		e.log(opts, "info", msg)
	})
}

func (e *Executor) executeRestoreVM(ctx context.Context, job contracts.JobView, opts RunOptions) (json.RawMessage, error) {
	if e.vmRunner == nil {
		return nil, &CodeError{Code: "E_JOB_TYPE",
			Message: "este agente no soporta restore de VMs (instálalo en el nodo PVE con agente >= 0.7.0)"}
	}
	var payload contracts.RestoreVMJobPayload
	if err := json.Unmarshal(job.Payload, &payload); err != nil {
		return nil, &CodeError{Code: "E_PAYLOAD", Message: "payload del job inválido"}
	}
	return e.vmRunner.RestoreVM(ctx, opts.Credentials, payload, func(percent int, msg string, bytesDone int64) {
		e.progress(opts, Progress{Percent: percent, Message: msg, BytesDone: bytesDone, BytesTotal: bytesDone})
		e.log(opts, "info", msg)
	})
}

func (e *Executor) executeBackupImage(ctx context.Context, job contracts.JobView, opts RunOptions) (json.RawMessage, error) {
	if e.imageRunner == nil {
		return nil, &CodeError{Code: "E_JOB_TYPE",
			Message: "este agente no soporta captura de imagen (actualiza a >= 0.6.0)"}
	}
	var payload contracts.ImageJobPayload
	if err := json.Unmarshal(job.Payload, &payload); err != nil {
		return nil, &CodeError{Code: "E_PAYLOAD", Message: "payload del job inválido"}
	}
	return e.imageRunner.Capture(ctx, opts.Credentials, payload, func(percent int, msg string, bytesDone int64) {
		e.progress(opts, Progress{Percent: percent, Message: msg, BytesDone: bytesDone, BytesTotal: bytesDone})
		e.log(opts, "info", msg)
	})
}

func (e *Executor) executeBackup(ctx context.Context, job contracts.JobView, opts RunOptions) (json.RawMessage, error) {
	var payload contracts.BackupJobPayload
	if err := json.Unmarshal(job.Payload, &payload); err != nil {
		return nil, &CodeError{Code: "E_PAYLOAD", Message: "payload del job inválido"}
	}
	if len(payload.Paths) == 0 {
		return nil, &CodeError{Code: "E_PLAN_EMPTY", Message: "el plan no tiene rutas"}
	}

	start := time.Now()
	result := contracts.BackupJobResult{Snapshots: make([]contracts.SnapshotSummary, 0, len(payload.Paths))}
	if payload.Retention != nil {
		result.RetentionNote = "política de retención aplicada a las rutas del plan"
	}

	for i, p := range payload.Paths {
		if err := ctx.Err(); err != nil {
			return nil, &CodeError{Code: "E_CANCELED", Message: "job cancelado"}
		}
		base := (i * 100) / len(payload.Paths)
		e.progress(opts, Progress{Percent: base, Message: "capturando " + p, BytesDone: result.BytesTotal})
		e.log(opts, "info", fmt.Sprintf("iniciando captura de %s", p))

		res, err := e.Runner.Snapshot(ctx, SnapshotRequest{
			Path:        p,
			JobID:       job.ID,
			PlanID:      payload.PlanID,
			Excludes:    payload.Excludes,
			Retention:   payload.Retention,
			Credentials: opts.Credentials,
		})
		if err != nil {
			if ctx.Err() != nil {
				return nil, &CodeError{Code: "E_CANCELED", Message: "job cancelado"}
			}
			return nil, normalizeRunnerError(err)
		}
		result.Snapshots = append(result.Snapshots, res.Summary)
		result.BytesTotal += res.Summary.SizeBytes
		if res.KopiaVersion != "" {
			result.KopiaVersion = res.KopiaVersion
		}

		done := ((i + 1) * 100) / len(payload.Paths)
		e.progress(opts, Progress{
			Percent: done, Message: fmt.Sprintf("capturado %s", p),
			BytesDone: result.BytesTotal, BytesTotal: result.BytesTotal,
		})
		e.log(opts, "info", fmt.Sprintf("captura completada de %s (%d bytes)", p, res.Summary.SizeBytes))
	}

	result.DurationMS = time.Since(start).Milliseconds()
	out, err := json.Marshal(result)
	if err != nil {
		return nil, &CodeError{Code: "E_INTERNAL", Message: "no se pudo serializar el resultado"}
	}
	return out, nil
}

func (e *Executor) executeRestore(ctx context.Context, job contracts.JobView, opts RunOptions) (json.RawMessage, error) {
	var payload contracts.RestoreJobPayload
	if err := json.Unmarshal(job.Payload, &payload); err != nil {
		return nil, &CodeError{Code: "E_PAYLOAD", Message: "payload del job inválido"}
	}
	sourceID := strings.TrimSpace(payload.SnapshotID)
	if sourceID == "" {
		sourceID = strings.TrimSpace(payload.RootObjectID)
	}
	if sourceID == "" {
		return nil, &CodeError{Code: "E_PAYLOAD", Message: "el job de restauración no indica snapshot"}
	}

	items := payload.Items
	if len(items) == 0 {
		items = []string{""}
	}
	for _, item := range items {
		if item != "" {
			if err := ValidateRestoreItem(item); err != nil {
				return nil, err
			}
		}
	}

	baseDest := payload.Destination
	if payload.InPlace {
		baseDest = payload.SourcePath
	}
	if err := ValidateRestoreDestination(baseDest, payload.InPlace); err != nil {
		return nil, err
	}

	start := time.Now()
	result := contracts.RestoreJobResult{Restored: make([]contracts.RestoredItem, 0, len(items))}
	for i, item := range items {
		if err := ctx.Err(); err != nil {
			return nil, &CodeError{Code: "E_CANCELED", Message: "job cancelado"}
		}
		dest := baseDest
		if item != "" {
			dest = filepath.Join(baseDest, filepath.FromSlash(item))
		}
		if !payload.Overwrite {
			if exists, err := pathExists(dest); err != nil || exists {
				return nil, &CodeError{
					Code:    "E_RESTORE_DEST",
					Message: "el destino ya existe y no se permite sobrescribir: " + dest,
				}
			}
		}
		if item != "" {
			if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
				return nil, &CodeError{Code: "E_RESTORE_DEST", Message: "no se pudo crear el directorio destino: " + err.Error()}
			}
		}

		source := sourceID
		label := "restaurando snapshot completo"
		if item != "" {
			source = sourceID + "/" + item
			label = "restaurando " + item
		}
		e.progress(opts, Progress{Percent: (i * 100) / len(items), Message: label})
		e.log(opts, "info", label+" en "+dest)

		res, err := e.Runner.Restore(ctx, RestoreSpec{
			Source:      source,
			Destination: dest,
			Overwrite:   payload.Overwrite,
			Credentials: opts.Credentials,
		})
		if err != nil {
			if ctx.Err() != nil {
				return nil, &CodeError{Code: "E_CANCELED", Message: "job cancelado"}
			}
			return nil, normalizeRunnerError(err)
		}
		result.Restored = append(result.Restored, contracts.RestoredItem{
			Item:        item,
			Destination: dest,
			Files:       res.Files,
			Dirs:        res.Dirs,
			Bytes:       res.Bytes,
		})
		e.progress(opts, Progress{Percent: ((i + 1) * 100) / len(items), Message: "restaurado " + dest})
		e.log(opts, "info", fmt.Sprintf("restauración completada en %s (%d archivos)", dest, res.Files))
	}

	result.DurationMS = time.Since(start).Milliseconds()
	out, err := json.Marshal(result)
	if err != nil {
		return nil, &CodeError{Code: "E_INTERNAL", Message: "no se pudo serializar el resultado"}
	}
	return out, nil
}

// ValidateRestoreItem comprueba que un elemento sea relativo al snapshot y no
// permita salir de él. Acepta separador "/" (y "\" por compatibilidad Windows).
func ValidateRestoreItem(item string) error {
	s := strings.TrimSpace(item)
	if s == "" {
		return &CodeError{Code: "E_PAYLOAD", Message: "elemento de restauración vacío"}
	}
	norm := strings.ReplaceAll(s, `\`, "/")
	if strings.HasPrefix(norm, "/") || isAbsPattern(norm, true) {
		return &CodeError{Code: "E_PAYLOAD", Message: "el elemento de restauración no puede ser absoluto: " + item}
	}
	clean := path.Clean(norm)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") {
		return &CodeError{Code: "E_PAYLOAD", Message: "el elemento de restauración sale del snapshot: " + item}
	}
	for _, part := range strings.Split(norm, "/") {
		if part == ".." {
			return &CodeError{Code: "E_PAYLOAD", Message: "el elemento de restauración sale del snapshot: " + item}
		}
	}
	return nil
}

// ValidateRestoreDestination exige un destino absoluto (salvo restauración
// en el sitio, donde la base es SourcePath y también debe ser absoluta).
func ValidateRestoreDestination(dest string, inPlace bool) error {
	if strings.TrimSpace(dest) == "" {
		if inPlace {
			return &CodeError{Code: "E_RESTORE_DEST", Message: "la restauración en el sitio requiere source_path"}
		}
		return &CodeError{Code: "E_RESTORE_DEST", Message: "el destino de restauración es obligatorio"}
	}
	if !filepath.IsAbs(dest) {
		return &CodeError{Code: "E_RESTORE_DEST", Message: "el destino debe ser una ruta absoluta: " + dest}
	}
	return nil
}

func pathExists(p string) (bool, error) {
	_, err := os.Lstat(p)
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return true, err
}

func (e *Executor) progress(opts RunOptions, p Progress) {
	if opts.Progress != nil {
		opts.Progress(p)
	}
}

func (e *Executor) log(opts RunOptions, level, message string) {
	if opts.Log != nil {
		opts.Log(level, message)
	}
	if e.Log != nil {
		switch level {
		case "warn":
			e.Log.Warn(message)
		case "error":
			e.Log.Error(message)
		default:
			e.Log.Info(message)
		}
	}
}

func normalizeRunnerError(err error) error {
	var ce *CodeError
	if errors.As(err, &ce) {
		return ce
	}
	return &CodeError{Code: "E_KOPIA_FAILED", Message: err.Error()}
}

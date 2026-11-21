// Package agent implementa el ciclo de ejecución del agente.
package agent

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/juansoler/bdr/apps/agent/internal/client"
	"github.com/juansoler/bdr/apps/agent/internal/config"
	"github.com/juansoler/bdr/apps/agent/internal/identity"
	"github.com/juansoler/bdr/apps/agent/internal/inventory"
	"github.com/juansoler/bdr/apps/agent/internal/jobs"
	"github.com/juansoler/bdr/apps/agent/internal/upgrade"
	"github.com/juansoler/bdr/packages/contracts"
	"github.com/juansoler/bdr/packages/logging"
	"github.com/juansoler/bdr/packages/version"
)

// Options configura el ciclo de ejecución.
type Options struct {
	Client            *client.Client
	Config            config.Config
	DataDir           string
	Log               *logging.Logger
	Executor          *jobs.Executor
	HeartbeatInterval time.Duration
	InventoryInterval time.Duration
	JobInterval       time.Duration
	// LeaseWaitSeconds usa long-poll al pedir jobs (0 en modo --once).
	LeaseWaitSeconds int
	// RestartSelf re-ejecuta el proceso con el binario recién reemplazado
	// (no retorna). Lo inyecta el main según plataforma; si es nil, la
	// autoactualización queda deshabilitada.
	RestartSelf func(exe string) error
}

// Agent mantiene el heartbeat y ejecuta los jobs asignados.
type Agent struct {
	client     *client.Client
	dataDir    string
	log        *logging.Logger
	executor   *jobs.Executor
	hbInterval time.Duration
	invEvery   time.Duration
	jobEvery   time.Duration
	leaseWait  int

	cfgMu sync.Mutex
	cfg   config.Config

	restartSelf func(exe string) error

	mu          sync.Mutex
	activeID    string
	activeState string
	cancel      context.CancelFunc
}

// New crea un agente a partir de las opciones.
func New(opts Options) *Agent {
	hb := opts.HeartbeatInterval
	if hb <= 0 {
		hb = time.Duration(opts.Config.HeartbeatSeconds) * time.Second
	}
	if hb <= 0 {
		hb = 30 * time.Second
	}
	if hb < 5*time.Second {
		hb = 5 * time.Second
	}
	inv := opts.InventoryInterval
	if inv <= 0 {
		inv = 6 * time.Hour
	}
	jobEvery := opts.JobInterval
	if jobEvery <= 0 {
		jobEvery = 15 * time.Second
	}
	if jobEvery < 5*time.Second {
		jobEvery = 5 * time.Second
	}
	return &Agent{
		client:      opts.Client,
		dataDir:     opts.DataDir,
		log:         opts.Log,
		executor:    opts.Executor,
		hbInterval:  hb,
		invEvery:    inv,
		jobEvery:    jobEvery,
		leaseWait:   opts.LeaseWaitSeconds,
		cfg:         opts.Config,
		restartSelf: opts.RestartSelf,
	}
}

// Run arranca el heartbeat y el bucle de jobs hasta que se cancele ctx.
func (a *Agent) Run(ctx context.Context, once bool) error {
	hbCtx, hbCancel := context.WithCancel(ctx)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		a.heartbeatLoop(hbCtx)
	}()

	a.sendInventory(ctx)

	if once {
		a.runOneJob(ctx)
		hbCancel()
		wg.Wait()
		return nil
	}

	jobTicker := time.NewTicker(a.jobEvery)
	invTicker := time.NewTicker(a.invEvery)
	defer jobTicker.Stop()
	defer invTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			hbCancel()
			wg.Wait()
			a.log.Info("agente detenido")
			return nil
		case <-jobTicker.C:
			a.runOneJob(ctx)
		case <-invTicker.C:
			a.sendInventory(ctx)
		}
	}
}

// --- Heartbeat y certificado ---

func (a *Agent) heartbeatLoop(ctx context.Context) {
	a.beat(ctx)
	ticker := time.NewTicker(a.hbInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			a.beat(ctx)
		}
	}
}

func (a *Agent) beat(ctx context.Context) {
	state, jobID := a.current()
	reqCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	resp, err := a.client.Heartbeat(reqCtx, contracts.HeartbeatRequest{
		AgentVersion: version.Version,
		State:        state,
		CurrentJobID: jobID,
	})
	if err != nil {
		if ctx.Err() == nil {
			a.log.Warn("heartbeat fallido", "error", err.Error())
		}
		return
	}
	a.log.Debug("heartbeat enviado", "server_time", resp.ServerTime.Format(time.RFC3339), "state", state)
	for _, id := range resp.CancelRequested {
		a.requestCancel(id)
	}
	if resp.CertRenewAfter != nil && time.Now().After(*resp.CertRenewAfter) {
		a.renewCertificate(ctx)
	}
	if state == contracts.StateIdle && upgrade.OfferValid(resp.Upgrade, version.Version) {
		a.applyUpgrade(ctx, *resp.Upgrade)
	}
	if resp.AgentURL != "" && resp.AgentURL != a.cfg.Endpoint() {
		a.log.Info("agente: URL del plano de control actualizada por el servidor", "agent_url", resp.AgentURL, "anterior", a.cfg.Endpoint())
		if err := a.updateEndpoint(ctx, resp.AgentURL); err != nil {
			a.log.Warn("no se pudo actualizar la URL del agente", "error", err.Error())
		}
	}
}

// updateEndpoint persiste y aplica la URL del plano de control reportada por
// el servidor (heartbeat). Cubre el drift entre el --agent-url con el que se
// enroló el agente y el actual (p. ej. LAN → NetBird): sin esto el TLS se
// sigue validando contra un host obsoleto y todas las llamadas fallan.
func (a *Agent) updateEndpoint(ctx context.Context, endpoint string) error {
	a.cfgMu.Lock()
	a.cfg.AgentURL = endpoint
	cfg := a.cfg
	a.cfgMu.Unlock()
	if err := config.Save(a.dataDir, cfg); err != nil {
		return err
	}
	keyPEM, certPEM, caPEM, err := identity.Load(a.dataDir)
	if err != nil {
		return err
	}
	opts := client.Options{BaseURL: endpoint, APIKey: cfg.APIKey, CertPEM: certPEM, KeyPEM: keyPEM, CAPEM: caPEM}
	c, err := client.NewWithOptions(opts)
	if err != nil {
		return err
	}
	a.mu.Lock()
	a.client = c
	a.mu.Unlock()
	return nil
}

// applyUpgrade se autoactualiza: descarga, verifica y reemplaza el binario;
// restartSelf no retorna (re-ejecución Unix o reinicio por el gestor de
// servicios de Windows). Un fallo aquí solo se registra: el siguiente
// heartbeat reintenta.
func (a *Agent) applyUpgrade(ctx context.Context, u contracts.AgentUpgrade) {
	if a.restartSelf == nil {
		return
	}
	// URL relativa: se resuelve contra el servidor con el que ya habla el
	// agente (NetBird, LAN…) — el control no necesita --public-url.
	if strings.HasPrefix(u.URL, "/") {
		u.URL = strings.TrimRight(a.cfg.Endpoint(), "/") + u.URL
	}
	a.log.Info("autoactualización disponible", "version", u.Version)
	if err := upgrade.Replace(ctx, a.log, u, a.restartSelf); err != nil {
		a.log.Warn("autoactualización fallida", "error", err.Error())
	}
}

func (a *Agent) renewCertificate(ctx context.Context) {
	keyPEM, _, _, err := identity.Load(a.dataDir)
	if err != nil {
		a.log.Warn("no se puede renovar el certificado: falta la clave", "error", err.Error())
		return
	}
	hostname := a.hostname()
	csrPEM, err := identity.NewCSR(keyPEM, hostname)
	if err != nil {
		a.log.Warn("no se pudo crear la CSR de renovación", "error", err.Error())
		return
	}
	reqCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	resp, err := a.client.RenewCert(reqCtx, string(csrPEM))
	if err != nil {
		a.log.Warn("renovación de certificado fallida", "error", err.Error())
		return
	}
	if err := identity.Save(a.dataDir, keyPEM, []byte(resp.CertificatePEM), []byte(resp.CAPEM)); err != nil {
		a.log.Error("no se pudo guardar el certificado renovado", "error", err.Error())
		return
	}
	if err := a.client.SetTLS([]byte(resp.CertificatePEM), keyPEM, []byte(resp.CAPEM)); err != nil {
		a.log.Error("no se pudo aplicar el certificado renovado", "error", err.Error())
		return
	}
	a.cfgMu.Lock()
	a.cfg.CertExpiresAt = &resp.CertificateExpiry
	cfg := a.cfg
	a.cfgMu.Unlock()
	if err := config.Save(a.dataDir, cfg); err != nil {
		a.log.Warn("no se pudo persistir la fecha del certificado", "error", err.Error())
	}
	a.log.Info("certificado mTLS renovado", "expira", resp.CertificateExpiry.Format(time.RFC3339))
}

func (a *Agent) hostname() string {
	a.cfgMu.Lock()
	h := a.cfg.Hostname
	a.cfgMu.Unlock()
	if h != "" {
		return h
	}
	if osHost, err := os.Hostname(); err == nil && osHost != "" {
		return osHost
	}
	return "bdr-agent"
}

// --- Inventario ---

func (a *Agent) sendInventory(ctx context.Context) {
	invCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	inv, err := inventory.Collect(invCtx)
	if err != nil {
		a.log.Warn("inventario no recogido", "error", err.Error())
		return
	}
	if err := a.client.PutInventory(invCtx, inv); err != nil {
		a.log.Warn("inventario no enviado", "error", err.Error())
		return
	}
	a.log.Debug("inventario enviado", "hostname", inv.Hostname)
}

// --- Jobs ---

func (a *Agent) runOneJob(ctx context.Context) {
	wait := a.leaseWait
	if wait < 0 {
		wait = 0
	}
	leaseCtx, cancel := context.WithTimeout(ctx, time.Duration(wait+10)*time.Second)
	defer cancel()
	job, err := a.client.LeaseJob(leaseCtx, wait)
	if err != nil {
		if ctx.Err() == nil {
			a.log.Warn("no se pudo pedir job", "error", err.Error())
		}
		return
	}
	if job == nil {
		return
	}
	a.executeJob(ctx, job)
}

func (a *Agent) executeJob(ctx context.Context, job *contracts.JobView) {
	state := contracts.StateRunning
	if job.Type == contracts.JobTypeRestoreFile {
		state = contracts.StateRestoring
	}

	jobCtx, jobCancel := context.WithCancel(ctx)
	defer jobCancel()
	a.setActive(job.ID, state, jobCancel)
	defer a.clearActive(job.ID)

	a.log.Info("job recibido", "job_id", job.ID, "type", job.Type)

	// Si el plan apunta a un destino de almacenamiento propio, las
	// credenciales son del repositorio de ese destino (Kopia S3); si no,
	// las del repo primario.
	targetID := storageTargetFromJob(*job)
	var creds *contracts.RepositoryCredentials
	var err error
	if targetID != "" {
		a.log.Info("usando repositorio del destino", "target", targetID)
		creds, err = a.client.RepositoryCredentialsFor(ctx, targetID)
	} else {
		creds, err = a.client.RepositoryCredentials(ctx)
	}
	if err != nil {
		a.completeWithError(ctx, job.ID, jobs.ErrorCode(err), "no se pudieron obtener las credenciales del repositorio: "+err.Error())
		return
	}
	if creds == nil {
		a.log.Info("repositorio no configurado en el servidor; se usará la configuración local de Kopia")
	}
	if creds != nil {
		a.log.Debug("credenciales de repositorio recibidas", "kind", creds.Kind, "version", creds.Version)
	}

	batcher := newLogBatcher(a.client, job.ID, a.log)
	defer batcher.Close()

	opts := jobs.RunOptions{
		Credentials: creds,
		Progress: func(p jobs.Progress) {
			pctx, cancel := context.WithTimeout(ctx, 15*time.Second)
			defer cancel()
			resp, perr := a.client.JobProgress(pctx, job.ID, contracts.JobProgressRequest{
				Progress:   p.Percent,
				Message:    p.Message,
				BytesDone:  p.BytesDone,
				BytesTotal: p.BytesTotal,
			})
			if perr != nil {
				a.log.Debug("progreso no enviado", "job_id", job.ID, "error", perr.Error())
				return
			}
			if resp.CancelRequested {
				a.log.Info("cancelación solicitada por el servidor", "job_id", job.ID)
				a.requestCancel(job.ID)
			}
		},
		Log: func(level, message string) {
			batcher.Add(level, message)
		},
	}

	batcher.Add("info", "job iniciado ("+job.Type+")")
	result, execErr := a.executor.Execute(jobCtx, *job, opts)
	if execErr != nil {
		if jobCtx.Err() != nil || jobs.ErrorCode(execErr) == "E_CANCELED" {
			batcher.Add("warn", "job cancelado")
			a.complete(ctx, job.ID, contracts.JobCompleteRequest{
				State:        contracts.JobCanceled,
				ErrorCode:    "E_CANCELED",
				ErrorMessage: "cancelado a petición del administrador",
			})
			a.log.Info("job cancelado", "job_id", job.ID)
			return
		}
		code := jobs.ErrorCode(execErr)
		batcher.Add("error", "job fallido: "+execErr.Error())
		a.completeWithError(ctx, job.ID, code, execErr.Error())
		a.log.Error("job fallido", "job_id", job.ID, "code", code, "error", execErr.Error())
		return
	}

	batcher.Add("info", "job completado correctamente")
	if err := a.complete(ctx, job.ID, contracts.JobCompleteRequest{
		State: contracts.JobSucceeded, Result: result,
	}); err != nil {
		a.log.Warn("no se pudo cerrar el job", "job_id", job.ID, "error", err.Error())
		return
	}
	a.log.Info("job completado", "job_id", job.ID)
}

func (a *Agent) completeWithError(ctx context.Context, jobID, code, message string) {
	if err := a.complete(ctx, jobID, contracts.JobCompleteRequest{
		State: contracts.JobFailed, ErrorCode: code, ErrorMessage: message,
	}); err != nil {
		a.log.Warn("no se pudo reportar el fallo del job", "job_id", jobID, "error", err.Error())
	}
}

func (a *Agent) complete(ctx context.Context, jobID string, req contracts.JobCompleteRequest) error {
	cctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	return a.client.CompleteJob(cctx, jobID, req)
}

// --- Estado compartido con el heartbeat ---

func (a *Agent) setActive(jobID, state string, cancel context.CancelFunc) {
	a.mu.Lock()
	a.activeID, a.activeState, a.cancel = jobID, state, cancel
	a.mu.Unlock()
}

func (a *Agent) clearActive(jobID string) {
	a.mu.Lock()
	if a.activeID == jobID {
		a.activeID, a.activeState, a.cancel = "", "", nil
	}
	a.mu.Unlock()
}

// storageTargetFromJob extrae el destino de almacenamiento del payload
// (BackupJobPayload / ImageJobPayload); vacío si no aplica.
func storageTargetFromJob(job contracts.JobView) string {
	var p struct {
		StorageTargetID string `json:"storage_target_id"`
	}
	if json.Unmarshal(job.Payload, &p) != nil {
		return ""
	}
	return p.StorageTargetID
}

func (a *Agent) current() (state, jobID string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.activeID == "" {
		return contracts.StateIdle, ""
	}
	return a.activeState, a.activeID
}

func (a *Agent) requestCancel(jobID string) {
	a.mu.Lock()
	id := a.activeID
	cancel := a.cancel
	a.mu.Unlock()
	if id == jobID && cancel != nil {
		cancel()
	}
}

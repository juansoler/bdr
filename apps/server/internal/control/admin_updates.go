// Chequeo de actualizaciones del plano de control (modo notificación).
//
// Un bucle consulta el manifiesto firmado del canal de releases y compara
// versiones. Si hay una versión posterior, la deja expuesta en el estado
// en memoria (el chequeo se repite al arrancar, así que no necesita
// persistencia) y la registra en auditoría. El panel muestra la
// notificación y SOLO aplica si el administrador lo confirma: la
// instalación descarga el bundle, verifica SHA-256, sustituye el binario
// (con .old) y re-ejecuta el proceso en Unix. En Windows devuelve
// instrucciones: el binario no puede sustituirse a sí mismo.
package control

import (
	"context"
	"math/rand"
	"net/http"
	"os"
	"sync/atomic"
	"time"

	"github.com/juansoler/bdr/apps/server/internal/selfupdate"
	"github.com/juansoler/bdr/packages/contracts"
	"github.com/juansoler/bdr/packages/version"
)

// UpdateURL es la URL del manifiesto; UpdatePublicKey la clave pública del
// canal (vacía = chequeo deshabilitado). Se fijan en main tras New.
var (
	// checkInterval es el periodo base entre chequeos (1 h menos jitter):
	// ágil para que una release recién publicada se notice en el panel sin
	// esperar horas, sin abusar del canal (fichero estático de GitHub).
	checkInterval = time.Hour
	// initialDelay retrasa el primer chequeo para no retrasar el arranque.
	initialDelay = 5 * time.Second
)

// Estado del último chequeo (en memoria; el bucle corre también al arrancar).
var updateState struct {
	available atomic.Pointer[selfupdate.Manifest]
	// lastManifest guarda el último manifiesto verificado (aunque no sea
	// más nuevo que la versión en marcha): alimenta las ofertas de agente
	// cuando falta agents.json junto al binario del control.
	lastManifest atomic.Pointer[selfupdate.Manifest]
	checkedAt    atomic.Pointer[time.Time]
	lastError    atomic.Pointer[string]
	// installing evita lanzar dos instalaciones a la vez.
	installing atomic.Bool
	// checking evita solapar el chequeo manual (POST /check) con el del bucle.
	checking atomic.Bool
}

// auditCtx audita con un contexto propio (chequeos fuera de una petición).
func (s *Server) auditCtx(ctx context.Context, a *actor, action, targetType, targetID, result string, details map[string]string) {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "/", nil)
	s.audit(req, a, action, targetType, targetID, result, details)
}

// startUpdateLoop lanza el bucle de chequeo con jitter para que varios
// servidores no consulten a la vez.
func (s *Server) startUpdateLoop(ctx context.Context) {
	if !s.updateCheckEnabled() {
		s.Logger.Info("canal de actualizaciones deshabilitado (sin --update-url o sin clave pública embebida)")
		return
	}
	go func() {
		timer := time.NewTimer(initialDelay)
		defer timer.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-timer.C:
				s.checkUpdate(ctx)
				// Siguientes chequeos con jitter para que varios
				// servidores no consulten a la vez.
				next := checkInterval + time.Duration(rand.Int63n(int64(checkInterval/6)))
				timer.Reset(next)
			}
		}
	}()
}

// updateCheckEnabled informa si el canal está operativo.
func (s *Server) updateCheckEnabled() bool {
	return s.UpdateURL != "" && s.UpdatePublicKey != ""
}

// checkUpdate consulta el manifiesto y, si hay versión posterior, la deja
// en memoria y audita el aviso. Un fallo solo se registra.
func (s *Server) checkUpdate(ctx context.Context) {
	m, err := selfupdate.Check(ctx, nil, s.UpdateURL, s.UpdatePublicKey)
	now := s.Now().UTC()
	ts := now
	updateState.checkedAt.Store(&ts)
	if err != nil {
		msg := err.Error()
		updateState.lastError.Store(&msg)
		s.Logger.Debug("chequeo de actualizaciones fallido", "error", msg)
		return
	}
	updateState.lastError.Store(nil)
	updateState.lastManifest.Store(m)
	_, newer := m.Newer(version.Version)
	if newer {
		if prev := updateState.available.Load(); prev == nil || prev.Version != m.Version {
			s.auditCtx(ctx, s.systemActor(), "update.available", "system", m.Version, "ok",
				map[string]string{"current": version.Version, "channel": m.Channel})
		}
		updateState.available.Store(m)
		s.Logger.Info("actualización disponible", "current", version.Version, "latest", m.Version)
	} else {
		updateState.available.Store(nil)
	}
}

// systemActor es el actor usado en la auditoría del propio sistema.
func (s *Server) systemActor() *actor {
	return &actor{kind: "system", id: "system", name: "bdr-control", role: contracts.RoleAdmin}
}

func (s *Server) handleSystemUpdate(w http.ResponseWriter, r *http.Request) {
	if !allowMethod(w, r, http.MethodGet) {
		return
	}
	if _, _, ok := s.authorize(w, r, "read"); !ok {
		return
	}
	writeJSON(w, http.StatusOK, s.updateStatusPayload())
}

// updateStatusPayload construye el estado del canal de actualizaciones.
func (s *Server) updateStatusPayload() contracts.UpdateStatus {
	status := contracts.UpdateStatus{
		CheckEnabled: s.updateCheckEnabled(),
		Current:      version.Version,
		Platform:     selfupdate.PlatformKey(),
	}
	if ts := updateState.checkedAt.Load(); ts != nil {
		t := *ts
		status.CheckedAt = &t
	}
	if msg := updateState.lastError.Load(); msg != nil {
		status.LastError = *msg
	}
	if m := updateState.available.Load(); m != nil {
		status.Latest = m.Version
		status.Channel = m.Channel
		status.NotesURL = m.NotesURL
		status.UpdateAvailable = true
	}
	return status
}

// handleSystemUpdateCheck fuerza un chequeo inmediato del canal y devuelve
// el estado fresco: así el panel no depende de la cadencia del bucle (1 h)
// para enterarse de una release recién publicada.
func (s *Server) handleSystemUpdateCheck(w http.ResponseWriter, r *http.Request) {
	if !allowMethod(w, r, http.MethodPost) {
		return
	}
	if _, _, ok := s.authorize(w, r, "admin"); !ok {
		return
	}
	if s.updateCheckEnabled() && !updateState.checking.Swap(true) {
		defer updateState.checking.Store(false)
		// Contexto propio: el chequeo completa aunque el cliente cancele la
		// petición a medias.
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		s.checkUpdate(ctx)
	}
	writeJSON(w, http.StatusOK, s.updateStatusPayload())
}

// handleSystemUpdateInstall aplica la actualización pendiente bajo
// confirmación explícita del administrador (modo notificación).
func (s *Server) handleSystemUpdateInstall(w http.ResponseWriter, r *http.Request) {
	if !allowMethod(w, r, http.MethodPost) {
		return
	}
	r, a, ok := s.authorize(w, r, "admin")
	if !ok {
		return
	}
	m := updateState.available.Load()
	if m == nil {
		s.problem(w, r, http.StatusConflict, "E_UPDATE_NONE", "No hay actualización pendiente")
		return
	}
	if updateState.installing.Swap(true) {
		s.problem(w, r, http.StatusConflict, "E_UPDATE_BUSY", "Ya hay una instalación en curso")
		return
	}
	defer updateState.installing.Store(false)

	exe, err := os.Executable()
	if err != nil {
		s.problem(w, r, http.StatusInternalServerError, "E_UPDATE_EXE", "No se resolvió el binario en ejecución: "+err.Error())
		return
	}
	if _, ok := m.Newer(version.Version); !ok {
		s.problem(w, r, http.StatusConflict, "E_UPDATE_NONE", "El manifiesto ya no ofrece una versión posterior")
		return
	}
	s.audit(r, a, "update.install.started", "system", m.Version, "ok",
		map[string]string{"from": version.Version, "platform": selfupdate.PlatformKey()})

	placed, err := selfupdate.Apply(r.Context(), nil, m, exe, s.DownloadsDir)
	if err != nil {
		s.audit(r, a, "update.install.failed", "system", m.Version, "error",
			map[string]string{"error": err.Error()})
		s.problem(w, r, http.StatusBadRequest, "E_UPDATE_FAILED", err.Error())
		return
	}
	s.audit(r, a, "update.install.staged", "system", m.Version, "ok",
		map[string]string{"path": placed})

	// Re-ejecución in-place: el proceso se sustituye con la misma línea de
	// argumentos; systemd/launchd/servicio de Windows no perciben cambio de
	// PID. Si exec falla, el binario .old permite rollback manual.
	go func() {
		time.Sleep(200 * time.Millisecond)
		_ = reexecSelf(placed)
	}()
	writeJSON(w, http.StatusAccepted, contracts.UpdateInstallResponse{
		Restarting: true,
		Version:    m.Version,
		Message:    "Actualización preparada; el servidor se reinicia en unos segundos",
	})
}

// reexecSelf reemplaza el proceso por el binario nuevo (Unix). En Windows
// Apply devuelve error antes de llegar aquí.
func reexecSelf(exe string) error {
	return syscallExec(exe, os.Args, os.Environ())
}

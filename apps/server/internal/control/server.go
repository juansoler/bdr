// Package control implementa la API HTTP del plano de control.
package control

import (
	"bufio"
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/juansoler/bdr/apps/server/internal/auth"
	"github.com/juansoler/bdr/apps/server/internal/pki"
	"github.com/juansoler/bdr/apps/server/internal/remoteaccess"
	"github.com/juansoler/bdr/apps/server/internal/secrets"
	"github.com/juansoler/bdr/apps/server/internal/store"
	"github.com/juansoler/bdr/apps/server/internal/webui"
	"github.com/juansoler/bdr/packages/contracts"
	"github.com/juansoler/bdr/packages/logging"
)

const (
	defaultHeartbeat = 30
	maxBodyAdmin     = 1 << 20 // 1 MiB
	maxBodyAgent     = 4 << 20 // 4 MiB
	requestIDHeader  = "X-Request-Id"
	sessionCookie    = "bdr_session"
	csrfHeader       = "X-CSRF-Token"
	problemMediaType = "application/problem+json"
	onlineWindowBase = 90 * time.Second
	maxTokenTTL      = 24 * time.Hour
	leaseDuration    = 10 * time.Minute
	leaseGrace       = 2 * time.Minute
	idempotencyTTL   = 24 * time.Hour
)

type ctxKey string

const (
	requestIDKey ctxKey = "request_id"
	actorKey     ctxKey = "actor"
)

// Capabilities por rol.
var roleCaps = map[string][]string{
	contracts.RoleViewer:   {"read"},
	contracts.RoleOperator: {"read", "operate", contracts.CapTerminalRead, contracts.CapTerminalConnect},
	contracts.RoleAdmin: {"read", "operate", "admin",
		contracts.CapTerminalRead, contracts.CapTerminalConnect,
		contracts.CapTerminalElevated, contracts.CapTerminalTerminate, contracts.CapTerminalAudit},
}

// Server contiene las dependencias de la API.
type Server struct {
	Store            store.Store
	AdminToken       string
	Logger           *logging.Logger
	HeartbeatSeconds int
	Version          string
	Now              func() time.Time
	Box              *secrets.Box
	CA               *pki.CA
	Kopia            *KopiaControl
	// DirLister sustituye el acceso de lectura al snapshot en tests (nil en
	// producción: se usa Kopia si hay conexión directa).
	DirLister DirLister
	// DownloadsDir sirve los binarios de agentes para los instaladores
	// preconfigurados (bdr-agent-{osarch} dentro del directorio).
	DownloadsDir string
	// PublicURL es la URL con la que los agentes alcanzan la API HTTP.
	PublicURL string
	// AllowedOrigins lista de orígenes con permiso CORS para la API de
	// administración (solo desarrollo: el panel embebido es mismo origen).
	AllowedOrigins []string
	// UpdateURL es el manifiesto del canal de releases (latest.json).
	UpdateURL string
	// UpdatePublicKey es la clave pública Ed25519 base64 que verifica el
	// manifiesto. Vacía: chequeo deshabilitado (fallo cerrado).
	UpdatePublicKey string
	// AgentPublicURL es el listener TLS para agentes, si está activo.
	AgentPublicURL string
	// CertFingerprint es la huella de la CA interna ("sha256:<hex>").
	CertFingerprint string
	// RepoKeyProvider devuelve la contraseña del repositorio Kopia.
	RepoPassword   string
	StartedAt      time.Time
	notify         *Notifier
	mu             sync.Mutex
	loginAttempts  map[string][]time.Time
	alertInFlight  map[string]bool
	binarySHA      *shaCache
	lastTick       time.Time
	SchedulerEvery time.Duration

	// RemoteAccess es el gestor de sesiones de terminal remota
	// (subsistema independiente; comparte RBAC, mTLS y auditoría).
	RemoteAccess *remoteaccess.Manager
}

// New crea un servidor de control.
func New(st store.Store, adminToken string, logger *logging.Logger, version string) *Server {
	if logger == nil {
		logger = logging.New("info")
	}
	s := &Server{
		Store:            st,
		AdminToken:       adminToken,
		Logger:           logger,
		HeartbeatSeconds: defaultHeartbeat,
		Version:          version,
		Now:              time.Now,
		StartedAt:        time.Now().UTC(),
		loginAttempts:    map[string][]time.Time{},
		alertInFlight:    map[string]bool{},
		binarySHA:        newSHACache(),
		SchedulerEvery:   time.Minute,
		RemoteAccess:     remoteaccess.New(st, logger),
	}
	s.notify = NewNotifier(s)
	return s
}

// Start arranca los bucles de fondo (programador, notificaciones, réplicas
// y chequeo del canal de actualizaciones).
func (s *Server) Start(ctx context.Context) {
	go s.notify.Run(ctx)
	go s.RunScheduler(ctx)
	go s.storageSyncLoop(ctx)
	go s.startUpdateLoop(ctx)
	go s.terminalReaperLoop(ctx)
}

// terminalReaperLoop expira sesiones de terminal pendientes sin respuesta
// del agente y cierra las expiradas; sin esto, una sesión huérfana deja al
// navegador en «conectando» indefinidamente.
func (s *Server) terminalReaperLoop(ctx context.Context) {
	if s.RemoteAccess == nil {
		return
	}
	t := time.NewTicker(30 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			s.RemoteAccess.ReapSessions()
		}
	}
}

// Handler devuelve el router completo con middlewares.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", s.handleHealth)
	mux.HandleFunc("/readyz", s.handleReady)

	mux.HandleFunc("/api/v1/download/", s.handleDownloads)
	mux.HandleFunc("/api/v1/admin/installers", s.handleInstallers)
	mux.HandleFunc("/api/v1/admin/proxmox/instances", s.handleProxmoxInstances)
	mux.HandleFunc("/api/v1/admin/proxmox/instances/", s.handleProxmoxInstances)
	mux.HandleFunc("/api/v1/admin/proxmox/restore", s.handleProxmoxRestore)
	mux.HandleFunc("/api/v1/admin/proxmox/manifest/", s.handleProxmoxManifestRoute)
	mux.HandleFunc("/api/v1/admin/storage/targets", s.handleStorageTargets)
	mux.HandleFunc("/api/v1/admin/storage/targets/", s.handleStorageTargets)

	// Autenticación de administración (sesión + TOTP).
	mux.HandleFunc("/api/v1/auth/bootstrap", s.handleAuthBootstrap)
	mux.HandleFunc("/api/v1/auth/login", s.handleAuthLogin)
	mux.HandleFunc("/api/v1/auth/mfa", s.handleAuthMFA)
	mux.HandleFunc("/api/v1/auth/logout", s.handleAuthLogout)
	mux.HandleFunc("/api/v1/auth/me", s.handleAuthMe)
	mux.HandleFunc("/api/v1/auth/step-up", s.handleAuthStepUp)
	mux.HandleFunc("/api/v1/auth/password", s.handleAuthPassword)
	mux.HandleFunc("/api/v1/auth/totp/setup", s.handleTOTPSetup)
	mux.HandleFunc("/api/v1/auth/totp/enable", s.handleTOTPEnable)
	mux.HandleFunc("/api/v1/auth/totp/disable", s.handleTOTPDisable)

	// Administración.
	mux.HandleFunc("/api/v1/admin/users", s.handleAdminUsers)
	mux.HandleFunc("/api/v1/admin/users/", s.handleAdminUser)
	mux.HandleFunc("/api/v1/admin/api-tokens", s.handleAPITokens)
	mux.HandleFunc("/api/v1/admin/api-tokens/", s.handleAPITokenAction)
	mux.HandleFunc("/api/v1/admin/enrollment-tokens", s.handleEnrollmentTokens)
	mux.HandleFunc("/api/v1/admin/enrollment-tokens/", s.handleEnrollmentTokenAction)
	mux.HandleFunc("/api/v1/admin/agents", s.handleAdminAgents)
	// Terminal remota (Remote Access).
	mux.HandleFunc("GET /api/v1/admin/agents/{id}/terminal", s.handleAgentTerminalCapabilities)
	mux.HandleFunc("POST /api/v1/admin/agents/{id}/terminal/sessions", s.handleCreateTerminalSession)
	mux.HandleFunc("GET /api/v1/admin/terminal/sessions", s.handleListTerminalSessions)
	mux.HandleFunc("GET /api/v1/admin/terminal/sessions/{id}", s.handleGetTerminalSession)
	mux.HandleFunc("POST /api/v1/admin/terminal/sessions/{id}/close", s.handleCloseTerminalSession)
	mux.HandleFunc("GET /api/v1/admin/terminal/sessions/{id}/stream", s.handleTerminalStream)
	mux.HandleFunc("GET /api/v1/agent/terminal/connect", s.handleAgentTerminalConnect)
	mux.HandleFunc("/api/v1/admin/agents/", s.handleAgentAction)
	mux.HandleFunc("/api/v1/admin/groups", s.handleGroups)
	mux.HandleFunc("/api/v1/admin/groups/", s.handleGroupAction)
	mux.HandleFunc("/api/v1/admin/backup-plans", s.handlePlans)
	mux.HandleFunc("/api/v1/admin/backup-plans/", s.handlePlanAction)
	mux.HandleFunc("/api/v1/admin/jobs", s.handleAdminJobs)
	mux.HandleFunc("/api/v1/admin/jobs/", s.handleAdminJob)
	mux.HandleFunc("/api/v1/admin/restore-points", s.handleRestorePoints)
	mux.HandleFunc("/api/v1/admin/restore-points/", s.handleRestorePointAction)
	mux.HandleFunc("/api/v1/admin/restores/files", s.handleRestoreFiles)
	mux.HandleFunc("/api/v1/admin/alerts", s.handleAlerts)
	mux.HandleFunc("/api/v1/admin/alerts/", s.handleAlertAction)
	mux.HandleFunc("/api/v1/admin/alert-rules", s.handleAlertRules)
	mux.HandleFunc("/api/v1/admin/audit", s.handleAudit)
	mux.HandleFunc("/api/v1/admin/audit/verify", s.handleAuditVerify)
	mux.HandleFunc("/api/v1/admin/audit/export", s.handleAuditExport)
	mux.HandleFunc("/api/v1/admin/settings", s.handleSettings)
	mux.HandleFunc("/api/v1/admin/settings/test-notification", s.handleTestNotification)
	mux.HandleFunc("/api/v1/admin/repositories", s.handleRepositories)
	mux.HandleFunc("/api/v1/admin/summary", s.handleSummary)
	mux.HandleFunc("/api/v1/admin/system/health", s.handleSystemHealth)
	mux.HandleFunc("/api/v1/admin/system/version", s.handleSystemVersion)
	mux.HandleFunc("/api/v1/admin/system/update", s.handleSystemUpdate)
	mux.HandleFunc("/api/v1/admin/system/update/check", s.handleSystemUpdateCheck)
	mux.HandleFunc("/api/v1/admin/system/update/install", s.handleSystemUpdateInstall)

	// Agentes.
	mux.HandleFunc("/api/v1/agent/enroll", s.handleAgentEnroll)
	mux.HandleFunc("/api/v1/agent/renew-cert", s.handleAgentRenewCert)
	mux.HandleFunc("/api/v1/agent/heartbeat", s.handleAgentHeartbeat)
	mux.HandleFunc("/api/v1/agent/inventory", s.handleAgentInventory)
	mux.HandleFunc("/api/v1/agent/me", s.handleAgentMe)
	mux.HandleFunc("/api/v1/agent/plans", s.handleAgentPlans)
	mux.HandleFunc("/api/v1/agent/jobs/lease", s.handleAgentLease)
	mux.HandleFunc("/api/v1/agent/jobs/", s.handleAgentJobAction)
	mux.HandleFunc("/api/v1/agent/repository-credentials", s.handleRepoCredentials)

	// Panel web embebido: mismo origen que la API. Los assets del export
	// estático y el fallback SPA se sirven desde el propio binario.
	mux.Handle("/", webui.Handler())

	return s.requestID(s.logRequests(s.recoverer(s.cors(mux))))
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	if !allowMethod(w, r, http.MethodGet) {
		return
	}
	writeJSON(w, http.StatusOK, contracts.Health{Status: "ok", Version: s.Version})
}

func (s *Server) handleReady(w http.ResponseWriter, r *http.Request) {
	if !allowMethod(w, r, http.MethodGet) {
		return
	}
	if err := s.Store.Ping(r.Context()); err != nil {
		s.problem(w, r, http.StatusServiceUnavailable, "E_STORE_UNAVAILABLE", "El almacenamiento no está disponible")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
}

// cors aplica CORS restringido a la API de administración. En producción el
// panel se sirve embebido (mismo origen) y no hace falta; la variable
// --cors-origins existe para el desarrollo con el panel en localhost:3000.
func (s *Server) cors(next http.Handler) http.Handler {
	if len(s.AllowedOrigins) == 0 {
		return next
	}
	allowed := map[string]bool{}
	for _, o := range s.AllowedOrigins {
		allowed[strings.ToLower(strings.TrimSpace(o))] = true
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := strings.ToLower(r.Header.Get("Origin"))
		if origin != "" && allowed[origin] {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Vary", "Origin")
			w.Header().Set("Access-Control-Allow-Credentials", "false")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, X-Request-Id, X-CSRF-Token")
			w.Header().Set("Access-Control-Max-Age", "600")
		}
		if r.Method == http.MethodOptions && r.Header.Get("Origin") != "" {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// --- Identidad de la petición ---

type actor struct {
	kind    string // admin | user | api | agent | anon
	id      string
	name    string
	role    string
	session *store.Session
	caps    []string
}

func (a *actor) can(cap string) bool {
	for _, c := range a.caps {
		if c == cap {
			return true
		}
	}
	return false
}

func (a *actor) auditName() string {
	if a.name != "" {
		return a.name
	}
	return a.id
}

// resolveActor identifica la petición: cookie de sesión, token de API,
// token de administración de arranque o API key de agente.
func (s *Server) resolveActor(r *http.Request) (*actor, *store.Agent) {
	// Agente por API key.
	if key := auth.Bearer(r); key != "" {
		var agent *store.Agent
		_ = s.Store.View(r.Context(), func(st *store.State) error {
			if a := st.AgentByAPIKeyHash(auth.Hash(key)); a != nil && a.RevokedAt == nil {
				c := *a
				agent = &c
			}
			return nil
		})
		if agent != nil {
			return &actor{kind: "agent", id: agent.ID, name: agent.Hostname}, agent
		}
	}

	// Token de administración de arranque o token de integración.
	tok := auth.Bearer(r)
	if s.AdminToken != "" && tok != "" && auth.SecureEqual(tok, s.AdminToken) {
		return &actor{kind: "admin", id: "bootstrap", name: "admin-token", role: contracts.RoleAdmin, caps: roleCaps[contracts.RoleAdmin]}, nil
	}
	if strings.HasPrefix(tok, "bdt_") {
		var apiTok *store.APIToken
		_ = s.Store.View(r.Context(), func(st *store.State) error {
			if t := st.APITokenByHash(auth.Hash(tok)); t != nil && t.RevokedAt == nil && (t.ExpiresAt == nil || t.ExpiresAt.After(s.Now())) {
				c := *t
				apiTok = &c
			}
			return nil
		})
		if apiTok != nil {
			now := s.Now().UTC()
			_ = s.Store.UpdateLazy(r.Context(), func(st *store.State) error {
				if t := st.APITokens[apiTok.ID]; t != nil {
					t.LastUsedAt = &now
				}
				return nil
			})
			return &actor{kind: "api", id: apiTok.ID, name: apiTok.Name, role: apiTok.Role, caps: roleCaps[apiTok.Role]}, nil
		}
	}

	// Sesión de panel por cookie.
	if c, err := r.Cookie(sessionCookie); err == nil && c.Value != "" {
		var sess *store.Session
		var user *store.User
		_ = s.Store.View(r.Context(), func(st *store.State) error {
			if x := st.Sessions[auth.Hash(c.Value)]; x != nil && x.ExpiresAt.After(s.Now()) {
				cc := *x
				sess = &cc
				if u := st.Users[x.UserID]; u != nil {
					uc := *u
					user = &uc
				}
			}
			return nil
		})
		if sess != nil && user != nil && !user.Disabled {
			a := &actor{kind: "user", id: user.ID, name: user.Email, role: user.Role, session: sess, caps: roleCaps[user.Role]}
			if !sess.MFAPassed && user.TOTPEnabled {
				a.caps = []string{} // sin TOTP no hay permisos
			}
			return a, nil
		}
	}
	return &actor{kind: "anon"}, nil
}

// authorize resuelve el actor y comprueba capacidad y CSRF. Devuelve la
// petición con el actor en el contexto.
func (s *Server) authorize(w http.ResponseWriter, r *http.Request, cap string) (*http.Request, *actor, bool) {
	a, agent := s.resolveActor(r)
	if agent != nil {
		// Los endpoints de agente comprueban su propia auth; aquí es un error.
		s.problem(w, r, http.StatusUnauthorized, "E_AUTH_TOKEN", "Credencial de administración inválida")
		return nil, nil, false
	}
	if !a.can(cap) {
		if a.kind == "anon" {
			s.problem(w, r, http.StatusUnauthorized, "E_AUTH_TOKEN", "Credencial de administración inválida")
		} else {
			s.problem(w, r, http.StatusForbidden, "E_FORBIDDEN", "Tu rol no permite esta acción")
		}
		return nil, nil, false
	}
	if a.kind == "user" && isMutable(r.Method) {
		if subtle.ConstantTimeCompare([]byte(r.Header.Get(csrfHeader)), []byte(a.session.CSRF)) != 1 {
			s.problem(w, r, http.StatusForbidden, "E_CSRF", "Falta la cabecera "+csrfHeader+" de la sesión")
			return nil, nil, false
		}
	}
	s.touchSession(r, a)
	r = r.WithContext(context.WithValue(r.Context(), actorKey, a))
	return r, a, true
}

func isMutable(method string) bool {
	return method == http.MethodPost || method == http.MethodPut || method == http.MethodPatch || method == http.MethodDelete
}

// requireAdmin mantiene compatibilidad con el panel 0.3 (bearer de administración).
func (s *Server) requireAdmin(w http.ResponseWriter, r *http.Request) bool {
	tok := auth.Bearer(r)
	if tok == "" || s.AdminToken == "" || !auth.SecureEqual(tok, s.AdminToken) {
		s.problem(w, r, http.StatusUnauthorized, "E_AUTH_TOKEN", "Credencial de administración inválida")
		return false
	}
	return true
}

// requireAgent autentica a un agente y devuelve su registro.
func (s *Server) requireAgent(w http.ResponseWriter, r *http.Request) (store.Agent, bool) {
	tok := auth.Bearer(r)
	if tok == "" {
		s.problem(w, r, http.StatusUnauthorized, "E_AUTH_TOKEN", "Falta la API key del agente")
		return store.Agent{}, false
	}
	var agent *store.Agent
	err := s.Store.View(r.Context(), func(st *store.State) error {
		if a := st.AgentByAPIKeyHash(auth.Hash(tok)); a != nil {
			c := *a
			agent = &c
		}
		return nil
	})
	if err != nil || agent == nil {
		s.problem(w, r, http.StatusUnauthorized, "E_AUTH_TOKEN", "API key de agente inválida")
		return store.Agent{}, false
	}
	if agent.RevokedAt != nil {
		s.problem(w, r, http.StatusForbidden, "E_AGENT_REVOKED", "El agente está revocado")
		return store.Agent{}, false
	}
	// Ligadura identidad TLS ↔ API key (docs/06 §2.2): el certificado de
	// cliente presentado debe ser el del propio agente. En HTTP claro no hay
	// par TLS (listener de control o transición con --insecure-agents).
	if r.TLS != nil && len(r.TLS.PeerCertificates) > 0 {
		if certID, _, ok := pki.AgentIdentity(r.TLS.PeerCertificates[0]); ok && certID != agent.ID {
			s.Logger.Warn("certificado de cliente no coincide con el agente de la API key",
				"agent_id", agent.ID, "cert_agent_id", certID)
			s.problem(w, r, http.StatusUnauthorized, "E_AUTH_CERT", "El certificado no corresponde a este agente")
			return store.Agent{}, false
		}
	}
	return *agent, true
}

// VerifyAgentHandshake aplica la política de certificados de agente en el
// handshake TLS: el certificado debe ser el vigente de un agente no revocado.
// El listener la invoca en cada handshake nuevo (sin reutilización de
// sesión), de modo que revocar o renovar surte efecto de inmediato, sin
// reiniciar (puerta del roadmap: revocación efectiva < 5 min).
func (s *Server) VerifyAgentHandshake(agentID, serial string) error {
	var reason string
	err := s.Store.View(context.Background(), func(st *store.State) error {
		a := st.Agents[agentID]
		switch {
		case a == nil:
			reason = "agente desconocido"
		case a.RevokedAt != nil:
			reason = "agente revocado"
		case a.CertSerial == "" || a.CertSerial != serial:
			reason = "certificado no vigente"
		default:
			return nil
		}
		return errAgentCertRejected
	})
	if err != nil {
		s.Logger.Warn("handshake de agente rechazado", "agent_id", agentID, "motivo", reason)
		return fmt.Errorf("certificado de agente rechazado: %s", reason)
	}
	return nil
}

// errAgentCertRejected marca internamente el rechazo del handshake.
var errAgentCertRejected = errors.New("certificado rechazado")

// auditSession registra la actividad de la sesión (best effort).
func (s *Server) touchSession(r *http.Request, a *actor) {
	if a == nil || a.session == nil {
		return
	}
	now := s.Now().UTC()
	_ = s.Store.UpdateLazy(r.Context(), func(st *store.State) error {
		if x := st.Sessions[a.session.IDHash]; x != nil {
			x.LastSeenAt = now
		}
		return nil
	})
}

// audit registra un evento de auditoría.
func (s *Server) audit(r *http.Request, a *actor, action, targetType, targetID, result string, details map[string]string) {
	ev := contracts.AuditEvent{
		At:         s.Now().UTC(),
		Action:     action,
		TargetType: targetType,
		TargetID:   targetID,
		Result:     result,
		IP:         clientIP(r),
		RequestID:  requestIDFrom(r.Context()),
		Details:    details,
	}
	if a != nil {
		ev.ActorType, ev.ActorID, ev.ActorName = a.kind, a.id, a.auditName()
	} else {
		ev.ActorType = "system"
	}
	if _, err := s.Store.Audit().Append(ev); err != nil {
		s.Logger.Error("no se pudo registrar en la auditoría", "error", err.Error())
	}
}

func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		if i := strings.IndexByte(xff, ','); i > 0 {
			return strings.TrimSpace(xff[:i])
		}
		return strings.TrimSpace(xff)
	}
	host := r.RemoteAddr
	if i := strings.LastIndexByte(host, ':'); i > 0 && !strings.Contains(host, "]") {
		host = host[:i]
	}
	return strings.Trim(host, "[]")
}

func actorFrom(r *http.Request) *actor {
	if v, ok := r.Context().Value(actorKey).(*actor); ok {
		return v
	}
	return &actor{kind: "anon"}
}

// --- Idempotencia ---

// withIdempotency aplica Idempotency-Key a un POST que crea recursos.
// handler devuelve (status, cuerpo); devolver (0, nil) significa "ya escrito";
// devolver errInternal lo convierte en 500.
func (s *Server) withIdempotency(w http.ResponseWriter, r *http.Request, a *actor, handler func() (int, any)) bool {
	key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	body, _ := io.ReadAll(io.LimitReader(r.Body, maxBodyAdmin))
	r.Body = io.NopCloser(strings.NewReader(string(body)))
	emit := func(status int, out any) {
		switch e := out.(type) {
		case errInternal:
			s.internalError(w, r, e.err)
			return
		case errProblem:
			// Validación fallida: problema RFC 7807 con su estado HTTP
			// (aunque el handler devuelva status 0).
			s.problem(w, r, e.Status, e.Code, e.Detail)
			return
		}
		if status == 0 {
			return
		}
		writeJSON(w, status, out)
	}
	if key == "" {
		status, out := handler()
		emit(status, out)
		return true
	}
	if len(key) > 128 {
		s.problem(w, r, http.StatusBadRequest, "E_VALIDATION", "Idempotency-Key demasiado larga")
		return false
	}
	recKey := a.id + "|" + r.URL.Path + "|" + key
	bodyHash := auth.Hash(string(body))
	now := s.Now().UTC()

	var replay *store.IdempotencyRecord
	_ = s.Store.View(r.Context(), func(st *store.State) error {
		if rec := st.Idempotency[recKey]; rec != nil && now.Sub(rec.CreatedAt) < idempotencyTTL {
			c := *rec
			replay = &c
		}
		return nil
	})
	if replay != nil {
		if replay.BodyHash != bodyHash {
			s.problem(w, r, http.StatusConflict, "E_IDEMPOTENCY_CONFLICT", "La clave se usó con otro cuerpo")
			return false
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(replay.Status)
		_, _ = w.Write(replay.Response)
		return true
	}
	status, out := handler()
	switch e := out.(type) {
	case errInternal:
		s.internalError(w, r, e.err)
		return true
	case errProblem:
		s.problem(w, r, e.Status, e.Code, e.Detail)
		return true
	}
	if status == 0 {
		return true
	}
	resp, _ := json.Marshal(out)
	rec := store.IdempotencyRecord{Key: recKey, BodyHash: bodyHash, Status: status, Response: resp, CreatedAt: now}
	if err := s.Store.Update(r.Context(), func(st *store.State) error {
		st.Idempotency[recKey] = &rec
		for k, v := range st.Idempotency {
			if now.Sub(v.CreatedAt) >= idempotencyTTL {
				delete(st.Idempotency, k)
			}
		}
		return nil
	}); err != nil {
		s.Logger.Warn("no se pudo guardar la idempotencia", "error", err.Error())
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(resp)
	return true
}

type errInternal struct{ err error }

// --- Respuestas y validación ---

func (s *Server) problem(w http.ResponseWriter, r *http.Request, status int, code, detail string) {
	p := contracts.Problem{
		Type:      "about:blank",
		Title:     http.StatusText(status),
		Status:    status,
		Detail:    detail,
		Code:      code,
		RequestID: requestIDFrom(r.Context()),
	}
	w.Header().Set("Content-Type", problemMediaType)
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(p)
}

func (s *Server) internalError(w http.ResponseWriter, r *http.Request, err error) {
	s.Logger.Error("internal error", "error", err.Error(), "request_id", requestIDFrom(r.Context()))
	s.problem(w, r, http.StatusInternalServerError, "E_INTERNAL", "Error interno")
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func decodeJSON(w http.ResponseWriter, r *http.Request, dst any, limit int64) bool {
	r.Body = http.MaxBytesReader(w, r.Body, limit)
	dec := json.NewDecoder(r.Body)
	if err := dec.Decode(dst); err != nil {
		msg := "El cuerpo no es JSON válido"
		if errors.Is(err, io.EOF) {
			msg = "El cuerpo está vacío"
		}
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			msg = "El cuerpo supera el tamaño permitido"
		}
		p := contracts.Problem{
			Type: "about:blank", Title: "Bad Request", Status: http.StatusBadRequest,
			Detail: msg, Code: "E_VALIDATION", RequestID: requestIDFrom(r.Context()),
		}
		w.Header().Set("Content-Type", problemMediaType)
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(p)
		return false
	}
	return true
}

func allowMethod(w http.ResponseWriter, r *http.Request, methods ...string) bool {
	for _, m := range methods {
		if r.Method == m {
			return true
		}
	}
	methodNotAllowed(w, methods...)
	return false
}

func methodNotAllowed(w http.ResponseWriter, methods ...string) {
	w.Header().Set("Allow", strings.Join(methods, ", "))
	w.Header().Set("Content-Type", problemMediaType)
	w.WriteHeader(http.StatusMethodNotAllowed)
	_ = json.NewEncoder(w).Encode(contracts.Problem{
		Type: "about:blank", Title: "Method Not Allowed", Status: http.StatusMethodNotAllowed,
		Detail: "Método no permitido", Code: "E_METHOD",
	})
}

func requestIDFrom(ctx context.Context) string {
	if v, ok := ctx.Value(requestIDKey).(string); ok {
		return v
	}
	return ""
}

func pathID(prefix, path string) string {
	return strings.Trim(strings.TrimPrefix(path, prefix), "/")
}

func newID() (string, error) { return auth.NewID() }

func fmtErr(err error) string { return fmt.Sprint(err) }

// --- Middlewares ---

type statusRecorder struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

func (r *statusRecorder) Write(b []byte) (int, error) {
	if r.status == 0 {
		r.status = http.StatusOK
	}
	n, err := r.ResponseWriter.Write(b)
	r.bytes += n
	return n, err
}

// Hijack delega el upgrade WebSocket: coder/websocket exige http.Hijacker en
// el handshake y, sin esta delegación, todo endpoint WS respondía 501
// (Not Implemented) aunque la ruta y las credenciales fueran correctas.
func (r *statusRecorder) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	hj, ok := r.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, fmt.Errorf("http.ResponseWriter no implementa http.Hijacker")
	}
	return hj.Hijack()
}

// Flush delega http.Flusher (streaming y long-poll).
func (r *statusRecorder) Flush() {
	if f, ok := r.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (s *Server) requestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := strings.TrimSpace(r.Header.Get(requestIDHeader))
		if id == "" || len(id) > 128 {
			generated, err := auth.NewID()
			if err != nil {
				generated = fmt.Sprintf("%d", time.Now().UnixNano())
			}
			id = generated
		}
		w.Header().Set(requestIDHeader, id)
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), requestIDKey, id)))
	})
}

func (s *Server) logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w}
		next.ServeHTTP(rec, r)
		if rec.status == 0 {
			rec.status = http.StatusOK
		}
		s.Logger.Info("http request",
			"method", r.Method,
			// El instalador lleva el token de enrolamiento en la URL:
			// se redacta para que nunca aparezca en los logs.
			"path", redactPath(r.URL.Path),
			"status", rec.status,
			"bytes", rec.bytes,
			"duration_ms", time.Since(start).Milliseconds(),
			"request_id", requestIDFrom(r.Context()),
		)
	})
}

// redactPath oculta el último segmento de las descargas de instaladores
// (el token en la URL es el secreto).
func redactPath(p string) string {
	const prefix = "/api/v1/download/installer/"
	if strings.HasPrefix(p, prefix) {
		rest := p[len(prefix):]
		if i := strings.IndexByte(rest, '/'); i >= 0 {
			return prefix + rest[:i+1] + "REDACTED"
		}
	}
	return p
}

func (s *Server) recoverer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				s.Logger.Error("panic", "panic", fmt.Sprint(rec), "request_id", requestIDFrom(r.Context()))
				s.problem(w, r, http.StatusInternalServerError, "E_INTERNAL", "Error interno")
			}
		}()
		next.ServeHTTP(w, r)
	})
}

func orDefaultPtr(v *string, def string) string {
	if v == nil || strings.TrimSpace(*v) == "" {
		return def
	}
	return *v
}

// contextWithTimeout crea un contexto con timeout.
func contextWithTimeout(d time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), d)
}

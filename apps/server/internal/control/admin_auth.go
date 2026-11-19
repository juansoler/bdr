package control

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/juansoler/bdr/apps/server/internal/auth"
	"github.com/juansoler/bdr/apps/server/internal/store"
	"github.com/juansoler/bdr/packages/contracts"
)

const maxLoginAttempts = 10
const loginWindow = time.Minute
const maxMFAFailures = 5

// --- Bootstrap y login ---

// handleAuthBootstrap crea el primer usuario administrador. Solo funciona
// mientras no exista ningún usuario.
func (s *Server) handleAuthBootstrap(w http.ResponseWriter, r *http.Request) {
	if !allowMethod(w, r, http.MethodPost) {
		return
	}
	var req struct {
		Email    string `json:"email"`
		Name     string `json:"name"`
		Password string `json:"password"`
	}
	if !decodeJSON(w, r, &req, maxBodyAdmin) {
		return
	}
	req.Email = strings.ToLower(strings.TrimSpace(req.Email))
	if req.Email == "" || !strings.Contains(req.Email, "@") {
		s.problem(w, r, http.StatusBadRequest, "E_VALIDATION", "email no válido")
		return
	}
	if err := auth.ValidatePassword(req.Password); err != nil {
		s.problem(w, r, http.StatusBadRequest, "E_VALIDATION", err.Error())
		return
	}
	hash, err := auth.HashPassword(req.Password)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	id, err := newID()
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	now := s.Now().UTC()
	user := store.User{
		ID: id, Email: req.Email, Name: orDefault(req.Name, req.Email),
		Role: contracts.RoleAdmin, PasswordHash: hash,
		CreatedAt: now, UpdatedAt: now, PasswordChangedAt: now,
	}
	err = s.Store.Update(r.Context(), func(st *store.State) error {
		if len(st.Users) > 0 {
			return errBootstrapDone
		}
		st.Users[user.ID] = &user
		return nil
	})
	if err != nil {
		if err == errBootstrapDone {
			s.problem(w, r, http.StatusConflict, "E_BOOTSTRAP_DONE", "Ya existen usuarios; entra con tu cuenta")
			return
		}
		s.internalError(w, r, err)
		return
	}
	s.audit(r, &actor{kind: "anon"}, "auth.bootstrap", "user", user.ID, "ok", nil)
	s.Logger.Info("usuario administrador inicial creado", "email", req.Email)
	me := s.meResponse(r, &user, nil, false)
	writeJSON(w, http.StatusCreated, me)
}

var errBootstrapDone = &codedError{"bootstrap done"}

type codedError struct{ msg string }

func (e *codedError) Error() string { return e.msg }

func (s *Server) rateLimited(ip string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.Now()
	recent := s.loginAttempts[ip][:0]
	for _, t := range s.loginAttempts[ip] {
		if now.Sub(t) < loginWindow {
			recent = append(recent, t)
		}
	}
	if len(recent) >= maxLoginAttempts {
		s.loginAttempts[ip] = recent
		return true
	}
	s.loginAttempts[ip] = append(recent, now)
	return false
}

func (s *Server) handleAuthLogin(w http.ResponseWriter, r *http.Request) {
	if !allowMethod(w, r, http.MethodPost) {
		return
	}
	if s.rateLimited(clientIP(r)) {
		w.Header().Set("Retry-After", "60")
		s.problem(w, r, http.StatusTooManyRequests, "E_RATE_LIMIT", "Demasiados intentos; vuelve a intentarlo en un minuto")
		return
	}
	var req contracts.LoginRequest
	if !decodeJSON(w, r, &req, maxBodyAdmin) {
		return
	}
	req.Email = strings.ToLower(strings.TrimSpace(req.Email))
	var user *store.User
	_ = s.Store.View(r.Context(), func(st *store.State) error {
		if u := st.UserByEmail(req.Email); u != nil && !u.Disabled {
			c := *u
			user = &c
		}
		return nil
	})
	if user == nil {
		auth.DummyPasswordCheck(req.Password)
		s.audit(r, &actor{kind: "anon"}, "auth.login", "user", req.Email, "denied", nil)
		s.problem(w, r, http.StatusUnauthorized, "E_AUTH_TOKEN", "Credenciales incorrectas")
		return
	}
	if !auth.CheckPassword(user.PasswordHash, req.Password) {
		s.audit(r, &actor{kind: "anon"}, "auth.login", "user", user.ID, "denied", nil)
		s.problem(w, r, http.StatusUnauthorized, "E_AUTH_TOKEN", "Credenciales incorrectas")
		return
	}
	sess, cookieValue, err := s.createSession(r, user.ID)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	s.setSessionCookie(w, r, sess, cookieValue)
	now := s.Now().UTC()
	_ = s.Store.Update(r.Context(), func(st *store.State) error {
		if u := st.Users[user.ID]; u != nil {
			u.LastLoginAt = &now
		}
		return nil
	})
	s.audit(r, &actor{kind: "anon"}, "auth.login", "user", user.ID, "ok", nil)
	writeJSON(w, http.StatusOK, s.meResponse(r, user, &sess, false))
}

// handleAuthMFA completa el login con TOTP o inicia la recuperación.
func (s *Server) handleAuthMFA(w http.ResponseWriter, r *http.Request) {
	if !allowMethod(w, r, http.MethodPost) {
		return
	}
	a, _ := s.resolveActor(r)
	if a.kind != "user" || a.session == nil {
		s.problem(w, r, http.StatusUnauthorized, "E_AUTH_TOKEN", "Inicia sesión primero")
		return
	}
	var req contracts.MFARequest
	if !decodeJSON(w, r, &req, maxBodyAdmin) {
		return
	}
	var user *store.User
	err := s.Store.Update(r.Context(), func(st *store.State) error {
		sess := st.Sessions[a.session.IDHash]
		u := st.Users[a.id]
		if sess == nil || u == nil {
			return errBootstrapDone
		}
		if u.TOTPEnabled {
			if rc := strings.TrimSpace(req.RecoveryCode); rc != "" {
				h := auth.Hash(auth.NormalizeRecoveryCode(rc))
				found := false
				rest := make([]string, 0, len(u.RecoveryHashes))
				for _, x := range u.RecoveryHashes {
					if x == h {
						found = true
						continue
					}
					rest = append(rest, x)
				}
				if !found {
					return errMFAFailed
				}
				u.RecoveryHashes = rest
				sess.MFAPassed = true
				sess.MFAFailures = 0
			} else {
				code := strings.TrimSpace(req.Code)
				step, ok := auth.VerifyTOTP(u.TOTPSecret, code, s.Now(), u.TOTPLastStep)
				if !ok {
					sess.MFAFailures++
					if sess.MFAFailures >= maxMFAFailures {
						delete(st.Sessions, sess.IDHash)
					}
					return errMFAFailed
				}
				u.TOTPLastStep = step
				sess.MFAPassed = true
				sess.MFAFailures = 0
			}
		} else {
			sess.MFAPassed = true
		}
		cu := *u
		user = &cu
		return nil
	})
	if err != nil {
		if err == errMFAFailed {
			s.audit(r, a, "auth.mfa", "user", a.id, "denied", nil)
			s.problem(w, r, http.StatusUnauthorized, "E_MFA", "Código TOTP incorrecto")
			return
		}
		s.problem(w, r, http.StatusUnauthorized, "E_AUTH_TOKEN", "Sesión no válida")
		return
	}
	s.audit(r, a, "auth.mfa", "user", user.ID, "ok", nil)
	writeJSON(w, http.StatusOK, s.meResponse(r, user, a.session, true))
}

var errMFAFailed = &codedError{"mfa failed"}

func (s *Server) handleAuthLogout(w http.ResponseWriter, r *http.Request) {
	if !allowMethod(w, r, http.MethodPost) {
		return
	}
	if c, err := r.Cookie(sessionCookie); err == nil && c.Value != "" {
		h := auth.Hash(c.Value)
		_ = s.Store.Update(r.Context(), func(st *store.State) error {
			delete(st.Sessions, h)
			return nil
		})
		s.audit(r, actorFrom(r), "auth.logout", "user", actorFrom(r).id, "ok", nil)
	}
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: "", Path: "/", MaxAge: -1, HttpOnly: true, SameSite: http.SameSiteLaxMode})
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleAuthMe(w http.ResponseWriter, r *http.Request) {
	if !allowMethod(w, r, http.MethodGet) {
		return
	}
	a, _ := s.resolveActor(r)
	switch a.kind {
	case "user":
		var user *store.User
		_ = s.Store.View(r.Context(), func(st *store.State) error {
			if u := st.Users[a.id]; u != nil {
				c := *u
				user = &c
			}
			return nil
		})
		if user == nil {
			s.problem(w, r, http.StatusUnauthorized, "E_AUTH_TOKEN", "Sesión no válida")
			return
		}
		writeJSON(w, http.StatusOK, s.meResponse(r, user, a.session, a.session.MFAPassed))
	case "api":
		writeJSON(w, http.StatusOK, contracts.MeResponse{
			User:        contracts.UserView{ID: a.id, Email: a.name, Role: a.role},
			Session:     contracts.SessionInfo{AuthMethod: "api_token", MFAPassed: true},
			Permissions: a.caps,
		})
	case "admin":
		writeJSON(w, http.StatusOK, contracts.MeResponse{
			User:        contracts.UserView{ID: "bootstrap", Email: "admin-token", Role: contracts.RoleAdmin},
			Session:     contracts.SessionInfo{AuthMethod: "api_token", MFAPassed: true},
			Permissions: a.caps,
		})
	default:
		s.problem(w, r, http.StatusUnauthorized, "E_AUTH_TOKEN", "Credencial de administración inválida")
	}
}

// --- Step-up y contraseña ---

func (s *Server) handleAuthStepUp(w http.ResponseWriter, r *http.Request) {
	if !allowMethod(w, r, http.MethodPost) {
		return
	}
	r, a, ok := s.authorize(w, r, "read")
	if !ok {
		return
	}
	if a.kind != "user" {
		s.problem(w, r, http.StatusBadRequest, "E_VALIDATION", "El step-up solo aplica a sesiones de panel")
		return
	}
	var req contracts.MFARequest
	if !decodeJSON(w, r, &req, maxBodyAdmin) {
		return
	}
	var user *store.User
	_ = s.Store.View(r.Context(), func(st *store.State) error {
		if u := st.Users[a.id]; u != nil {
			c := *u
			user = &c
		}
		return nil
	})
	if user == nil || !user.TOTPEnabled {
		s.problem(w, r, http.StatusBadRequest, "E_VALIDATION", "No hay un segundo factor configurado")
		return
	}
	step, ok := auth.VerifyTOTP(user.TOTPSecret, strings.TrimSpace(req.Code), s.Now(), user.TOTPLastStep)
	if !ok {
		s.audit(r, a, "auth.step_up", "user", user.ID, "denied", nil)
		s.problem(w, r, http.StatusUnauthorized, "E_MFA", "Código TOTP incorrecto")
		return
	}
	until := s.Now().UTC().Add(5 * time.Minute)
	_ = s.Store.Update(r.Context(), func(st *store.State) error {
		if u := st.Users[user.ID]; u != nil {
			u.TOTPLastStep = step
		}
		if sess := st.Sessions[a.session.IDHash]; sess != nil {
			sess.StepUpUntil = &until
		}
		return nil
	})
	s.audit(r, a, "auth.step_up", "user", user.ID, "ok", nil)
	writeJSON(w, http.StatusOK, map[string]any{"step_up_until": until})
}

func (s *Server) handleAuthPassword(w http.ResponseWriter, r *http.Request) {
	if !allowMethod(w, r, http.MethodPost) {
		return
	}
	r, a, ok := s.authorize(w, r, "read")
	if !ok || a.kind != "user" {
		if ok {
			s.problem(w, r, http.StatusBadRequest, "E_VALIDATION", "Solo para sesiones de panel")
		}
		return
	}
	var req contracts.PasswordChangeRequest
	if !decodeJSON(w, r, &req, maxBodyAdmin) {
		return
	}
	if err := auth.ValidatePassword(req.NewPassword); err != nil {
		s.problem(w, r, http.StatusBadRequest, "E_VALIDATION", err.Error())
		return
	}
	var user *store.User
	_ = s.Store.View(r.Context(), func(st *store.State) error {
		if u := st.Users[a.id]; u != nil {
			c := *u
			user = &c
		}
		return nil
	})
	if user == nil || !auth.CheckPassword(user.PasswordHash, req.CurrentPassword) {
		s.problem(w, r, http.StatusUnauthorized, "E_AUTH_TOKEN", "Contraseña actual incorrecta")
		return
	}
	hash, err := auth.HashPassword(req.NewPassword)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	now := s.Now().UTC()
	err = s.Store.Update(r.Context(), func(st *store.State) error {
		u := st.Users[user.ID]
		if u == nil {
			return store.ErrNotFound
		}
		u.PasswordHash = hash
		u.PasswordChangedAt = now
		u.UpdatedAt = now
		// Revoca todas las sesiones menos la actual.
		for h, sess := range st.Sessions {
			if sess.UserID == u.ID && h != a.session.IDHash {
				delete(st.Sessions, h)
			}
		}
		return nil
	})
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	s.audit(r, a, "auth.password_changed", "user", user.ID, "ok", nil)
	writeJSON(w, http.StatusOK, map[string]string{"status": "changed"})
}

// --- TOTP ---

func (s *Server) handleTOTPSetup(w http.ResponseWriter, r *http.Request) {
	if !allowMethod(w, r, http.MethodPost) {
		return
	}
	r, a, ok := s.authorize(w, r, "read")
	if !ok || a.kind != "user" {
		if ok {
			s.problem(w, r, http.StatusBadRequest, "E_VALIDATION", "Solo para sesiones de panel")
		}
		return
	}
	secret, err := auth.NewTOTPSecret()
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	enc := s.Box.MustSeal(secret)
	if err := s.Store.Update(r.Context(), func(st *store.State) error {
		if u := st.Users[a.id]; u != nil {
			u.TOTPPending = enc
		}
		return nil
	}); err != nil {
		s.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, contracts.TOTPSetupResponse{
		Secret: secret, OTPAuthURL: auth.TOTPURL("BDR", a.name, secret), Issuer: "BDR", Account: a.name,
	})
}

func (s *Server) handleTOTPEnable(w http.ResponseWriter, r *http.Request) {
	if !allowMethod(w, r, http.MethodPost) {
		return
	}
	r, a, ok := s.authorize(w, r, "read")
	if !ok || a.kind != "user" {
		if ok {
			s.problem(w, r, http.StatusBadRequest, "E_VALIDATION", "Solo para sesiones de panel")
		}
		return
	}
	var req contracts.MFARequest
	if !decodeJSON(w, r, &req, maxBodyAdmin) {
		return
	}
	var codes []string
	enc := s.Box.MustSeal("")
	var user *store.User
	err := s.Store.View(r.Context(), func(st *store.State) error {
		u := st.Users[a.id]
		if u == nil {
			return store.ErrNotFound
		}
		c := *u
		user = &c
		enc = u.TOTPPending
		return nil
	})
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	secret, err := s.Box.Open(enc)
	if err != nil || secret == "" {
		s.problem(w, r, http.StatusBadRequest, "E_VALIDATION", "Ejecuta primero POST /auth/totp/setup")
		return
	}
	step, ok := auth.VerifyTOTP(secret, strings.TrimSpace(req.Code), s.Now(), user.TOTPLastStep)
	if !ok {
		s.problem(w, r, http.StatusUnauthorized, "E_MFA", "Código incorrecto; comprueba la hora del dispositivo")
		return
	}
	codes, err = auth.NewRecoveryCodes(10)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	hashes := make([]string, 0, len(codes))
	for _, c := range codes {
		hashes = append(hashes, auth.Hash(auth.NormalizeRecoveryCode(c)))
	}
	if err := s.Store.Update(r.Context(), func(st *store.State) error {
		u := st.Users[a.id]
		if u == nil {
			return store.ErrNotFound
		}
		u.TOTPSecret = enc
		u.TOTPPending = ""
		u.TOTPEnabled = true
		u.TOTPLastStep = step
		u.RecoveryHashes = hashes
		u.UpdatedAt = s.Now().UTC()
		if sess := st.Sessions[a.session.IDHash]; sess != nil {
			sess.MFAPassed = true
		}
		return nil
	}); err != nil {
		s.internalError(w, r, err)
		return
	}
	s.audit(r, a, "auth.totp_enabled", "user", user.ID, "ok", nil)
	writeJSON(w, http.StatusOK, contracts.TOTPEnableResponse{RecoveryCodes: codes})
}

func (s *Server) handleTOTPDisable(w http.ResponseWriter, r *http.Request) {
	if !allowMethod(w, r, http.MethodPost) {
		return
	}
	r, a, ok := s.authorize(w, r, "read")
	if !ok || a.kind != "user" {
		if ok {
			s.problem(w, r, http.StatusBadRequest, "E_VALIDATION", "Solo para sesiones de panel")
		}
		return
	}
	if err := s.Store.Update(r.Context(), func(st *store.State) error {
		u := st.Users[a.id]
		if u == nil {
			return store.ErrNotFound
		}
		u.TOTPSecret, u.TOTPPending, u.TOTPEnabled, u.RecoveryHashes = "", "", false, nil
		u.UpdatedAt = s.Now().UTC()
		return nil
	}); err != nil {
		s.internalError(w, r, err)
		return
	}
	s.audit(r, a, "auth.totp_disabled", "user", a.id, "ok", nil)
	w.WriteHeader(http.StatusNoContent)
}

// --- Usuarios ---

func (s *Server) handleAdminUsers(w http.ResponseWriter, r *http.Request) {
	if !allowMethod(w, r, http.MethodGet, http.MethodPost) {
		return
	}
	r, a, ok := s.authorize(w, r, "admin")
	if !ok {
		return
	}
	switch r.Method {
	case http.MethodGet:
		var users []store.User
		_ = s.Store.View(r.Context(), func(st *store.State) error {
			for _, u := range st.Users {
				users = append(users, *u)
			}
			return nil
		})
		items := make([]contracts.UserView, 0, len(users))
		for _, u := range users {
			items = append(items, s.userView(u))
		}
		writeJSON(w, http.StatusOK, contracts.UserList{Items: items})
	case http.MethodPost:
		var req contracts.UserRequest
		if !decodeJSON(w, r, &req, maxBodyAdmin) {
			return
		}
		email := strings.ToLower(strings.TrimSpace(req.Email))
		if email == "" || !strings.Contains(email, "@") {
			s.problem(w, r, http.StatusBadRequest, "E_VALIDATION", "email no válido")
			return
		}
		if err := auth.ValidatePassword(req.Password); err != nil {
			s.problem(w, r, http.StatusBadRequest, "E_VALIDATION", err.Error())
			return
		}
		role := req.Role
		if role == "" {
			role = contracts.RoleViewer
		}
		if _, ok := roleCaps[role]; !ok {
			s.problem(w, r, http.StatusBadRequest, "E_VALIDATION", "role debe ser admin, operator o viewer")
			return
		}
		hash, err := auth.HashPassword(req.Password)
		if err != nil {
			s.internalError(w, r, err)
			return
		}
		id, err := newID()
		if err != nil {
			s.internalError(w, r, err)
			return
		}
		now := s.Now().UTC()
		u := store.User{ID: id, Email: email, Name: orDefaultPtr(req.Name, email), Role: role,
			PasswordHash: hash, CreatedAt: now, UpdatedAt: now, PasswordChangedAt: now,
			Disabled: req.Disabled != nil && *req.Disabled}
		if err := s.Store.Update(r.Context(), func(st *store.State) error {
			if st.UserByEmail(email) != nil {
				return store.ErrConflict
			}
			st.Users[u.ID] = &u
			return nil
		}); err != nil {
			if err == store.ErrConflict {
				s.problem(w, r, http.StatusConflict, "E_CONFLICT", "Ya existe un usuario con ese email")
				return
			}
			s.internalError(w, r, err)
			return
		}
		s.audit(r, a, "user.created", "user", u.ID, "ok", map[string]string{"email": email, "role": role})
		writeJSON(w, http.StatusCreated, s.userView(u))
	}
	_ = r
	_ = a
}

func (s *Server) handleAdminUser(w http.ResponseWriter, r *http.Request) {
	if !allowMethod(w, r, http.MethodPatch, http.MethodDelete) {
		return
	}
	r, a, ok := s.authorize(w, r, "admin")
	if !ok {
		return
	}
	id := pathID("/api/v1/admin/users/", r.URL.Path)
	if id == "" {
		s.problem(w, r, http.StatusNotFound, "E_NOT_FOUND", "Usuario no encontrado")
		return
	}
	switch r.Method {
	case http.MethodPatch:
		var req contracts.UserRequest
		if !decodeJSON(w, r, &req, maxBodyAdmin) {
			return
		}
		if req.Role != "" {
			if _, ok := roleCaps[req.Role]; !ok {
				s.problem(w, r, http.StatusBadRequest, "E_VALIDATION", "role inválido")
				return
			}
		}
		now := s.Now().UTC()
		err := s.Store.Update(r.Context(), func(st *store.State) error {
			u := st.Users[id]
			if u == nil {
				return store.ErrNotFound
			}
			if req.Name != nil {
				u.Name = *req.Name
			}
			if req.Role != "" {
				u.Role = req.Role
			}
			if req.Disabled != nil {
				u.Disabled = *req.Disabled
			}
			u.UpdatedAt = now
			return nil
		})
		if err != nil {
			s.userActionError(w, r, a, err, id)
			return
		}
		s.audit(r, a, "user.updated", "user", id, "ok", nil)
		var u *store.User
		_ = s.Store.View(r.Context(), func(st *store.State) error {
			if x := st.Users[id]; x != nil {
				c := *x
				u = &c
			}
			return nil
		})
		writeJSON(w, http.StatusOK, s.userView(*u))
	case http.MethodDelete:
		if id == a.id {
			s.problem(w, r, http.StatusBadRequest, "E_VALIDATION", "No puedes eliminar tu propia cuenta")
			return
		}
		err := s.Store.Update(r.Context(), func(st *store.State) error {
			u := st.Users[id]
			if u == nil {
				return store.ErrNotFound
			}
			delete(st.Users, id)
			for h, sess := range st.Sessions {
				if sess.UserID == id {
					delete(st.Sessions, h)
				}
			}
			return nil
		})
		if err != nil {
			s.userActionError(w, r, a, err, id)
			return
		}
		s.audit(r, a, "user.deleted", "user", id, "ok", nil)
		w.WriteHeader(http.StatusNoContent)
	}
}

func (s *Server) userActionError(w http.ResponseWriter, r *http.Request, a *actor, err error, id string) {
	if err == store.ErrNotFound {
		s.problem(w, r, http.StatusNotFound, "E_NOT_FOUND", "Usuario no encontrado")
		return
	}
	s.audit(r, a, "user.updated", "user", id, "error", map[string]string{"error": err.Error()})
	s.internalError(w, r, err)
}

func (s *Server) userView(u store.User) contracts.UserView {
	return contracts.UserView{
		ID: u.ID, Email: u.Email, Name: u.Name, Role: u.Role,
		TOTPEnabled: u.TOTPEnabled, Disabled: u.Disabled,
		CreatedAt: u.CreatedAt, LastLoginAt: u.LastLoginAt,
	}
}

// --- Tokens de API ---

func (s *Server) handleAPITokens(w http.ResponseWriter, r *http.Request) {
	if !allowMethod(w, r, http.MethodGet, http.MethodPost) {
		return
	}
	r, a, ok := s.authorize(w, r, "admin")
	if !ok {
		return
	}
	switch r.Method {
	case http.MethodGet:
		var toks []store.APIToken
		_ = s.Store.View(r.Context(), func(st *store.State) error {
			for _, t := range st.APITokens {
				toks = append(toks, *t)
			}
			return nil
		})
		items := make([]contracts.APITokenView, 0, len(toks))
		for _, t := range toks {
			items = append(items, apiTokenView(t))
		}
		writeJSON(w, http.StatusOK, contracts.APITokenList{Items: items})
	case http.MethodPost:
		var req contracts.APITokenRequest
		if !decodeJSON(w, r, &req, maxBodyAdmin) {
			return
		}
		req.Name = strings.TrimSpace(req.Name)
		if req.Name == "" {
			s.problem(w, r, http.StatusBadRequest, "E_VALIDATION", "name es obligatorio")
			return
		}
		role := req.Role
		if role == "" {
			role = contracts.RoleViewer
		}
		if _, ok := roleCaps[role]; !ok {
			s.problem(w, r, http.StatusBadRequest, "E_VALIDATION", "role inválido")
			return
		}
		ttl := time.Duration(req.ExpiresInSeconds) * time.Second
		if ttl > 10*365*24*time.Hour {
			ttl = 10 * 365 * 24 * time.Hour
		}
		secret, err := auth.NewSecret(32)
		if err != nil {
			s.internalError(w, r, err)
			return
		}
		token := "bdt_" + secret
		id, err := newID()
		if err != nil {
			s.internalError(w, r, err)
			return
		}
		now := s.Now().UTC()
		rec := store.APIToken{ID: id, Name: req.Name, Role: role, Prefix: token[:10],
			TokenHash: auth.Hash(token), CreatedAt: now, CreatedBy: a.auditName()}
		if ttl > 0 {
			exp := now.Add(ttl)
			rec.ExpiresAt = &exp
		}
		if err := s.Store.Update(r.Context(), func(st *store.State) error {
			st.APITokens[id] = &rec
			return nil
		}); err != nil {
			s.internalError(w, r, err)
			return
		}
		s.audit(r, a, "api_token.created", "api_token", id, "ok", map[string]string{"name": req.Name, "role": role})
		out := contracts.APITokenCreated{APITokenView: apiTokenView(rec), Token: token}
		writeJSON(w, http.StatusCreated, out)
	}
}

func (s *Server) handleAPITokenAction(w http.ResponseWriter, r *http.Request) {
	if !allowMethod(w, r, http.MethodDelete) {
		return
	}
	r, a, ok := s.authorize(w, r, "admin")
	if !ok {
		return
	}
	id := pathID("/api/v1/admin/api-tokens/", r.URL.Path)
	now := s.Now().UTC()
	err := s.Store.Update(r.Context(), func(st *store.State) error {
		t := st.APITokens[id]
		if t == nil {
			return store.ErrNotFound
		}
		t.RevokedAt = &now
		return nil
	})
	if err != nil {
		if err == store.ErrNotFound {
			s.problem(w, r, http.StatusNotFound, "E_NOT_FOUND", "Token no encontrado")
			return
		}
		s.internalError(w, r, err)
		return
	}
	s.audit(r, a, "api_token.revoked", "api_token", id, "ok", nil)
	w.WriteHeader(http.StatusNoContent)
}

func apiTokenView(t store.APIToken) contracts.APITokenView {
	return contracts.APITokenView{
		ID: t.ID, Name: t.Name, Role: t.Role, Prefix: t.Prefix,
		CreatedAt: t.CreatedAt, CreatedBy: t.CreatedBy,
		ExpiresAt: t.ExpiresAt, LastUsedAt: t.LastUsedAt, RevokedAt: t.RevokedAt,
	}
}

// --- Sesiones ---

func (s *Server) createSession(r *http.Request, userID string) (store.Session, string, error) {
	secret, err := auth.NewSecret(32)
	if err != nil {
		return store.Session{}, "", err
	}
	csrf, err := auth.NewSecret(24)
	if err != nil {
		return store.Session{}, "", err
	}
	now := s.Now().UTC()
	hours := 12
	_ = s.Store.View(r.Context(), func(st *store.State) error {
		hours = st.Settings.SessionHours
		return nil
	})
	if hours <= 0 {
		hours = 12
	}
	ua := r.Header.Get("User-Agent")
	if len(ua) > 200 {
		ua = ua[:200]
	}
	sess := store.Session{
		IDHash:     auth.Hash(secret),
		UserID:     userID,
		CSRF:       csrf,
		CreatedAt:  now,
		ExpiresAt:  now.Add(time.Duration(hours) * time.Hour),
		LastSeenAt: now,
		MFAPassed:  false,
		IP:         clientIP(r),
		UserAgent:  ua,
	}
	err = s.Store.Update(r.Context(), func(st *store.State) error {
		st.Sessions[sess.IDHash] = &sess
		// Limpia sesiones caducadas.
		for h, x := range st.Sessions {
			if x.ExpiresAt.Before(now) {
				delete(st.Sessions, h)
			}
		}
		return nil
	})
	if err != nil {
		return store.Session{}, "", err
	}
	return sess, secret, nil
}

func (s *Server) setSessionCookie(w http.ResponseWriter, r *http.Request, sess store.Session, value string) {
	secure := r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookie, Value: value, Path: "/",
		Expires: sess.ExpiresAt, HttpOnly: true, Secure: secure, SameSite: http.SameSiteLaxMode,
	})
}

func (s *Server) meResponse(r *http.Request, u *store.User, sess *store.Session, mfaPassed bool) contracts.MeResponse {
	var csrf string
	setupRequired := false
	if sess != nil {
		csrf = sess.CSRF
		setupRequired = !u.TOTPEnabled
	}
	v := contracts.MeResponse{
		User:        s.userView(*u),
		CSRFToken:   csrf,
		Permissions: roleCaps[u.Role],
	}
	v.Session = contracts.SessionInfo{
		AuthMethod:       "session",
		MFAPassed:        mfaPassed,
		MFASetupRequired: setupRequired,
	}
	if sess != nil {
		exp := sess.ExpiresAt
		v.Session.ExpiresAt = &exp
		if sess.StepUpUntil != nil {
			v.Session.StepUpUntil = sess.StepUpUntil
		}
	}
	return v
}

func orDefault(v, def string) string {
	if strings.TrimSpace(v) == "" {
		return def
	}
	return v
}

// CreateInitialUser crea el primer usuario administrador desde main.
// Falla si ya existen usuarios.
func (s *Server) CreateInitialUser(email, passwordHash string, now time.Time) error {
	email = strings.ToLower(strings.TrimSpace(email))
	err := s.Store.Update(context.Background(), func(st *store.State) error {
		if len(st.Users) > 0 {
			return errBootstrapDone
		}
		id, err := newID()
		if err != nil {
			return err
		}
		st.Users[id] = &store.User{
			ID: id, Email: email, Name: email, Role: contracts.RoleAdmin,
			PasswordHash: passwordHash, CreatedAt: now, UpdatedAt: now, PasswordChangedAt: now,
		}
		return nil
	})
	if err != nil {
		return err
	}
	_, _ = s.Store.Audit().Append(contracts.AuditEvent{
		At: now, ActorType: "system", Action: "auth.bootstrap", TargetType: "user", Result: "ok",
	})
	return nil
}
